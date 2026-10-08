// Package sender poste les lots au site (SPEC §6).
//
//	POST /api/ingest/census
//	Content-Type: application/json; charset=utf-8
//	Content-Encoding: gzip
//	Authorization: Bearer <jeton>
//	X-Source: forever-pulse-census
//	X-Schema: 4
//
// 0.7.0 (02/10/2026) : les statistiques de personnages partent vers
// <site_url>/api/ingest/stats (X-Source: forever-pulse-stats, X-Schema: 1 ; 2 depuis
// la 0.9.0 avec repli sur 1, talents), avec
// les mêmes règles : aucune redirection suivie, accusé JSON exact.
//
// 0.6.4 (29/09/2026) : la destination est toujours
// <site_url>/api/ingest/census, jamais une redirection. Un lot n'est marqué
// envoyé que sur l'accusé JSON exact de cette route : 202 si au moins un lot est
// nouveau, 200 si tous sont des doublons, et accepted + duplicates + rejected =
// nombre de lots envoyés.
package sender

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"wowsync/internal/i18n"
)

// MaxBody : 3 Mo de JSON non compressé par envoi (le site refuse au-delà, 413).
const MaxBody = 3 << 20

// MaxCensusBody : 0.7.1 (03/10/2026). Plafond des envois de lots de recensement.
// Le premier envoi réel vers la production (19 000 personnages, 2,7 Mo par
// requête) a pris jusqu’à 27 s côté site, pour une limite de 30 s : 1 Mo
// (≈ 7 000 personnages) garde une marge large, même pour un rattrapage où tous
// les personnages sont nouveaux. Un lot seul plus gros part quand même seul.
const MaxCensusBody = 1 << 20

// Verdict : ce que le compagnon doit faire de la réponse.
type Verdict int

const (
	Accepte      Verdict = iota // 202 ou 200 : marquer comme envoyés
	Invalide                    // 400 : corps refusé, rien n'est rangé
	JetonRefuse                 // 401
	SourceCoupee                // 403 source_disabled (ou jeton révoqué)
	Incompatible                // 409 : arrêter, ne pas boucler
	TropGros                    // 413
	Quota                       // 429 : respecter Retry-After
	Panne                       // 5xx ou réseau : recul exponentiel
)

// Rejet : un lot (batch_id) ou une fiche de statistiques (id) refusé par le site.
type Rejet struct {
	BatchID string `json:"batch_id"`
	ID      string `json:"id"`
	Reason  string `json:"reason"`
}

// Cle : l'identifiant refusé, lot ou fiche.
func (r Rejet) Cle() string {
	if r.BatchID != "" {
		return r.BatchID
	}
	return r.ID
}

type Reponse struct {
	Accepted   int     `json:"accepted"`
	Duplicates int     `json:"duplicates"`
	Rejected   []Rejet `json:"rejected"`
	Error      string  `json:"error"`
	// Supported : schémas acceptés, annoncés par un 409 unsupported_schema.
	Supported []int `json:"supported"`
}

// Accepte dit si un 409 annonce le schéma n parmi les schémas acceptés.
func (r Reponse) Accepte(n int) bool {
	for _, s := range r.Supported {
		if s == n {
			return true
		}
	}
	return false
}

type Resultat struct {
	Status     int
	Verdict    Verdict
	RetryAfter time.Duration
	Reponse    Reponse
	Detail     string // court, sans jeton ni nom de personnage
}

type Sender struct {
	BaseURL string
	Client  *http.Client
	Version string
}

// Route : chemin de la route d'envoi, ajouté par le compagnon à site_url.
const Route = "/api/ingest/census"

// RouteStats : route des statistiques de personnages (0.7.0).
const RouteStats = "/api/ingest/stats"

// Cible renvoie l'adresse exacte du POST pour un site_url : l'origine du site
// (https://forever-pulse.com), sans chemin, suivie de Route. Refusés : un chemin
// (y compris /api/ingest/census, que le compagnon ajoute déjà), une requête, un
// fragment, des identifiants dans l'URL, http hors de la machine locale, et
// Supabase : le compagnon parle au site, jamais directement à la base.
func Cible(site string) (string, error) { return CibleRoute(site, Route) }

// CibleRoute : comme Cible, pour une route d'envoi donnée (Route ou RouteStats).
func CibleRoute(site, route string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(site))
	if err != nil || u.Host == "" || u.Opaque != "" {
		return "", errors.New("site_url invalide : origine https:// attendue")
	}
	h := strings.ToLower(u.Hostname())
	switch {
	case u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "":
		return "", errors.New("site_url invalide : ni identifiants, ni requête, ni fragment")
	case strings.Trim(u.Path, "/") != "":
		return "", fmt.Errorf("site_url invalide : chemin %q en trop, %s est ajouté par le compagnon", u.Path, Route)
	case h == "supabase.co" || strings.HasSuffix(h, ".supabase.co") || h == "supabase.in" || strings.HasSuffix(h, ".supabase.in"):
		return "", errors.New("site_url invalide : le compagnon envoie au site, jamais directement à Supabase")
	case u.Scheme == "https":
	case u.Scheme == "http" && locale(h):
	default:
		return "", errors.New("site_url invalide : https:// obligatoire hors de la machine locale")
	}
	return u.Scheme + "://" + u.Host + route, nil
}

