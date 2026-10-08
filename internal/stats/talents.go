package stats

// Talents des joueurs croisés (addon 4.2.0, contrat « spécialisations » v1, §3).
//
// Deux nouvelles clés de ForeverPulseStatsDB, ignorées par les compagnons
// antérieurs ; la version du stockage reste 4 :
//
//	personnages[guid].ta = "<t>|<niveau>|<source>|<arbre>|<b1>,<b2>,<b3>|<dom>|<hg>|<paires>"
//	arbres[<treeID>]     = { v = 1, t, build, locale, c, n = "<nœud>|<x>|<y>|<max>|<branche>|<spell>|<nom>[|<e>:<s>;…]\n…", hg = "<nœud>,…" }
//	talents_v            = 1
//
// Le compagnon valide les mêmes bornes que le site (contrat §4.2) : un relevé de
// talents illisible ou hors bornes est écarté et compté, la fiche part sans lui ;
// un arbre hors bornes est écarté et compté (le site refuserait tout le corps).

import (
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
	// VersionTalents : seule version du sous-format talents (talents_v) lue.
	VersionTalents = 1
	// VersionArbres : seule version d'un arbre du catalogue (arbres[id].v) lue.
	VersionArbres = 1

	maxPairesTalents  = 200
	maxArbres         = 64
	maxNoeudsParArbre = 500
	maxHorsGrille     = 500
	maxEntrees        = 8
	maxNomNoeud       = 200
	maxCoordonnee     = 10_000_000
	maxTreeID         = 2_147_483_647
	maxHorsGrilleFich = 200
	// Le catalogue part avec chaque requête de statistiques : il reste petit
	// devant le plafond de 3 Mo d'un envoi.
	maxOctetsArbres = 1 << 20
	// Le site refuse un relevé daté de plus de 5 min dans le futur.
	avanceMax = 5 * time.Minute
)

var (
	taRe      = regexp.MustCompile(`^([0-9]{1,12})\|([0-9]{0,3})\|([a-z]?)\|([0-9]{1,9})\|([0-9]{1,3}),([0-9]{1,3}),([0-9]{1,3})\|([0-3])\|([0-9]{1,3})\|(.*)$`)
	paireRe   = regexp.MustCompile(`^([0-9]{1,9}):([0-9]{1,2})(?:@([0-9]{1,9}))?$`)
	classeRe  = regexp.MustCompile(`^[A-Z_]{1,40}$`)
	entreesRe = regexp.MustCompile(`^[0-9]{1,10}:[0-9]{0,10}(;[0-9]{1,10}:[0-9]{0,10})*$`)
)

// Le source des talents ne peut être que l'une de ces quatre unités.
var sourcesTalents = map[string]string{"n": "nameplate", "t": "target", "f": "focus", "m": "mouseover"}

// Point : un nœud avec des points ; Entree = 0 si inconnue (null).
type Point struct {
	Noeud  int64
	Points int
	Entree int64
}

// Talents : le dernier relevé de talents d'un personnage.
type Talents struct {
	T          int64
	Niveau     *int
	Source     string
	Arbre      int64
	Points     []Point
	Branches   [3]int
	Dominante  int
	HorsGrille int
}

// decodeTalents lit ta. illisible : la chaîne ne suit pas le format ; horsBornes :
// elle le suit mais une valeur sort des bornes du site.
func decodeTalents(v any, now time.Time) (t *Talents, illisible, horsBornes bool) {
	s, ok := v.(string)
	if !ok {
		return nil, true, false
	}
	m := taRe.FindStringSubmatch(s)
	if m == nil {
		return nil, true, false
	}
	var paires []Point
	if m[10] != "" {
		items := strings.Split(m[10], ",")
		if len(items) > maxPairesTalents {
			return nil, false, true
		}
		for _, it := range items {
			p := paireRe.FindStringSubmatch(it)
			if p == nil {
				return nil, true, false
			}
			n, _ := strconv.ParseInt(p[1], 10, 64)
			pts, _ := strconv.Atoi(p[2])
			pt := Point{Noeud: n, Points: pts}
			if p[3] != "" {
				// Entrée écrite : 1 ou plus (contrat §7.2) ; « @0 » est hors bornes.
				if pt.Entree, _ = strconv.ParseInt(p[3], 10, 64); pt.Entree < 1 {
					return nil, false, true
				}
			}
			paires = append(paires, pt)
		}
	}
	t = &Talents{}
	t.T, _ = strconv.ParseInt(m[1], 10, 64)
	if t.T <= 0 || time.Unix(t.T, 0).After(now.Add(avanceMax)) {
		return nil, false, true
	}
	if m[2] != "" {
		n, _ := strconv.Atoi(m[2])
		if n < 1 || n > 100 {
			return nil, false, true
		}
		t.Niveau = &n
	}
	if t.Source, ok = sourcesTalents[m[3]]; !ok {
		return nil, false, true
	}
	t.Arbre, _ = strconv.ParseInt(m[4], 10, 64)
	if t.Arbre < 1 || t.Arbre > maxTreeID {
		return nil, false, true
	}
	for i := 0; i < 3; i++ {
		t.Branches[i], _ = strconv.Atoi(m[5+i]) // 0–999 par l'expression
	}
	t.Dominante, _ = strconv.Atoi(m[8])
	t.HorsGrille, _ = strconv.Atoi(m[9])
	if t.HorsGrille > maxHorsGrilleFich {
		return nil, false, true
	}
	// Nœuds distincts, triés croissants (l'addon les écrit déjà ainsi), 1 à 99 points.
	sort.SliceStable(paires, func(i, j int) bool { return paires[i].Noeud < paires[j].Noeud })
	for i, p := range paires {
		if p.Noeud < 1 || p.Points < 1 || p.Points > 99 || (i > 0 && paires[i-1].Noeud == p.Noeud) {
			return nil, false, true
		}
	}
	t.Points = paires
	return t, false, false
}

