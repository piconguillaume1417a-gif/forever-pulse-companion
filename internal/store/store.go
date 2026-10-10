// Package store : la base locale companion.db (SQLite, pur Go, sans CGO).
//
// Trois choses y vivent, et survivent à un redémarrage comme à une coupure réseau :
//   - files  : les fichiers déjà traités, par empreinte SHA-256 ;
//   - queue  : un lot décodé par ligne, état pending / sent / rejected ;
//   - cumul_* : le cumul de personnages distincts observés, par périmètre.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"

	"wowsync/internal/cumul"
)

const schemaSQL = `
create table if not exists meta(key text primary key, value text not null);
create table if not exists files(
  sha256 text primary key,
  path text not null,
  origin text not null check(origin in ('current','backup','manual')),
  addon_version text not null,
  observer_session_id text not null,
  scopes text not null,
  processed_at integer not null,
  batches_total integer not null,
  batches_queued integer not null,
  batches_discarded integer not null
);
create table if not exists queue(
  batch_id text primary key,
  file_sha256 text not null references files(sha256),
  scope_id text not null,
  observed_at integer not null,
  characters integer not null,
  payload text not null,
  bytes integer not null,
  state text not null check(state in ('pending','sent','rejected')),
  attempts integer not null default 0,
  reason text,
  queued_at integer not null,
  sent_at integer
);
create index if not exists queue_pending on queue(state, file_sha256, observed_at);
create table if not exists cumul_lots(batch_id text primary key, seq integer not null);
create table if not exists cumul_scopes(scope_id text primary key, depuis integer);
create table if not exists cumul_persos(scope_id text not null, cle text not null, data text not null, primary key(scope_id, cle));
create table if not exists cumul_noms(scope_id text not null, nom text not null, cle text not null, primary key(scope_id, nom));
-- 0.7.0 : statistiques de personnages (ForeverPulseStatsDB). Une ligne par fiche
-- (contexte/GUID) : la dernière version connue, avec son empreinte. Une fiche dont
-- l'empreinte n'a pas changé n'est jamais remise en file.
create table if not exists stats_files(
  sha256 text primary key,
  processed_at integer not null,
  sheets integer not null,
  queued integer not null,
  discarded integer not null,
  meta text not null
);
create table if not exists stats_queue(
  id text primary key,
  file_sha256 text not null references stats_files(sha256),
  digest text not null,
  fresh integer not null default 0,
  payload text not null,
  bytes integer not null,
  state text not null check(state in ('pending','sent','rejected')),
  attempts integer not null default 0,
  reason text,
  queued_at integer not null,
  sent_at integer
);
create index if not exists stats_queue_pending on stats_queue(state, queued_at);
`

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(wal)&_pragma=foreign_keys(1)&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaSQL + auctionsSQL); err != nil {
		db.Close()
		return nil, err
	}
	if err := migreSentSchema(db); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

// migreSentSchema (0.9.0) : colonne additive stats_queue.sent_schema, le schéma
// HTTP (1 ou 2) sous lequel la fiche a été confirmée. Une base antérieure est
// gardée telle quelle : ses lignes ont sent_schema nul (envoyées avant les talents).
func migreSentSchema(db *sql.DB) error {
	rows, err := db.Query(`select name from pragma_table_info('stats_queue')`)
	if err != nil {
		return err
	}
	present := false
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return err
		}
		present = present || n == "sent_schema"
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if present {
		return nil
	}
	_, err = db.Exec(`alter table stats_queue add column sent_schema integer check(sent_schema in (1,2))`)
	return err
}

func (s *Store) Close() error { return s.db.Close() }

// Wipe efface toutes les données locales : fichiers lus, file d'envoi (lots en
// attente compris), cumul et état. Le jeton, rangé à part, n'est pas touché.
func (s *Store) Wipe() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"auctions_parts", "auctions_markets", "auctions_queue", "auctions_files", "queue", "files", "stats_queue", "stats_files", "cumul_lots", "cumul_scopes", "cumul_persos", "cumul_noms", "meta"} {
		if _, err := tx.Exec("delete from " + t); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	_, _ = s.db.Exec("vacuum")
	return nil
}

// Meta ------------------------------------------------------------------------

func (s *Store) Get(key string) string {
	var v string
	_ = s.db.QueryRow(`select value from meta where key=?`, key).Scan(&v)
	return v
}

