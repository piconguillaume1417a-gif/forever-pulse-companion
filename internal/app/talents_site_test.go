package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
	"time"

	"wowsync/internal/connect"
	"wowsync/internal/logx"
	"wowsync/internal/sender"
	"wowsync/internal/store"
)

// Talents de bout en bout (contrat « spécialisations » v1, §3-§4, §6), SYNTHÉTIQUE.
// Test opt-in lancé par le harnais PostgreSQL du site (scripts/supabase/talent-specs-e2e.mjs) :
// vraies routes du site, base éphémère, coffre en mémoire, aucun fichier installé.
//
// Entrée : testdata/talents-e2e/ForeverPulse-{1,2}.lua, écrits par le VRAI code de l'addon 4.2.0
// dans un client simulé (addon-4.2.0/outils/fixture_talents_e2e.lua). Leurs époques sont décalées
// d'un même écart pour que le dernier relevé date d'il y a 10 minutes (rétention du site).
//
// À chaque étape, /test-step demande au harnais de contrôler l'ACK et la base ; /test-schema
// fait répondre la route comme le site PROD actuel (409 { supported: [1] }) ou comme le site 2.
func TestTalentsActualLocalSite(t *testing.T) {
	site := os.Getenv("PULSE_COMPANION_TEST_SITE")
	if site == "" || os.Getenv("PULSE_TALENTS_E2E") != "1" {
		t.Skip("requires the site's owned PostgreSQL harness (talent-specs-e2e.mjs)")
	}
	u, e := url.Parse(site)
	if e != nil || u.Hostname() != "127.0.0.1" || u.Scheme != "http" {
		t.Fatal("non-local test site refused")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	dir := t.TempDir()

	get := func(path string) {
		t.Helper()
		r, e := http.Get(site + path)
		if e != nil {
			t.Fatalf("%s : transport", path)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("%s : %d %s", path, r.StatusCode, b)
		}
		t.Logf("%s : %s", path, b)
	}

	// Fixtures : un même écart pour toutes les époques des deux fichiers.
	epoque := regexp.MustCompile(`\b179[0-9]{7}\b`)
	var bruts [2][]byte
	var max int64
	for i := range bruts {
		b, e := os.ReadFile(filepath.Join("testdata", "talents-e2e", fmt.Sprintf("ForeverPulse-%d.lua", i+1)))
		if e != nil {
			t.Fatal(e)
		}
		bruts[i] = b
		for _, m := range epoque.FindAll(b, -1) {
			if v, _ := strconv.ParseInt(string(m), 10, 64); v > max {
				max = v
			}
		}
	}
	ecart := time.Now().Add(-10*time.Minute).Unix() - max
	var chemins [2]string
	for i, b := range bruts {
		d := epoque.ReplaceAllFunc(b, func(m []byte) []byte {
			v, _ := strconv.ParseInt(string(m), 10, 64)
			return []byte(strconv.FormatInt(v+ecart, 10))
		})
		chemins[i] = filepath.Join(dir, fmt.Sprintf("session-%d", i+1), "ForeverPulse.lua")
		if e := os.MkdirAll(filepath.Dir(chemins[i]), 0o700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(chemins[i], d, 0o600); e != nil {
			t.Fatal(e)
		}
	}
	get(fmt.Sprintf("/test-step?name=shift&delta=%d", ecart))

	// Une installation = une base locale ; le jeton (synthétique) est commun.
	token := ""
	ouvre := func(nom string) (*App, *store.Store) {
		t.Helper()
		st, e := store.Open(filepath.Join(dir, nom+".db"))
		if e != nil {
			t.Fatal(e)
		}
		log, e := logx.Open(filepath.Join(dir, nom+".log"))
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { st.Close(); log.Close() })
		a, e := New(st, log, sender.New(site, Version))
		if e != nil {
			t.Fatal(e)
		}
		a.Token = func() (string, error) {
			if token == "" {
				return "", errors.New("absent")
			}
			return token, nil
		}
		a.SaveToken = func(s string) error { token = s; return nil }
		return a, st
	}
	// traite : nombre de fiches de statistiques mises en file (ProcessFile compte les lots).
	traite := func(a *App, chemin string) int {
		t.Helper()
		_, e := a.ProcessFile(ctx, chemin)
		if e != nil {
			if data, re := os.ReadFile(chemin); re == nil {
				if f, _, _, de := DecodeTout(data, a.Now()); de == nil {
					t.Fatalf("%v : %q", e, f.Fatales)
				}
			}
			t.Fatal(e)
		}
		return a.Etat().StatsPending
	}
	envoie := func(a *App, manuel bool) {
		t.Helper()
		if e := a.Flush(ctx, manuel); e != nil {
			t.Fatal(e)
		}
		if s := a.Etat(); s.StatsPending != 0 || s.StatsRejected != 0 {
			t.Fatalf("file locale : %d en attente, %d refusées", s.StatsPending, s.StatsRejected)
		}
	}

	// Association par le vrai parcours d'appareil (approbation par le harnais).
	a, st := ouvre("a")
	c := connect.Client{Site: site, Vault: &testConnectionVault{}, SaveToken: a.SetToken, Open: func(uri string) {
		q, _ := url.Parse(uri)
		r, e := http.Get(site + "/test-approve?request=" + q.Query().Get("request"))
		if e != nil {
			t.Error("test approval transport failed")
			return
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Error("test approval failed")
		}
	}}
	if e = c.Start(ctx); e != nil {
		t.Fatal(e)
	}
	if token == "" {
		t.Fatal("authorization missing")
	}

	// 1. Site au schéma 1 seul (PROD actuelle) : repli, statistiques acceptées sans talents.
	get("/test-schema?only=1")
	n := traite(a, chemins[0])
	envoie(a, true)
	get(fmt.Sprintf("/test-step?name=fallback&queued=%d", n))

	// 2. Site au schéma 2, 6 h plus tard (horloge du compagnon) : la session 2 part en schéma 2,
	//    puis les fiches à talents envoyées en schéma 1 repartent une fois.
	get("/test-schema?only=0")
	a.Now = func() time.Time { return time.Now().Add(ReessaiSchema2 + time.Minute) }
	n = traite(a, chemins[1])
	envoie(a, false)
	get(fmt.Sprintf("/test-step?name=schema2&queued=%d", n))

	// 3. Une fois seulement : ni un nouvel envoi automatique, ni un redémarrage ne les renvoient.
	a.Now = func() time.Time { return time.Now().Add(2*ReessaiSchema2 + time.Hour) }
	envoie(a, false)
	st.Close()
	a, st = ouvre("a")
	envoie(a, true)
	if n = traite(a, chemins[1]); n != 0 {
		t.Fatalf("fichier déjà traité remis en file : %d", n)
	}
	envoie(a, true)
	get("/test-step?name=once")

	// 4. Renvoi identique (autre installation, même jeton) : doublons, base inchangée.
	b, _ := ouvre("b")
	n = traite(b, chemins[1])
	envoie(b, true)
	get(fmt.Sprintf("/test-step?name=duplicates&queued=%d", n))

	// 5. Relevé plus ancien : le compagnon qui connaît le récent ne renvoie rien ; une installation
	//    neuve l'envoie et le site l'ignore.
	if n = traite(b, chemins[0]); n != 0 {
		t.Fatalf("relevé plus ancien remis en file par le compagnon : %d", n)
	}
	envoie(b, true)
	get("/test-step?name=older-local")
	c3, _ := ouvre("c")
	n = traite(c3, chemins[0])
	envoie(c3, true)
	get(fmt.Sprintf("/test-step?name=older&queued=%d", n))
}
