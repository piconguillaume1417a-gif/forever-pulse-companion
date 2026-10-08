package app

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"wowsync/internal/logx"
	"wowsync/internal/sender"
	"wowsync/internal/store"
	tl "wowsync/internal/testlua"
)

// site : un faux site qui applique le contrat des deux routes d'envoi.
//
//	/api/ingest/census : accusé { accepted, duplicates, rejected } par lot ;
//	/api/ingest/stats  : accusé par fiche ; une fiche déjà reçue à l'identique est
//	                     un doublon, une fiche plus ancienne que celle connue aussi.
//
// 0.9.0 (contrat « spécialisations » §4.1-4.2) : la route des statistiques
// accepte les schémas de s.schemas (1 et 2 par défaut) et répond sinon 409
// { error: unsupported_schema, supported }. Le corps doit porter le numéro de
// l'en-tête ; schéma 1 strict (talent_trees ou file.talents_version → 400, fiche
// avec talents → invalid_sheet) ; schéma 2 : talent_trees et file.talents_version
// obligatoires.
type site struct {
	mu          sync.Mutex
	lots        map[string]bool
	fiches      map[string]string // id → fiche reçue (JSON)
	fraicheur   map[string]string // id → read_at le plus récent reçu
	statsCodes  []int             // codes forcés pour la route des statistiques
	statsBrut   []http.HandlerFunc
	refuse400   string // toute requête qui contient cette fiche est refusée en 400
	appelsLots  int
	appelsStats int
	corpsStats  [][]byte
	entetes     http.Header

	schemas      []int             // schémas de statistiques acceptés ; nil : 1 et 2
	schemasVus   []string          // X-Schema de chaque appel de statistiques
	talents      map[string]string // id → talents reçus (JSON) au schéma 2
	recusTalents map[string]int    // id → nombre de réceptions acceptées portant des talents
	arbres       json.RawMessage   // dernier talent_trees reçu
}

func nouveauSite() *site {
	return &site{lots: map[string]bool{}, fiches: map[string]string{}, fraicheur: map[string]string{},
		talents: map[string]string{}, recusTalents: map[string]int{}}
}

// schemaStats : contrôle de version du faux site. Rend false après avoir répondu.
func (s *site) schemaStats(w http.ResponseWriter, r *http.Request, b []byte) (int, bool) {
	schemas := s.schemas
	if schemas == nil {
		schemas = []int{1, 2}
	}
	h := r.Header.Get("X-Schema")
	n := 0
	for _, x := range schemas {
		if fmt.Sprint(x) == h {
			n = x
		}
	}
	if n == 0 {
		w.WriteHeader(409)
		sup, _ := json.Marshal(schemas)
		fmt.Fprintf(w, `{"error":"unsupported_schema","supported":%s}`, sup)
		return 0, false
	}
	var c map[string]json.RawMessage
	var file map[string]json.RawMessage
	_ = json.Unmarshal(b, &c)
	_ = json.Unmarshal(c["file"], &file)
	_, arbres := c["talent_trees"]
	_, version := file["talents_version"]
	if string(c["schema"]) != h || (n == 1 && (arbres || version)) || (n == 2 && (!arbres || !version)) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":"invalid_body"}`)
		return 0, false
	}
	if n == 2 {
		s.arbres = c["talent_trees"]
	}
	return n, true
}