func (t *Talents) objet() schema.Obj {
	points := make([]any, 0, len(t.Points))
	for _, p := range t.Points {
		var e any
		if p.Entree > 0 {
			e = p.Entree
		}
		points = append(points, []any{p.Noeud, p.Points, e})
	}
	return schema.Obj{
		{K: "read_at", V: rfc3339(t.T)}, {K: "level", V: entierOuNil(t.Niveau)},
		{K: "source", V: t.Source}, {K: "tree_id", V: t.Arbre}, {K: "points", V: points},
		{K: "branch_points", V: []int{t.Branches[0], t.Branches[1], t.Branches[2]}},
		{K: "dominant", V: t.Dominante}, {K: "off_grid", V: t.HorsGrille},
	}
}

// arbres lit le catalogue des arbres de talents. Rend la liste pour
// meta.talent_trees (jamais nil). Une clé qui n'est pas un identifiant entier
// (« ["1114"] » à côté de « [1114] ») est écartée et comptée.
func arbres(a *lua.Table, f *Fichier) []any {
	out := []any{}
	if a == nil {
		return out
	}
	n := len(a.Int) + len(a.Str) + len(a.Other)
	if n == 0 {
		return out
	}
	if n > maxArbres {
		f.ArbresIgnores += n
		f.Infos = append(f.Infos, fmt.Sprintf("talents : %d arbres au catalogue (plus de %d), catalogue non transmis", n, maxArbres))
		return out
	}
	f.ArbresIgnores += len(a.Str) + len(a.Other) // clé qui n'est pas un identifiant d'arbre
	ids := make([]int64, 0, len(a.Int))
	for id := range a.Int {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	taille := 0
	vus := map[int64]bool{} // défense : un tree_id ne part qu'une fois
	for _, id := range ids {
		if vus[id] {
			f.ArbresIgnores++
			continue
		}
		vus[id] = true
		o, motif := arbre(id, lua.AsTable(a.Int[id]), f)
		if motif != "" {
			f.ArbresIgnores++
			f.Infos = append(f.Infos, fmt.Sprintf("talents : arbre %d écarté (%s)", id, motif))
			continue
		}
		b, err := schema.Marshal(o)
		if err != nil || taille+len(b) > maxOctetsArbres {
			f.ArbresIgnores++
			f.Infos = append(f.Infos, fmt.Sprintf("talents : arbre %d écarté (catalogue au-delà de %d octets)", id, maxOctetsArbres))
			continue
		}
		taille += len(b)
		out = append(out, o)
	}
	return out
}

// arbre lit un arbre du catalogue. Contrat §7.2 : un nœud illisible, hors bornes
// ou en double est écarté et compté (f.NoeudsIgnores), l'arbre est gardé ; un
// champ facultatif de l'arbre hors bornes (classe, build, langue) devient null ;
// sans version connue, sans date d'export ou sans aucun nœud valable, l'arbre
// est écarté.
func arbre(id int64, a *lua.Table, f *Fichier) (schema.Obj, string) {
	if a == nil {
		return nil, "pas une table"
	}
	if id < 1 || id > maxTreeID {
		return nil, "identifiant hors bornes"
	}
	if v, ok := lua.AsInt(a.Get("v")); !ok || v != VersionArbres {
		return nil, "version inconnue"
	}
	t, ok := lua.AsInt(a.Get("t"))
	if !ok || t <= 0 {
		return nil, "date d'export absente"
	}
	note := func(champ string) {
		f.Infos = append(f.Infos, fmt.Sprintf("talents : arbre %d, %s hors bornes, transmis null", id, champ))
	}
	var classe, build, locale any
	if x := a.Get("c"); x != nil {
		if s, _ := x.(string); classeRe.MatchString(s) {
			classe = s
		} else {
			note("classe")
		}
	}
	if x := a.Get("build"); x != nil {
		if n, ok := lua.AsInt(x); ok && n >= 0 && n <= maxTreeID {
			build = n
		} else {
			note("build")
		}
	}
	if x := a.Get("locale"); x != nil {
		if s, ok := texteBorne(x, 10); ok {
			locale = chaineOuNil(s)
		} else {
			note("langue")
		}
	}
	horsGrille := []int64{}
	if x := a.Get("hg"); x != nil {
		s, _ := x.(string)
		vus := map[int64]bool{}
		for _, it := range strings.Split(s, ",") {
			if it = strings.TrimSpace(it); it == "" {
				continue
			}
			n, err := strconv.ParseInt(it, 10, 64)
			if err != nil || n < 1 || n > maxTreeID {
				f.NoeudsIgnores++ // identifiant hors grille illisible : écarté seul
				continue
			}
			if !vus[n] {
				vus[n] = true
				horsGrille = append(horsGrille, n)
			}
		}
		if _, ok := x.(string); !ok {
			f.NoeudsIgnores++
		}
		sort.Slice(horsGrille, func(i, j int) bool { return horsGrille[i] < horsGrille[j] })
		if len(horsGrille) > maxHorsGrille {
			f.NoeudsIgnores += len(horsGrille) - maxHorsGrille
			horsGrille = horsGrille[:maxHorsGrille]
		}
	}
	var lignes []string
	for _, l := range schema.Lignes(lua.Str(a.Get("n"))) {
		if l = strings.TrimSuffix(l, "\r"); l != "" { // ligne vide (fin de texte) : rien
			lignes = append(lignes, l)
		}
	}
	if len(lignes) > maxNoeudsParArbre {
		return nil, "trop de nœuds"
	}
	type noeud struct {
		id int64
		o  schema.Obj
	}
	noeuds := make([]noeud, 0, len(lignes))
	occurrences := map[int64]int{}
	for _, l := range lignes {
		id, o, ok := ligneNoeud(l)
		if !ok {
			f.NoeudsIgnores++
			continue
		}
		occurrences[id]++
		noeuds = append(noeuds, noeud{id, o})
	}
	// Un nœud écrit deux fois est ambigu : toutes ses lignes sont écartées.
	garde := noeuds[:0]
	for _, n := range noeuds {
		if occurrences[n.id] > 1 {
			f.NoeudsIgnores++
			continue
		}
		garde = append(garde, n)
	}
	if len(garde) == 0 {
		return nil, "aucun nœud valable"
	}
	sort.Slice(garde, func(i, j int) bool { return garde[i].id < garde[j].id })
	liste := make([]any, len(garde))
	for i, n := range garde {
		liste[i] = n.o
	}
	return schema.Obj{{K: "tree_id", V: id}, {K: "class", V: classe}, {K: "build", V: build},
		{K: "locale", V: locale}, {K: "exported_at", V: rfc3339(t)}, {K: "off_grid", V: horsGrille},
		{K: "nodes", V: liste}}, ""
}

// ligneNoeud lit « nœud|x|y|max|branche|spell|nom[|e:s;e:s] ». Le nom peut porter
// un « | » : le dernier champ n'est pris pour les entrées que s'il en a la forme.
func ligneNoeud(l string) (int64, schema.Obj, bool) {
	p := strings.SplitN(l, "|", 7)
	if len(p) != 7 {
		return 0, nil, false
	}
	entier := func(s string, min, max int64) (int64, bool) {
		n, err := strconv.ParseInt(s, 10, 64)
		return n, err == nil && n >= min && n <= max
	}
	id, ok1 := entier(p[0], 1, maxTreeID)
	x, ok2 := entier(p[1], -maxCoordonnee, maxCoordonnee)
	y, ok3 := entier(p[2], -maxCoordonnee, maxCoordonnee)
	rangs, ok4 := entier(p[3], 1, 99)
	branche, ok5 := entier(p[4], 1, 3)
	if !(ok1 && ok2 && ok3 && ok4 && ok5) {
		return 0, nil, false
	}
	var spell any
	if p[5] != "" {
		n, ok := entier(p[5], 0, maxTreeID)
		if !ok {
			return 0, nil, false
		}
		spell = n
	}
	nom, brut := p[6], ""
	if i := strings.LastIndexByte(p[6], '|'); i >= 0 && entreesRe.MatchString(p[6][i+1:]) {
		nom, brut = p[6][:i], p[6][i+1:]
	}
	if len([]rune(nom)) > maxNomNoeud || strings.ContainsFunc(nom, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return 0, nil, false
	}
	var entrees any
	if brut != "" {
		items := strings.Split(brut, ";")
		if len(items) > maxEntrees {
			return 0, nil, false
		}
		l := make([]any, 0, len(items))
		for _, it := range items {
			es := strings.SplitN(it, ":", 2)
			e, ok := entier(es[0], 1, maxTreeID)
			if !ok {
				return 0, nil, false
			}
			var s any
			if es[1] != "" {
				n, ok := entier(es[1], 0, maxTreeID)
				if !ok {
					return 0, nil, false
				}
				s = n
			}
			l = append(l, []any{e, s})
		}
		entrees = l
	}
	return id, schema.Obj{{K: "id", V: id}, {K: "x", V: x}, {K: "y", V: y}, {K: "max_ranks", V: rangs},
		{K: "branch", V: branche}, {K: "spell_id", V: spell}, {K: "name", V: chaineOuNil(nom)},
		{K: "entries", V: entrees}}, true
}
