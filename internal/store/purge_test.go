package store

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Purge des lots envoyés : seul le contenu des lots envoyés avant la date limite
// est vidé. Lignes, identifiants (anti-doublon) et compteurs restent ; les lots en
// attente ou refusés et les fiches de statistiques ne sont jamais touchés.
func TestPurgeLotsEnvoyesGardeIdentifiantsEtCompteurs(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "companion.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	charge := []byte(`{"characters":[` + strings.Repeat(`{"n":"SYNTHETIC"},`, 50) + `{}]}`)
	var lots []QueuedBatch
	for i := 0; i < 6; i++ {
		lots = append(lots, QueuedBatch{BatchID: fmt.Sprintf("lot-%d", i), ScopeID: "s", ObservedAt: int64(i), Characters: 51, Payload: charge})
	}
	f := File{SHA256: "f1", Path: "x", Origin: "current", AddonVersion: "4.2.0", ObserverSessionID: "o", Scopes: []byte(`[]`)}
	if n, err := s.AddFile(ctx, f, lots, nil); err != nil || n != 6 {
		t.Fatalf("mise en file : %d %v", n, err)
	}
	if err := s.MarkSent([]string{"lot-0", "lot-1", "lot-2", "lot-3"}); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkRejected([]string{"lot-4"}, "synthetic"); err != nil {
		t.Fatal(err)
	}
	// lot-5 reste en attente.
	if _, err := s.db.Exec(`insert into stats_files values('sf',1,1,1,0,'{}')`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`insert into stats_queue(id,file_sha256,digest,fresh,payload,bytes,state,attempts,queued_at,sent_at)
		values('c/1','sf','d',1,'{"id":"c/1"}',12,'sent',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	avant := s.Counts()

	// Date limite dans le passé : aucun lot n'est assez ancien.
	if n, err := s.PurgeLotsEnvoyes(time.Now().Add(-time.Hour).Unix(), 200); err != nil || n != 0 {
		t.Fatalf("lots récents purgés : %d %v", n, err)
	}
	// Tranches : 3 puis 1, puis plus rien.
	limite := time.Now().Add(time.Hour).Unix()
	for i, attendu := range []int{3, 1, 0} {
		if n, err := s.PurgeLotsEnvoyes(limite, 3); err != nil || n != attendu {
			t.Fatalf("tranche %d : %d %v", i, n, err)
		}
	}
	var vides, gardes int
	_ = s.db.QueryRow(`select count(*) from queue where payload=''`).Scan(&vides)
	_ = s.db.QueryRow(`select count(*) from queue where payload<>'' and state in ('pending','rejected')`).Scan(&gardes)
	if vides != 4 || gardes != 2 {
		t.Fatalf("contenus vidés %d, gardés (attente + refusé) %d", vides, gardes)
	}
	var octets, persos int
	_ = s.db.QueryRow(`select sum(bytes), sum(characters) from queue where state='sent'`).Scan(&octets, &persos)
	if octets != 4*len(charge) || persos != 4*51 {
		t.Fatalf("compteurs des lots envoyés perdus : %d octets, %d personnages", octets, persos)
	}
	if apres := s.Counts(); fmt.Sprint(apres) != fmt.Sprint(avant) {
		t.Fatalf("comptes changés : %v → %v", avant, apres)
	}
	var stat string
	_ = s.db.QueryRow(`select payload from stats_queue where id='c/1'`).Scan(&stat)
	if stat != `{"id":"c/1"}` {
		t.Fatalf("fiche de statistiques touchée : %q", stat)
	}
	// Anti-doublon : relire un fichier (.bak) aux mêmes lots ne les remet pas en file.
	f.SHA256 = "f2"
	if n, err := s.AddFile(ctx, f, lots, nil); err != nil || n != 0 {
		t.Fatalf("lots purgés remis en file : %d %v", n, err)
	}
	// Ce qui attend l'envoi garde son contenu intact.
	_, enAttente, err := s.Pending(1 << 20)
	if err != nil || len(enAttente) != 1 || enAttente[0].BatchID != "lot-5" || string(enAttente[0].Payload) != string(charge) {
		t.Fatalf("lot en attente : %+v %v", enAttente, err)
	}
}

// Compacte : la première fois, la base passe en auto_vacuum incrémental et rend la
// place ; les fois suivantes, seules les pages libres sont rendues.
func TestCompacteRendLaPlace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "companion.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	charge := []byte(strings.Repeat("x", 64<<10))
	var lots []QueuedBatch
	var ids []string
	for i := 0; i < 64; i++ {
		id := fmt.Sprintf("lot-%d", i)
		ids = append(ids, id)
		lots = append(lots, QueuedBatch{BatchID: id, ScopeID: "s", Characters: 1, Payload: charge})
	}
	if _, err := s.AddFile(ctx, File{SHA256: "f1", Scopes: []byte(`[]`)}, lots, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkSent(ids); err != nil {
		t.Fatal(err)
	}
	if err := s.Compacte(); err != nil { // première fois : passage en incrémental
		t.Fatal(err)
	}
	taille := func() int64 {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return st.Size()
	}
	pleine := taille()
	if n, err := s.PurgeLotsEnvoyes(time.Now().Add(time.Hour).Unix(), 1000); err != nil || n != 64 {
		t.Fatalf("purge : %d %v", n, err)
	}
	if err := s.Compacte(); err != nil {
		t.Fatal(err)
	}
	var mode int
	_ = s.db.QueryRow(`pragma auto_vacuum`).Scan(&mode)
	if mode != 2 {
		t.Fatalf("auto_vacuum %d, attendu 2 (incrémental)", mode)
	}
	if apres := taille(); apres > pleine/4 {
		t.Fatalf("place non rendue : %d → %d octets", pleine, apres)
	}
	if c := s.Counts(); c["sent"] != 64 {
		t.Fatalf("comptes : %v", c)
	}
}
