package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const (
	clefEssai  = "region_requeue_v1"
	plancher   = int64(1793703600) // 2026-11-03T11:00:00Z
	borneLarge = 50_000
)

type lot struct {
	id     string
	at     int64
	raison string // vide : envoyé
}

type fiche struct {
	id          string
	fresh, file int64 // fresh, queued_at
	raison      string
}

func ouvreEssai(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// jeu met en base des lots et des fiches refusés, sans passer par le réseau.
func jeu(t *testing.T, s *Store, lots []lot, fiches []fiche) {
	t.Helper()
	ctx := context.Background()
	var q []QueuedBatch
	for _, l := range lots {
		q = append(q, QueuedBatch{BatchID: l.id, ScopeID: "s", ObservedAt: l.at, Characters: 1, Payload: []byte(`{"batch_id":"` + l.id + `"}`)})
	}
	sha := fmt.Sprintf("sha-%d", len(lots)+len(fiches))
	if _, err := s.AddFile(ctx, File{SHA256: sha, Path: "p", Origin: "current", AddonVersion: "1", ObserverSessionID: "o",
		Scopes: []byte(`{"s":{"region":"95"}}`)}, q, nil); err != nil {
		t.Fatal(err)
	}
	for _, l := range lots {
		if l.raison == "" {
			if err := s.MarkSent([]string{l.id}); err != nil {
				t.Fatal(err)
			}
		} else if l.raison != "pending" {
			if err := s.MarkRejected([]string{l.id}, l.raison); err != nil {
				t.Fatal(err)
			}
		}
	}
	var sh []StatSheet
	for _, f := range fiches {
		sh = append(sh, StatSheet{ID: f.id, Digest: "d" + f.id, Fresh: f.fresh, Payload: []byte(`{"id":"` + f.id + `"}`)})
	}
	if _, err := s.AddStatsFile(ctx, sha, []byte(`{"contexts":{"c":{"region":"95"}}}`), sh, len(sh), 0); err != nil {
		t.Fatal(err)
	}
	for i, f := range fiches {
		if f.raison == "" {
			if err := s.MarkStats(sh[i:i+1], "sent", ""); err != nil {
				t.Fatal(err)
			}
		} else if f.raison != "pending" {
			if err := s.MarkStats(sh[i:i+1], "rejected", f.raison); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.db.Exec(`update stats_queue set queued_at=? where id=?`, f.file, f.id); err != nil {
			t.Fatal(err)
		}
	}
}

func etats(t *testing.T, s *Store, table, cle string) map[string]string {
	t.Helper()
	rows, err := s.db.Query(`select ` + cle + `, state from ` + table)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			t.Fatal(err)
		}
		out[k] = v
	}
	return out
}

func attendEtats(t *testing.T, obtenu, attendu map[string]string) {
	t.Helper()
	if len(obtenu) != len(attendu) {
		t.Fatalf("obtenu %v, attendu %v", obtenu, attendu)
	}
	for k, v := range attendu {
		if obtenu[k] != v {
			t.Fatalf("%s : obtenu %q, attendu %q (%v)", k, obtenu[k], v, obtenu)
		}
	}
}