func (s *Store) Set(key, value string) error {
	_, err := s.db.Exec(`insert into meta(key,value) values(?,?) on conflict(key) do update set value=excluded.value`, key, value)
	return err
}

// Fichiers et file d'envoi ------------------------------------------------------

func (s *Store) FileKnown(sha string) bool {
	var n int
	_ = s.db.QueryRow(`select count(*) from files where sha256=?`, sha).Scan(&n)
	return n > 0
}

type File struct {
	SHA256, Path, Origin, AddonVersion, ObserverSessionID string
	Scopes                                                json.RawMessage
	Total, Queued, Discarded                              int
}

type QueuedBatch struct {
	BatchID, ScopeID string
	ObservedAt       int64
	Characters       int
	Payload          []byte
}

// AddFile enregistre un fichier et met ses lots en file, dans une seule
// transaction avec l'intégration au cumul : un fichier est traité en entier ou pas du tout.
// Un batch_id déjà en file (relecture du .bak) est ignoré.
func (s *Store) AddFile(ctx context.Context, f File, batches []QueuedBatch, c *cumul.Cumul) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, err := tx.Exec(`insert into files values(?,?,?,?,?,?,?,?,?,?)`, f.SHA256, f.Path, f.Origin, f.AddonVersion,
		f.ObserverSessionID, string(f.Scopes), now, f.Total, len(batches), f.Discarded); err != nil {
		return 0, err
	}
	added := 0
	for _, b := range batches {
		r, err := tx.Exec(`insert into queue(batch_id,file_sha256,scope_id,observed_at,characters,payload,bytes,state,queued_at)
			values(?,?,?,?,?,?,?,'pending',?) on conflict(batch_id) do nothing`,
			b.BatchID, f.SHA256, b.ScopeID, b.ObservedAt, b.Characters, string(b.Payload), len(b.Payload), now)
		if err != nil {
			return 0, err
		}
		n, _ := r.RowsAffected()
		added += int(n)
	}
	if c != nil {
		if err := saveCumul(tx, c); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	if c != nil {
		c.Enregistre()
	}
	return added, nil
}

// Pending : les lots en attente d'un même fichier (le plus ancien d'abord).
func (s *Store) Pending(limitBytes int) (*File, []QueuedBatch, error) {
	var sha string
	err := s.db.QueryRow(`select file_sha256 from queue where state='pending' order by queued_at, observed_at limit 1`).Scan(&sha)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	f := &File{}
	var scopes string
	if err := s.db.QueryRow(`select sha256,path,origin,addon_version,observer_session_id,scopes from files where sha256=?`, sha).
		Scan(&f.SHA256, &f.Path, &f.Origin, &f.AddonVersion, &f.ObserverSessionID, &scopes); err != nil {
		return nil, nil, err
	}
	f.Scopes = json.RawMessage(scopes)
	rows, err := s.db.Query(`select batch_id,scope_id,observed_at,characters,payload,bytes from queue
		where state='pending' and file_sha256=? order by observed_at, batch_id`, sha)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	var out []QueuedBatch
	total := 0
	for rows.Next() {
		var b QueuedBatch
		var p string
		var n int
		if err := rows.Scan(&b.BatchID, &b.ScopeID, &b.ObservedAt, &b.Characters, &p, &n); err != nil {
			return nil, nil, err
		}
		b.Payload = []byte(p)
		if len(out) > 0 && total+n > limitBytes {
			break
		}
		total += n
		out = append(out, b)
	}
	return f, out, rows.Err()
}

func (s *Store) MarkSent(ids []string) error {
	return s.mark(ids, "sent", "")
}

func (s *Store) MarkRejected(ids []string, reason string) error {
	return s.mark(ids, "rejected", reason)
}

