package app

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"wowsync/internal/i18n"
	"wowsync/internal/logx"
	"wowsync/internal/secret"
	"wowsync/internal/sender"
	"wowsync/internal/store"
	tl "wowsync/internal/testlua"
)

const jeton = "fpc_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

func TestBodyNeTransmetPasIdentifiantDurableAddon(t *testing.T) {
	addonID := "11111111-1111-4111-8111-111111111111"
	f := &store.File{SHA256: strings.Repeat("a", 64), Origin: "current", AddonVersion: "3.7.9",
		ObserverSessionID: addonID, Scopes: json.RawMessage(`{}`)}
	ids := map[string]bool{}
	for range 2 {
		body, err := Body(f, nil)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte(addonID)) {
			t.Fatal("identifiant durable de l'addon transmis")
		}
		var payload struct {
			File struct {
				ObserverSessionID string `json:"observer_session_id"`
			} `json:"file"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Fatal(err)
		}
		id := payload.File.ObserverSessionID
		if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(id) || ids[id] {
			t.Fatalf("identifiant d'envoi invalide ou réutilisé : %q", id)
		}
		ids[id] = true
	}
}

// fixture : un fichier au schéma 4 avec n lots de zone de `taille` personnages.
func fixture(n, taille int, seed int) []byte {
	var lots tl.L
	for i := 0; i < n; i++ {
		var rows []string
		for j := 0; j < taille; j++ {
			rows = append(rows, fmt.Sprintf("%08X|Nom%d|Perso%d|1|1|2|%d", seed*1_000_000+i*10_000+j, j, j, 1+j%60))
		}
		lots = append(lots, tl.M{"batch_id": fmt.Sprintf("%08d-0000-4000-8000-%012d", seed, i), "method": "channel_roster",
			"kind": "full", "chain_index": 0, "observed_at": 1790180000 + i*600, "requested_at": 1790179995 + i*600,
			"zone_name": "Silverpine Forest", "ui_map_id": 1421, "channel_name": "General - Silverpine Forest",
			"trigger": "refresh", "zone_count": taille, "read": taille, "scope_id": "4619-PVP-Alliance",
			"guid_prefix": "Player-4619-", "fields": "id|surname|name|class|race|sex|level",
			"classes": tl.L{"MAGE"}, "races": tl.L{"Human"}, "rows": strings.Join(rows, "\n"), "rows_count": taille})
	}
	return tl.Fichier(tl.M{"schema": 4, "addon": tl.M{"name": "ForeverPulse", "version": "3.4.0"},
		"observer": tl.M{"id": "ae536a98-ca7c-4d1f-9f95-6ab41e6b0001", "locale": "enUS"},
		"scopes":   tl.M{"4619-PVP-Alliance": tl.M{"realm_id": 4619, "ruleset": "PVP", "faction": "Alliance", "region": "90", "build": 69977}},
		"batches":  lots})
}

type faux struct {
	mu      sync.Mutex
	codes   []int
	retry   string
	appels  int
	corps   [][]byte
	rejete  string
	entetes http.Header
	// brut : réponses d'intermédiaires (protection Vercel, redirection, page
	// HTML), servies avant le contrat normal.
	brut []http.HandlerFunc
}

func (f *faux) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appels++
	f.entetes = r.Header.Clone()
	gz, err := gzip.NewReader(r.Body)
	if err != nil {
		w.WriteHeader(400)
		return
	}
	b, _ := io.ReadAll(gz)
	f.corps = append(f.corps, b)
	if len(f.brut) > 0 {
		h := f.brut[0]
		f.brut = f.brut[1:]
		h(w, r)
		return
	}
	code := 202
	if len(f.codes) > 0 {
		code, f.codes = f.codes[0], f.codes[1:]
	}
	if code == 429 {
		w.Header().Set("Retry-After", f.retry)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	var corps struct {
		Batches []struct {
			BatchID string `json:"batch_id"`
		} `json:"batches"`
	}
	_ = json.Unmarshal(b, &corps)
	// Même contrat que private.census_ingest : chaque lot compté une seule fois,
	// 202 si au moins un lot est nouveau, 200 si tous sont déjà connus.
	switch code {
	case 200, 202:
		rej, n := "[]", len(corps.Batches)
		if f.rejete != "" {
			rej, n = fmt.Sprintf(`[{"batch_id":%q,"reason":"invalid_character"}]`, f.rejete), n-1
		}
		acc, dup := n, 0
		if code == 200 {
			acc, dup = 0, n
		}
		fmt.Fprintf(w, `{"accepted":%d,"duplicates":%d,"rejected":%s}`, acc, dup, rej)
	default:
		erreur := map[int]string{400: "invalid_body", 401: "token_invalid", 403: "source_disabled",
			409: "unsupported_schema", 413: "payload_too_large", 429: "rate_limited"}[code]
		if erreur == "" {
			erreur = "unavailable"
		}
		fmt.Fprintf(w, `{"error":%q}`, erreur)
	}
}

type banc struct {
	t   *testing.T
	a   *App
	st  *store.Store
	srv *faux
	now time.Time
	dir string
}

func nouveauBanc(t *testing.T) *banc {
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "wowsync.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f := &faux{}
	hs := httptest.NewServer(f)
	t.Cleanup(hs.Close)
	log, _ := logx.Open(filepath.Join(dir, "wowsync.log"))
	t.Cleanup(log.Close)
	a, err := New(st, log, sender.New(hs.URL, Version))
	if err != nil {
		t.Fatal(err)
	}
	b := &banc{t: t, a: a, st: st, srv: f, now: time.Date(2026, 9, 23, 20, 0, 0, 0, time.UTC), dir: dir}
	a.Now = func() time.Time { return b.now }
	a.Token = func() (string, error) { return jeton, nil }
	return b
}

func (b *banc) fichier(nom string, data []byte) string {
	p := filepath.Join(b.dir, nom)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		b.t.Fatal(err)
	}
	return p
}

func TestEnvoiNominalEtIdempotence(t *testing.T) {
	b := nouveauBanc(t)
	ctx := context.Background()
	data := fixture(3, 50, 1)
	p := b.fichier("ForeverPulse.lua", data)
	if n, err := b.a.ProcessFile(ctx, p); err != nil || n != 3 {
		t.Fatalf("mise en file : %d, %v", n, err)
	}
	if e := b.a.Etat(); e.Couleur != Orange || e.Pending != 3 {
		t.Fatalf("attendu orange, 3 en attente : %+v", e)
	}
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	h := b.srv.entetes
	if h.Get("Authorization") != "Bearer "+jeton || h.Get("X-Source") != "forever-pulse-census" || h.Get("X-Schema") != "4" ||
		h.Get("Content-Encoding") != "gzip" || h.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("en-têtes : %v", h)
	}
	var corps map[string]any
	if err := json.Unmarshal(b.srv.corps[0], &corps); err != nil || corps["schema"] != float64(4) {
		t.Fatalf("corps : %v", err)
	}
	if bytes.Contains(b.srv.corps[0], []byte("occurred_at")) {
		t.Fatal("occurred_at dans le corps")
	}
	if e := b.a.Etat(); e.Couleur != Vert || e.Sent != 3 || e.DernierEnvoi == "" {
		t.Fatalf("attendu vert : %+v", e)
	}
	// Même fichier relu : ignoré. Même contenu en .bak : ignoré (même empreinte).
	if n, _ := b.a.ProcessFile(ctx, p); n != 0 {
		t.Fatal("fichier relu remis en file")
	}
	if n, _ := b.a.ProcessFile(ctx, b.fichier("ForeverPulse.lua.bak", data)); n != 0 {
		t.Fatal(".bak identique remis en file")
	}
	// Autre empreinte, mêmes lots (fichier réécrit) : les batch_id déjà en file sont ignorés.
	if n, _ := b.a.ProcessFile(ctx, b.fichier("autre.lua", append(data, '\n'))); n != 0 {
		t.Fatal("batch_id déjà connus remis en file")
	}
	if b.srv.appels != 1 {
		t.Fatalf("%d appels", b.srv.appels)
	}
}

func TestReponses200409429500(t *testing.T) {
	b := nouveauBanc(t)
	ctx := context.Background()
	b.a.ProcessFile(ctx, b.fichier("a.lua", fixture(1, 10, 1)))

	// 200 : déjà connus → envoyés, sans nouvel essai.
	b.srv.codes = []int{200}
	if err := b.a.Flush(ctx, false); err != nil || b.a.Etat().Sent != 1 {
		t.Fatalf("200 : %v", err)
	}

	// 409 : arrêt, rouge, aucune boucle.
	b.a.ProcessFile(ctx, b.fichier("b.lua", fixture(1, 10, 2)))
	b.srv.codes = []int{409}
	if err := b.a.Flush(ctx, false); err == nil {
		t.Fatal("409 sans erreur")
	}
	appels := b.srv.appels
	for i := 0; i < 5; i++ {
		b.now = b.now.Add(time.Hour)
		_ = b.a.Flush(ctx, false)
	}
	if b.srv.appels != appels {
		t.Fatal("409 : le compagnon a réessayé tout seul")
	}
	if e := b.a.Etat(); e.Couleur != Rouge || !strings.Contains(e.Message, "incompatible") {
		t.Fatalf("409 : %+v", e)
	}

	// « Envoyer maintenant » lève le blocage une fois ; 429 : Retry-After respecté.
	b.srv.codes, b.srv.retry = []int{429}, "120"
	if err := b.a.Flush(ctx, true); err == nil {
		t.Fatal("429 sans erreur")
	}
	appels = b.srv.appels
	b.now = b.now.Add(60 * time.Second)
	_ = b.a.Flush(ctx, false)
	if b.srv.appels != appels {
		t.Fatal("429 : envoi avant Retry-After")
	}
	b.now = b.now.Add(61 * time.Second)

	// 500 : recul exponentiel 30 s, puis 60 s.
	b.srv.codes = []int{500, 500, 202}
	_ = b.a.Flush(ctx, false)
	if b.srv.appels != appels+1 {
		t.Fatal("429 : pas de nouvel essai après Retry-After")
	}
	b.now = b.now.Add(29 * time.Second)
	_ = b.a.Flush(ctx, false)
	if b.srv.appels != appels+1 {
		t.Fatal("500 : essai avant 30 s")
	}
	b.now = b.now.Add(2 * time.Second)
	_ = b.a.Flush(ctx, false) // 2e 500 → 60 s
	b.now = b.now.Add(59 * time.Second)
	_ = b.a.Flush(ctx, false)
	if b.srv.appels != appels+2 {
		t.Fatal("500 : le recul ne double pas")
	}
	b.now = b.now.Add(2 * time.Second)
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if e := b.a.Etat(); e.Couleur != Vert || e.Sent != 2 {
		t.Fatalf("après reprise : %+v", e)
	}
}

// Ni une redirection, ni une page HTML, ni un 200 sans accusé valide ne font
// sortir un lot de la file : il reste en attente, jamais envoyé ni refusé, et le
// jeton n'est pas déclaré refusé sur la page 401 de Vercel Authentication.
func TestLotJamaisMarqueSansAccuse(t *testing.T) {
	b := nouveauBanc(t)
	ctx := context.Background()
	ailleurs := 0
	autre := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ailleurs++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		fmt.Fprint(w, `{"accepted":1,"duplicates":0,"rejected":[]}`)
	}))
	defer autre.Close()
	html := func(code int) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(code)
			fmt.Fprint(w, `<!doctype html><title>Vercel</title>`)
		}
	}
	b.srv.brut = []http.HandlerFunc{
		func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, autre.URL+"/api/ingest/census", 307) },
		func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, autre.URL+"/sso", 302) },
		html(200),
		html(401),
		html(400),
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"accepted":0,"duplicates":0,"rejected":[]}`) // 200 qui ne rend compte d'aucun lot
		},
		func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(202)
			fmt.Fprint(w, `{"accepted":1,"duplicates":0,"rejected":[]}`) // sans Content-Type JSON
		},
	}
	n := len(b.srv.brut)
	if k, err := b.a.ProcessFile(ctx, b.fichier("a.lua", fixture(1, 10, 1))); err != nil || k != 1 {
		t.Fatalf("mise en file : %d, %v", k, err)
	}
	for i := 0; i < n; i++ {
		if err := b.a.Flush(ctx, true); err == nil {
			t.Fatalf("réponse %d prise pour un succès", i)
		}
		if e := b.a.Etat(); e.Pending != 1 || e.Sent != 0 || e.Rejected != 0 || e.Code != "pending" {
			t.Fatalf("réponse %d : %+v", i, e)
		}
	}
	if ailleurs != 0 || b.srv.appels != n {
		t.Fatalf("redirection suivie (%d) ou appels %d", ailleurs, b.srv.appels)
	}
	if b.st.Get("last_send") != "" {
		t.Fatal("dernier envoi noté sans accusé")
	}
	// L'accusé exact de la route : le lot part enfin.
	if err := b.a.Flush(ctx, true); err != nil {
		t.Fatal(err)
	}
	if e := b.a.Etat(); e.Pending != 0 || e.Sent != 1 {
		t.Fatalf("après accusé : %+v", e)
	}
}

