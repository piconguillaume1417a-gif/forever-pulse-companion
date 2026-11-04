package app

import (
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"wowsync/internal/logx"
	"wowsync/internal/sender"
	"wowsync/internal/store"
)

// siteRegions : un faux site qui garde chaque corps reçu, accepte ou refuse tout.
type siteRegions struct {
	mu     sync.Mutex
	census [][]byte
	stats  [][]byte
	refuse bool
}

func (s *siteRegions) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gz, err := gzip.NewReader(r.Body)
	if err != nil {
		w.WriteHeader(400)
		return
	}
	b, _ := io.ReadAll(gz)
	var c struct {
		Batches []struct {
			BatchID string `json:"batch_id"`
		} `json:"batches"`
		Characters []struct {
			ID string `json:"id"`
		} `json:"characters"`
	}
	_ = json.Unmarshal(b, &c)
	var ids []string
	cle := "batch_id"
	if strings.Contains(r.URL.Path, "stats") {
		s.stats = append(s.stats, b)
		cle = "id"
		for _, x := range c.Characters {
			ids = append(ids, x.ID)
		}
	} else {
		s.census = append(s.census, b)
		for _, x := range c.Batches {
			ids = append(ids, x.BatchID)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if s.refuse {
		rej := []string{}
		for _, id := range ids {
			rej = append(rej, fmt.Sprintf(`{%q:%q,"reason":"source_disabled"}`, cle, id))
		}
		w.WriteHeader(200)
		fmt.Fprintf(w, `{"accepted":0,"duplicates":0,"rejected":[%s]}`, strings.Join(rej, ","))
		return
	}
	w.WriteHeader(202)
	fmt.Fprintf(w, `{"accepted":%d,"duplicates":0,"rejected":[]}`, len(ids))
}

type bancRegions struct {
	t      *testing.T
	a      *App
	st     *store.Store
	log    *logx.Logger
	srv    *siteRegions
	hs     *httptest.Server
	dir    string
	wtf    string
	chemin string
}

// nouveauBancRegions : un dossier de jeu synthétique sous un dossier temporaire. Le nom
// du dossier de compte (123456#1) n'est jamais une des fausses valeurs de Config.wtf.
func nouveauBancRegions(t *testing.T, dossier string) *bancRegions {
	t.Helper()
	dir := t.TempDir()
	wtf := filepath.Join(dir, "World of Warcraft", dossier, "WTF")
	sv := filepath.Join(wtf, "Account", "123456#1", "SavedVariables")
	if err := os.MkdirAll(sv, 0o700); err != nil {
		t.Fatal(err)
	}
	b := &bancRegions{t: t, srv: &siteRegions{}, dir: dir, wtf: wtf, chemin: filepath.Join(sv, "ForeverPulse.lua")}
	b.hs = httptest.NewServer(b.srv)
	t.Cleanup(b.hs.Close)
	b.ouvre()
	t.Cleanup(b.ferme)
	return b
}

func (b *bancRegions) ouvre() {
	b.t.Helper()
	st, err := store.Open(filepath.Join(b.dir, "companion.db"))
	if err != nil {
		b.t.Fatal(err)
	}
	log, err := logx.Open(filepath.Join(b.dir, "companion.log"))
	if err != nil {
		b.t.Fatal(err)
	}
	a, err := New(st, log, sender.New(b.hs.URL, Version))
	if err != nil {
		b.t.Fatal(err)
	}
	a.Now = func() time.Time { return time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC) }
	a.Token = func() (string, error) { return jeton, nil }
	b.a, b.st, b.log = a, st, log
}

func (b *bancRegions) ferme() {
	if b.st != nil {
		b.st.Close()
		b.log.Close()
		b.st, b.log = nil, nil
	}
}

func (b *bancRegions) redemarre() {
	b.t.Helper()
	b.ferme()
	b.ouvre()
}

func (b *bancRegions) config(contenu string) {
	b.t.Helper()
	if err := os.WriteFile(filepath.Join(b.wtf, "Config.wtf"), []byte(contenu), 0o600); err != nil {
		b.t.Fatal(err)
	}
}

// fichierRegion : un fichier avec un lot de recensement et une fiche de statistiques,
// dont le périmètre et le contexte portent ce code de région, dans l'environnement live.
func fichierRegion(seed int, code string) []byte {
	return fichierRegionEnv(seed, code, "live")
}