func (s *Store) mark(ids []string, state, reason string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	for _, id := range ids {
		if _, err := tx.Exec(`update queue set state=?, reason=nullif(?,''), attempts=attempts+1,
			sent_at=case when ?='sent' then ? else sent_at end where batch_id=?`, state, reason, state, now, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Attempt(ids []string) {
	for _, id := range ids {
		_, _ = s.db.Exec(`update queue set attempts=attempts+1 where batch_id=?`, id)
	}
}

// HasPending : au moins un lot en attente ? (index queue_pending, sans parcours)
func (s *Store) HasPending() bool {
	var n int
	_ = s.db.QueryRow(`select exists(select 1 from queue where state='pending')`).Scan(&n)
	return n == 1
}

// Counts : lots par état.
func (s *Store) Counts() map[string]int {
	out := map[string]int{"pending": 0, "sent": 0, "rejected": 0}
	rows, err := s.db.Query(`select state, count(*) from queue group by state`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		_ = rows.Scan(&k, &n)
		out[k] = n
	}
	return out
}

// Purge des lots envoyés (0.10.0) ----------------------------------------------
//
// Le contenu d'un lot envoyé n'est jamais relu : seuls son identifiant (un lot
// déjà connu n'est pas remis en file, voir AddFile) et ses compteurs (état,
// personnages, octets, dates) servent. Les fiches de statistiques ne sont pas
// concernées : leur contenu sert à les renvoyer (StatsRequeueTalents).

// PurgeLotsEnvoyes vide le contenu d'au plus max lots envoyés avant avant
// (secondes Unix). La ligne reste : identifiant et compteurs sont gardés. Les
// lots en attente ou refusés ne sont jamais touchés.
func (s *Store) PurgeLotsEnvoyes(avant int64, max int) (int, error) {
	r, err := s.db.Exec(`update queue set payload='' where batch_id in (select batch_id from queue
		where state='sent' and payload<>'' and coalesce(sent_at, queued_at) < ? limit ?)`, avant, max)
	if err != nil {
		return 0, err
	}
	n, _ := r.RowsAffected()
	return int(n), nil
}

// Compacte rend au disque la place libérée. La première fois, la base passe en
// auto_vacuum incrémental, ce qui demande un VACUUM complet (une seule fois) ;
// ensuite seules les pages libres sont rendues, sans réécrire la base.
func (s *Store) Compacte() error {
	var mode int
	if err := s.db.QueryRow(`pragma auto_vacuum`).Scan(&mode); err != nil {
		return err
	}
	if mode != 2 {
		if _, err := s.db.Exec(`pragma auto_vacuum=incremental`); err != nil {
			return err
		}
		if _, err := s.db.Exec(`vacuum`); err != nil {
			return err
		}
	} else if _, err := s.db.Exec(`pragma incremental_vacuum`); err != nil {
		return err
	}
	_, err := s.db.Exec(`pragma wal_checkpoint(truncate)`)
	return err
}

// Statistiques de personnages ---------------------------------------------------

// StatsFileKnown : les statistiques de ce fichier ont déjà été mises en file.
func (s *Store) StatsFileKnown(sha string) bool {
	var n int
	_ = s.db.QueryRow(`select count(*) from stats_files where sha256=?`, sha).Scan(&n)
	return n > 0
}

// StatSheet : une fiche de statistiques prête à l'envoi.
type StatSheet struct {
	ID, Digest string
	Fresh      int64 // date la plus récente portée par la fiche (vue, relevé)
	Payload    []byte
}

// AddStatsFile enregistre les fiches d'un fichier dans une seule transaction. Une
// fiche dont l'empreinte est identique à la dernière connue (envoyée, en attente
// ou refusée) est laissée telle quelle ; une fiche nouvelle ou modifiée remplace
// l'ancienne et repasse en attente. Une fiche plus ancienne que celle déjà connue
// (lecture du .bak après le fichier courant) ne la remplace jamais.
// Renvoie le nombre de fiches mises en file.
func (s *Store) AddStatsFile(ctx context.Context, sha string, meta []byte, sheets []StatSheet, total, discarded int) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, err := tx.Exec(`insert into stats_files(sha256,processed_at,sheets,queued,discarded,meta) values(?,?,?,0,?,?)`,
		sha, now, total, discarded, string(meta)); err != nil {
		return 0, err
	}
	added := 0
	for _, f := range sheets {
		var digest string
		var fresh int64
		err := tx.QueryRow(`select digest, fresh from stats_queue where id=?`, f.ID).Scan(&digest, &fresh)
		if err == nil && (digest == f.Digest || f.Fresh < fresh) {
			continue
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if _, err := tx.Exec(`insert into stats_queue(id,file_sha256,digest,fresh,payload,bytes,state,attempts,reason,queued_at,sent_at)
			values(?,?,?,?,?,?,'pending',0,null,?,null)
			on conflict(id) do update set file_sha256=excluded.file_sha256, digest=excluded.digest, fresh=excluded.fresh,
				payload=excluded.payload, bytes=excluded.bytes, state='pending', attempts=0, reason=null,
				queued_at=excluded.queued_at, sent_at=null, sent_schema=null`,
			f.ID, sha, f.Digest, f.Fresh, string(f.Payload), len(f.Payload), now); err != nil {
			return 0, err
		}
		added++
	}
	if _, err := tx.Exec(`update stats_files set queued=? where sha256=?`, added, sha); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return added, nil
}

// StatsPending : les fiches en attente d'un même fichier (le plus ancien d'abord),
// avec ce qui accompagne chaque envoi de ce fichier (meta).
func (s *Store) StatsPending(limitBytes int) (meta []byte, sheets []StatSheet, err error) {
	var sha string
	err = s.db.QueryRow(`select file_sha256 from stats_queue where state='pending' order by queued_at, id limit 1`).Scan(&sha)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var m string
	if err := s.db.QueryRow(`select meta from stats_files where sha256=?`, sha).Scan(&m); err != nil {
		return nil, nil, err
	}
	rows, err := s.db.Query(`select id,digest,payload,bytes from stats_queue where state='pending' and file_sha256=? order by id`, sha)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	total := len(m)
	for rows.Next() {
		var f StatSheet
		var p string
		var n int
		if err := rows.Scan(&f.ID, &f.Digest, &p, &n); err != nil {
			return nil, nil, err
		}
		if len(sheets) > 0 && total+n+1 > limitBytes {
			break
		}
		total += n + 1
		f.Payload = []byte(p)
		sheets = append(sheets, f)
	}
	return []byte(m), sheets, rows.Err()
}

// MarkStats change l'état de fiches, seulement si leur empreinte est toujours celle
// qui a été envoyée : une fiche remplacée entre-temps par une version plus récente
// reste en attente.
func (s *Store) MarkStats(sheets []StatSheet, state, reason string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	for _, f := range sheets {
		if _, err := tx.Exec(`update stats_queue set state=?, reason=nullif(?,''), attempts=attempts+1,
			sent_at=case when ?='sent' then ? else sent_at end where id=? and digest=?`, state, reason, state, now, f.ID, f.Digest); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// MarkStatsSent marque des fiches envoyées sous un schéma HTTP donné (1 ou 2),
// avec la même garde d'empreinte que MarkStats.
func (s *Store) MarkStatsSent(sheets []StatSheet, schema int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	for _, f := range sheets {
		if _, err := tx.Exec(`update stats_queue set state='sent', reason=null, attempts=attempts+1, sent_at=?, sent_schema=?
			where id=? and digest=?`, now, schema, f.ID, f.Digest); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// StatsRequeueTalents (0.9.0) : au retour du schéma 2, les fiches confirmées en
// schéma 1 dont la charge porte des talents (retirés de cet envoi) repassent en
// attente, une fois : leur empreinte ne change pas, et sent_schema est effacé.
// La clé « talents » n'apparaît dans une charge qu'au premier niveau (une chaîne
// JSON ne peut contenir de guillemet nu). Renvoie le nombre de fiches remises en file.
func (s *Store) StatsRequeueTalents() (int, error) {
	r, err := s.db.Exec(`update stats_queue set state='pending', reason=null, sent_schema=null
		where state='sent' and sent_schema=1 and instr(payload, ',"talents":{') > 0`)
	if err != nil {
		return 0, err
	}
	n, _ := r.RowsAffected()
	return int(n), nil
}

// StatsAttempt compte un essai sans changer l'état.
func (s *Store) StatsAttempt(sheets []StatSheet) {
	for _, f := range sheets {
		_, _ = s.db.Exec(`update stats_queue set attempts=attempts+1 where id=? and digest=?`, f.ID, f.Digest)
	}
}

// StatsHasPending : au moins une fiche en attente ?
func (s *Store) StatsHasPending() bool {
	var n int
	_ = s.db.QueryRow(`select exists(select 1 from stats_queue where state='pending')`).Scan(&n)
	return n == 1
}

// StatsCounts : fiches par état.
func (s *Store) StatsCounts() map[string]int {
	out := map[string]int{"pending": 0, "sent": 0, "rejected": 0}
	rows, err := s.db.Query(`select state, count(*) from stats_queue group by state`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		_ = rows.Scan(&k, &n)
		out[k] = n
	}
	return out
}

// Cumul -----------------------------------------------------------------------

// LoadCumul relit tout le cumul (quelques dizaines de milliers de lignes au plus).
func (s *Store) LoadCumul() (*cumul.Cumul, error) {
	c := cumul.Nouveau()
	rows, err := s.db.Query(`select batch_id from cumul_lots order by seq`)
	if err != nil {
		return nil, err
	}
	var lots []string
	for rows.Next() {
		var b string
		_ = rows.Scan(&b)
		lots = append(lots, b)
	}
	rows.Close()
	c.ChargeLots(lots)
	rows, err = s.db.Query(`select scope_id, depuis from cumul_scopes`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sid string
		var d sql.NullInt64
		_ = rows.Scan(&sid, &d)
		c.ChargePortee(sid, d)
	}
	rows.Close()
	rows, err = s.db.Query(`select scope_id, cle, data from cumul_persos`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sid, cle, data string
		_ = rows.Scan(&sid, &cle, &data)
		e := &cumul.Entree{}
		if err := json.Unmarshal([]byte(data), e); err != nil {
			rows.Close()
			return nil, fmt.Errorf("cumul illisible (%s) : %w", sid, err)
		}
		c.ChargeEntree(sid, cle, e)
	}
	rows.Close()
	rows, err = s.db.Query(`select scope_id, nom, cle from cumul_noms`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sid, nom, cle string
		_ = rows.Scan(&sid, &nom, &cle)
		c.ChargeNom(sid, nom, cle)
	}
	rows.Close()
	c.Enregistre()
	return c, nil
}

// PorteeCumul : une ligne du résumé du cumul, lue en SQL.
type PorteeCumul struct {
	ScopeID string
	Depuis  *int64
	Total   int
}

// ResumeCumul compte les personnages distincts de chaque périmètre sans charger le
// cumul (0.7.4) : mêmes périmètres et mêmes totaux que cumul.Resume sur le cumul
// relu par LoadCumul (un périmètre présent dans l'une des trois tables compte,
// même vide). Une lecture d'index, quelques millisecondes pour 200 000 personnages.
func (s *Store) ResumeCumul() ([]PorteeCumul, error) {
	rows, err := s.db.Query(`select scope_id, max(depuis), count(cle) from (
		select scope_id, depuis, null as cle from cumul_scopes
		union all select scope_id, null, cle from cumul_persos
		union all select distinct scope_id, null, null from cumul_noms
	) group by scope_id order by scope_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PorteeCumul
	for rows.Next() {
		var p PorteeCumul
		var d sql.NullInt64
		if err := rows.Scan(&p.ScopeID, &d, &p.Total); err != nil {
			return nil, err
		}
		if d.Valid {
			v := d.Int64
			p.Depuis = &v
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func saveCumul(tx *sql.Tx, c *cumul.Cumul) error {
	ch := c.Changements()
	base := 0
	_ = tx.QueryRow(`select coalesce(max(seq),0) from cumul_lots`).Scan(&base)
	for i, b := range ch.Lots {
		if _, err := tx.Exec(`insert into cumul_lots values(?,?) on conflict do nothing`, b, base+i+1); err != nil {
			return err
		}
	}
	for sid, d := range ch.Scopes {
		if _, err := tx.Exec(`insert into cumul_scopes values(?,?) on conflict(scope_id) do update set depuis=excluded.depuis`, sid, d); err != nil {
			return err
		}
	}
	for _, k := range ch.PersosSupprimes {
		if _, err := tx.Exec(`delete from cumul_persos where scope_id=? and cle=?`, k[0], k[1]); err != nil {
			return err
		}
	}
	for k, e := range ch.Persos {
		data, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(`insert into cumul_persos values(?,?,?) on conflict(scope_id,cle) do update set data=excluded.data`, k[0], k[1], string(data)); err != nil {
			return err
		}
	}
	for k, cle := range ch.Noms {
		var err error
		if cle == "" {
			_, err = tx.Exec(`delete from cumul_noms where scope_id=? and nom=?`, k[0], k[1])
		} else {
			_, err = tx.Exec(`insert into cumul_noms values(?,?,?) on conflict(scope_id,nom) do update set cle=excluded.cle`, k[0], k[1], cle)
		}
		if err != nil {
			return err
		}
	}
	return nil
}
