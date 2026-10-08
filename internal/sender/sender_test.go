package sender

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRetryAfterEtRecul(t *testing.T) {
	now := time.Date(2026, 9, 23, 20, 0, 0, 0, time.UTC)
	if retryAfter("120", now) != 120*time.Second || retryAfter("", now) != 60*time.Second ||
		retryAfter("99999", now) != time.Hour || retryAfter("Wed, 23 Sep 2026 20:05:00 GMT", now) != 5*time.Minute {
		t.Fatal("Retry-After")
	}
	want := []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 16 * time.Minute, 30 * time.Minute, 30 * time.Minute}
	for i, w := range want {
		if Backoff(i+1) != w {
			t.Fatalf("recul %d : %s", i+1, Backoff(i+1))
		}
	}
}

// Corps de test : deux lots, comme ceux que construit app.Body.
const lotA, lotB = "00000001-0000-4000-8000-00000000000a", "00000001-0000-4000-8000-00000000000b"

var deuxLots = []byte(`{"schema":4,"batches":[{"batch_id":"` + lotA + `"},{"batch_id":"` + lotB + `"}]}`)

// Un 200 ou 202 ne marque des lots comme envoyés que s'il est l'accusé exact de la
// route : JSON, 202 ⇔ accepted > 0, et accepted + duplicates + rejected = lots
// envoyés. Page HTML, hébergeur de domaine, JSON étranger ou incohérent : panne,
// réessayée plus tard, rien n'est perdu ni marqué.
func TestAccuseDeReceptionExige(t *testing.T) {
	const js = "application/json"
	cas := []struct {
		code  int
		ct    string
		corps string
		want  Verdict
	}{
		{202, js, `{"accepted":2,"duplicates":0,"rejected":[]}`, Accepte},
		{202, "application/json; charset=utf-8", `{"accepted":1,"duplicates":1,"rejected":[]}`, Accepte},
		{202, js, `{"accepted":1,"duplicates":0,"rejected":[{"batch_id":"` + lotB + `","reason":"retention_expired"}]}`, Accepte},
		{200, js, `{"accepted":0,"duplicates":2,"rejected":[]}`, Accepte},
		{200, js, `{"accepted":0,"duplicates":0,"rejected":[{"batch_id":"` + lotA + `","reason":"x"},{"batch_id":"` + lotB + `","reason":"x"}]}`, Accepte},
		// Pas la route d'envoi.
		{200, "text/html; charset=utf-8", `<!doctype html><html><body>forever-pulse.com</body></html>`, Panne},
		{200, js, ``, Panne},
		{200, js, `{"ok":true}`, Panne},
		{200, "text/plain", `{"accepted":0,"duplicates":2,"rejected":[]}`, Panne},
		{200, "", `{"accepted":0,"duplicates":2,"rejected":[]}`, Panne},
		// Accusés incohérents avec l'envoi.
		{200, js, `{"accepted":0}`, Panne},
		{200, js, `{"accepted":0,"duplicates":0,"rejected":[]}`, Panne},
		{202, js, `{"accepted":3,"duplicates":0,"rejected":[]}`, Panne},
		{202, js, `{"accepted":0,"duplicates":2,"rejected":[]}`, Panne},
		{200, js, `{"accepted":2,"duplicates":0,"rejected":[]}`, Panne},
		{202, js, `{"accepted":1,"duplicates":0,"rejected":[{"batch_id":"autre","reason":"x"}]}`, Panne},
		{200, js, `{"accepted":0,"duplicates":0,"rejected":[{"batch_id":"` + lotA + `","reason":"x"},{"batch_id":"` + lotA + `","reason":"x"}]}`, Panne},
		{202, js, `{"accepted":2,"duplicates":0,"rejected":null}`, Panne},
		{202, js, `{"accepted":2,"duplicates":0}`, Panne},
		{202, js, `{"accepted":"2","duplicates":0,"rejected":[]}`, Panne},
		{202, js, `{"accepted":1.5,"duplicates":0.5,"rejected":[]}`, Panne},
		{202, js, `{"accepted":3,"duplicates":-1,"rejected":[]}`, Panne},
		{202, js, `{"accepted":2,"duplicates":0,"rejected":[]} <html>`, Panne},
		{201, js, `{"accepted":2,"duplicates":0,"rejected":[]}`, Panne},
		{404, "text/html", `not found`, Panne},
	}
	for _, k := range cas {
		hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != Route {
				t.Errorf("chemin %s", r.URL.Path)
			}
			if k.ct != "" {
				w.Header().Set("Content-Type", k.ct)
			} else {
				w.Header()["Content-Type"] = nil
			}
			w.WriteHeader(k.code)
			fmt.Fprint(w, k.corps)
		}))
		r := New(hs.URL+"/", "test").Post(context.Background(), "fpc_x", deuxLots)
		hs.Close()
		if r.Verdict != k.want {
			t.Fatalf("%d %q %q : verdict %d, attendu %d (%s)", k.code, k.ct, k.corps, r.Verdict, k.want, r.Detail)
		}
		if r.Verdict != Accepte && (r.Reponse.Accepted != 0 || r.Reponse.Duplicates != 0 || len(r.Reponse.Rejected) != 0) {
			t.Fatalf("%q : accusé gardé malgré le refus : %+v", k.corps, r.Reponse)
		}
	}
}