func TestRefus401403400EtRejetPartiel(t *testing.T) {
	b := nouveauBanc(t)
	ctx := context.Background()
	b.a.ProcessFile(ctx, b.fichier("a.lua", fixture(2, 10, 1)))

	b.srv.codes = []int{401}
	_ = b.a.Flush(ctx, false)
	if e := b.a.Etat(); e.Couleur != Rouge || e.Code != "token" || !strings.Contains(e.Message, "reconnectez") {
		t.Fatalf("401 : %+v", e)
	}
	b.srv.codes = []int{403}
	_ = b.a.Flush(ctx, true)
	if e := b.a.Etat(); e.Couleur != Rouge || !strings.Contains(e.Message, "désactivée") {
		t.Fatalf("403 : %+v", e)
	}
	appels := b.srv.appels
	_ = b.a.Flush(ctx, false)
	if b.srv.appels != appels {
		t.Fatal("403 : réessai automatique")
	}

	// 202 avec un lot refusé : l'autre est envoyé.
	b.srv.codes, b.srv.rejete = []int{202}, "00000001-0000-4000-8000-000000000001"
	if err := b.a.Flush(ctx, true); err != nil {
		t.Fatal(err)
	}
	if e := b.a.Etat(); e.Sent != 1 || e.Rejected != 1 {
		t.Fatalf("rejet partiel : %+v", e)
	}

	// 400 : corps refusé, rien de rangé côté site, lots marqués refusés, rouge.
	b.srv.rejete = ""
	b.a.ProcessFile(ctx, b.fichier("b.lua", fixture(1, 10, 3)))
	b.srv.codes = []int{400}
	_ = b.a.Flush(ctx, false)
	if e := b.a.Etat(); e.Couleur != Rouge || e.Rejected != 2 || e.Pending != 0 {
		t.Fatalf("400 : %+v", e)
	}
}

