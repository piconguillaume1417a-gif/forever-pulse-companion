// Package stats décode ForeverPulseStatsDB (statistiques de personnages) et le
// convertit au contrat d'envoi « forever-pulse-stats » schéma 2 (repli : schéma 1).
//
// Source : la troisième variable de ForeverPulse.lua, écrite par l'addon depuis la
// 3.5.0 (schéma de stockage v4 compact depuis la 3.6.0, référence StatsBase.lua) :
//
//	personnages[guid] = { n, r, c, ra, f, s, l, x, d, o, v = { "date|niveau|source|id:n,…" } }
//
// Contrat d'envoi (CONTRAT-DONNEES.md, POST /api/ingest/stats) : une fiche par
// personnage et par contexte, valeurs en texte décimal (aucune perte de précision),
// unités nommées (count, copper, percent), horodatages RFC 3339 UTC.
//
// Trois états ne sont jamais confondus :
//   - une valeur écrite, zéro compris, est transmise telle quelle ;
//   - une statistique absente d'une fiche n'est pas transmise (le site ne la
//     compte ni comme un zéro ni dans le dénominateur d'une moyenne) ;
//   - une valeur illisible est écartée et comptée (Fiche.Illisibles), jamais
//     remplacée par zéro.
package stats

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"wowsync/internal/lua"
	"wowsync/internal/schema"
)

const (
	// Variable : nom de la variable dans le fichier SavedVariables.
	Variable = "ForeverPulseStatsDB"
	// SchemaStockage : identifiant du schéma écrit par l'addon.
	SchemaStockage = "forever-pulse-stats"
	// VersionStockage : seule version du stockage que ce compagnon sait lire.
	VersionStockage = 4
	// SchemaEnvoi : version du contrat d'envoi (en-tête X-Schema). Le schéma 2
	// (0.9.0, talents) ajoute talent_trees, file.talents_version et
	// characters[].talents au schéma 1.
	SchemaEnvoi = 2
	// SchemaEnvoiRepli : schéma de repli quand le site ne connaît pas encore le 2.
	SchemaEnvoiRepli = 1
	// VersionCatalogue : version du catalogue exporté par l'addon (3.9.8).
	VersionCatalogue = 1

	maxValeursParFiche = 2000
	maxRelevesParFiche = 400
)