func (s *site) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	gz, err := gzip.NewReader(r.Body)
	if err != nil {
		w.WriteHeader(400)
		return
	}
	b, _ := io.ReadAll(gz)
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case sender.Route:
		s.appelsLots++
		var c struct {
			Batches []struct {
				BatchID string `json:"batch_id"`
			} `json:"batches"`
		}
		_ = json.Unmarshal(b, &c)
		acc, dup := 0, 0
		for _, l := range c.Batches {
			if s.lots[l.BatchID] {
				dup++
			} else {
				s.lots[l.BatchID] = true
				acc++
			}
		}
		code := 200
		if acc > 0 {
			code = 202
		}
		w.WriteHeader(code)
		fmt.Fprintf(w, `{"accepted":%d,"duplicates":%d,"rejected":[]}`, acc, dup)
	case sender.RouteStats:
		s.appelsStats++
		s.entetes = r.Header.Clone()
		s.corpsStats = append(s.corpsStats, b)
		if len(s.statsBrut) > 0 {
			h := s.statsBrut[0]
			s.statsBrut = s.statsBrut[1:]
			h(w, r)
			return
		}
		if len(s.statsCodes) > 0 {
			code := s.statsCodes[0]
			s.statsCodes = s.statsCodes[1:]
			w.WriteHeader(code)
			erreur := map[int]string{400: "invalid_body", 401: "token_invalid", 403: "source_disabled",
				409: "unsupported_schema", 413: "payload_too_large", 429: "rate_limited"}[code]
			if erreur == "" {
				erreur = "unavailable"
			}
			fmt.Fprintf(w, `{"error":%q}`, erreur)
			return
		}
		s.schemasVus = append(s.schemasVus, r.Header.Get("X-Schema"))
		n, ok := s.schemaStats(w, r, b)
		if !ok {
			return
		}
		var c struct {
			Characters []json.RawMessage `json:"characters"`
		}
		_ = json.Unmarshal(b, &c)
		if s.refuse400 != "" && strings.Contains(string(b), s.refuse400) {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":"invalid_body"}`)
			return
		}
		acc, dup := 0, 0
		var rejets []string
		for _, raw := range c.Characters {
			var f struct {
				ID      string          `json:"id"`
				SeenAt  string          `json:"seen_at"`
				Talents json.RawMessage `json:"talents"`
			}
			_ = json.Unmarshal(raw, &f)
			switch {
			case n == 1 && f.Talents != nil:
				rejets = append(rejets, fmt.Sprintf(`{"id":%q,"reason":"invalid_sheet"}`, f.ID))
			case s.fiches[f.ID] == string(raw), f.SeenAt < s.fraicheur[f.ID]:
				dup++
			default:
				s.fiches[f.ID], s.fraicheur[f.ID] = string(raw), f.SeenAt
				if f.Talents != nil {
					s.talents[f.ID] = string(f.Talents)
					s.recusTalents[f.ID]++
				}
				acc++
			}
		}
		code := 200
		if acc > 0 {
			code = 202
		}
		w.WriteHeader(code)
		fmt.Fprintf(w, `{"accepted":%d,"duplicates":%d,"rejected":[%s]}`, acc, dup, strings.Join(rejets, ","))
	default:
		w.WriteHeader(404)
	}
}

type bancStats struct {
	t   *testing.T
	a   *App
	st  *store.Store
	srv *site
	hs  *httptest.Server
	now time.Time
	dir string
}

func (b *bancStats) ouvre() {
	st, err := store.Open(filepath.Join(b.dir, "companion.db"))
	if err != nil {
		b.t.Fatal(err)
	}
	log, _ := logx.Open(filepath.Join(b.dir, "companion.log"))
	a, err := New(st, log, sender.New(b.hs.URL, Version))
	if err != nil {
		b.t.Fatal(err)
	}
	a.Now = func() time.Time { return b.now }
	a.Token = func() (string, error) { return jeton, nil }
	b.a, b.st = a, st
	b.t.Cleanup(func() { st.Close(); log.Close() })
}

func nouveauBancStats(t *testing.T) *bancStats {
	b := &bancStats{t: t, srv: nouveauSite(), now: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC), dir: t.TempDir()}
	b.hs = httptest.NewServer(b.srv)
	t.Cleanup(b.hs.Close)
	b.ouvre()
	return b
}

func (b *bancStats) fichier(nom string, data []byte) string {
	p := filepath.Join(b.dir, nom)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		b.t.Fatal(err)
	}
	return p
}