func TestPaquetsDe1Mo(t *testing.T) {
	b := nouveauBanc(t)
	ctx := context.Background()
	// 12 lots de 3 000 personnages ≈ 12 × 400 Ko : au plus deux lots par envoi (0.7.1).
	if n, err := b.a.ProcessFile(ctx, b.fichier("gros.lua", fixture(12, 3000, 1))); err != nil || n != 12 {
		t.Fatalf("%d %v", n, err)
	}
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if len(b.srv.corps) < 6 {
		t.Fatalf("%d envois, au moins 6 attendus avec le plafond de 1 Mo", len(b.srv.corps))
	}
	total := 0
	for _, c := range b.srv.corps {
		if len(c) > sender.MaxCensusBody {
			t.Fatalf("corps de %d octets > 1 Mo", len(c))
		}
		var x struct{ Batches []any }
		_ = json.Unmarshal(c, &x)
		total += len(x.Batches)
	}
	if total != 12 || b.a.Etat().Sent != 12 {
		t.Fatalf("%d lots reçus", total)
	}
}

func TestJournalSansNomNiJeton(t *testing.T) {
	b := nouveauBanc(t)
	ctx := context.Background()
	b.a.ProcessFile(ctx, b.fichier("a.lua", fixture(2, 10, 1)))
	b.srv.codes = []int{500}
	_ = b.a.Flush(ctx, false)
	b.a.Log.Close()
	j, _ := os.ReadFile(filepath.Join(b.dir, "wowsync.log"))
	if bytes.Contains(j, []byte("Perso")) || bytes.Contains(j, []byte("Nom1")) || bytes.Contains(j, []byte(jeton)) ||
		bytes.Contains(j, []byte("fpc_")) {
		t.Fatalf("le journal contient un nom ou le jeton :\n%s", j)
	}
	if !bytes.Contains(j, []byte("00000001-0000-4000-8000-000000000000")) && !bytes.Contains(j, []byte("2 lots")) {
		t.Fatalf("journal sans comptes :\n%s", j)
	}
}