func locale(h string) bool {
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

func New(baseURL, version string) *Sender {
	return &Sender{BaseURL: strings.TrimRight(baseURL, "/"), Version: version,
		Client: &http.Client{Timeout: 60 * time.Second,
			// Jamais de redirection suivie : le corps (noms de personnages) et le
			// jeton ne partent que vers la route du site. Une 3xx (protection
			// Vercel, changement de domaine) reste un échec, réessayé plus tard.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

// Post envoie un corps de lots de recensement déjà construit.
func (s *Sender) Post(ctx context.Context, token string, body []byte) Resultat {
	return s.envoie(ctx, token, body, Route, "forever-pulse-census", "4", lotsDuCorps(body))
}

// PostStats envoie un corps de fiches de statistiques déjà construit, sous le
// schéma donné (X-Schema : 2 depuis la 0.9.0, 1 en repli).
func (s *Sender) PostStats(ctx context.Context, token string, body []byte, schema int) Resultat {
	return s.envoie(ctx, token, body, RouteStats, "forever-pulse-stats", strconv.Itoa(schema), fichesDuCorps(body))
}

// envoie poste un corps JSON vers une route d'envoi. Le jeton n'est jamais journalisé.
func (s *Sender) envoie(ctx context.Context, token string, body []byte, route, source, schema string, ids []string) Resultat {
	var gz bytes.Buffer
	w, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	_, _ = w.Write(body)
	_ = w.Close()
	cible, err := CibleRoute(s.BaseURL, route)
	if err != nil {
		return Resultat{Verdict: Panne, Detail: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cible, &gz)
	if err != nil {
		return Resultat{Verdict: Panne, Detail: i18n.T("err.request")}
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Source", source)
	req.Header.Set("X-Schema", schema)
	req.Header.Set("User-Agent", "wowsync/"+s.Version)
	resp, err := s.Client.Do(req)
	if err != nil {
		return Resultat{Verdict: Panne, Detail: i18n.T("err.network", scrub(err.Error(), token))}
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	r := Resultat{Status: resp.StatusCode}
	estJSON := jsonDeLaRoute(resp.Header.Get("Content-Type"))
	if estJSON {
		_ = decodeJSON(raw, &r.Reponse)
	}
	switch {
	case resp.StatusCode == 202 || resp.StatusCode == 200:
		// Un 200 ou 202 ne vaut accusé de réception que s'il vient de la route
		// d'envoi et rend compte de chaque lot envoyé. Une page d'accueil, un
		// hébergeur de domaine, une page de protection ou un JSON étranger ne fait
		// jamais marquer des lots comme envoyés : nouvel essai plus tard.
		if rep, ok := accuse(resp.StatusCode, estJSON, raw, ids); ok {
			r.Reponse, r.Verdict = rep, Accepte
		} else {
			r.Reponse = Reponse{}
			r.Verdict = Panne
			r.Detail = fmt.Sprintf("HTTP %d ", resp.StatusCode) + i18n.T("err.notsite")
			return r
		}
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		r.Verdict = Panne
		r.Detail = fmt.Sprintf("HTTP %d ", resp.StatusCode) + i18n.T("err.redirect")
		return r
	case resp.StatusCode == 429:
		r.Verdict = Quota
		r.RetryAfter = retryAfter(resp.Header.Get("Retry-After"), time.Now())
		if route == RouteAuctions {
			r.RetryAfter = auctionRetryAfter(resp.Header.Get("Retry-After"), time.Now())
		}
	case !estJSON || r.Reponse.Error == "":
		// 400, 401, 403, 409, 413 bloquent ou refusent des lots : seulement sur
		// l'erreur JSON de la route, jamais sur la page d'un intermédiaire.
		r.Reponse = Reponse{}
		r.Verdict = Panne
		r.Detail = fmt.Sprintf("HTTP %d ", resp.StatusCode) + i18n.T("err.notsite")
		return r
	case resp.StatusCode == 400:
		r.Verdict = Invalide
	case resp.StatusCode == 401:
		r.Verdict = JetonRefuse
	case resp.StatusCode == 403:
		r.Verdict = SourceCoupee
	case resp.StatusCode == 409:
		r.Verdict = Incompatible
	case resp.StatusCode == 413:
		r.Verdict = TropGros
	default:
		r.Verdict = Panne
	}
	r.Detail = fmt.Sprintf("HTTP %d", resp.StatusCode)
	if r.Reponse.Error != "" {
		r.Detail += " " + scrub(r.Reponse.Error, token)
	}
	return r
}

// jsonDeLaRoute : la route répond toujours en application/json.
func jsonDeLaRoute(ct string) bool {
	t, _, err := mime.ParseMediaType(ct)
	return err == nil && t == "application/json"
}

func decodeJSON(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	if err := d.Decode(v); err != nil {
		return err
	}
	if d.More() {
		return errors.New("données après l'objet JSON")
	}
	return nil
}

// lotsDuCorps : les batch_id du corps envoyé (le compagnon l'a construit).
func lotsDuCorps(body []byte) []string {
	var c struct {
		Batches []struct {
			BatchID string `json:"batch_id"`
		} `json:"batches"`
	}
	if json.Unmarshal(body, &c) != nil {
		return nil
	}
	ids := make([]string, len(c.Batches))
	for i, b := range c.Batches {
		ids[i] = b.BatchID
	}
	return ids
}

// fichesDuCorps : les identifiants de fiches (contexte/GUID) du corps envoyé.
func fichesDuCorps(body []byte) []string {
	var c struct {
		Characters []struct {
			ID string `json:"id"`
		} `json:"characters"`
	}
	if json.Unmarshal(body, &c) != nil {
		return nil
	}
	ids := make([]string, len(c.Characters))
	for i, f := range c.Characters {
		ids[i] = f.ID
	}
	return ids
}

// accuse vérifie l'accusé de réception d'une route d'envoi (SPEC §6) :
// objet JSON { accepted, duplicates, rejected[] }, entiers positifs ou nuls,
// 202 si accepted > 0 et 200 si accepted = 0, et chaque lot envoyé compté une
// seule fois : accepted + duplicates + len(rejected) = nombre de lots, chaque
// batch_id refusé appartenant à l'envoi, sans doublon.
func accuse(status int, estJSON bool, raw []byte, ids []string) (Reponse, bool) {
	if !estJSON {
		return Reponse{}, false
	}
	var m map[string]json.RawMessage
	if decodeJSON(raw, &m) != nil {
		return Reponse{}, false
	}
	entier := func(k string) (int, bool) {
		v, ok := m[k]
		if !ok {
			return 0, false
		}
		// Un entier JSON littéral : ni chaîne, ni décimal, ni exposant, ni signe.
		t := string(bytes.TrimSpace(v))
		if t == "" || len(t) > 9 || strings.Trim(t, "0123456789") != "" || len(t) > 1 && t[0] == '0' {
			return 0, false
		}
		i, err := strconv.Atoi(t)
		return i, err == nil
	}
	var r Reponse
	var ok bool
	if r.Accepted, ok = entier("accepted"); !ok {
		return Reponse{}, false
	}
	if r.Duplicates, ok = entier("duplicates"); !ok {
		return Reponse{}, false
	}
	rej, ok := m["rejected"]
	if !ok || decodeJSON(rej, &r.Rejected) != nil || r.Rejected == nil && string(bytes.TrimSpace(rej)) != "[]" {
		return Reponse{}, false
	}
	if (status == 202) != (r.Accepted > 0) {
		return Reponse{}, false
	}
	if r.Accepted+r.Duplicates+len(r.Rejected) != len(ids) {
		return Reponse{}, false
	}
	envoyes := make(map[string]bool, len(ids))
	for _, id := range ids {
		envoyes[id] = true
	}
	vus := map[string]bool{}
	for _, x := range r.Rejected {
		cle := x.Cle()
		if cle == "" || !envoyes[cle] || vus[cle] {
			return Reponse{}, false
		}
		vus[cle] = true
	}
	return r, true
}

func scrub(s, token string) string {
	if token != "" {
		s = strings.ReplaceAll(s, token, "fpc_***")
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// retryAfter : secondes ou date HTTP ; 60 s par défaut, 1 h au plus.
func retryAfter(v string, now time.Time) time.Duration {
	d := 60 * time.Second
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
		d = time.Duration(n) * time.Second
	} else if t, err := http.ParseTime(v); err == nil {
		d = t.Sub(now)
	}
	if d < time.Second {
		d = time.Second
	}
	if d > time.Hour {
		d = time.Hour
	}
	return d
}

// Backoff : recul exponentiel de 30 s à 30 min.
func Backoff(echecs int) time.Duration {
	d := 30 * time.Second
	for i := 1; i < echecs && d < 30*time.Minute; i++ {
		d *= 2
	}
	if d > 30*time.Minute {
		d = 30 * time.Minute
	}
	return d
}
