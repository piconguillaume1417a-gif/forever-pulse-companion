// Package app : le moteur de Forever Pulse Companion, sans interface.
//
// Fichier stable → lecture partagée → SHA-256 (déjà connu : ignoré) → décodage du
// schéma 4 → contrôles → reconstitution des deltas → file d'envoi + cumul (une
// transaction SQLite) → envoi par paquets de 3 Mo au plus.
//
// 0.7.0 : le même fichier porte aussi ForeverPulseStatsDB (statistiques de
// personnages). Ses fiches sont décodées, mises en file (une ligne par fiche, la
// dernière version connue) et envoyées à /api/ingest/stats après les lots. Seules
// les fiches nouvelles ou modifiées depuis le dernier envoi confirmé repartent.
package app

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"wowsync/internal/cumul"
	"wowsync/internal/i18n"
	"wowsync/internal/logx"
	"wowsync/internal/lua"
	"wowsync/internal/schema"
	"wowsync/internal/secret"
	"wowsync/internal/sender"
	"wowsync/internal/stats"
	"wowsync/internal/store"
	"wowsync/internal/watch"
)

const (
	Version = "0.10.0-rc.1"
	Nom     = "forever-pulse-companion"
)

// Couleur de l'icône.
type Couleur int

const (
	Vert   Couleur = iota // tout est envoyé
	Orange                // envoi en attente
	Rouge                 // erreur à traiter
)

// Blocages persistés dans meta.blocked : aucun envoi automatique tant qu'ils durent.
const (
	BlocSchema = "schema"   // 409 : version du site incompatible
	BlocJeton  = "token"    // 401 : jeton absent ou refusé
	BlocSource = "disabled" // 403 : source désactivée par le site
	BlocCorps  = "invalid"  // 400 : lots refusés, à examiner
)

type Etat struct {
	Couleur      Couleur
	Code         string // cause de l'état (fenêtre) : allsent, pending, notoken, token, schema, source, body
	Message      string
	Pending      int
	Sent         int
	Rejected     int
	DernierEnvoi string // heure, lots, personnages
	Cumul        []string
	Portees      []Portee // le même cumul, en champs séparés (fenêtre)

	// 0.7.0 : statistiques de personnages et détail de l'état.
	StatsPending    int
	StatsSent       int
	StatsRejected   int
	AuctionPending  int
	AuctionSent     int
	AuctionUnproven int
	Details         []string // fichier suivi, dernière lecture, derniers envois, attente, dernière erreur
}

// Portee : une ligne du cumul « N personnages distincts observés depuis le … ».
type Portee struct {
	ScopeID string
	Total   int
	Depuis  string // date déjà formatée, « ? » si inconnue
}

type App struct {
	Store     *store.Store
	Log       *logx.Logger
	Sender    *sender.Sender
	Token     func() (string, error)
	SaveToken func(string) error
	Now       func() time.Time

	mu       sync.Mutex
	echecs   int
	prochain time.Time // pas d'envoi automatique avant
	OnChange func()

	// 0.7.0 : recul propre aux statistiques ; une panne de leur route ne retarde
	// jamais l'envoi des lots.
	echecsStats   int
	prochainStats time.Time

	// 0.6.0 : résumé du cumul gardé entre deux lectures de l'état. Il ne change
	// qu'avec le cumul (nouveau fichier, effacement) ou la langue.
	resume      []Portee
	resumeLang  i18n.Lang
	resumeValid bool
}

// New ne charge plus le cumul (0.7.4) : il est relu de la base le temps d'intégrer
// un fichier, puis libéré. Gardé en mémoire, ses quelque 200 000 personnages
// occupaient environ 200 Mo entre deux /reload pour quelques secondes de travail.
func New(st *store.Store, log *logx.Logger, snd *sender.Sender) (*App, error) {
	if err := st.AuctionVersion(Version); err != nil {
		return nil, err
	}
	return &App{Store: st, Log: log, Sender: snd, Token: secret.Get, SaveToken: secret.Set, Now: time.Now}, nil
}

func (a *App) changed() {
	if a.OnChange != nil {
		a.OnChange()
	}
}

// Origin : « current » pour ForeverPulse.lua, « backup » pour le .bak.
func Origin(path string) string {
	if strings.HasSuffix(strings.ToLower(path), ".bak") {
		return "backup"
	}
	return "current"
}

// Decode lit un fichier en mémoire et analyse ForeverPulseCensusDB.
func Decode(data []byte, now time.Time) (*schema.Fichier, *lua.Table, error) {
	f, db, _, err := DecodeTout(data, now)
	return f, db, err
}