func TestFichierReelSiPresent(t *testing.T) {
	p := "../../testdata/reel/ForeverPulse.lua"
	if _, err := os.Stat(p); err != nil {
		t.Skip("fichier réel absent")
	}
	b := nouveauBanc(t)
	n, err := b.a.ProcessFile(context.Background(), p)
	if err != nil || n == 0 {
		t.Fatalf("%d %v", n, err)
	}
	if err := b.a.Flush(context.Background(), false); err != nil || b.a.Etat().Sent != n {
		t.Fatal(err)
	}
}

func TestEffacerDonnees(t *testing.T) {
	b := nouveauBanc(t)
	ctx := context.Background()
	b.a.ProcessFile(ctx, b.fichier("a.lua", fixture(2, 10, 1)))
	if b.a.Etat().Pending != 2 || len(b.a.Etat().Cumul) != 1 {
		t.Fatal("préparation")
	}
	if err := b.a.EffacerDonnees(); err != nil {
		t.Fatal(err)
	}
	e := b.a.Etat()
	if e.Pending != 0 || e.Sent != 0 || len(e.Cumul) != 0 || e.DernierEnvoi != "" {
		t.Fatalf("données restantes : %+v", e)
	}
	// Le même fichier peut être relu et remis en file.
	if n, _ := b.a.ProcessFile(ctx, b.fichier("a.lua", fixture(2, 10, 1))); n != 2 {
		t.Fatal("fichier non relu après effacement")
	}
}