// fichierStats : un fichier complet (un lot de recensement et des fiches de statistiques).
// fiches : GUID court → groupes de valeurs. vu : date « d » commune.
func fichierStats(seed int, vu int, fiches map[string][]string) []byte {
	return fichierTalents(seed, vu, fiches, nil, nil)
}

// fichierTalents : comme fichierStats, avec l'ajout de l'addon 4.2.0. ta : GUID
// court → relevé de talents ; arbres non nil : talents_v = 1 et catalogue des arbres.
func fichierTalents(seed int, vu int, fiches map[string][]string, ta map[string]string, arbres tl.I) []byte {
	persos := tl.M{}
	guids := make([]string, 0, len(fiches))
	for g := range fiches {
		guids = append(guids, g)
	}
	sort.Strings(guids)
	for _, g := range guids {
		groupes := tl.L{}
		for _, v := range fiches[g] {
			groupes = append(groupes, v)
		}
		persos["Player-4619-"+g] = tl.M{"n": "Nom " + g, "c": "MAGE", "ra": "Human", "f": "A", "s": 2, "l": 20, "x": 1,
			"d": vu, "o": vu, "v": groupes}
		if t, ok := ta[g]; ok {
			persos["Player-4619-"+g].(tl.M)["ta"] = t
		}
	}
	statsDB := tl.M{"schema": "forever-pulse-stats", "version": 4, "locale": "enUS", "ctx": tl.L{"beta-4619-PVP"},
		"contextes":   tl.M{"beta-4619-PVP": tl.M{"environment": "beta", "realm_id": 4619, "ruleset": "PVP", "region": "90"}},
		"personnages": persos}
	if arbres != nil {
		statsDB["talents_v"] = 1
		statsDB["arbres"] = arbres
	}
	census := string(fixture(1, 3, seed))
	return []byte(census + "ForeverPulseStatsDB = " + tl.Serialise(statsDB) + "\n")
}

func (b *bancStats) etat(pending, sent, rejected int) {
	b.t.Helper()
	e := b.a.Etat()
	if e.StatsPending != pending || e.StatsSent != sent || e.StatsRejected != rejected {
		b.t.Fatalf("statistiques : attendu %d en attente, %d envoyées, %d refusées ; obtenu %d, %d, %d",
			pending, sent, rejected, e.StatsPending, e.StatsSent, e.StatsRejected)
	}
}