func TestRemisePlancherMotifEtat(t *testing.T) {
	s := ouvreEssai(t, filepath.Join(t.TempDir(), "c.db"))
	defer s.Close()
	jeu(t, s, []lot{
		{"apres", plancher + 100, "source_disabled"},
		{"pile", plancher, "source_disabled"},
		{"avant", plancher - 1, "source_disabled"},
		{"beta", 1790180000, "source_disabled"},
		{"conflit", plancher + 100, "scope_conflict"},
		{"schema", plancher + 100, "schema"},
		{"http400", plancher + 100, "http_400"},
		{"envoye", plancher + 100, ""},
		{"attente", plancher + 100, "pending"},
	}, []fiche{
		{"apres", plancher + 100, plancher + 200, "source_disabled"},
		{"pile", plancher, plancher, "source_disabled"},
		{"betarelue", plancher - 1000, plancher + 200, "source_disabled"}, // fiche de la bêta lue après le plancher
		{"fauxfresh", plancher + 100, plancher - 1, "source_disabled"},
		{"sansdate", 0, plancher + 200, "source_disabled"},
		{"http400", plancher + 100, plancher + 200, "http_400"},
		{"envoyee", plancher + 100, plancher + 200, ""},
	})
	b, err := s.RemiseEnFile(clefEssai, plancher, borneLarge)
	if err != nil {
		t.Fatal(err)
	}
	if b.DejaFaite || b.Lots != 2 || b.Fiches != 2 || b.LotsLaisses != 0 || b.FichesLaisses != 0 {
		t.Fatalf("bilan %+v", b)
	}
	attendEtats(t, etats(t, s, "queue", "batch_id"), map[string]string{"apres": "pending", "pile": "pending", "avant": "rejected", "beta": "rejected",
		"conflit": "rejected", "schema": "rejected", "http400": "rejected", "envoye": "sent", "attente": "pending"})
	attendEtats(t, etats(t, s, "stats_queue", "id"), map[string]string{"apres": "pending", "pile": "pending", "betarelue": "rejected",
		"fauxfresh": "rejected", "sansdate": "rejected", "http400": "rejected", "envoyee": "sent"})
	// Le motif effacé, les périmètres et l'en-tête intacts : la base garde l'ancien code.
	var raison any
	_ = s.db.QueryRow(`select reason from queue where batch_id='apres'`).Scan(&raison)
	if raison != nil {
		t.Fatalf("motif gardé : %v", raison)
	}
	var scopes, meta string
	_ = s.db.QueryRow(`select scopes from files limit 1`).Scan(&scopes)
	_ = s.db.QueryRow(`select meta from stats_files limit 1`).Scan(&meta)
	if scopes != `{"s":{"region":"95"}}` || meta != `{"contexts":{"c":{"region":"95"}}}` {
		t.Fatalf("stockage modifié : %s %s", scopes, meta)
	}
}