func fichierRegionEnv(seed int, code, env string) []byte {
	data := string(fichierStats(seed, 1790505847, map[string][]string{"00000001": {"1790505847|20|n|60:0,94:5"}}))
	data = strings.ReplaceAll(data, `"beta"`, fmt.Sprintf("%q", env))
	return []byte(strings.ReplaceAll(data, `"90"`, fmt.Sprintf("%q", code)))
}

func (b *bancRegions) lit(data []byte) {
	b.t.Helper()
	if err := os.WriteFile(b.chemin, data, 0o600); err != nil {
		b.t.Fatal(err)
	}
	if n, err := b.a.ProcessFile(context.Background(), b.chemin); err != nil || n == 0 {
		b.t.Fatalf("lecture : %d, %v", n, err)
	}
}

// regions : la région du périmètre et du contexte, lue dans les derniers corps envoyés.
func (b *bancRegions) regions() (census, stats any) {
	b.t.Helper()
	if len(b.srv.census) == 0 || len(b.srv.stats) == 0 {
		b.t.Fatalf("corps reçus : %d recensement, %d statistiques", len(b.srv.census), len(b.srv.stats))
	}
	var c struct {
		Scopes map[string]map[string]any `json:"scopes"`
	}
	if err := json.Unmarshal(b.srv.census[len(b.srv.census)-1], &c); err != nil {
		b.t.Fatal(err)
	}
	var s struct {
		Contexts map[string]map[string]any `json:"contexts"`
	}
	if err := json.Unmarshal(b.srv.stats[len(b.srv.stats)-1], &s); err != nil {
		b.t.Fatal(err)
	}
	census, stats = c.Scopes["4619-PVP-Alliance"]["region"], s.Contexts["beta-4619-PVP"]["region"]
	return census, stats
}

func (b *bancRegions) journal() string {
	b.t.Helper()
	d, err := os.ReadFile(filepath.Join(b.dir, "companion.log"))
	if err != nil {
		b.t.Fatal(err)
	}
	return string(d)
}

func nbLignes(texte, motif string) int {
	n := 0
	for _, l := range strings.Split(texte, "\n") {
		if strings.Contains(l, motif) {
			n++
		}
	}
	return n
}

