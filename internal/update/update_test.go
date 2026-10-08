package update

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func init() { Lance = func(string, string) error { return nil } } // les exécutables de test ne sont pas de vrais programmes

func TestPlus(t *testing.T) {
	for _, k := range []struct {
		a, b string
		plus bool
	}{
		{"0.10.0", "0.9.0", true}, // numérique, pas lexical
		{"0.9.0", "0.10.0", false},
		{"0.9.0", "0.9.0-rc.2", true}, // la finale suit ses candidates
		{"0.9.0-rc.2", "0.9.0", false},
		{"0.9.0-rc.10", "0.9.0-rc.2", true},
		{"0.9.0-rc.2", "0.9.0-rc.2", false},
		{"1.0.0", "0.99.99", true},
		{"0.10.0-rc.1", "0.9.0", true},
		{"0.9.1-rc.1", "0.9.0", true},
	} {
		got, err := Plus(k.a, k.b)
		if err != nil || got != k.plus {
			t.Errorf("Plus(%q, %q) = %t, %v ; attendu %t", k.a, k.b, got, err, k.plus)
		}
	}
	for _, bad := range []string{"", "1.0", "1.0.0.0", "v1.0.0", "1.0.0-beta", "1.0.0-rc.0", "1.0.0-rc.01", "01.0.0", "1.-1.0"} {
		if _, err := Plus(bad, "0.0.0"); err == nil {
			t.Errorf("Plus(%q) accepté", bad)
		}
	}
}

// publication de test : clé, exécutable et manifeste signé servis en https.
type banc struct {
	srv       *httptest.Server
	priv      ed25519.PrivateKey
	exe       []byte
	brut, sig []byte
	c         *Client
}

func nouveauBanc(t *testing.T, version string, modifie func(*Manifeste)) *banc {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	b := &banc{priv: priv, exe: []byte("MZ programme " + version)}
	mux := http.NewServeMux()
	b.srv = httptest.NewTLSServer(mux)
	t.Cleanup(b.srv.Close)
	h := sha256.Sum256(b.exe)
	m := Manifeste{Format: 1, Produit: Produit, Version: version, URL: b.srv.URL + "/v/ForeverPulseCompanion.exe",
		SHA256: hex.EncodeToString(h[:]), Taille: int64(len(b.exe)), Publiee: "2026-10-08T00:00:00Z"}
	if modifie != nil {
		modifie(&m)
	}
	b.brut, _ = json.Marshal(m)
	b.sig = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, b.brut)))
	mux.HandleFunc("/latest/latest.json", func(w http.ResponseWriter, _ *http.Request) { w.Write(b.brut) })
	mux.HandleFunc("/latest/latest.json.sig", func(w http.ResponseWriter, _ *http.Request) { w.Write(b.sig) })
	mux.HandleFunc("/v/ForeverPulseCompanion.exe", func(w http.ResponseWriter, _ *http.Request) { w.Write(b.exe) })
	u, _ := url.Parse(b.srv.URL)
	b.c = &Client{Version: "0.9.0-rc.2", Cle: pub, Base: b.srv.URL + "/latest", HTTP: b.srv.Client(),
		Autorises: map[string]bool{u.Hostname(): true}}
	return b
}

func TestChercheTelechargeInstalle(t *testing.T) {
	b := nouveauBanc(t, "0.10.0", nil)
	o, err := b.c.Cherche(context.Background())
	if err != nil || !o.Nouvelle || o.Imposee {
		t.Fatalf("Cherche : %+v, %v", o, err)
	}
	dir := t.TempDir()
	if err := b.c.Telecharge(context.Background(), o, dir); err != nil {
		t.Fatal(err)
	}
	m, pret, ok := b.c.Prete(dir)
	if !ok || m.Version != "0.10.0" {
		t.Fatalf("Prete : %v %q", ok, pret)
	}
	exe := filepath.Join(t.TempDir(), "ForeverPulseCompanion.exe")
	os.WriteFile(exe, []byte("ancien"), 0o600)
	if err := Installe(exe, dir, "0.9.0-rc.2", m, pret); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != string(b.exe) {
		t.Fatal("programme non remplacé")
	}
	if got, _ := os.ReadFile(exe + ".old"); string(got) != "ancien" {
		t.Fatal("ancienne version non gardée")
	}
	if _, _, ok := b.c.Prete(dir); ok {
		t.Fatal("version installée encore prête")
	}
	// Premier démarrage de la nouvelle version, puis confirmation.
	if _, retour := Demarre(dir, "0.10.0"); retour {
		t.Fatal("retour arrière dès le premier démarrage")
	}
	if e, ok := Confirme(exe, dir, "0.10.0"); !ok || e.De != "0.9.0-rc.2" {
		t.Fatalf("Confirme : %+v %v", e, ok)
	}
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal("ancienne version gardée après confirmation")
	}
}