func TestStatsEnvoiNominalEtIdempotence(t *testing.T) {
	b := nouveauBancStats(t)
	ctx := context.Background()
	data := fichierStats(1, 1790505847, map[string][]string{
		"00000001": {"1790505847|20|n|60:0,94:5,326:18815m"},
		"00000002": {"1790505847|20|t|60:7"},
		"00000003": {}, // aucune valeur : rien à envoyer
	})
	p := b.fichier("ForeverPulse.lua", data)
	if n, err := b.a.ProcessFile(ctx, p); err != nil || n != 1 {
		t.Fatalf("mise en file : %d, %v", n, err)
	}
	b.etat(2, 0, 0)
	if e := b.a.Etat(); e.Couleur != Orange || len(e.Details) < 6 || !strings.Contains(strings.Join(e.Details, "\n"), p) {
		t.Fatalf("état : %+v", e)
	}
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	b.etat(0, 2, 0)
	if e := b.a.Etat(); e.Couleur != Vert || e.Sent != 1 {
		t.Fatalf("attendu vert : %+v", e)
	}
	h := b.srv.entetes
	if h.Get("Authorization") != "Bearer "+jeton || h.Get("X-Source") != "forever-pulse-stats" || h.Get("X-Schema") != "2" ||
		h.Get("Content-Encoding") != "gzip" || h.Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("en-têtes : %v", h)
	}
	var corps struct {
		Schema    int `json:"schema"`
		Companion struct {
			Version string `json:"version"`
		} `json:"companion"`
		File struct {
			SHA256       string `json:"sha256"`
			Origin       string `json:"origin"`
			StatsVersion int    `json:"stats_version"`
		} `json:"file"`
		Contexts   map[string]map[string]any `json:"contexts"`
		Catalog    any                       `json:"catalog"`
		Characters []struct {
			ID       string `json:"id"`
			Readings []struct {
				Values map[string]map[string]string `json:"values"`
			} `json:"readings"`
		} `json:"characters"`
	}
	if err := json.Unmarshal(b.srv.corpsStats[0], &corps); err != nil {
		t.Fatal(err)
	}
	if corps.Schema != 2 || corps.Companion.Version != Version || len(corps.File.SHA256) != 64 || corps.File.Origin != "current" ||
		corps.File.StatsVersion != 4 || corps.Contexts["beta-4619-PVP"]["realm_id"] != float64(4619) || corps.Catalog != nil ||
		len(corps.Characters) != 2 {
		t.Fatalf("corps : %s", b.srv.corpsStats[0])
	}
	// Zéro écrit transmis ; statistique absente non transmise.
	v := corps.Characters[0].Readings[0].Values
	if v["count"]["60"] != "0" || v["copper"]["326"] != "18815" {
		t.Fatalf("valeurs : %v", v)
	}
	if _, ok := corps.Characters[1].Readings[0].Values["count"]["94"]; ok {
		t.Fatal("statistique absente transmise")
	}
	if strings.Contains(string(b.srv.corpsStats[0]), "ForeverPulseScanDB") || strings.Contains(string(b.srv.corpsStats[0]), "secret interne") {
		t.Fatal("l'état interne de l'addon a été transmis")
	}

	// Même fichier relu, puis même contenu sous une autre empreinte : rien ne repart.
	if n, _ := b.a.ProcessFile(ctx, p); n != 0 {
		t.Fatal("fichier relu remis en file")
	}
	b.a.ProcessFile(ctx, b.fichier("autre.lua", append(data, '\n')))
	b.etat(0, 2, 0)
	_ = b.a.Flush(ctx, false)
	if b.srv.appelsStats != 1 || b.srv.appelsLots != 1 {
		t.Fatalf("appels : %d statistiques, %d lots", b.srv.appelsStats, b.srv.appelsLots)
	}
}

func TestStatsObservationPlusRecentePuisPlusAncienne(t *testing.T) {
	b := nouveauBancStats(t)
	ctx := context.Background()
	b.a.ProcessFile(ctx, b.fichier("a.lua", fichierStats(1, 1790505847, map[string][]string{
		"00000001": {"1790505847|20|n|60:1"}, "00000002": {"1790505847|20|n|60:7"}})))
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	b.etat(0, 2, 0)

	// Session suivante : une seule fiche a un relevé plus récent. Elle seule repart.
	recent := fichierStats(2, 1790600000, map[string][]string{
		"00000001": {"1790600000|21|n|60:2"}, "00000002": {"1790505847|20|n|60:7"}})
	// (la fiche 2 garde son relevé mais sa date « vue » avance : elle repart aussi, c'est attendu)
	b.a.ProcessFile(ctx, b.fichier("b.lua", recent))
	b.etat(2, 0, 0)
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	b.etat(0, 2, 0)
	if !strings.Contains(b.srv.fiches["beta-4619-PVP/Player-4619-00000001"], `"60":"2"`) {
		t.Fatalf("la valeur récente n'est pas arrivée : %s", b.srv.fiches["beta-4619-PVP/Player-4619-00000001"])
	}

	// Lecture tardive d'un .bak plus ancien : il ne remplace rien et rien ne repart.
	ancien := fichierStats(3, 1790505847, map[string][]string{"00000001": {"1790505847|20|n|60:1"}})
	b.a.ProcessFile(ctx, b.fichier("ForeverPulse.lua.bak", ancien))
	b.etat(0, 2, 0)
	appels := b.srv.appelsStats
	_ = b.a.Flush(ctx, false)
	if b.srv.appelsStats != appels {
		t.Fatal("une fiche plus ancienne a été renvoyée")
	}
	if !strings.Contains(b.srv.fiches["beta-4619-PVP/Player-4619-00000001"], `"60":"2"`) {
		t.Fatal("la valeur récente a été remplacée par l'ancienne")
	}
}