var (
	guidRe    = regexp.MustCompile(`^Player-[0-9]+-[0-9A-Fa-f]+$`)
	ctxRe     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$`)
	enteteRe  = regexp.MustCompile(`^([0-9]{1,12})\|([0-9]{0,3})\|([a-z]?)\|(.*)$`)
	entierRe  = regexp.MustCompile(`^-?[0-9]{1,30}$`)
	decimalRe = regexp.MustCompile(`^-?[0-9]{1,30}(\.[0-9]{1,20})?$`)
)

var sources = map[string]string{"n": "nameplate", "t": "target", "f": "focus", "m": "mouseover",
	"g": "group", "r": "raid", "s": "self", "": "unknown"}

var factions = map[string]string{"A": "Alliance", "H": "Horde", "N": "Neutral"}

// Valeur : une statistique lue. Texte est le nombre décimal exact écrit par l'addon.
type Valeur struct {
	ID    int64
	Texte string
	Unite string // count, copper, percent
}

// Releve : un groupe de v, c'est-à-dire les valeurs obtenues à une date donnée.
type Releve struct {
	T       int64
	Niveau  *int
	Source  string
	Valeurs []Valeur
}

// Fiche : un personnage dans un contexte.
type Fiche struct {
	ID         string // contexte/GUID : clé d'idempotence côté compagnon et côté site
	Contexte   string
	GUID       string
	Nom        string
	Royaume    string
	Classe     string
	Race       string
	Faction    string
	Sexe       *int
	Niveau     *int
	Vu         int64
	Lu         int64
	Releves    []Releve
	Valeurs    int // valeurs transmises
	Illisibles int // valeurs ou groupes écartés parce qu'illisibles
	Talents    *Talents // nil : spé inconnue (pas de ta lisible), clé absente du JSON
	Payload    []byte
	Empreinte  string
}

// Fichier : le résultat du décodage.
type Fichier struct {
	Present    bool // la variable existe dans le fichier
	Version    int
	Locale     string
	Contextes  schema.Obj // nom → description
	Catalogue  any        // nil si l'addon ne l'exporte pas (avant 3.9.8)
	Fiches     []*Fiche
	Total      int      // fiches présentes dans le fichier
	SansValeur int      // fiches sans aucune valeur lisible : rien à envoyer
	Ecartees   []string // motifs, sans nom de personnage
	Fatales    []string
	Infos      []string

	// 0.9.0 : talents (addon 4.2.0). TalentsVersion nil : l'addon n'en écrit pas.
	TalentsVersion    *int
	Arbres            []any // meta.talent_trees, jamais nil
	AvecTalents       int   // fiches envoyables portant un relevé de talents
	TalentsIllisibles int   // ta illisibles, ignorés (fiche gardée)
	TalentsHorsBornes int   // ta hors des bornes du site, ignorés (fiche gardée)
	ArbresIgnores     int   // arbres du catalogue écartés
	NoeudsIgnores     int   // nœuds (ou identifiants hors grille) écartés, arbre gardé
}

// Decode analyse la table ForeverPulseStatsDB. Une table absente n'est pas une
// erreur : l'addon antérieur à la 3.5.0 n'en écrit pas.
func Decode(db *lua.Table) *Fichier { return DecodeAt(db, time.Now()) }

// DecodeAt : comme Decode, à l'heure now (borne « 5 min dans le futur » des talents).
func DecodeAt(db *lua.Table, now time.Time) *Fichier {
	f := &Fichier{Arbres: []any{}}
	if db == nil {
		return f
	}
	f.Present = true
	if s := lua.Str(db.Get("schema")); s != SchemaStockage {
		f.Fatales = append(f.Fatales, fmt.Sprintf("schéma des statistiques inconnu : %q", s))
		return f
	}
	v, ok := lua.AsInt(db.Get("version"))
	f.Version = int(v)
	if !ok || v != VersionStockage {
		f.Fatales = append(f.Fatales, fmt.Sprintf("version %d du stockage des statistiques non prise en charge (attendu %d)", v, VersionStockage))
		return f
	}
	f.Locale = lua.Str(db.Get("locale"))
	// talents_v absent : pas de talents attendus (addon antérieur à 4.2.0) ; une
	// autre version : format inconnu, ni ta ni arbres ne sont lus.
	talents := false
	if x := db.Get("talents_v"); x != nil {
		if tv, ok := lua.AsInt(x); ok && tv == VersionTalents {
			v := VersionTalents
			f.TalentsVersion, talents = &v, true
		} else {
			f.Infos = append(f.Infos, "talents : version du format inconnue, talents non transmis")
		}
	}

	// Contextes : liste indexée (ctx) et description (contextes).
	var noms []string
	for _, x := range lua.AsTable(db.Get("ctx")).List() {
		noms = append(noms, lua.Str(x))
	}
	desc := lua.AsTable(db.Get("contextes"))
	utilises := map[string]bool{}

	persos := lua.AsTable(db.Get("personnages"))
	if persos == nil {
		f.Infos = append(f.Infos, "statistiques : aucune fiche")
		return f
	}
	guids := make([]string, 0, len(persos.Str))
	for g := range persos.Str {
		guids = append(guids, g)
	}
	sort.Strings(guids)
	f.Total = len(guids)
	motifs := map[string]int{}
	for _, guid := range guids {
		p := lua.AsTable(persos.Str[guid])
		fiche, motif := decodeFiche(guid, p, noms)
		if motif != "" {
			motifs[motif]++
			continue
		}
		if fiche.Valeurs == 0 {
			f.SansValeur++
			continue
		}
		if talents && p.Get("ta") != nil {
			t, illisible, horsBornes := decodeTalents(p.Get("ta"), now)
			switch {
			case illisible:
				f.TalentsIllisibles++
			case horsBornes:
				f.TalentsHorsBornes++
			default:
				fiche.Talents = t
				f.AvecTalents++
			}
		}
		utilises[fiche.Contexte] = true
		f.Fiches = append(f.Fiches, fiche)
	}
	for _, m := range sortedKeys(motifs) {
		f.Ecartees = append(f.Ecartees, fmt.Sprintf("%d fiches écartées : %s", motifs[m], m))
	}

	for _, nom := range noms {
		if !utilises[nom] {
			continue
		}
		f.Contextes = append(f.Contextes, schema.KV{K: nom, V: contexte(nom, lua.AsTable(desc.Get(nom)))})
	}
	f.Catalogue = catalogue(lua.AsTable(db.Get("cat")), f)
	if talents {
		f.Arbres = arbres(lua.AsTable(db.Get("arbres")), f)
		f.Infos = append(f.Infos, fmt.Sprintf("talents : %d fiches avec talents, %d illisibles et %d hors bornes ignorés ; %d arbres au catalogue, %d écartés, %d nœuds écartés",
			f.AvecTalents, f.TalentsIllisibles, f.TalentsHorsBornes, len(f.Arbres), f.ArbresIgnores, f.NoeudsIgnores))
	}

	for _, fiche := range f.Fiches {
		body, err := schema.Marshal(fiche.objet())
		if err != nil {
			f.Fatales = append(f.Fatales, "statistiques : fiche non sérialisable")
			return f
		}
		fiche.Payload = body
		sum := sha256.Sum256(body)
		fiche.Empreinte = hex.EncodeToString(sum[:])
	}
	valeurs, illisibles := 0, 0
	for _, fiche := range f.Fiches {
		valeurs += fiche.Valeurs
		illisibles += fiche.Illisibles
	}
	f.Infos = append(f.Infos, fmt.Sprintf("statistiques : %d fiches, %d envoyables (%d valeurs), %d sans valeur, %d valeurs illisibles écartées",
		f.Total, len(f.Fiches), valeurs, f.SansValeur, illisibles))
	return f
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func optInt(v any, min, max int64) (*int, bool) {
	if v == nil {
		return nil, true
	}
	n, ok := lua.AsInt(v)
	if !ok || n < min || n > max {
		return nil, false
	}
	i := int(n)
	return &i, true
}

func texteBorne(v any, max int) (string, bool) {
	if v == nil {
		return "", true
	}
	s, ok := v.(string)
	if !ok || len([]rune(s)) > max || strings.ContainsAny(s, "\x00\r\n\t") {
		return "", false
	}
	return s, true
}

func decodeFiche(guid string, p *lua.Table, contextes []string) (*Fiche, string) {
	if p == nil {
		return nil, "fiche qui n'est pas une table"
	}
	if !guidRe.MatchString(guid) || len(guid) > 60 {
		return nil, "GUID mal formé"
	}
	x, ok := lua.AsInt(p.Get("x"))
	if !ok || x < 1 || int(x) > len(contextes) || !ctxRe.MatchString(contextes[x-1]) {
		return nil, "contexte absent ou inconnu"
	}
	fi := &Fiche{GUID: guid, Contexte: contextes[x-1]}
	fi.ID = fi.Contexte + "/" + guid
	if fi.Nom, ok = texteBorne(p.Get("n"), 120); !ok || fi.Nom == "" {
		return nil, "nom absent ou mal formé"
	}
	if fi.Royaume, ok = texteBorne(p.Get("r"), 80); !ok {
		return nil, "royaume mal formé"
	}
	if fi.Classe, ok = texteBorne(p.Get("c"), 40); !ok {
		return nil, "classe mal formée"
	}
	if fi.Race, ok = texteBorne(p.Get("ra"), 40); !ok {
		return nil, "race mal formée"
	}
	if code := lua.Str(p.Get("f")); code != "" {
		if fi.Faction, ok = factions[code]; !ok {
			return nil, "faction inconnue"
		}
	}
	if fi.Sexe, ok = optInt(p.Get("s"), 0, 3); !ok {
		return nil, "sexe hors bornes"
	}
	if fi.Niveau, ok = optInt(p.Get("l"), 1, 100); !ok {
		return nil, "niveau hors bornes"
	}
	if d, ok := lua.AsInt(p.Get("d")); ok && d > 0 {
		fi.Vu = d
	}
	if o, ok := lua.AsInt(p.Get("o")); ok && o > 0 {
		fi.Lu = o
	}
	groupes := lua.AsTable(p.Get("v")).List()
	if len(groupes) > maxRelevesParFiche {
		return nil, "trop de relevés dans une fiche"
	}
	vus := map[int64]bool{}
	for _, g := range groupes {
		s, ok := g.(string)
		if !ok {
			fi.Illisibles++
			continue
		}
		m := enteteRe.FindStringSubmatch(s)
		if m == nil {
			fi.Illisibles++
			continue
		}
		t, _ := strconv.ParseInt(m[1], 10, 64)
		source, okSource := sources[m[3]]
		if t <= 0 || !okSource {
			fi.Illisibles++
			continue
		}
		r := Releve{T: t, Source: source}
		if m[2] != "" {
			n, _ := strconv.Atoi(m[2])
			if n >= 1 && n <= 100 {
				r.Niveau = &n
			}
		}
		for _, item := range strings.Split(m[4], ",") {
			if item == "" {
				continue
			}
			val, ok := decodeValeur(item)
			// Une statistique figure dans un seul groupe (StatsBase.EnregistrerReleve) ;
			// si un fichier forgé la répète, seul le groupe le plus récent (le premier) compte.
			if !ok || vus[val.ID] {
				fi.Illisibles++
				continue
			}
			vus[val.ID] = true
			r.Valeurs = append(r.Valeurs, val)
		}
		if len(r.Valeurs) == 0 {
			continue
		}
		sort.Slice(r.Valeurs, func(i, j int) bool { return r.Valeurs[i].ID < r.Valeurs[j].ID })
		fi.Valeurs += len(r.Valeurs)
		if fi.Valeurs > maxValeursParFiche {
			return nil, "trop de valeurs dans une fiche"
		}
		fi.Releves = append(fi.Releves, r)
	}
	// Ordre stable : relevé le plus récent d'abord, comme dans le fichier.
	sort.SliceStable(fi.Releves, func(i, j int) bool { return fi.Releves[i].T > fi.Releves[j].T })
	return fi, ""
}

// decodeValeur lit « id:nombre » suivi de « m » (monnaie, en cuivre) ou « p »
// (pourcentage) ; sans lettre : compteur.
func decodeValeur(item string) (Valeur, bool) {
	i := strings.IndexByte(item, ':')
	if i <= 0 || i > 12 {
		return Valeur{}, false
	}
	id, err := strconv.ParseInt(item[:i], 10, 64)
	if err != nil || id <= 0 {
		return Valeur{}, false
	}
	n, unite := item[i+1:], "count"
	switch {
	case strings.HasSuffix(n, "m"):
		n, unite = n[:len(n)-1], "copper"
	case strings.HasSuffix(n, "p"):
		n, unite = n[:len(n)-1], "percent"
	}
	texte, ok := nombre(n)
	if !ok {
		return Valeur{}, false
	}
	return Valeur{ID: id, Texte: texte, Unite: unite}, true
}

// nombre rend l'écriture décimale exacte d'un nombre de l'addon. Les entiers sont
// repris caractère pour caractère : aucun passage par un flottant.
func nombre(s string) (string, bool) {
	if entierRe.MatchString(s) {
		neg := strings.HasPrefix(s, "-")
		d := strings.TrimLeft(strings.TrimPrefix(s, "-"), "0")
		if d == "" {
			return "0", true
		}
		if neg {
			return "-" + d, true
		}
		return d, true
	}
	// « %.10g » de l'addon : décimal, ou notation exponentielle pour les très grands.
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f != f || f > 1e30 || f < -1e30 {
		return "", false
	}
	t := strconv.FormatFloat(f, 'f', -1, 64)
	if !decimalRe.MatchString(t) {
		return "", false
	}
	return t, true
}

func rfc3339(t int64) any {
	if t <= 0 {
		return nil
	}
	return time.Unix(t, 0).UTC().Format(time.RFC3339)
}

func chaineOuNil(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func entierOuNil(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func (fi *Fiche) objet() schema.Obj {
	releves := make([]any, 0, len(fi.Releves))
	for _, r := range fi.Releves {
		// values : unité → { identifiant de statistique → nombre décimal en texte }.
		// Les trois unités sont toujours présentes, vides s'il le faut.
		parUnite := map[string]schema.Obj{"count": {}, "copper": {}, "percent": {}}
		for _, v := range r.Valeurs {
			parUnite[v.Unite] = append(parUnite[v.Unite], schema.KV{K: strconv.FormatInt(v.ID, 10), V: v.Texte})
		}
		valeurs := schema.Obj{{K: "count", V: parUnite["count"]}, {K: "copper", V: parUnite["copper"]}, {K: "percent", V: parUnite["percent"]}}
		releves = append(releves, schema.Obj{{K: "read_at", V: rfc3339(r.T)}, {K: "level", V: entierOuNil(r.Niveau)},
			{K: "source", V: r.Source}, {K: "values", V: valeurs}})
	}
	o := schema.Obj{
		{K: "id", V: fi.ID}, {K: "context", V: fi.Contexte}, {K: "guid", V: fi.GUID},
		{K: "name", V: fi.Nom}, {K: "realm", V: chaineOuNil(fi.Royaume)},
		{K: "class", V: chaineOuNil(fi.Classe)}, {K: "race", V: chaineOuNil(fi.Race)},
		{K: "faction", V: chaineOuNil(fi.Faction)}, {K: "sex", V: entierOuNil(fi.Sexe)},
		{K: "level", V: entierOuNil(fi.Niveau)},
		{K: "seen_at", V: rfc3339(fi.Vu)}, {K: "read_at", V: rfc3339(fi.Lu)},
		{K: "readings", V: releves},
	}
	// Clé ajoutée seulement pour un ta lisible : une fiche sans talents garde
	// exactement la charge et l'empreinte de la 0.9.0-rc.1 (aucun renvoi massif).
	if fi.Talents != nil {
		o = append(o, schema.KV{K: "talents", V: fi.Talents.objet()})
	}
	return o
}

// contexte : description d'un contexte, champs absents rendus nil (jamais devinés).
func contexte(nom string, d *lua.Table) schema.Obj {
	o := schema.Obj{}
	texte := func(k string, max int) {
		if s, ok := texteBorne(d.Get(k), max); ok && s != "" {
			o = append(o, schema.KV{K: k, V: s})
		} else {
			o = append(o, schema.KV{K: k, V: nil})
		}
	}
	entier := func(k string) {
		if n, ok := lua.AsInt(d.Get(k)); ok && n >= 0 {
			o = append(o, schema.KV{K: k, V: n})
		} else {
			o = append(o, schema.KV{K: k, V: nil})
		}
	}
	texte("environment", 40)
	entier("realm_id")
	texte("ruleset", 40)
	texte("region", 16)
	texte("game_version", 40)
	entier("build")
	return o
}

// catalogue lit l'export de l'addon 3.9.8 :
//
//	cat = { v = 1, t = 1790…, locale = "enUS", build = 70170,
//	        cats = "id|parent|nom\n…", stats = "id|categorie|genre|nom\n…" }
//
// genre : « n » nombre (un zéro implicite est possible côté addon), « t » texte
// (jamais transmis). Un catalogue absent ou illisible rend nil : le site garde
// alors les identifiants sans libellé, il n'en invente pas.
func catalogue(c *lua.Table, f *Fichier) any {
	if c == nil {
		return nil
	}
	v, ok := lua.AsInt(c.Get("v"))
	if !ok || v != VersionCatalogue {
		f.Infos = append(f.Infos, "statistiques : catalogue d'une version inconnue, non transmis")
		return nil
	}
	var cats, stats []any
	for _, ligne := range schema.Lignes(lua.Str(c.Get("cats"))) {
		p := strings.SplitN(ligne, "|", 3)
		if len(p) != 3 {
			continue
		}
		id, err := strconv.ParseInt(p[0], 10, 64)
		if err != nil || id <= 0 || p[2] == "" || len([]rune(p[2])) > 120 {
			continue
		}
		var parent any
		if n, err := strconv.ParseInt(p[1], 10, 64); err == nil && n > 0 {
			parent = n
		}
		cats = append(cats, schema.Obj{{K: "id", V: id}, {K: "parent_id", V: parent}, {K: "name", V: p[2]}})
	}
	for _, ligne := range schema.Lignes(lua.Str(c.Get("stats"))) {
		p := strings.SplitN(ligne, "|", 4)
		if len(p) != 4 {
			continue
		}
		id, err := strconv.ParseInt(p[0], 10, 64)
		cat, err2 := strconv.ParseInt(p[1], 10, 64)
		if err != nil || err2 != nil || id <= 0 || cat <= 0 || p[3] == "" || len([]rune(p[3])) > 160 {
			continue
		}
		genre := "number"
		if p[2] == "t" {
			genre = "text"
		}
		stats = append(stats, schema.Obj{{K: "id", V: id}, {K: "category_id", V: cat}, {K: "kind", V: genre}, {K: "name", V: p[3]}})
	}
	if len(stats) == 0 || len(cats) == 0 || len(stats) > 5000 || len(cats) > 500 {
		f.Infos = append(f.Infos, "statistiques : catalogue vide ou hors bornes, non transmis")
		return nil
	}
	o := schema.Obj{{K: "version", V: VersionCatalogue}}
	if s, ok := texteBorne(c.Get("locale"), 10); ok && s != "" {
		o = append(o, schema.KV{K: "locale", V: s})
	} else {
		o = append(o, schema.KV{K: "locale", V: nil})
	}
	if n, ok := lua.AsInt(c.Get("build")); ok && n > 0 {
		o = append(o, schema.KV{K: "build", V: n})
	} else {
		o = append(o, schema.KV{K: "build", V: nil})
	}
	if t, ok := lua.AsInt(c.Get("t")); ok {
		o = append(o, schema.KV{K: "exported_at", V: rfc3339(t)})
	} else {
		o = append(o, schema.KV{K: "exported_at", V: nil})
	}
	o = append(o, schema.KV{K: "categories", V: cats}, schema.KV{K: "statistics", V: stats})
	f.Infos = append(f.Infos, fmt.Sprintf("statistiques : catalogue de %d catégories et %d statistiques", len(cats), len(stats)))
	return o
}

// Fraicheur : la date la plus récente portée par la fiche (vue, lue, relevés).
func (fi *Fiche) Fraicheur() int64 {
	t := fi.Vu
	if fi.Lu > t {
		t = fi.Lu
	}
	for _, r := range fi.Releves {
		if r.T > t {
			t = r.T
		}
	}
	if fi.Talents != nil && fi.Talents.T > t {
		t = fi.Talents.T
	}
	return t
}

// Meta : ce qui accompagne chaque envoi des fiches d'un même fichier, dans la
// forme du schéma 2 (file.talents_version, talent_trees). Le corps du schéma 1
// en retire ces deux clés (app.StatsBody).
func (f *Fichier) Meta(sha256hex, origin, addonVersion string) ([]byte, error) {
	arbres := f.Arbres
	if arbres == nil {
		arbres = []any{}
	}
	return schema.Marshal(schema.Obj{
		{K: "file", V: schema.Obj{{K: "sha256", V: sha256hex}, {K: "origin", V: origin},
			{K: "addon_version", V: addonVersion}, {K: "stats_version", V: f.Version}, {K: "locale", V: chaineOuNil(f.Locale)},
			{K: "talents_version", V: entierOuNil(f.TalentsVersion)}}},
		{K: "contexts", V: f.Contextes},
		{K: "catalog", V: f.Catalogue},
		{K: "talent_trees", V: arbres},
	})
}

// CorpsSchema rend meta (forme du schéma 2, ou forme antérieure à la 0.9.0) dans
// la forme du schéma demandé : schéma 2, file.talents_version et talent_trees
// toujours présents (null et [] par défaut) ; schéma 1, ces deux clés retirées,
// tout le reste identique octet pour octet.
func CorpsSchema(meta []byte, schemaEnvoi int) ([]byte, error) {
	ms, err := schema.Membres(meta)
	if err != nil {
		return nil, err
	}
	out := make([]schema.Membre, 0, len(ms)+1)
	arbres := false
	for _, m := range ms {
		switch m.K {
		case "talent_trees":
			if schemaEnvoi == SchemaEnvoiRepli {
				continue
			}
			arbres = true
		case "file":
			fm, err := schema.Membres(m.V)
			if err != nil {
				return nil, err
			}
			garde := make([]schema.Membre, 0, len(fm)+1)
			version := false
			for _, x := range fm {
				if x.K == "talents_version" {
					if schemaEnvoi == SchemaEnvoiRepli {
						continue
					}
					version = true
				}
				garde = append(garde, x)
			}
			if schemaEnvoi != SchemaEnvoiRepli && !version {
				garde = append(garde, schema.Membre{K: "talents_version", V: []byte("null")})
			}
			if len(garde) != len(fm) {
				m.V = schema.Assemble(garde)
			}
		}
		out = append(out, m)
	}
	if schemaEnvoi != SchemaEnvoiRepli && !arbres {
		out = append(out, schema.Membre{K: "talent_trees", V: []byte("[]")})
	}
	return schema.Assemble(out), nil
}

// FicheSchema rend la charge d'une fiche pour le schéma demandé : schéma 1, sans
// talents ; schéma 2, telle quelle.
func FicheSchema(payload []byte, schemaEnvoi int) ([]byte, error) {
	if schemaEnvoi != SchemaEnvoiRepli {
		return payload, nil
	}
	return schema.SansCles(payload, "talents")
}