func TestRetourArriere(t *testing.T) {
	b := nouveauBanc(t, "0.10.0", nil)
	o, _ := b.c.Cherche(context.Background())
	dir := t.TempDir()
	_ = b.c.Telecharge(context.Background(), o, dir)
	m, pret, _ := b.c.Prete(dir)
	exe := filepath.Join(t.TempDir(), "ForeverPulseCompanion.exe")
	os.WriteFile(exe, []byte("ancien"), 0o600)
	if err := Installe(exe, dir, "0.9.0-rc.2", m, pret); err != nil {
		t.Fatal(err)
	}
	var e Essai
	var retour bool
	for i := 0; i <= MaxTentatives; i++ {
		e, retour = Demarre(dir, "0.10.0")
	}
	if !retour {
		t.Fatal("pas de retour arrière après MaxTentatives+1 démarrages")
	}
	if err := Retablit(exe, dir, e); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(exe); string(got) != "ancien" {
		t.Fatal("ancienne version non rétablie")
	}
	if !Refusee(dir, "0.10.0") {
		t.Fatal("version fautive non refusée")
	}
	// L'ancienne version retélécharge la même publication : jamais reproposée.
	b.c.Version = "0.9.0-rc.2"
	_ = b.c.Telecharge(context.Background(), o, dir)
	if _, _, ok := b.c.Prete(dir); ok {
		t.Fatal("version refusée de nouveau prête")
	}
}

func TestRefus(t *testing.T) {
	ctx := context.Background()
	t.Run("signature d'une autre clé", func(t *testing.T) {
		b := nouveauBanc(t, "0.10.0", nil)
		autre, _, _ := ed25519.GenerateKey(rand.Reader)
		b.c.Cle = autre
		if _, err := b.c.Cherche(ctx); err == nil || !strings.Contains(err.Error(), "signature") {
			t.Fatal(err)
		}
	})
	t.Run("manifeste modifié après signature", func(t *testing.T) {
		b := nouveauBanc(t, "0.10.0", nil)
		b.brut = []byte(strings.Replace(string(b.brut), "0.10.0", "0.11.0", 1))
		if _, err := b.c.Cherche(ctx); err == nil {
			t.Fatal("accepté")
		}
	})
	t.Run("autre produit", func(t *testing.T) {
		b := nouveauBanc(t, "0.10.0", func(m *Manifeste) { m.Produit = "autre" })
		if _, err := b.c.Cherche(ctx); err == nil {
			t.Fatal("accepté")
		}
	})
	t.Run("exécutable sur un hôte non autorisé", func(t *testing.T) {
		b := nouveauBanc(t, "0.10.0", func(m *Manifeste) { m.URL = "https://exemple.test/x.exe" })
		if _, err := b.c.Cherche(ctx); err == nil {
			t.Fatal("accepté")
		}
	})
	t.Run("exécutable différent de l'empreinte", func(t *testing.T) {
		b := nouveauBanc(t, "0.10.0", nil)
		o, err := b.c.Cherche(ctx)
		if err != nil {
			t.Fatal(err)
		}
		b.exe[0] = 'X'
		dir := t.TempDir()
		if err := b.c.Telecharge(ctx, o, dir); err == nil {
			t.Fatal("accepté")
		}
		if _, _, ok := b.c.Prete(dir); ok {
			t.Fatal("fichier gardé")
		}
	})
	t.Run("redirection hors liste", func(t *testing.T) {
		b := nouveauBanc(t, "0.10.0", nil)
		ailleurs := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(b.brut) }))
		defer ailleurs.Close()
		b.c.Base = b.srv.URL + "/redirige"
		b.srv.Config.Handler.(*http.ServeMux).HandleFunc("/redirige/latest.json", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, strings.Replace(ailleurs.URL, "127.0.0.1", "localhost", 1)+"/latest.json", http.StatusFound)
		})
		if _, err := b.c.Cherche(ctx); err == nil || !strings.Contains(err.Error(), "refusée") {
			t.Fatal(err)
		}
	})
	t.Run("version plus ancienne ou égale", func(t *testing.T) {
		b := nouveauBanc(t, "0.9.0-rc.1", nil)
		o, err := b.c.Cherche(ctx)
		if err != nil || o.Nouvelle {
			t.Fatalf("%+v %v", o, err)
		}
	})
	t.Run("version minimale", func(t *testing.T) {
		b := nouveauBanc(t, "0.10.0", func(m *Manifeste) { m.MinVersion = "0.9.0" })
		o, err := b.c.Cherche(ctx)
		if err != nil || !o.Imposee {
			t.Fatalf("%+v %v", o, err)
		}
	})
}