func TestStatsPanneReseauPuisReprise(t *testing.T) {
	b := nouveauBancStats(t)
	ctx := context.Background()
	b.a.ProcessFile(ctx, b.fichier("a.lua", fichierStats(1, 1790505847, map[string][]string{"00000001": {"1790505847|20|n|60:1"}})))
	b.srv.statsCodes = []int{503}
	if err := b.a.Flush(ctx, false); err == nil {
		t.Fatal("503 sans erreur")
	}
	// Les lots sont partis ; la fiche attend, avec un recul propre aux statistiques.
	if e := b.a.Etat(); e.Sent != 1 || e.Pending != 0 || e.StatsPending != 1 || e.Couleur != Orange ||
		!strings.Contains(strings.Join(e.Details, "\n"), "503") {
		t.Fatalf("état après la panne : %+v", e)
	}
	appels := b.srv.appelsStats
	_ = b.a.Flush(ctx, false) // encore en recul : aucun appel
	if b.srv.appelsStats != appels {
		t.Fatal("nouvel essai avant la fin du recul")
	}
	b.now = b.now.Add(31 * time.Second)
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	b.etat(0, 1, 0)
	if e := b.a.Etat(); e.Couleur != Vert {
		t.Fatalf("attendu vert après la reprise : %+v", e)
	}
}

func TestStatsRedemarrageAvecDonneesEnAttente(t *testing.T) {
	b := nouveauBancStats(t)
	ctx := context.Background()
	b.a.ProcessFile(ctx, b.fichier("a.lua", fichierStats(1, 1790505847, map[string][]string{
		"00000001": {"1790505847|20|n|60:1"}, "00000002": {"1790505847|20|n|60:2"}})))
	b.srv.statsBrut = []http.HandlerFunc{func(w http.ResponseWriter, r *http.Request) {
		// Réponse d'un intermédiaire : jamais un accusé de réception.
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(200)
		fmt.Fprint(w, "<html>ok</html>")
	}}
	if err := b.a.Flush(ctx, false); err == nil {
		t.Fatal("page HTML prise pour un accusé")
	}
	b.etat(2, 0, 0)
	// Arrêt du compagnon, puis redémarrage sur la même base.
	b.st.Close()
	b.ouvre()
	b.etat(2, 0, 0)
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	b.etat(0, 2, 0)
	if len(b.srv.fiches) != 2 {
		t.Fatalf("fiches reçues : %d", len(b.srv.fiches))
	}
}

func TestStatsFichierIncompletPuisComplet(t *testing.T) {
	b := nouveauBancStats(t)
	ctx := context.Background()
	data := fichierStats(1, 1790505847, map[string][]string{"00000001": {"1790505847|20|n|60:1"}})
	// Écriture en cours : le fichier est coupé au milieu de la table des statistiques.
	p := b.fichier("ForeverPulse.lua", data[:len(data)-40])
	if n, err := b.a.ProcessFile(ctx, p); err == nil || n != 0 {
		t.Fatal("fichier incomplet accepté")
	}
	if e := b.a.Etat(); e.Pending != 0 || e.StatsPending != 0 {
		t.Fatalf("un fichier incomplet a laissé des données en file : %+v", e)
	}
	// Le fichier complet arrive : il est traité normalement.
	p = b.fichier("ForeverPulse.lua", data)
	if n, err := b.a.ProcessFile(ctx, p); err != nil || n != 1 {
		t.Fatalf("fichier complet : %d, %v", n, err)
	}
	b.etat(1, 0, 0)
}