// OnChange relit l'état comme la fenêtre et l'icône : aucune opération ne doit
// l'appeler en tenant le verrou (blocage définitif du moteur et du menu en 0.2.0–0.2.2).
func TestOnChangePeutRelireEtat(t *testing.T) {
	b := nouveauBanc(t)
	ctx := context.Background()
	appels := 0
	b.a.OnChange = func() { _ = b.a.Etat(); appels++ }
	sans := func(nom string, f func()) {
		t.Helper()
		fini := make(chan struct{})
		go func() { f(); close(fini) }()
		select {
		case <-fini:
		case <-time.After(5 * time.Second):
			t.Fatalf("%s : blocage (OnChange appelé sous verrou)", nom)
		}
	}
	sans("ProcessFile", func() { b.a.ProcessFile(ctx, b.fichier("a.lua", fixture(2, 10, 1))) })
	sans("Flush", func() { _ = b.a.Flush(ctx, false) })
	b.st.Set("blocked", BlocJeton)
	sans("Flush bloqué", func() { _ = b.a.Flush(ctx, false) })
	sans("Flush manuel", func() { _ = b.a.Flush(ctx, true) })
	sans("EffacerDonnees", func() { _ = b.a.EffacerDonnees() })
	// « Flush bloqué » n'a rien à faire : depuis 0.6.0, il n'appelle plus OnChange.
	if appels < 4 {
		t.Fatalf("OnChange appelé %d fois", appels)
	}
}

// 0.6.0 : le tic de fond (envoi automatique à chaque sondage) ne relit ni l'état ni
// le jeton quand il n'y a rien à faire ; il le fait dès qu'un envoi a lieu.
func TestTicAuReposSansTravail(t *testing.T) {
	b := nouveauBanc(t)
	ctx := context.Background()
	appels, jetons := 0, 0
	b.a.OnChange = func() { appels++ }
	tok := b.a.Token
	b.a.Token = func() (string, error) { jetons++; return tok() }
	for i := 0; i < 10; i++ {
		if err := b.a.Flush(ctx, false); err != nil {
			t.Fatal(err)
		}
	}
	if appels != 0 || jetons != 0 {
		t.Fatalf("file vide : OnChange %d fois, jeton lu %d fois", appels, jetons)
	}
	b.a.ProcessFile(ctx, b.fichier("a.lua", fixture(2, 10, 1)))
	appels = 0
	if err := b.a.Flush(ctx, false); err != nil || appels != 1 || b.a.Etat().Sent != 2 {
		t.Fatalf("envoi : err %v, OnChange %d fois, %+v", err, appels, b.a.Etat())
	}
	b.st.Set("blocked", BlocSchema)
	appels = 0
	_ = b.a.Flush(ctx, false)
	if appels != 0 {
		t.Fatalf("bloqué : OnChange %d fois", appels)
	}
}

