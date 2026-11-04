package store

import (
	"fmt"
	"time"
)

// RaisonRemise : le seul motif de refus remis en file. scope_conflict, schema,
// http_400 et les autres restent refusés.
const RaisonRemise = "source_disabled"

// BilanRemise : ce qu'a fait une remise en file unique. Des comptes seulement.
type BilanRemise struct {
	DejaFaite     bool // la clé existait : rien n'a été touché
	Lots, Fiches  int  // remis en attente
	LotsLaisses   int  // au-delà de la borne, laissés refusés
	FichesLaisses int
}

// avantCommitRemise : point d'essai des tests, juste avant la validation.
var avantCommitRemise func() error

// RemiseEnFile remet en attente, une seule fois pour cette clé, les lots et les fiches
// refusés avec RaisonRemise et datés d'après plancher (secondes Unix).
//
// Clé et mises à jour sont dans la même transaction : un arrêt brutal avant la
// validation ne laisse rien et la remise repassera au prochain démarrage ; après, tout
// est fait et elle ne repassera pas. Rien n'est effacé ni réécrit : ni charge, ni
// périmètres, ni en-tête.
//
// Date retenue. queue : observed_at, la date que le site compare à son plancher.
// stats_queue n'a pas d'observed_at : fresh (la date la plus récente portée par la
// fiche) et queued_at doivent tous deux atteindre le plancher. fresh écarte une fiche
// de la bêta relue après le plancher ; queued_at seul la laisserait passer. Limite :
// une fiche de la bêta revue après le plancher a un fresh récent et repart ; le site
// juge alors chacun de ses relevés sur sa propre date.
//
// borne vaut par table. Le surplus reste refusé et il est compté ; les plus anciens
// passent d'abord, car les récents seront vus de nouveau en jeu.
func (s *Store) RemiseEnFile(cle string, plancher int64, borne int) (BilanRemise, error) {
	var b BilanRemise
	tx, err := s.db.Begin()
	if err != nil {
		return b, err
	}
	defer tx.Rollback()
	r, err := tx.Exec(`insert into meta(key,value) values(?,'') on conflict(key) do nothing`, cle)
	if err != nil {
		return b, err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		b.DejaFaite = true
		return b, nil
	}

	const lots = `from queue where state='rejected' and reason=? and observed_at>=?`
	var total int
	if err := tx.QueryRow(`select count(*) `+lots, RaisonRemise, plancher).Scan(&total); err != nil {
		return b, err
	}
	r, err = tx.Exec(`update queue set state='pending', reason=null where batch_id in (select batch_id `+lots+`
		order by observed_at, batch_id limit ?)`, RaisonRemise, plancher, borne)
	if err != nil {
		return b, err
	}
	n, _ := r.RowsAffected()
	b.Lots, b.LotsLaisses = int(n), total-int(n)

	const fiches = `from stats_queue where state='rejected' and reason=? and fresh>=? and queued_at>=?`
	if err := tx.QueryRow(`select count(*) `+fiches, RaisonRemise, plancher, plancher).Scan(&total); err != nil {
		return b, err
	}
	r, err = tx.Exec(`update stats_queue set state='pending', reason=null where id in (select id `+fiches+`
		order by fresh, id limit ?)`, RaisonRemise, plancher, plancher, borne)
	if err != nil {
		return b, err
	}
	n, _ = r.RowsAffected()
	b.Fiches, b.FichesLaisses = int(n), total-int(n)

	if _, err := tx.Exec(`update meta set value=? where key=?`,
		fmt.Sprintf("%s|lots=%d|fiches=%d|laisses=%d|%d", time.Now().UTC().Format(time.RFC3339), b.Lots, b.Fiches, b.LotsLaisses, b.FichesLaisses), cle); err != nil {
		return b, err
	}
	if avantCommitRemise != nil {
		if err := avantCommitRemise(); err != nil {
			return BilanRemise{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return BilanRemise{}, err
	}
	return b, nil
}
