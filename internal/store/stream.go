package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
	"wowsync/internal/cumul"
)

// FileWriter stages a complete validated input in one disk-backed transaction.
// Only the current logical observation/statistics chunk is in Go memory.
type FileWriter struct {
	tx    *sql.Tx
	ctx   context.Context
	sha   string
	now   int64
	Added int
}

func (s *Store) WriteStream(ctx context.Context, f File, census, stats bool, meta []byte, work func(*FileWriter) error) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	w := &FileWriter{tx: tx, ctx: ctx, sha: f.SHA256, now: time.Now().Unix()}
	if census {
		if _, err = tx.ExecContext(ctx, "insert into files values(?,?,?,?,?,?,?,?,?,?)", f.SHA256, f.Path, f.Origin, f.AddonVersion, f.ObserverSessionID, string(f.Scopes), w.now, 0, 0, 0); err != nil {
			return 0, err
		}
	}
	if stats {
		if _, err = tx.ExecContext(ctx, "insert into stats_files(sha256,processed_at,sheets,queued,discarded,meta) values(?,?,0,0,0,?)", w.sha, w.now, string(meta)); err != nil {
			return 0, err
		}
	}
	if err = work(w); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return w.Added, nil
}

func (w *FileWriter) Batch(b QueuedBatch, lot cumul.Lot) error {
	r, err := w.tx.ExecContext(w.ctx, "insert into queue(batch_id,file_sha256,scope_id,observed_at,characters,payload,bytes,state,queued_at) values(?,?,?,?,?,?,?,'pending',?) on conflict(batch_id) do nothing", b.BatchID, w.sha, b.ScopeID, b.ObservedAt, b.Characters, string(b.Payload), len(b.Payload), w.now)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	w.Added += int(n)
	return w.integrate(lot)
}

func (w *FileWriter) Counts(total, valid int) error {
	_, err := w.tx.ExecContext(w.ctx, "update files set batches_total=?,batches_queued=?,batches_discarded=? where sha256=?", total, valid, total-valid, w.sha)
	return err
}

func (w *FileWriter) Stats(sheets []StatSheet, total int) error {
	added := 0
	for _, f := range sheets {
		r, err := w.tx.ExecContext(w.ctx, `insert into stats_queue(id,file_sha256,digest,fresh,payload,bytes,state,attempts,reason,queued_at,sent_at)
			values(?,?,?,?,?,?,'pending',0,null,?,null)
			on conflict(id) do update set file_sha256=excluded.file_sha256,digest=excluded.digest,fresh=excluded.fresh,
				payload=excluded.payload,bytes=excluded.bytes,state='pending',attempts=0,reason=null,
				queued_at=excluded.queued_at,sent_at=null,sent_schema=null
			where excluded.fresh>=stats_queue.fresh and excluded.digest<>stats_queue.digest`, f.ID, w.sha, f.Digest, f.Fresh, string(f.Payload), len(f.Payload), w.now)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		added += int(n)
	}
	_, err := w.tx.ExecContext(w.ctx, "update stats_files set sheets=sheets+?,queued=queued+?,discarded=discarded+? where sha256=?", total, added, total-len(sheets), w.sha)
	return err
}

// Load just the identities and aliases that the shared cumulative merge can
// touch. SQLite's existing primary keys keep this independent of corpus size.
func (w *FileWriter) integrate(l cumul.Lot) error {
	var known int
	err := w.tx.QueryRowContext(w.ctx, "select 1 from cumul_lots where batch_id=?", l.BatchID).Scan(&known)
	if err == nil {
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	c := cumul.Nouveau()
	var since sql.NullInt64
	err = w.tx.QueryRowContext(w.ctx, "select depuis from cumul_scopes where scope_id=?", l.ScopeID).Scan(&since)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	c.ChargePortee(l.ScopeID, since)
	names, keys := []string{}, []string{}
	for _, r := range l.Roster {
		names = append(names, cumul.CleNom(r.DisplayName))
		keys = append(keys, r.GUID)
	}
	for _, r := range l.Who {
		n := cumul.CleNom(r.Name)
		names = append(names, n)
		keys = append(keys, "nom:"+n)
	}
	namesJSON, _ := json.Marshal(names)
	read := func(query string, args []any, each func(*sql.Rows) error) error {
		rows, err := w.tx.QueryContext(w.ctx, query, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := each(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	if err := read("select nom,cle from cumul_noms where scope_id=? and nom in (select value from json_each(?))", []any{l.ScopeID, string(namesJSON)}, func(rows *sql.Rows) error {
		var n, k string
		if err := rows.Scan(&n, &k); err != nil {
			return err
		}
		keys = append(keys, k)
		c.ChargeNom(l.ScopeID, n, k)
		return nil
	}); err != nil {
		return err
	}
	keyJSON, _ := json.Marshal(keys)
	var oldNames []string
	if err := read("select cle,data from cumul_persos where scope_id=? and cle in (select value from json_each(?))", []any{l.ScopeID, string(keyJSON)}, func(rows *sql.Rows) error {
		var k, data string
		if err := rows.Scan(&k, &data); err != nil {
			return err
		}
		e := &cumul.Entree{}
		if err := json.Unmarshal([]byte(data), e); err != nil {
			return err
		}
		if e.DisplayName != nil {
			oldNames = append(oldNames, cumul.CleNom(*e.DisplayName))
		}
		c.ChargeEntree(l.ScopeID, k, e)
		return nil
	}); err != nil {
		return err
	}
	oldJSON, _ := json.Marshal(oldNames)
	if err := read("select nom,cle from cumul_noms where scope_id=? and nom in (select value from json_each(?))", []any{l.ScopeID, string(oldJSON)}, func(rows *sql.Rows) error {
		var n, k string
		if err := rows.Scan(&n, &k); err != nil {
			return err
		}
		c.ChargeNom(l.ScopeID, n, k)
		return nil
	}); err != nil {
		return err
	}
	c.Integrer([]cumul.Lot{l})
	return saveCumul(w.tx, c)
}
