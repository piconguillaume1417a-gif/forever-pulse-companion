package watch

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDiscoverSansCheminEnDur(t *testing.T) {
	wow := t.TempDir()
	mk := func(rel string) string {
		p := filepath.Join(wow, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o600)
		return p
	}
	a := mk("_classic_beta_/WTF/Account/123456789#1/SavedVariables/ForeverPulse.lua")
	b := mk("_classic_beta_/WTF/Account/123456789#1/SavedVariables/ForeverPulse.lua.bak")
	c := mk("_classic_era_/WTF/Account/AUTRE/SavedVariables/ForeverPulse.lua")
	mk("_classic_beta_/WTF/Account/123456789#1/SavedVariables/Questie.lua")
	mk("_classic_beta_/WTF/Account/123456789#1/Serveur/Perso/SavedVariables/ForeverPulse.lua")
	got := Discover(wow)
	want := []string{a, b, c}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%v\n%v", got, want)
	}
}

func TestStableApres3s(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ForeverPulse.lua")
	_ = os.WriteFile(p, []byte("a"), 0o600)
	w := New()
	t0 := time.Now()
	if len(w.Ready([]string{p}, t0)) != 0 {
		t.Fatal("prêt dès la première vue")
	}
	if len(w.Ready([]string{p}, t0.Add(2*time.Second))) != 0 {
		t.Fatal("prêt avant 3 s")
	}
	if len(w.Ready([]string{p}, t0.Add(3*time.Second))) != 1 {
		t.Fatal("pas prêt après 3 s")
	}
	if len(w.Ready([]string{p}, t0.Add(10*time.Second))) != 0 {
		t.Fatal("traité deux fois")
	}
	_ = os.WriteFile(p, []byte("ab"), 0o600) // le jeu réécrit
	if len(w.Ready([]string{p}, t0.Add(11*time.Second))) != 0 || len(w.Ready([]string{p}, t0.Add(14*time.Second))) != 1 {
		t.Fatal("réécriture non détectée")
	}
	data, sha, err := Read(p)
	if err != nil || string(data) != "ab" || len(sha) != 64 {
		t.Fatal(err)
	}
}

// 0.6.0 : un changement repéré au sondage (10 min) est signalé « en attente » pour
// être confirmé 3 s plus tard, pas au sondage suivant.
func TestEnAttenteJusquAuTraitement(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ForeverPulse.lua")
	_ = os.WriteFile(p, []byte("a"), 0o600)
	w := New()
	w.Mark([]string{p})
	if w.EnAttente() {
		t.Fatal("rien n'a changé")
	}
	_ = os.WriteFile(p, []byte("abc"), 0o600)
	t0 := time.Now()
	if len(w.Ready([]string{p}, t0)) != 0 || !w.EnAttente() {
		t.Fatal("changement non signalé")
	}
	if len(w.Ready([]string{p}, t0.Add(w.Stable))) != 1 || w.EnAttente() {
		t.Fatal("non traité après la confirmation")
	}
}

func TestAuctionDiscoveryL1AndSiblingStability(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "_classic_beta_", "WTF", "Account", "Synthetic", "SavedVariables")
	os.MkdirAll(dir, 0700)
	p := filepath.Join(dir, "Auctionator.lua")
	note := filepath.Join(dir, "ForeverPulse.lua")
	os.WriteFile(p, []byte("data"), 0600)
	os.WriteFile(p+".bak", []byte("older"), 0600)
	os.WriteFile(note, []byte("note"), 0600)
	got := DiscoverAuctions(root)
	if len(got) != 1 || got[0] != p {
		t.Fatal("L1 backup read")
	}
	sig, e := PairSignature(p)
	if e != nil {
		t.Fatal(e)
	}
	w := New()
	now := time.Now()
	w.Ready([]string{p, note}, now)
	if w.IsStable(p, now.Add(2*time.Second)) {
		t.Fatal("L1 less than 3 seconds")
	}
	if !w.IsStable(p, now.Add(3*time.Second)) {
		t.Fatal("L1 not stable")
	}
	os.WriteFile(note, []byte("changed note"), 0600)
	if w.IsStable(note, now.Add(4*time.Second)) {
		t.Fatal("J4 stale sibling metadata")
	}
	newSig, e := PairSignature(p)
	if e != nil || newSig == sig {
		t.Fatal("J4 sibling change not detected")
	}
}