// Un code connu part tel quel, octet pour octet, et Config.wtf n'est même pas ouvert :
// le journal ne dit rien du portail alors qu'une autre valeur y est écrite.
func TestRepliCodeConnuInchange(t *testing.T) {
	for i, code := range []string{"1", "2", "3", "4", "5", "91", "92", "93", "94", "95", "US", "NA", "KR", "EU", "TW", "CN",
		"us", "na", "kr", "eu", "tw", "cn", " 93 ", " eu"} {
		t.Run(strings.TrimSpace(code)+fmt.Sprint(i), func(t *testing.T) {
			b := nouveauBancRegions(t, "_forever_")
			b.config(`SET portal "KR"` + "\n")
			if strings.EqualFold(strings.TrimSpace(code), "KR") {
				b.config(`SET portal "TW"` + "\n")
			}
			b.lit(fichierRegion(i+1, code))
			if err := b.a.Flush(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			if c, s := b.regions(); c != code || s != code {
				t.Fatalf("envoyé : %v / %v, attendu %q", c, s, code)
			}
			if j := b.journal(); strings.Contains(j, "portal") {
				t.Fatalf("Config.wtf lu pour un code connu :\n%s", j)
			}
		})
	}
}

func TestRepliInconnuAvecPortal(t *testing.T) {
	for i, code := range []string{"90", "110", "96", "0", "UNKNOWN", ""} {
		t.Run("code "+code, func(t *testing.T) {
			b := nouveauBancRegions(t, "_forever_")
			b.config(`SET portal "EU"` + "\r\n")
			b.lit(fichierRegion(i+1, code))
			if err := b.a.Flush(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			if c, s := b.regions(); c != "EU" || s != "EU" {
				t.Fatalf("envoyé : %v / %v", c, s)
			}
			if j := b.journal(); nbLignes(j, "portal utilisé : EU") != 1 {
				t.Fatalf("journal :\n%s", j)
			}
		})
	}
}

// _forever_ a aussi servi à la bêta : un contexte de test dans ce dossier garde son code
// et son refus, même avec un portail clair, et Config.wtf n'est pas ouvert s'il est seul.
func TestRepliEnvironnementDeTestInchange(t *testing.T) {
	for i, env := range []string{"beta", "BETA", " ptr "} {
		t.Run(env, func(t *testing.T) {
			b := nouveauBancRegions(t, "_forever_")
			b.config(`SET portal "EU"` + "\n")
			b.lit(fichierRegionEnv(i+1, "90", env))
			if err := b.a.Flush(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			if len(b.srv.stats) == 0 {
				t.Fatal("aucune statistique envoyée")
			}
			var s struct {
				Contexts map[string]map[string]any `json:"contexts"`
			}
			if err := json.Unmarshal(b.srv.stats[len(b.srv.stats)-1], &s); err != nil {
				t.Fatal(err)
			}
			if got := s.Contexts["beta-4619-PVP"]["region"]; got != "90" {
				t.Fatalf("contexte de test envoyé en %v", got)
			}
		})
	}
}

// Un dossier _FOREVER_ écrit autrement reste le même dossier sous Windows.
func TestRepliCasseDuDossier(t *testing.T) {
	b := nouveauBancRegions(t, "_FOREVER_")
	b.config(`SET portal "TW"`)
	b.lit(fichierRegion(1, "96"))
	if err := b.a.Flush(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if c, s := b.regions(); c != "TW" || s != "TW" {
		t.Fatalf("envoyé : %v / %v", c, s)
	}
}

func TestRepliSansValeurClaire(t *testing.T) {
	gros := `SET portal "EU"` + "\n" + strings.Repeat("SET x \"y\"\n", 30_000)
	cas := []struct {
		nom     string
		prepare func(b *bancRegions)
	}{
		{"absent", func(b *bancRegions) {}},
		{"trop gros", func(b *bancRegions) { b.config(gros) }},
		{"illisible", func(b *bancRegions) {
			if err := os.Mkdir(filepath.Join(b.wtf, "Config.wtf"), 0o700); err != nil {
				b.t.Fatal(err)
			}
		}},
		{"sans ligne portal", func(b *bancRegions) { b.config(`SET accountName "FAKE-ACCOUNT-TESTONLY"`) }},
		{"test", func(b *bancRegions) { b.config(`SET portal "test"`) }},
		{"public-test", func(b *bancRegions) { b.config(`SET portal "public-test"`) }},
		{"deux valeurs", func(b *bancRegions) { b.config("SET portal \"EU\"\nSET portal \"KR\"\n") }},
	}
	for i, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			b := nouveauBancRegions(t, "_forever_")
			c.prepare(b)
			b.lit(fichierRegion(i+1, "96"))
			if err := b.a.Flush(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			if c, s := b.regions(); c != "96" || s != "96" {
				t.Fatalf("envoyé : %v / %v", c, s)
			}
			if j := b.journal(); nbLignes(j, "portal non résolu") != 1 || strings.Contains(j, "portal utilisé") {
				t.Fatalf("journal :\n%s", j)
			}
		})
	}
}

// Région vide et sans valeur claire : le contexte garde null, le périmètre sa chaîne vide.
func TestRepliVideSansPortalReste(t *testing.T) {
	b := nouveauBancRegions(t, "_forever_")
	b.lit(fichierRegion(1, ""))
	if err := b.a.Flush(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if c, s := b.regions(); c != "" || s != nil {
		t.Fatalf("envoyé : %#v / %#v", c, s)
	}
}

func TestRepliDossierAutreQueForever(t *testing.T) {
	for i, d := range []string{"_beta_", "_ptr_", "_xptr_", "_classic_beta_", "_retail_"} {
		t.Run(d, func(t *testing.T) {
			b := nouveauBancRegions(t, d)
			b.config(`SET portal "EU"`)
			b.lit(fichierRegion(i+1, "90"))
			if err := b.a.Flush(context.Background(), false); err != nil {
				t.Fatal(err)
			}
			if c, s := b.regions(); c != "90" || s != "90" {
				t.Fatalf("envoyé : %v / %v", c, s)
			}
			if strings.Contains(b.journal(), "portal utilisé") {
				t.Fatal("repli appliqué hors de _forever_")
			}
		})
	}
}

// Un fichier passé hors d'un dossier de jeu (--sync <fichier>) ne déclenche rien.
func TestRepliFichierHorsDossier(t *testing.T) {
	b := nouveauBancRegions(t, "_forever_")
	b.config(`SET portal "EU"`)
	b.chemin = filepath.Join(b.dir, "ForeverPulse.lua")
	b.lit(fichierRegion(1, "90"))
	if err := b.a.Flush(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if c, s := b.regions(); c != "90" || s != "90" {
		t.Fatalf("envoyé : %v / %v", c, s)
	}
}

// bruts : colonnes de la base locale, sans passer par le paquet store.
func (b *bancRegions) bruts() (scopes, meta string) {
	b.t.Helper()
	db, err := sql.Open("sqlite3", "file:"+filepath.Join(b.dir, "companion.db"))
	if err != nil {
		b.t.Fatal(err)
	}
	defer db.Close()
	if err := db.QueryRow(`select scopes from files limit 1`).Scan(&scopes); err != nil {
		b.t.Fatal(err)
	}
	if err := db.QueryRow(`select meta from stats_files limit 1`).Scan(&meta); err != nil {
		b.t.Fatal(err)
	}
	return scopes, meta
}

// Le repli se joue à l'envoi : un lot déjà en base en profite, la base garde l'ancien code.
func TestRepliLotDejaEnBase(t *testing.T) {
	b := nouveauBancRegions(t, "_forever_")
	b.lit(fichierRegion(1, "96")) // pas encore de Config.wtf
	scopes, meta := b.bruts()
	if !strings.Contains(scopes, `"region":"96"`) || !strings.Contains(meta, `"region":"96"`) {
		t.Fatalf("stockage : %s / %s", scopes, meta)
	}
	b.config(`SET portal "CN"`)
	if err := b.a.Flush(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if c, s := b.regions(); c != "CN" || s != "CN" {
		t.Fatalf("envoyé : %v / %v", c, s)
	}
	s2, m2 := b.bruts()
	if s2 != scopes || m2 != meta {
		t.Fatalf("la base a changé : %s / %s", s2, m2)
	}
}

// Une fausse base de compte : rien d'autre que la valeur du portail ne sort.
func TestRepliDonneesDeCompteNeSortentPas(t *testing.T) {
	b := nouveauBancRegions(t, "_forever_")
	b.config("\xef\xbb\xbfSET accountName \"FAKE-ACCOUNT-TESTONLY\"\r\n" +
		"SET accountList \"FAKE-LIST-TESTONLY!\"\r\n" +
		"SET lastCharacterIndex \"FAKE-INDEX-TESTONLY\"\r\n" +
		"SET realmName \"FAKE-REALM-TESTONLY\"\r\n" +
		"SET portal \"EU\"\r\n" +
		"SET lastSelectedCharacter \"FAKE-CHARACTER-TESTONLY\"\r\n")
	b.lit(fichierRegion(1, "96"))
	if err := b.a.Flush(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if c, s := b.regions(); c != "EU" || s != "EU" {
		t.Fatalf("envoyé : %v / %v", c, s)
	}
	// Dans la base : Etat ouvert, journal et fichiers bruts, journal WAL compris.
	_ = b.a.Etat()
	journal := b.journal()
	if nbLignes(journal, "portal utilisé : EU") != 1 {
		t.Fatalf("journal :\n%s", journal)
	}
	b.ferme()
	interdits := []string{"FAKE-", "TESTONLY", "accountName", "accountList", "lastCharacterIndex", "realmName", "lastSelectedCharacter", "Config.wtf"}
	sorties := map[string][]byte{"journal": []byte(journal)}
	for i, c := range b.srv.census {
		sorties[fmt.Sprintf("corps recensement %d", i)] = c
	}
	for i, c := range b.srv.stats {
		sorties[fmt.Sprintf("corps statistiques %d", i)] = c
	}
	fichiers, err := filepath.Glob(filepath.Join(b.dir, "companion.*"))
	if err != nil || len(fichiers) < 2 {
		t.Fatalf("fichiers locaux : %v, %v", fichiers, err)
	}
	for _, f := range fichiers {
		d, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		sorties[filepath.Base(f)] = d
	}
	for nom, contenu := range sorties {
		for _, x := range interdits {
			if strings.Contains(string(contenu), x) {
				t.Errorf("%q trouvé dans %s", x, nom)
			}
		}
	}
	b.ouvre()
}

func TestPlancherRegions(t *testing.T) {
	paris, err := time.LoadLocation("Europe/Paris")
	if err == nil && !PlancherRegions.Equal(time.Date(2026, 11, 3, 12, 0, 0, 0, paris)) {
		t.Fatalf("plancher : %v", PlancherRegions)
	}
	if PlancherRegions.UTC().Format(time.RFC3339) != "2026-11-03T11:00:00Z" {
		t.Fatalf("plancher : %v", PlancherRegions)
	}
	if CleRemiseRegions != "region_requeue_v1" || BorneRemise != 50_000 {
		t.Fatalf("%s %d", CleRemiseRegions, BorneRemise)
	}
}

// Le parcours complet sur deux démarrages : refus du site, remise en file une seule
// fois, envoi avec le repli, comptes au journal une seule fois.
func TestRemiseDeuxDemarrages(t *testing.T) {
	ctx := context.Background()
	b := nouveauBancRegions(t, "_forever_")
	b.srv.refuse = true
	b.lit(fichierRegion(1, "96"))
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if e := b.a.Etat(); e.Rejected != 1 || e.StatsRejected != 1 {
		t.Fatalf("refus : %+v", e)
	}
	// Plancher de test : queued_at suit l'horloge réelle, observed_at celle du fichier.
	plancher := int64(1)

	// Premier démarrage de la rc.8.
	b.redemarre()
	b.config(`SET portal "EU"`)
	b.srv.refuse = false
	b.a.remiseRegions(plancher, BorneRemise)
	if e := b.a.Etat(); e.Pending != 1 || e.StatsPending != 1 || e.Rejected != 0 || e.StatsRejected != 0 {
		t.Fatalf("après remise : %+v", e)
	}
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if c, s := b.regions(); c != "EU" || s != "EU" {
		t.Fatalf("envoyé : %v / %v", c, s)
	}
	if e := b.a.Etat(); e.Sent != 1 || e.StatsSent != 1 {
		t.Fatalf("après envoi : %+v", e)
	}

	// Second démarrage : rien à refaire, rien de plus au journal.
	nb := len(b.srv.census) + len(b.srv.stats)
	b.redemarre()
	b.a.remiseRegions(plancher, BorneRemise)
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(b.srv.census)+len(b.srv.stats) != nb {
		t.Fatal("nouvel envoi au second démarrage")
	}
	j := b.journal()
	if nbLignes(j, "remise en file unique (region_requeue_v1) : 1 lots et 1 fiches") != 1 {
		t.Fatalf("journal :\n%s", j)
	}
	if strings.Contains(j, "borne de") {
		t.Fatalf("dépassement au journal sans dépassement :\n%s", j)
	}
}

// Les lots que le plancher réel écarte restent refusés : la fixture date de septembre 2026.
func TestRemisePlancherReel(t *testing.T) {
	b := nouveauBancRegions(t, "_forever_")
	b.srv.refuse = true
	b.lit(fichierRegion(1, "96"))
	if err := b.a.Flush(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	b.a.RemiseEnFileRegions()
	if e := b.a.Etat(); e.Pending != 0 || e.StatsPending != 0 || e.Rejected != 1 || e.StatsRejected != 1 {
		t.Fatalf("avant le plancher : %+v", e)
	}
	if b.st.Get(CleRemiseRegions) == "" {
		t.Fatal("clé non posée")
	}
}

// Refusé de nouveau après la remise : reste refusé, aucune nouvelle tentative.
func TestRemiseRefusDeNouveau(t *testing.T) {
	ctx := context.Background()
	b := nouveauBancRegions(t, "_forever_")
	b.srv.refuse = true
	b.lit(fichierRegion(1, "96"))
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	b.a.remiseRegions(1, BorneRemise)
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if e := b.a.Etat(); e.Rejected != 1 || e.StatsRejected != 1 || e.Pending != 0 || e.StatsPending != 0 {
		t.Fatalf("second refus : %+v", e)
	}
	nb := len(b.srv.census) + len(b.srv.stats)
	b.a.remiseRegions(1, BorneRemise)
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(b.srv.census)+len(b.srv.stats) != nb {
		t.Fatal("nouvelle tentative après un second refus")
	}
}

func TestRemiseDepassementAuJournal(t *testing.T) {
	ctx := context.Background()
	b := nouveauBancRegions(t, "_forever_")
	b.srv.refuse = true
	b.lit(fichierRegion(1, "96"))
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	b.a.remiseRegions(1, 0)
	if e := b.a.Etat(); e.Rejected != 1 || e.StatsRejected != 1 {
		t.Fatalf("%+v", e)
	}
	if j := b.journal(); !strings.Contains(j, "borne de 0 lignes dépassée, 1 lots et 1 fiches laissés refusés") {
		t.Fatalf("journal :\n%s", j)
	}
}