// DecodeTout lit un fichier en mémoire et l'analyse. ForeverPulseCensusDB et
// ForeverPulseStatsDB sont construits ; ForeverPulseScanDB (l'état interne de
// l'addon) est vérifié syntaxiquement puis oublié : il n'est jamais lu ni transmis.
// Le fichier est analysé comme des données, sans interpréteur Lua.
func DecodeTout(data []byte, now time.Time) (*schema.Fichier, *lua.Table, *stats.Fichier, error) {
	v, err := lua.Parse(data, map[string]bool{"ForeverPulseCensusDB": true, stats.Variable: true})
	if err != nil {
		return nil, nil, nil, err
	}
	db := lua.AsTable(v["ForeverPulseCensusDB"])
	if db == nil {
		return nil, nil, nil, errors.New("ForeverPulseCensusDB absent du fichier")
	}
	return schema.Analyse(db, now), db, stats.DecodeAt(lua.AsTable(v[stats.Variable]), now), nil
}

// CumulLots : les lots d'un fichier dans la forme que lit le cumul. Même règle que
// la référence (cumul.integrer_fichier) : tous les lots décodables, toutes sources,
// deltas reconstitués ; un lot dont la chaîne de deltas échoue est laissé de côté
// (non marqué traité, il pourra l'être plus tard).
func CumulLots(db *lua.Table) []cumul.Lot {
	// 0.4.0 : schéma 5 développé en schéma 4 (même règle que cumul.py).
	lots, _ := schema.DevelopperLots(db)
	var roster []*lua.Table
	for _, l := range lots {
		if lua.Str(l.Get("method")) == "channel_roster" {
			roster = append(roster, l)
		}
	}
	etats, _ := schema.Reconstruit(roster)
	var out []cumul.Lot
	for _, l := range lots {
		t, _ := lua.AsInt(l.Get("observed_at"))
		method := lua.Str(l.Get("method"))
		cl := cumul.Lot{BatchID: lua.Str(l.Get("batch_id")), ScopeID: lua.Str(l.Get("scope_id")), ObservedAt: t,
			Source: cumul.Source(method, lua.Str(l.Get("source_scope")))}
		if cl.Source == cumul.SrcWho {
			champs := strings.Split(lua.Str(l.Get("fields")), "|")
			for _, ligne := range schema.Lignes(lua.Str(l.Get("rows"))) {
				parts := strings.Split(ligne, "|")
				c := map[string]string{}
				for i, k := range champs {
					if i < len(parts) {
						c[k] = parts[i]
					}
				}
				var lv *int
				if n, err := strconv.Atoi(c["level"]); err == nil && isDigits(c["level"]) {
					lv = &n
				}
				cl.Who = append(cl.Who, cumul.WhoPerso{Name: c["name"], ClassLoc: c["class_loc"], RaceLoc: c["race_loc"],
					Guild: c["guild"], Zone: c["zone"], Level: lv})
			}
		} else {
			e, ok := etats[cl.BatchID]
			if !ok {
				continue
			}
			var zone *string
			if cl.Source == cumul.SrcZone {
				if z, ok := l.Get("zone_name").(string); ok {
					zone = &z
				}
			}
			for _, p := range e.Persos() {
				cl.Roster = append(cl.Roster, cumul.RosterPerso{GUID: p.GUID, DisplayName: p.DisplayName,
					Class: p.Class, Race: p.Race, Level: p.Level, Zone: zone})
			}
		}
		out = append(out, cl)
	}
	return out
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ProcessFile traite un fichier s'il est nouveau. Renvoie le nombre de lots mis en file.
func (a *App) ProcessFile(ctx context.Context, path string) (int, error) {
	if AuctionPath(path) {
		return a.ProcessAuctions(ctx, path)
	}
	data, sha, err := watch.Read(path)
	if err != nil {
		return 0, err
	}
	short := sha[:12]
	lotsConnus, statsConnues := a.Store.FileKnown(sha), a.Store.StatsFileKnown(sha)
	if lotsConnus && statsConnues {
		a.Log.Printf("fichier %s (%s) déjà traité, ignoré", filepath.Base(path), short)
		return 0, nil
	}
	// 0.6.0 : un gros fichier réclame quelques centaines de Mo le temps du
	// décodage ; ils sont rendus à Windows dès la fin du traitement, sans attendre
	// le ramasse-miettes (le compagnon reste léger en fond).
	defer debug.FreeOSMemory()
	f, db, sf, err := DecodeTout(data, a.Now())
	if err != nil {
		a.Log.Printf("fichier %s (%s) illisible : %s", filepath.Base(path), short, strings.SplitN(err.Error(), "—", 2)[0])
		return 0, err
	}
	_ = a.Store.Set("last_read", fmt.Sprintf("%s|%s|%s", a.Now().Format(time.RFC3339), short, path))
	if len(f.Fatales) > 0 {
		for _, e := range f.Fatales {
			a.Log.Printf("fichier %s (%s) refusé : %s", filepath.Base(path), short, e)
		}
		return 0, fmt.Errorf("fichier refusé (%d défauts)", len(f.Fatales))
	}
	if lotsConnus {
		// Fichier déjà traité par une version antérieure à la 0.7.0 : seules ses
		// statistiques restent à mettre en file.
		if _, err := a.traiteStats(ctx, sha, path, f.AddonVersion, sf); err != nil {
			return 0, err
		}
		a.changed()
		return 0, nil
	}
	var batches []store.QueuedBatch
	ecartes := 0
	for _, l := range f.Lots {
		if !l.Valide() {
			ecartes++
			// Journal : identifiant de lot et motif. Les motifs ne portent pas de nom
			// de personnage (au plus un GUID ou un index de ligne).
			a.Log.Printf("lot %s écarté (%s %s) : %s", l.BatchID, l.Method, l.Kind, strings.Join(l.Erreurs, " ; "))
			continue
		}
		body, err := schema.Marshal(l.Payload)
		if err != nil {
			return 0, err
		}
		batches = append(batches, store.QueuedBatch{BatchID: l.BatchID, ScopeID: l.ScopeID, ObservedAt: l.ObservedAt,
			Characters: l.Personnages, Payload: body})
	}
	scopes, _ := schema.Marshal(scopeObj(f))
	a.mu.Lock()
	// 0.7.4 : cumul relu pour ce fichier seulement, puis abandonné (rendu à Windows
	// par le FreeOSMemory différé). Transaction annulée : rien n'est gardé à moitié,
	// le prochain fichier relira la base.
	c, err := a.Store.LoadCumul()
	if err != nil {
		a.mu.Unlock()
		return 0, err
	}
	st := c.Integrer(CumulLots(db))
	a.resumeValid = false
	added, err := a.Store.AddFile(ctx, store.File{SHA256: sha, Path: path, Origin: Origin(path), AddonVersion: f.AddonVersion,
		ObserverSessionID: f.ObserverID, Scopes: scopes, Total: len(f.Lots), Discarded: ecartes}, batches, c)
	c = nil
	if err != nil {
		a.mu.Unlock()
		return 0, err
	}
	a.mu.Unlock()
	a.Log.Printf("fichier %s (%s, %s, addon %s) : %d lots, %d en file, %d écartés ; cumul +%d personnages, %d fusions /who",
		filepath.Base(path), short, Origin(path), f.AddonVersion, len(f.Lots), added, ecartes, st.Nouveaux, st.Fusions)
	if !statsConnues {
		// Les statistiques ne bloquent jamais les lots : une erreur est journalisée et
		// le fichier sera relu au prochain changement.
		if _, err := a.traiteStats(ctx, sha, path, f.AddonVersion, sf); err != nil {
			a.Log.Printf("statistiques du fichier %s (%s) non mises en file : %v", filepath.Base(path), short, err)
		}
	}
	a.changed()
	return added, nil
}

// traiteStats met en file les fiches de statistiques d'un fichier. Le journal ne
// porte ni nom de personnage ni GUID : des comptes et des motifs seulement.
func (a *App) traiteStats(ctx context.Context, sha, path, addonVersion string, sf *stats.Fichier) (int, error) {
	short := sha[:12]
	if sf == nil || !sf.Present {
		_, err := a.Store.AddStatsFile(ctx, sha, []byte("{}"), nil, 0, 0)
		return 0, err
	}
	for _, l := range sf.Infos {
		a.Log.Printf("fichier %s (%s) : %s", filepath.Base(path), short, l)
	}
	for _, l := range sf.Ecartees {
		a.Log.Printf("fichier %s (%s) : statistiques, %s", filepath.Base(path), short, l)
	}
	if len(sf.Fatales) > 0 {
		for _, e := range sf.Fatales {
			a.Log.Printf("fichier %s (%s) : statistiques non envoyées : %s", filepath.Base(path), short, e)
		}
		_ = a.Store.Set("stats_note", sf.Fatales[0])
		_, err := a.Store.AddStatsFile(ctx, sha, []byte("{}"), nil, sf.Total, sf.Total)
		return 0, err
	}
	meta, err := sf.Meta(sha, Origin(path), addonVersion)
	if err != nil {
		return 0, err
	}
	sheets := make([]store.StatSheet, 0, len(sf.Fiches))
	for _, fi := range sf.Fiches {
		sheets = append(sheets, store.StatSheet{ID: fi.ID, Digest: fi.Empreinte, Fresh: fi.Fraicheur(), Payload: fi.Payload})
	}
	added, err := a.Store.AddStatsFile(ctx, sha, meta, sheets, sf.Total, sf.Total-len(sf.Fiches))
	if err != nil {
		return 0, err
	}
	_ = a.Store.Set("stats_note", "")
	a.Log.Printf("fichier %s (%s) : statistiques, %d fiches lues, %d nouvelles ou modifiées mises en file, %d inchangées",
		filepath.Base(path), short, len(sf.Fiches), added, len(sf.Fiches)-added)
	return added, nil
}

// StatsBody construit le corps d'un envoi de statistiques (contrat
// forever-pulse-stats). meta est l'objet { file, contexts, catalog, talent_trees }
// enregistré avec le fichier. Schéma 2 (0.9.0) : talent_trees, file.talents_version
// et characters[].talents ; schéma 1 (repli) : ces trois ajouts retirés, le corps
// est alors identique à celui de la 0.9.0-rc.1.
func StatsBody(meta []byte, sheets []store.StatSheet, schemaEnvoi int) ([]byte, error) {
	m := strings.TrimSpace(string(meta))
	if len(m) < 2 || m[0] != '{' || m[len(m)-1] != '}' || m == "{}" {
		return nil, errors.New("statistiques : en-tête de fichier absent")
	}
	if schemaEnvoi != stats.SchemaEnvoi && schemaEnvoi != stats.SchemaEnvoiRepli {
		return nil, fmt.Errorf("statistiques : schéma d'envoi %d inconnu", schemaEnvoi)
	}
	mb, err := stats.CorpsSchema([]byte(m), schemaEnvoi)
	if err != nil {
		return nil, errors.New("statistiques : en-tête de fichier illisible")
	}
	m = string(mb)
	comp, _ := schema.Marshal(schema.Obj{{K: "name", V: Nom}, {K: "version", V: Version}})
	var b strings.Builder
	b.WriteString(`{"schema":`)
	b.WriteString(strconv.Itoa(schemaEnvoi))
	b.WriteString(`,"companion":`)
	b.Write(comp)
	b.WriteByte(',')
	b.WriteString(m[1 : len(m)-1])
	b.WriteString(`,"characters":[`)
	for i, f := range sheets {
		if i > 0 {
			b.WriteByte(',')
		}
		p, err := stats.FicheSchema(f.Payload, schemaEnvoi)
		if err != nil {
			return nil, errors.New("statistiques : fiche illisible")
		}
		b.Write(p)
	}
	b.WriteString("]}")
	return []byte(b.String()), nil
}

// Repli du schéma 2 sur le schéma 1 des statistiques (0.9.0, contrat §3.4).
const (
	// ReessaiSchema2 : délai minimal avant de retenter le schéma 2 après un repli.
	ReessaiSchema2 = 6 * time.Hour
	// cleRepliSchema : meta, heure Unix avant laquelle le schéma 1 est employé.
	cleRepliSchema = "stats_schema1_until"
)

// schemaStats : le schéma d'envoi des statistiques à cet instant. Le mode schéma 1
// est persisté (meta.stats_schema1_until) : il survit à un redémarrage.
func (a *App) schemaStats() int {
	if t, ok := a.repliSchemaJusqu(); ok && a.Now().Before(t) {
		return stats.SchemaEnvoiRepli
	}
	return stats.SchemaEnvoi
}

func (a *App) repliSchemaJusqu() (time.Time, bool) {
	s := a.Store.Get(cleRepliSchema)
	if s == "" {
		return time.Time{}, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(n, 0), true
}

func scopeObj(f *schema.Fichier) schema.Obj {
	o := schema.Obj{}
	for _, sid := range f.ScopeIDs {
		o = append(o, schema.KV{K: sid, V: f.Scopes[sid]})
	}
	return o
}

// Body construit le corps d'un envoi sans transmettre l'identifiant durable de l'addon.
func Body(f *store.File, batches []store.QueuedBatch) ([]byte, error) {
	var uploadID [16]byte
	if _, err := rand.Read(uploadID[:]); err != nil {
		return nil, err
	}
	uploadID[6] = (uploadID[6] & 0x0f) | 0x40
	uploadID[8] = (uploadID[8] & 0x3f) | 0x80
	id := fmt.Sprintf("%x-%x-%x-%x-%x", uploadID[:4], uploadID[4:6], uploadID[6:8], uploadID[8:10], uploadID[10:])
	var b strings.Builder
	file, _ := schema.Marshal(schema.Obj{{K: "sha256", V: f.SHA256}, {K: "origin", V: f.Origin},
		{K: "addon_version", V: f.AddonVersion}, {K: "observer_session_id", V: id}})
	comp, _ := schema.Marshal(schema.Obj{{K: "name", V: Nom}, {K: "version", V: Version}})
	b.WriteString(`{"schema":4,"companion":`)
	b.Write(comp)
	b.WriteString(`,"file":`)
	b.Write(file)
	b.WriteString(`,"scopes":`)
	b.Write(f.Scopes)
	b.WriteString(`,"batches":[`)
	for i, x := range batches {
		if i > 0 {
			b.WriteByte(',')
		}
		b.Write(x.Payload)
	}
	b.WriteString("]}")
	return []byte(b.String()), nil
}

// Flush envoie tout ce qui est en attente. manual = « Envoyer maintenant » :
// lève les blocages et le recul, une seule fois (jamais de boucle).
//
// OnChange est appelé une fois le verrou relâché : il relit l'état (Etat prend le
// même verrou, qui n'est pas réentrant).
//
// 0.6.0 : un envoi automatique qui n'a rien à faire (bloqué, en recul, file vide)
// n'appelle plus OnChange et ne lit plus le jeton : le tic de fond (toutes les 5 min depuis 0.7.3, 30 s en 0.7.2, 10 min de 0.6.0 à 0.7.1, 2 s avant),
// ne coûte plus que deux petites requêtes.
func (a *App) Flush(ctx context.Context, manual bool) error {
	a.mu.Lock()
	rien, err := a.flush(ctx, manual)
	if a.Sender != nil {
		attempted, ae := a.envoieAuctions(ctx, manual)
		if attempted {
			rien = false
		}
		if ae != nil {
			if err == nil {
				err = ae
			}
			rien = false
		}
	}
	a.mu.Unlock()
	if !rien {
		a.changed()
	}
	return err
}

// flush : rien = vrai quand l'appel n'a rien tenté ni changé.
func (a *App) flush(ctx context.Context, manual bool) (rien bool, err error) {
	lots, fiches := manual, manual
	if manual {
		_ = a.Store.Set("blocked", "")
		_ = a.Store.Set("stats_blocked", "")
		a.prochain, a.prochainStats = time.Time{}, time.Time{}
	} else {
		if a.Store.Get("blocked") != "" {
			return true, nil
		}
		if !a.Now().Before(a.prochain) {
			if a.Store.HasPending() {
				lots = true
			} else {
				a.echecs = 0
			}
		}
		fiches = a.Store.Get("stats_blocked") == "" && !a.Now().Before(a.prochainStats) && a.Store.StatsHasPending()
		if !lots && !fiches {
			return true, nil
		}
	}
	if lots {
		err = a.envoie(ctx)
	}
	// Les statistiques partent après les lots, jamais pendant un blocage du jeton
	// ou du site, ni juste après un échec des lots (même site, même réseau).
	if err == nil && fiches && a.Store.Get("blocked") == "" && (manual || a.Store.StatsHasPending()) {
		err = a.envoieStats(ctx)
	}
	if err != nil {
		_ = a.Store.Set("last_error", fmt.Sprintf("%s|%s", a.Now().Format(time.RFC3339), err.Error()))
	} else {
		_ = a.Store.Set("last_error", "")
	}
	return false, err
}

// envoieStats envoie les fiches de statistiques en attente.
func (a *App) envoieStats(ctx context.Context) error {
	if !a.Store.StatsHasPending() {
		a.echecsStats = 0
		return nil
	}
	token, err := a.Token()
	if err != nil {
		_ = a.Store.Set("blocked", BlocJeton)
		if errors.Is(err, secret.ErrAbsent) {
			return errors.New(i18n.T("err.notoken"))
		}
		return err
	}
	for {
		meta, sheets, err := a.Store.StatsPending(sender.MaxBody - 64<<10)
		if err != nil {
			return err
		}
		if len(sheets) == 0 {
			a.echecsStats = 0
			return nil
		}
		if err := a.posteStats(ctx, token, meta, sheets, a.schemaStats()); err != nil {
			return err
		}
	}
}

// posteStats envoie un paquet de fiches. Un refus global (400, 413) d'un paquet de
// plusieurs fiches est isolé par moitiés : une fiche invalide ne fait jamais
// refuser les autres.
//
// 0.9.0 : un 409 au schéma 2 dont le corps annonce le schéma 1 mais pas le 2 fait
// renvoyer le même paquet en schéma 1 (sans talents) ; le schéma 2 est retenté au
// plus tôt ReessaiSchema2 plus tard. Quand il est de nouveau accepté, les fiches
// confirmées en schéma 1 dont la charge porte des talents repartent une fois.
func (a *App) posteStats(ctx context.Context, token string, meta []byte, sheets []store.StatSheet, sch int) error {
	body, err := StatsBody(meta, sheets, sch)
	if err != nil {
		// En-tête perdu : ces fiches ne pourront jamais partir telles quelles.
		_ = a.Store.MarkStats(sheets, "rejected", "en-tete absent")
		return nil
	}
	r := a.Sender.PostStats(ctx, token, body, sch)
	a.Log.Printf("envoi de %d fiches de statistiques (schéma %d, %d Ko) : %s", len(sheets), sch, len(body)>>10, r.Detail)
	switch r.Verdict {
	case sender.Accepte:
		rejet := map[string]string{}
		for _, x := range r.Reponse.Rejected {
			rejet[x.Cle()] = x.Reason
		}
		var ok []store.StatSheet
		for _, f := range sheets {
			if reason, bad := rejet[f.ID]; bad {
				_ = a.Store.MarkStats([]store.StatSheet{f}, "rejected", reason)
				// Pas de GUID au journal : l'empreinte courte de la fiche suffit au diagnostic.
				a.Log.Printf("fiche de statistiques %s refusée par le site : %s", f.Digest[:12], reason)
			} else {
				ok = append(ok, f)
			}
		}
		if err := a.Store.MarkStatsSent(ok, sch); err != nil {
			return err
		}
		a.echecsStats = 0
		_ = a.Store.Set("last_stats_send", fmt.Sprintf("%s|%d|%d", a.Now().Format(time.RFC3339), len(ok), r.Status))
		if sch == stats.SchemaEnvoi && a.Store.Get(cleRepliSchema) != "" {
			// Retour du schéma 2 : fin du repli, les talents retenus repartent une fois.
			_ = a.Store.Set(cleRepliSchema, "")
			n, err := a.Store.StatsRequeueTalents()
			if err != nil {
				return err
			}
			a.Log.Printf("statistiques : schéma 2 de nouveau accepté, %d fiches avec talents remises en file", n)
		}
		return nil
	case sender.Invalide, sender.TropGros:
		if len(sheets) == 1 {
			reason := "http_400"
			if r.Verdict == sender.TropGros {
				reason = "http_413"
			}
			_ = a.Store.MarkStats(sheets, "rejected", reason)
			a.Log.Printf("fiche de statistiques %s refusée par le site : %s", sheets[0].Digest[:12], reason)
			return nil
		}
		m := len(sheets) / 2
		if err := a.posteStats(ctx, token, meta, sheets[:m], sch); err != nil {
			return err
		}
		return a.posteStats(ctx, token, meta, sheets[m:], a.schemaStats())
	case sender.JetonRefuse:
		_ = a.Store.Set("blocked", BlocJeton)
		return errors.New(i18n.T("err.401"))
	case sender.SourceCoupee:
		_ = a.Store.Set("stats_blocked", BlocSource)
		return errors.New(i18n.T("err.stats403"))
	case sender.Incompatible:
		if sch == stats.SchemaEnvoi && r.Reponse.Accepte(stats.SchemaEnvoiRepli) && !r.Reponse.Accepte(stats.SchemaEnvoi) {
			jusqu := a.Now().Add(ReessaiSchema2)
			_ = a.Store.Set(cleRepliSchema, strconv.FormatInt(jusqu.Unix(), 10))
			a.Log.Printf("statistiques : le site ne connaît que le schéma 1, renvoi sans talents ; schéma 2 retenté après %s",
				jusqu.UTC().Format(time.RFC3339))
			return a.posteStats(ctx, token, meta, sheets, stats.SchemaEnvoiRepli)
		}
		_ = a.Store.Set("stats_blocked", BlocSchema)
		return errors.New(i18n.T("err.stats409"))
	case sender.Quota:
		a.prochainStats = a.Now().Add(r.RetryAfter)
		return errors.New(i18n.T("err.429", r.RetryAfter.Round(time.Second)))
	default:
		a.Store.StatsAttempt(sheets)
		a.echecsStats++
		d := sender.Backoff(a.echecsStats)
		a.prochainStats = a.Now().Add(d)
		return errors.New(i18n.T("err.statsretry", r.Detail, d.Round(time.Second)))
	}
}

func (a *App) envoie(ctx context.Context) error {
	token, err := a.Token()
	if err != nil {
		if a.Store.Counts()["pending"] > 0 {
			_ = a.Store.Set("blocked", BlocJeton)
		}
		if errors.Is(err, secret.ErrAbsent) {
			return errors.New(i18n.T("err.notoken"))
		}
		return err
	}
	for {
		f, batches, err := a.Store.Pending(sender.MaxCensusBody - 64<<10)
		if err != nil {
			return err
		}
		if f == nil || len(batches) == 0 {
			a.echecs = 0
			return nil
		}
		body, err := Body(f, batches)
		if err != nil {
			return err
		}
		ids := make([]string, len(batches))
		chars := 0
		for i, b := range batches {
			ids[i] = b.BatchID
			chars += b.Characters
		}
		r := a.Sender.Post(ctx, token, body)
		a.Log.Printf("envoi %d lots (%d personnages, %d Ko) du fichier %s : %s", len(ids), chars, len(body)>>10, f.SHA256[:12], r.Detail)
		switch r.Verdict {
		case sender.Accepte:
			rejet := map[string]string{}
			for _, x := range r.Reponse.Rejected {
				rejet[x.BatchID] = x.Reason
			}
			var ok []string
			for _, id := range ids {
				if reason, bad := rejet[id]; bad {
					_ = a.Store.MarkRejected([]string{id}, reason)
					a.Log.Printf("lot %s refusé par le site : %s", id, reason)
				} else {
					ok = append(ok, id)
				}
			}
			if err := a.Store.MarkSent(ok); err != nil {
				return err
			}
			a.echecs = 0
			_ = a.Store.Set("last_send", fmt.Sprintf("%s|%d|%d|%d", a.Now().Format(time.RFC3339), len(ok), chars, r.Status))
		case sender.Invalide:
			_ = a.Store.MarkRejected(ids, "http_400")
			_ = a.Store.Set("blocked_note", fmt.Sprintf("%d lots refusés (400)", len(ids)))
			_ = a.Store.Set("blocked", BlocCorps)
			return errors.New(i18n.T("err.400"))
		case sender.TropGros:
			if len(ids) == 1 {
				_ = a.Store.MarkRejected(ids, "http_413")
				continue
			}
			// Ne devrait pas arriver sous 3 Mo : on réessaie lot par lot.
			a.prochain = a.Now().Add(sender.Backoff(1))
			return errors.New(i18n.T("err.413"))
		case sender.JetonRefuse:
			_ = a.Store.Set("blocked", BlocJeton)
			return errors.New(i18n.T("err.401"))
		case sender.SourceCoupee:
			_ = a.Store.Set("blocked", BlocSource)
			return errors.New(i18n.T("err.403"))
		case sender.Incompatible:
			_ = a.Store.Set("blocked", BlocSchema)
			return errors.New(i18n.T("err.409"))
		case sender.Quota:
			a.prochain = a.Now().Add(r.RetryAfter)
			return errors.New(i18n.T("err.429", r.RetryAfter.Round(time.Second)))
		default:
			a.Store.Attempt(ids)
			a.echecs++
			d := sender.Backoff(a.echecs)
			a.prochain = a.Now().Add(d)
			return errors.New(i18n.T("err.retry", r.Detail, d.Round(time.Second)))
		}
	}
}

// EffacerDonnees vide la base locale (file d'envoi, cumul, fichiers déjà lus).
// Les fichiers du jeu encore présents seront relus au prochain démarrage ; le site
// ignore les lots qu'il a déjà reçus.
func (a *App) EffacerDonnees() error {
	a.mu.Lock()
	err := a.Store.Wipe()
	if err == nil {
		a.resumeValid = false
		a.echecs, a.prochain = 0, time.Time{}
		a.echecsStats, a.prochainStats = 0, time.Time{}
		a.Log.Printf("données locales effacées à la demande")
	}
	a.mu.Unlock()
	a.changed()
	return err
}

// LeverBlocageJeton lève le blocage « jeton » (et lui seul) : un nouveau jeton
// mérite un nouvel essai, un autre blocage (schéma, source, corps) non. Partagé par
// la fenêtre et par --store-token, qui sans cela laissait l'icône sur « Jeton
// absent ou refusé » jusqu'au prochain envoi manuel.
func LeverBlocageJeton(s *store.Store) bool {
	if s.Get("blocked") != BlocJeton {
		return false
	}
	_ = s.Set("blocked", "")
	return true
}

// SetToken enregistre un nouveau jeton et lève le blocage « jeton ».
func (a *App) SetToken(t string) error {
	if err := a.SaveToken(t); err != nil {
		return err
	}
	a.mu.Lock()
	LeverBlocageJeton(a.Store)
	a.mu.Unlock()
	a.Log.Printf("nouveau jeton enregistré dans le Gestionnaire d'identifiants")
	a.changed()
	return nil
}

// Etat : ce qu'affichent l'icône et le menu.
func (a *App) Etat() Etat {
	a.mu.Lock()
	defer a.mu.Unlock()
	c := a.Store.Counts()
	sc := a.Store.StatsCounts()
	ac := a.Store.AuctionCounts()
	ad := a.Store.AuctionDayCounts()
	e := Etat{Pending: c["pending"], Sent: c["sent"], Rejected: c["rejected"],
		StatsPending: sc["pending"], StatsSent: sc["sent"], StatsRejected: sc["rejected"]}
	e.AuctionPending, e.AuctionSent, e.AuctionUnproven = ac["pending"], ac["sent"], ad["jours_sans_faction_prouvee"]
	switch a.Store.Get("blocked") {
	case BlocSchema:
		e.Couleur, e.Code, e.Message = Rouge, "schema", i18n.T("st.schema")
	case BlocJeton:
		e.Couleur, e.Code, e.Message = Rouge, "token", i18n.T("st.token")
	case BlocSource:
		e.Couleur, e.Code, e.Message = Rouge, "source", i18n.T("st.source")
	case BlocCorps:
		e.Couleur, e.Code, e.Message = Rouge, "body", i18n.T("st.body")
	default:
		if _, err := a.Token(); err != nil {
			e.Couleur, e.Code, e.Message = Rouge, "notoken", i18n.T("st.notoken")
		} else if e.Pending > 0 {
			e.Couleur, e.Code = Orange, "pending"
			e.Message = i18n.T("st.pending", e.Pending)
			if a.Now().Before(a.prochain) {
				e.Message += i18n.T("st.retry", a.prochain.Format(i18n.T("fmt.time")))
			}
		} else if e.StatsPending > 0 && a.Store.Get("stats_blocked") == "" {
			e.Couleur, e.Code = Orange, "pending"
			e.Message = i18n.T("st.statspending", e.StatsPending)
			if a.Now().Before(a.prochainStats) {
				e.Message += i18n.T("st.retry", a.prochainStats.Format(i18n.T("fmt.time")))
			}
		} else if e.AuctionPending > 0 {
			e.Couleur, e.Code, e.Message = Orange, "pending", i18n.T("st.auctionpending", e.AuctionPending)
		} else {
			e.Couleur, e.Code, e.Message = Vert, "allsent", i18n.T("st.allsent")
		}
	}
	if s := a.Store.Get("last_send"); s != "" {
		p := strings.Split(s, "|")
		if len(p) >= 3 {
			if t, err := time.Parse(time.RFC3339, p[0]); err == nil {
				e.DernierEnvoi = i18n.T("st.lastsend", t.Format(i18n.T("fmt.datetime")), p[1], p[2])
			}
		}
	}
	if !a.resumeValid || a.resumeLang != i18n.Get() {
		a.resume = a.resumeCumul()
		a.resumeLang, a.resumeValid = i18n.Get(), true
	}
	for _, p := range a.resume {
		e.Portees = append(e.Portees, p)
		e.Cumul = append(e.Cumul, i18n.T("st.cumul", p.ScopeID, p.Total, p.Depuis))
	}
	e.Details = a.details(e)
	return e
}

// details : ce que l'utilisateur doit pouvoir lire sans ouvrir le journal. Aucun
// nom de personnage, aucun jeton ; le chemin du fichier est celui du jeu, sur ce PC.
func (a *App) details(e Etat) []string {
	date := func(s string) string {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			return t.Local().Format(i18n.T("fmt.datetime"))
		}
		return "?"
	}
	var out []string
	if p := strings.SplitN(a.Store.Get("last_read"), "|", 3); len(p) == 3 {
		out = append(out, i18n.T("det.file", p[2]), i18n.T("det.read", date(p[0]), p[1]))
	} else {
		out = append(out, i18n.T("det.nofile"))
	}
	if p := strings.Split(a.Store.Get("last_send"), "|"); len(p) >= 3 {
		out = append(out, i18n.T("det.sent", date(p[0]), p[1]))
	} else {
		out = append(out, i18n.T("det.nosent"))
	}
	if p := strings.Split(a.Store.Get("last_stats_send"), "|"); len(p) >= 2 {
		out = append(out, i18n.T("det.statssent", date(p[0]), p[1]))
	} else {
		out = append(out, i18n.T("det.nostatssent"))
	}
	out = append(out, i18n.T("det.pending", e.Pending, e.StatsPending))
	out = append(out, i18n.T("det.stats", e.StatsSent, e.StatsRejected))
	switch a.Store.Get("stats_blocked") {
	case BlocSource:
		out = append(out, i18n.T("det.statsblocked", i18n.T("err.stats403")))
	case BlocSchema:
		out = append(out, i18n.T("det.statsblocked", i18n.T("err.stats409")))
	}
	if t, ok := a.repliSchemaJusqu(); ok {
		out = append(out, i18n.T("det.statsschema1", t.Local().Format(i18n.T("fmt.datetime"))))
	}
	if n := a.Store.Get("stats_note"); n != "" {
		out = append(out, i18n.T("det.statsblocked", n))
	}
	if p := strings.SplitN(a.Store.Get("last_error"), "|", 2); len(p) == 2 {
		out = append(out, i18n.T("det.error", date(p[0]), p[1]))
	} else {
		out = append(out, i18n.T("det.noerror"))
	}
	out = append(out, a.auctionDetails()...)
	return out
}

// resumeCumul compte en SQL (0.7.4, le cumul n'est plus en mémoire). Etat ne
// l'appelle qu'après un changement du cumul ou de la langue. Une base illisible
// donne un résumé vide, jamais un faux total.
func (a *App) resumeCumul() []Portee {
	res, err := a.Store.ResumeCumul()
	if err != nil {
		a.Log.Printf("résumé du cumul illisible : %v", err)
		return nil
	}
	out := make([]Portee, 0, len(res))
	for _, r := range res {
		depuis := "?"
		if r.Depuis != nil {
			depuis = time.Unix(*r.Depuis, 0).Format(i18n.T("fmt.date"))
		}
		out = append(out, Portee{ScopeID: r.ScopeID, Total: r.Total, Depuis: depuis})
	}
	return out
}

// CumulDump : pour le test différentiel (le cumul est relu de la base).
func (a *App) CumulDump() ([]byte, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	c, err := a.Store.LoadCumul()
	if err != nil {
		return nil, err
	}
	return json.Marshal(c.Dump())
}