// La destination : l'origine du site, sans chemin, suivie de /api/ingest/census.
func TestCible(t *testing.T) {
	bons := map[string]string{
		"https://forever-pulse.com":                "https://forever-pulse.com/api/ingest/census",
		"https://forever-pulse.com/":               "https://forever-pulse.com/api/ingest/census",
		" https://forever-pulse.com ":              "https://forever-pulse.com/api/ingest/census",
		"https://forever-pulse-staging.vercel.app": "https://forever-pulse-staging.vercel.app/api/ingest/census",
		"http://localhost:3000":                    "http://localhost:3000/api/ingest/census",
		"http://127.0.0.1:43153":                   "http://127.0.0.1:43153/api/ingest/census",
		"http://[::1]:3000/":                       "http://[::1]:3000/api/ingest/census",
	}
	for site, want := range bons {
		if got, err := Cible(site); err != nil || got != want {
			t.Errorf("Cible(%q) = %q, %v ; attendu %q", site, got, err, want)
		}
	}
	for _, site := range []string{
		"https://forever-pulse.com/api/ingest/census", // le chemin est ajouté par le compagnon
		"https://forever-pulse.com/api/ingest/census/",
		"https://forever-pulse.com/fr",
		"https://abcdefghijklmnop.supabase.co",
		"https://abcdefghijklmnop.supabase.co/rest/v1",
		"https://SUPABASE.CO",
		"http://forever-pulse.com", // http hors machine locale
		"ftp://forever-pulse.com",
		"https://forever-pulse.com?x=1",
		"https://forever-pulse.com/#a",
		"https://jeton@forever-pulse.com",
		"forever-pulse.com",
		"",
	} {
		if got, err := Cible(site); err == nil {
			t.Errorf("Cible(%q) = %q accepté", site, got)
		}
	}
}

// Une destination refusée ne provoque aucune connexion.
type transportInterdit struct{ t *testing.T }

func (x transportInterdit) RoundTrip(r *http.Request) (*http.Response, error) {
	x.t.Errorf("connexion vers %s", r.URL.Host)
	return nil, fmt.Errorf("interdit")
}

func TestJamaisSupabaseNiCheminDouble(t *testing.T) {
	for _, site := range []string{"https://abcdefghijklmnop.supabase.co", "https://forever-pulse.com/api/ingest/census"} {
		s := New(site, "test")
		s.Client.Transport = transportInterdit{t}
		if r := s.Post(context.Background(), "fpc_x", deuxLots); r.Verdict != Panne || !strings.Contains(r.Detail, "site_url invalide") {
			t.Fatalf("%s : %+v", site, r)
		}
	}
}

// Contrat HTTP schéma 4, contre un faux site qui applique la même règle que
// private.census_ingest : 202 pour un lot nouveau, puis 200 pour son doublon.
func TestContratSchema4PremierEnvoiPuisDoublon(t *testing.T) {
	var mu sync.Mutex
	connus := map[string]bool{}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Method != http.MethodPost || r.URL.Path != "/api/ingest/census" || r.URL.RawQuery != "" ||
			r.Header.Get("Content-Type") != "application/json; charset=utf-8" || r.Header.Get("Content-Encoding") != "gzip" ||
			r.Header.Get("Authorization") != "Bearer fpc_jeton" || r.Header.Get("X-Source") != "forever-pulse-census" ||
			r.Header.Get("X-Schema") != "4" || !strings.HasPrefix(r.Header.Get("User-Agent"), "wowsync/") {
			t.Errorf("requête hors contrat : %s %s %v", r.Method, r.URL, r.Header)
		}
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Fatal("corps non gzip")
		}
		brut, _ := io.ReadAll(zr)
		var corps struct {
			Schema  int `json:"schema"`
			Batches []struct {
				BatchID string `json:"batch_id"`
			} `json:"batches"`
		}
		if err := json.Unmarshal(brut, &corps); err != nil || corps.Schema != 4 {
			t.Fatalf("JSON schéma 4 attendu : %v", err)
		}
		acc, dup := 0, 0
		for _, b := range corps.Batches {
			if connus[b.BatchID] {
				dup++
			} else {
				connus[b.BatchID] = true
				acc++
			}
		}
		w.Header().Set("Content-Type", "application/json")
		if acc > 0 {
			w.WriteHeader(202)
		}
		fmt.Fprintf(w, `{"accepted":%d,"duplicates":%d,"rejected":[]}`, acc, dup)
	}))
	defer hs.Close()
	un := []byte(`{"schema":4,"batches":[{"batch_id":"` + lotA + `"}]}`)
	s := New(hs.URL, "test")
	r := s.Post(context.Background(), "fpc_jeton", un)
	if r.Status != 202 || r.Verdict != Accepte || r.Reponse.Accepted != 1 || r.Reponse.Duplicates != 0 {
		t.Fatalf("premier envoi : %+v", r)
	}
	r = s.Post(context.Background(), "fpc_jeton", un)
	if r.Status != 200 || r.Verdict != Accepte || r.Reponse.Accepted != 0 || r.Reponse.Duplicates != 1 {
		t.Fatalf("doublon : %+v", r)
	}
}