func TestStatsUneFicheRefuseeNeBloquePasLesAutres(t *testing.T) {
	b := nouveauBancStats(t)
	ctx := context.Background()
	fiches := map[string][]string{}
	for i := 1; i <= 9; i++ {
		fiches[fmt.Sprintf("%08d", i)] = []string{fmt.Sprintf("1790505847|20|n|60:%d", i)}
	}
	b.a.ProcessFile(ctx, b.fichier("a.lua", fichierStats(1, 1790505847, fiches)))
	b.srv.refuse400 = "Player-4619-00000005"
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	b.etat(0, 8, 1)
	// Les lots ne sont jamais bloqués par un refus de statistiques.
	if e := b.a.Etat(); e.Couleur != Vert || e.Sent != 1 {
		t.Fatalf("état : %+v", e)
	}
}

func TestStatsSourceCoupeeOuRouteAbsente(t *testing.T) {
	b := nouveauBancStats(t)
	ctx := context.Background()
	b.a.ProcessFile(ctx, b.fichier("a.lua", fichierStats(1, 1790505847, map[string][]string{"00000001": {"1790505847|20|n|60:1"}})))
	b.srv.statsCodes = []int{403}
	if err := b.a.Flush(ctx, false); err == nil {
		t.Fatal("403 sans erreur")
	}
	// Les lots sont envoyés, l'icône n'est pas rouge, et aucun essai automatique ne suit.
	if e := b.a.Etat(); e.Sent != 1 || e.Couleur == Rouge || e.StatsPending != 1 {
		t.Fatalf("état : %+v", e)
	}
	appels := b.srv.appelsStats
	for i := 0; i < 3; i++ {
		b.now = b.now.Add(time.Hour)
		_ = b.a.Flush(ctx, false)
	}
	if b.srv.appelsStats != appels {
		t.Fatal("403 : nouvel essai automatique")
	}
	// « Envoyer maintenant » lève le blocage, une fois.
	if err := b.a.Flush(ctx, true); err != nil {
		t.Fatal(err)
	}
	b.etat(0, 1, 0)

	// Site ancien, sans la route des statistiques : la fiche attend, rien n'est perdu.
	b.a.ProcessFile(ctx, b.fichier("b.lua", fichierStats(2, 1790600000, map[string][]string{"00000001": {"1790600000|21|n|60:2"}})))
	b.srv.statsBrut = []http.HandlerFunc{func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(404)
	}}
	if err := b.a.Flush(ctx, false); err == nil {
		t.Fatal("404 sans erreur")
	}
	if e := b.a.Etat(); e.Sent != 2 || e.StatsPending != 1 {
		t.Fatalf("état : %+v", e)
	}
}

// Passage de la 0.6.4 à la 0.7.0 : un fichier déjà traité pour ses lots voit ses
// statistiques mises en file, sans que ses lots repartent.
func TestStatsFichierDejaTraitePourSesLots(t *testing.T) {
	b := nouveauBancStats(t)
	ctx := context.Background()
	data := fichierStats(1, 1790505847, map[string][]string{"00000001": {"1790505847|20|n|60:1"}})
	p := b.fichier("ForeverPulse.lua", data)
	f, _, _, err := DecodeTout(data, b.now)
	if err != nil {
		t.Fatal(err)
	}
	scopes, _ := json.Marshal(map[string]any{})
	sum := sha256Hex(data)
	if _, err := b.st.AddFile(ctx, store.File{SHA256: sum, Path: p, Origin: "current", AddonVersion: f.AddonVersion,
		ObserverSessionID: f.ObserverID, Scopes: scopes, Total: 1}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if n, err := b.a.ProcessFile(ctx, p); err != nil || n != 0 {
		t.Fatalf("lots remis en file : %d, %v", n, err)
	}
	b.etat(1, 0, 0)
	if e := b.a.Etat(); e.Pending != 0 {
		t.Fatalf("lots en file : %+v", e)
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