func TestRemiseUneSeuleFois(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.db")
	s := ouvreEssai(t, path)
	jeu(t, s, []lot{{"a", plancher + 1, "source_disabled"}}, []fiche{{"f", plancher + 1, plancher + 1, "source_disabled"}})
	if b, err := s.RemiseEnFile(clefEssai, plancher, borneLarge); err != nil || b.Lots != 1 || b.Fiches != 1 {
		t.Fatalf("premier passage : %+v, %v", b, err)
	}
	if s.Get(clefEssai) == "" {
		t.Fatal("clé non posée")
	}
	// Refusés de nouveau : ils restent refusés, sans nouvelle tentative, même après redémarrage.
	if err := s.MarkRejected([]string{"a"}, "source_disabled"); err != nil {
		t.Fatal(err)
	}
	if err := s.MarkStats([]StatSheet{{ID: "f", Digest: "df"}}, "rejected", "source_disabled"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s = ouvreEssai(t, path)
	defer s.Close()
	valeur := s.Get(clefEssai)
	b, err := s.RemiseEnFile(clefEssai, plancher, borneLarge)
	if err != nil || !b.DejaFaite || b.Lots != 0 || b.Fiches != 0 {
		t.Fatalf("second passage : %+v, %v", b, err)
	}
	if s.Counts()["rejected"] != 1 || s.StatsCounts()["rejected"] != 1 || s.Get(clefEssai) != valeur {
		t.Fatalf("second passage a touché la base : %v %v", s.Counts(), s.StatsCounts())
	}
	// Une version future pose une autre clé et repasse, une fois.
	if b, err := s.RemiseEnFile("region_requeue_v2", plancher, borneLarge); err != nil || b.DejaFaite || b.Lots != 1 || b.Fiches != 1 {
		t.Fatalf("clé suivante : %+v, %v", b, err)
	}
}

func TestRemiseBaseVidePoseLaCle(t *testing.T) {
	s := ouvreEssai(t, filepath.Join(t.TempDir(), "c.db"))
	defer s.Close()
	if b, err := s.RemiseEnFile(clefEssai, plancher, borneLarge); err != nil || b.DejaFaite || b.Lots != 0 {
		t.Fatalf("%+v, %v", b, err)
	}
	if b, _ := s.RemiseEnFile(clefEssai, plancher, borneLarge); !b.DejaFaite {
		t.Fatalf("%+v", b)
	}
}

func TestRemiseBorne(t *testing.T) {
	s := ouvreEssai(t, filepath.Join(t.TempDir(), "c.db"))
	defer s.Close()
	var lots []lot
	var fiches []fiche
	for i := int64(0); i < 5; i++ {
		lots = append(lots, lot{fmt.Sprintf("l%d", i), plancher + 5 - i, "source_disabled"})
		fiches = append(fiches, fiche{fmt.Sprintf("f%d", i), plancher + 5 - i, plancher + 5, "source_disabled"})
	}
	jeu(t, s, lots, fiches)
	b, err := s.RemiseEnFile(clefEssai, plancher, 3)
	if err != nil || b.Lots != 3 || b.LotsLaisses != 2 || b.Fiches != 3 || b.FichesLaisses != 2 {
		t.Fatalf("%+v, %v", b, err)
	}
	// Les plus anciens passent d'abord.
	attendEtats(t, etats(t, s, "queue", "batch_id"), map[string]string{"l0": "rejected", "l1": "rejected", "l2": "pending", "l3": "pending", "l4": "pending"})
	attendEtats(t, etats(t, s, "stats_queue", "id"), map[string]string{"f0": "rejected", "f1": "rejected", "f2": "pending", "f3": "pending", "f4": "pending"})
	// Le surplus n'est jamais repris, même avec une borne plus large.
	if b, _ := s.RemiseEnFile(clefEssai, plancher, borneLarge); !b.DejaFaite {
		t.Fatalf("%+v", b)
	}
	if s.Counts()["rejected"] != 2 {
		t.Fatalf("%v", s.Counts())
	}
}

// Une erreur juste avant la validation ne laisse rien : ni clé, ni ligne déplacée.
func TestRemiseErreurAvantValidation(t *testing.T) {
	s := ouvreEssai(t, filepath.Join(t.TempDir(), "c.db"))
	defer s.Close()
	jeu(t, s, []lot{{"a", plancher + 1, "source_disabled"}}, []fiche{{"f", plancher + 1, plancher + 1, "source_disabled"}})
	avantCommitRemise = func() error { return errors.New("arrêt simulé") }
	_, err := s.RemiseEnFile(clefEssai, plancher, borneLarge)
	avantCommitRemise = nil
	if err == nil {
		t.Fatal("erreur attendue")
	}
	if s.Get(clefEssai) != "" || s.Counts()["pending"] != 0 || s.StatsCounts()["pending"] != 0 {
		t.Fatalf("reste de transaction : %q %v %v", s.Get(clefEssai), s.Counts(), s.StatsCounts())
	}
	if b, err := s.RemiseEnFile(clefEssai, plancher, borneLarge); err != nil || b.Lots != 1 || b.Fiches != 1 {
		t.Fatalf("reprise : %+v, %v", b, err)
	}
}

// Un vrai arrêt brutal : un processus fils sort sans fermer la base, avant ou après la
// validation. Le processus suivant ne perd rien et ne repasse pas deux fois.
func TestRemiseProcessusFils(t *testing.T) {
	path := os.Getenv("WOWSYNC_REMISE_FILS_BASE")
	if path == "" {
		t.Skip("exécuté seulement comme processus fils de TestRemiseArretBrutal")
	}
	s, err := Open(path)
	if err != nil {
		os.Exit(90)
	}
	if os.Getenv("WOWSYNC_REMISE_FILS_MODE") == "avant" {
		avantCommitRemise = func() error { os.Exit(91); return nil }
	}
	if _, err := s.RemiseEnFile(clefEssai, plancher, borneLarge); err != nil {
		os.Exit(92)
	}
	os.Exit(93) // après la validation, sans fermer la base
}

func arretFils(t *testing.T, path, mode string, attendu int) {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^TestRemiseProcessusFils$")
	c.Env = append(os.Environ(), "WOWSYNC_REMISE_FILS_BASE="+path, "WOWSYNC_REMISE_FILS_MODE="+mode)
	err := c.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		t.Skipf("processus fils non lancé (%v) : arrêt brutal non testé ici", err)
	}
	if ee.ExitCode() != attendu {
		t.Fatalf("processus fils : code %d, attendu %d", ee.ExitCode(), attendu)
	}
}