// Une redirection (protection Vercel, changement de domaine) n'est jamais suivie :
// ni le corps ni le jeton ne partent ailleurs, et rien n'est marqué envoyé.
func TestRedirectionJamaisSuivie(t *testing.T) {
	appels := 0
	ailleurs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appels++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		fmt.Fprint(w, `{"accepted":2,"duplicates":0,"rejected":[]}`)
	}))
	defer ailleurs.Close()
	for _, code := range []int{301, 302, 303, 307, 308} {
		redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, ailleurs.URL+"/api/ingest/census", code)
		}))
		r := New(redir.URL, "test").Post(context.Background(), "fpc_x", deuxLots)
		redir.Close()
		if r.Verdict != Panne || r.Status != code || !strings.Contains(r.Detail, fmt.Sprint(code)) {
			t.Fatalf("%d : %+v", code, r)
		}
	}
	if appels != 0 {
		t.Fatalf("redirection suivie %d fois", appels)
	}
}

// Les refus qui bloquent l'envoi ou marquent des lots refusés n'arrivent que sur
// l'erreur JSON de la route. La page 401 de Vercel Authentication, un 400 ou un
// 403 HTML d'intermédiaire : panne, réessayée, les lots restent en attente.
func TestRefusSeulementDeLaRoute(t *testing.T) {
	cas := []struct {
		code  int
		ct    string
		corps string
		want  Verdict
	}{
		{401, "text/html; charset=utf-8", `<!doctype html><title>Authentication Required</title>`, Panne},
		{403, "text/html", `<html>Forbidden</html>`, Panne},
		{400, "text/html", `<html>Bad Request</html>`, Panne},
		{409, "text/plain", `conflict`, Panne},
		{413, "text/html", `too large`, Panne},
		{401, "application/json", `{"message":"x"}`, Panne},
		{401, "application/json", `{"error":"token_invalid"}`, JetonRefuse},
		{401, "application/json", `{"error":"token_required"}`, JetonRefuse},
		{403, "application/json", `{"error":"source_disabled"}`, SourceCoupee},
		{409, "application/json", `{"error":"unsupported_schema","supported":[4]}`, Incompatible},
		{400, "application/json", `{"error":"invalid_body","errors":["x"]}`, Invalide},
		{413, "application/json", `{"error":"payload_too_large","limit_bytes":3145728}`, TropGros},
		{429, "application/json", `{"error":"rate_limited"}`, Quota},
		{503, "application/json", `{"error":"unavailable"}`, Panne},
	}
	for _, k := range cas {
		hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", k.ct)
			w.WriteHeader(k.code)
			fmt.Fprint(w, k.corps)
		}))
		r := New(hs.URL, "test").Post(context.Background(), "fpc_x", deuxLots)
		hs.Close()
		if r.Verdict != k.want {
			t.Fatalf("%d %s %q : verdict %d, attendu %d (%s)", k.code, k.ct, k.corps, r.Verdict, k.want, r.Detail)
		}
	}
}

// Le jeton n'apparaît jamais dans le détail journalisé.
func TestDetailSansJeton(t *testing.T) {
	const jeton = "fpc_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		fmt.Fprintf(w, `{"error":"invalid_body %s"}`, r.Header.Get("Authorization"))
	}))
	defer hs.Close()
	r := New(hs.URL, "test").Post(context.Background(), jeton, deuxLots)
	if strings.Contains(r.Detail, jeton) {
		t.Fatalf("jeton dans le détail : %s", r.Detail)
	}
}