// Sans jeton et sans rien à envoyer, le tic ne renvoie plus d'erreur (le journal
// recevait « aucun jeton enregistré » toutes les 2 s en 0.5.0).
func TestSansJetonFileVideSilencieux(t *testing.T) {
	b := nouveauBanc(t)
	b.a.Token = func() (string, error) { return "", secret.ErrAbsent }
	if err := b.a.Flush(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if e := b.a.Etat(); e.Code != "notoken" || e.Couleur != Rouge {
		t.Fatalf("%+v", e)
	}
}

// Le résumé du cumul est gardé, mais suit un nouveau fichier, l'effacement et la langue.
func TestResumeCumulSuitLesChangements(t *testing.T) {
	defer i18n.Set(i18n.FR)
	b := nouveauBanc(t)
	ctx := context.Background()
	if len(b.a.Etat().Portees) != 0 {
		t.Fatal("cumul non vide au départ")
	}
	b.a.ProcessFile(ctx, b.fichier("a.lua", fixture(2, 10, 1)))
	e := b.a.Etat()
	if len(e.Portees) != 1 || e.Portees[0].Total == 0 || len(e.Cumul) != 1 {
		t.Fatalf("%+v", e)
	}
	i18n.Set(i18n.EN)
	if !strings.Contains(b.a.Etat().Cumul[0], "distinct characters") {
		t.Fatal("langue non suivie")
	}
	_ = b.a.EffacerDonnees()
	if len(b.a.Etat().Portees) != 0 {
		t.Fatal("effacement non suivi")
	}
}

func TestEtatEnAnglais(t *testing.T) {
	defer i18n.Set(i18n.FR)
	i18n.Set(i18n.EN)
	b := nouveauBanc(t)
	b.a.ProcessFile(context.Background(), b.fichier("a.lua", fixture(2, 10, 1)))
	e := b.a.Etat()
	if e.Message != "2 batches waiting to be sent" || !strings.Contains(e.Cumul[0], "distinct characters observed since Sep") {
		t.Fatalf("%q %q", e.Message, e.Cumul)
	}
}

// 0.7.2 : --store-token lève le blocage « jeton » comme la fenêtre ; sans cela
// l'icône restait sur « Jeton absent ou refusé » et rien ne partait tout seul.
// Un autre blocage (source coupée) n'est pas levé par un nouveau jeton.
func TestNouveauJetonLeveSeulementLeBlocageJeton(t *testing.T) {
	b := nouveauBanc(t)
	ctx := context.Background()
	if _, err := b.a.ProcessFile(ctx, b.fichier("ForeverPulse.lua", fixture(2, 10, 7))); err != nil {
		t.Fatal(err)
	}
	b.st.Set("blocked", BlocJeton)
	_ = b.a.Flush(ctx, false)
	if b.srv.appels != 0 || b.a.Etat().Code != "token" {
		t.Fatalf("bloqué : %d appels, état %q", b.srv.appels, b.a.Etat().Code)
	}
	if !LeverBlocageJeton(b.st) {
		t.Fatal("blocage jeton non levé")
	}
	if err := b.a.Flush(ctx, false); err != nil || b.srv.appels != 1 || b.a.Etat().Sent != 2 {
		t.Fatalf("envoi automatique après levée : %v, %d appels, %+v", err, b.srv.appels, b.a.Etat())
	}
	b.st.Set("blocked", BlocSource)
	if LeverBlocageJeton(b.st) || b.st.Get("blocked") != BlocSource {
		t.Fatal("un nouveau jeton a levé le blocage source")
	}
}

// 0.7.4 : le cumul n'est plus gardé en mémoire. Le résumé lu en SQL donne exactement
// les périmètres, totaux et dates que cumul.Resume sur le cumul complet relu de la
// base, à chaque fichier et après un redémarrage (nouvelle App sur la même base).
func TestResumeSQLIdentiqueAuCumulComplet(t *testing.T) {
	b := nouveauBanc(t)
	ctx := context.Background()
	compare := func(etape string, a *App) {
		t.Helper()
		c, err := b.st.LoadCumul()
		if err != nil {
			t.Fatal(err)
		}
		attendu := c.Resume(b.now.Unix())
		sql, err := b.st.ResumeCumul()
		if err != nil || len(sql) != len(attendu) {
			t.Fatalf("%s : %d périmètres en SQL, %d dans le cumul (%v)", etape, len(sql), len(attendu), err)
		}
		for _, p := range sql {
			r, ok := attendu[p.ScopeID]
			if !ok || r.Total != p.Total || (r.Depuis == nil) != (p.Depuis == nil) || (r.Depuis != nil && *r.Depuis != *p.Depuis) {
				t.Fatalf("%s : %s SQL %d / cumul %d", etape, p.ScopeID, p.Total, r.Total)
			}
		}
		if e := a.Etat(); len(e.Portees) != len(sql) || (len(sql) > 0 && e.Portees[0].Total != sql[0].Total) {
			t.Fatalf("%s : état %+v", etape, e.Portees)
		}
	}
	compare("vide", b.a)
	for i, seed := range []int{1, 2, 3} {
		if _, err := b.a.ProcessFile(ctx, b.fichier(fmt.Sprintf("f%d.lua", i), fixture(3, 40, seed))); err != nil {
			t.Fatal(err)
		}
		compare(fmt.Sprintf("fichier %d", i+1), b.a)
	}
	if got := b.a.Etat().Portees[0].Total; got != 3*3*40 {
		t.Fatalf("total %d, attendu %d", got, 3*3*40)
	}
	// Redémarrage : une nouvelle App ne charge rien et lit le même résumé.
	a2, err := New(b.st, b.a.Log, b.a.Sender)
	if err != nil {
		t.Fatal(err)
	}
	a2.Now = b.a.Now
	compare("redémarrage", a2)
	// Fichier déjà intégré : rien ne change.
	if n, _ := a2.ProcessFile(ctx, b.fichier("f9.lua", append(fixture(3, 40, 2), '\n'))); n != 0 {
		t.Fatal("lots déjà connus remis en file")
	}
	compare("lots déjà connus", a2)
}

// Purge (0.10.0) : le contenu d'un lot envoyé reste 3 jours, puis il est effacé ;
// les compteurs de la fenêtre ne bougent pas et un lot en attente n'est jamais touché.
func TestPurgeLotsApresTroisJours(t *testing.T) {
	b := nouveauBanc(t)
	ctx := context.Background()
	if _, err := b.a.ProcessFile(ctx, b.fichier("ForeverPulse.lua", fixture(3, 50, 1))); err != nil {
		t.Fatal(err)
	}
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if _, err := b.a.ProcessFile(ctx, b.fichier("ForeverPulse2.lua", fixture(2, 50, 2))); err != nil {
		t.Fatal(err)
	}
	avant := b.a.Etat()
	if avant.Sent != 3 || avant.Pending != 2 {
		t.Fatalf("départ : %+v", avant)
	}
	// sent_at est l'horloge réelle : « maintenant » d'abord, puis 3 jours et 1 heure plus tard.
	b.now = time.Now()
	if n, err := b.a.PurgeLots(ctx); err != nil || n != 0 {
		t.Fatalf("purge avant 3 jours : %d %v", n, err)
	}
	b.now = time.Now().Add(DelaiPurge + time.Hour)
	if n, err := b.a.PurgeLots(ctx); err != nil || n != 3 {
		t.Fatalf("purge après 3 jours : %d %v", n, err)
	}
	if apres := b.a.Etat(); apres.Sent != avant.Sent || apres.Pending != avant.Pending || apres.Rejected != avant.Rejected {
		t.Fatalf("compteurs changés : %+v → %+v", avant, apres)
	}
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if e := b.a.Etat(); e.Sent != 5 || e.Pending != 0 {
		t.Fatalf("les lots en attente doivent partir intacts : %+v", e)

// Installation neuve : le premier envoi sans jeton pose le blocage « jeton », mais
// la fenêtre dit « non connecté » et non « refusé ou révoqué ».
func TestInstallationNeuveSansJetonNEstPasUnRefus(t *testing.T) {
	b := nouveauBanc(t)
	ctx := context.Background()
	b.a.Token = func() (string, error) { return "", secret.ErrAbsent }
	if _, err := b.a.ProcessFile(ctx, b.fichier("ForeverPulse.lua", fixture(2, 10, 7))); err != nil {
		t.Fatal(err)
	}
	_ = b.a.Flush(ctx, false)
	if b.st.Get("blocked") != BlocJeton {
		t.Fatal("le premier envoi sans jeton doit poser le blocage jeton")
	}
	if e := b.a.Etat(); e.Code != "notoken" || e.Couleur != Rouge {
		t.Fatalf("installation neuve : %+v", e)
	}
	b.a.Token = func() (string, error) { return "", errors.New("coffre illisible") }
	if e := b.a.Etat(); e.Code != "token" {
		t.Fatalf("jeton illisible : %+v", e)
	}
}