func TestRemiseArretBrutal(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.db")
	s := ouvreEssai(t, path)
	jeu(t, s, []lot{{"a", plancher + 1, "source_disabled"}, {"b", plancher + 2, "source_disabled"}},
		[]fiche{{"f", plancher + 1, plancher + 1, "source_disabled"}})
	s.Close()

	arretFils(t, path, "avant", 91)
	s = ouvreEssai(t, path)
	if s.Get(clefEssai) != "" || s.Counts()["rejected"] != 2 || s.StatsCounts()["rejected"] != 1 {
		t.Fatalf("arrêt avant validation : %q %v %v", s.Get(clefEssai), s.Counts(), s.StatsCounts())
	}
	s.Close()

	arretFils(t, path, "apres", 93)
	s = ouvreEssai(t, path)
	defer s.Close()
	if s.Get(clefEssai) == "" || s.Counts()["pending"] != 2 || s.StatsCounts()["pending"] != 1 {
		t.Fatalf("arrêt après validation : %q %v %v", s.Get(clefEssai), s.Counts(), s.StatsCounts())
	}
	_ = s.MarkRejected([]string{"a"}, "source_disabled")
	if b, err := s.RemiseEnFile(clefEssai, plancher, borneLarge); err != nil || !b.DejaFaite || s.Counts()["rejected"] != 1 {
		t.Fatalf("redémarrage : %+v, %v, %v", b, err, s.Counts())
	}
}

// Le chemin d'origine accompagne l'en-tête ; une fiche sans ligne de files n'en a pas.
func TestStatsPendingSource(t *testing.T) {
	s := ouvreEssai(t, filepath.Join(t.TempDir(), "c.db"))
	defer s.Close()
	ctx := context.Background()
	if _, err := s.AddStatsFile(ctx, "seul", []byte(`{"file":{}}`), []StatSheet{{ID: "a", Digest: "d", Payload: []byte(`{}`)}}, 1, 0); err != nil {
		t.Fatal(err)
	}
	m, chemin, sh, err := s.StatsPendingSource(1 << 20)
	if err != nil || string(m) != `{"file":{}}` || chemin != "" || len(sh) != 1 {
		t.Fatalf("%s %q %v %v", m, chemin, sh, err)
	}
	if err := s.MarkStats(sh, "sent", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddFile(ctx, File{SHA256: "avec", Path: `C:\jeu\ForeverPulse.lua`, Origin: "current", Scopes: []byte(`{}`)}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddStatsFile(ctx, "avec", []byte(`{"file":{"x":1}}`), []StatSheet{{ID: "b", Digest: "d", Payload: []byte(`{}`)}}, 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, chemin, sh, err = s.StatsPendingSource(1 << 20); err != nil || chemin != `C:\jeu\ForeverPulse.lua` || len(sh) != 1 {
		t.Fatalf("%q %v %v", chemin, sh, err)
	}
}