// La clé intégrée doit être lisible : sinon aucune mise à jour ne passerait jamais.
func TestClePubliqueIntegree(t *testing.T) {
	if len(ClePublique()) != ed25519.PublicKeySize {
		t.Fatal("clé publique intégrée illisible")
	}
}

// Une copie que Windows refuse de lancer ne remplace jamais le programme en service.
func TestCopieBloqueeParWindows(t *testing.T) {
	b := nouveauBanc(t, "0.10.0", nil)
	o, _ := b.c.Cherche(context.Background())
	dir := t.TempDir()
	_ = b.c.Telecharge(context.Background(), o, dir)
	m, pret, ok := b.c.Prete(dir)
	if !ok {
		t.Fatal("non prête")
	}
	exe := filepath.Join(t.TempDir(), "ForeverPulseCompanion.exe")
	os.WriteFile(exe, []byte("ancien"), 0o600)
	avant := Lance
	Lance = func(string, string) error { return errors.New("bloqué") }
	defer func() { Lance = avant }()
	if err := Installe(exe, dir, "0.9.0-rc.2", m, pret); err == nil {
		t.Fatal("installée malgré le blocage")
	}
	if got, _ := os.ReadFile(exe); string(got) != "ancien" {
		t.Fatal("programme en service modifié")
	}
	for _, n := range []string{exe + ".new", exe + ".old", filepath.Join(dir, fichierEssai)} {
		if _, err := os.Stat(n); !os.IsNotExist(err) {
			t.Fatalf("%s laissé", n)
		}
	}
	if !Refusee(dir, "0.10.0") {
		t.Fatal("version bloquée non refusée (elle serait réessayée à chaque démarrage)")
	}
}

// Le numéro Windows suit l'ordre des versions (le MSI s'en sert pour les mises à niveau).
func TestNumeroWindows(t *testing.T) {
	ordre := []string{"0.8.0", "0.9.0-rc.2", "0.9.0", "0.10.0-rc.1", "0.10.0-rc.3", "0.10.0", "0.10.1-rc.1", "0.10.1", "1.0.0"}
	var avant [3]int
	for i, v := range ordre {
		n, err := NumeroWindows(v)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 && !(n[0] > avant[0] || n[0] == avant[0] && (n[1] > avant[1] || n[1] == avant[1] && n[2] > avant[2])) {
			t.Fatalf("%s → %v pas après %v", v, n, avant)
		}
		avant = n
	}
	if n, _ := NumeroWindows("0.10.0-rc.3"); n != [3]int{0, 10, 3} {
		t.Fatal(n)
	}
	if _, err := NumeroWindows("0.10.65-rc.1"); err == nil {
		t.Fatal("hors bornes accepté")
	}
}
