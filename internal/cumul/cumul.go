// Package cumul tient le cumul total sans doublon, toutes sources.
//
// Portage de forever-pulse-roadmap/outils/cumul.py (SPEC §6). Le client Forever ne
// recharge pas les SavedVariables : chaque fichier ne contient qu'une session. Le
// compagnon voit passer tous les fichiers, c'est donc lui qui tient le cumul.
//
//   - un cumul par périmètre (scope_id), deux périmètres ne se mélangent jamais ;
//   - clé = GUID pour les trois sources roster ; le /who (sans GUID) est rattaché par
//     le nom affiché complet, sinon gardé sous « nom:<nom> » et fusionné dès qu'un
//     roster apporte le GUID ;
//   - jamais de régression : l'observation la plus récente (observed_at) gagne ;
//   - idempotent par batch_id ; rien n'est supprimé.
//
// Vocabulaire : « personnages distincts observés depuis <date> ». Jamais une
// population, jamais additionné entre périmètres.
package cumul

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode"
)

const (
	SrcZone  = "zone"
	SrcCanal = "canal"
	SrcMonde = "monde"
	SrcWho   = "who"

	JoursActifs = 30
)

// champs qui suivent la règle « la plus récente gagne » (_maj), et ceux que la fusion reprend.
var champsMaj = []string{"class", "race", "level", "class_loc", "race_loc", "zone"}
var champsFusion = []string{"class", "race", "level", "class_loc", "race_loc", "zone", "guild"}

// Entree : un personnage du cumul. Sérialisée exactement comme le dict Python.
type Entree struct {
	GUID        *string
	Sources     []string
	Premier     *int64
	Dernier     *int64
	DisplayName *string
	NomT        *int64
	Vals        map[string]any   // class, race, level (int), class_loc, race_loc, zone, guild
	Ts          map[string]int64 // <champ>_t
}

func nouvelleEntree(guid *string) *Entree {
	return &Entree{GUID: guid, Sources: []string{}, Vals: map[string]any{}, Ts: map[string]int64{}}
}

func (e *Entree) t(champ string) int64 {
	if v, ok := e.Ts[champ]; ok {
		return v
	}
	return -1
}

func (e *Entree) MarshalJSON() ([]byte, error) {
	m := map[string]any{"guid": e.GUID, "sources": e.Sources}
	if e.Premier != nil {
		m["premier"] = *e.Premier
	}
	if e.Dernier != nil {
		m["dernier"] = *e.Dernier
	}
	if e.DisplayName != nil {
		m["display_name"] = *e.DisplayName
	}
	if e.NomT != nil {
		m["nom_t"] = *e.NomT
	}
	for k, v := range e.Vals {
		m[k] = v
	}
	for k, v := range e.Ts {
		m[k+"_t"] = v
	}
	return json.Marshal(m)
}

func (e *Entree) UnmarshalJSON(b []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	*e = *nouvelleEntree(nil)
	for k, raw := range m {
		switch {
		case k == "guid":
			_ = json.Unmarshal(raw, &e.GUID)
		case k == "sources":
			_ = json.Unmarshal(raw, &e.Sources)
			if e.Sources == nil {
				e.Sources = []string{}
			}
		case k == "premier":
			_ = json.Unmarshal(raw, &e.Premier)
		case k == "dernier":
			_ = json.Unmarshal(raw, &e.Dernier)
		case k == "display_name":
			_ = json.Unmarshal(raw, &e.DisplayName)
		case k == "nom_t":
			_ = json.Unmarshal(raw, &e.NomT)
		case strings.HasSuffix(k, "_t"):
			var n int64
			_ = json.Unmarshal(raw, &n)
			e.Ts[strings.TrimSuffix(k, "_t")] = n
		case k == "level":
			var n *int
			_ = json.Unmarshal(raw, &n)
			if n != nil {
				e.Vals[k] = *n
			} else {
				e.Vals[k] = nil
			}
		default:
			var s *string
			_ = json.Unmarshal(raw, &s)
			if s != nil {
				e.Vals[k] = *s
			} else {
				e.Vals[k] = nil
			}
		}
	}
	return nil
}

// Portee : le cumul d'un périmètre.
type Portee struct {
	Depuis *int64
	Persos map[string]*Entree
	Noms   map[string]string
}

// Obs : une observation d'un personnage.
type Obs struct {
	GUID        string // vide pour le /who
	DisplayName string
	Vals        map[string]any // class, race, level, class_loc, race_loc, zone
	Guild       *string        // seul le /who la donne ; "" = « sans guilde », une mesure
	Source      string
}

// Stats : compteurs d'une intégration (mêmes noms que la référence).
type Stats struct {
	Lots         int `json:"lots"`
	LotsDeja     int `json:"lots_deja"`
	Observations int `json:"observations"`
	Nouveaux     int `json:"nouveaux"`
	Fusions      int `json:"fusions"`
	Renommages   int `json:"renommages"`
	NomsRepris   int `json:"noms_repris"`
	WhoRattaches int `json:"who_rattaches"`
}

// Cumul : l'état complet, avec le suivi de ce qui a changé depuis le dernier
// enregistrement (pour ne réécrire que les lignes touchées dans SQLite).
type Cumul struct {
	LotsTraites []string
	deja        map[string]bool
	Scopes      map[string]*Portee

	sale      map[[2]string]bool // (scope, clé) de personnage modifié
	supprime  map[[2]string]bool // (scope, clé) de personnage retiré (fusion)
	nomsSales map[[2]string]bool // (scope, nom) modifié ou retiré
	scopesSal map[string]bool
	lotsNeufs []string
}

// Nouveau : cumul vide.
func Nouveau() *Cumul {
	c := &Cumul{Scopes: map[string]*Portee{}, deja: map[string]bool{}}
	c.resetSale()
	return c
}

func (c *Cumul) resetSale() {
	c.sale = map[[2]string]bool{}
	c.supprime = map[[2]string]bool{}
	c.nomsSales = map[[2]string]bool{}
	c.scopesSal = map[string]bool{}
	c.lotsNeufs = nil
}

// Deja : ce lot a-t-il déjà été intégré ?
func (c *Cumul) Deja(batchID string) bool { return c.deja[batchID] }

func (c *Cumul) portee(sid string) *Portee {
	p, ok := c.Scopes[sid]
	if !ok {
		p = &Portee{Persos: map[string]*Entree{}, Noms: map[string]string{}}
		c.Scopes[sid] = p
	}
	return p
}

// CleNom : (nom or "").strip().lower(), avec les deux cas où Python diffère de Go.
func CleNom(nom string) string {
	s := strings.TrimFunc(nom, func(r rune) bool { return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f) })
	s = strings.ReplaceAll(s, "İ", "i̇") // İ → i + point, comme Python
	return strings.ToLower(s)
}

func insereSource(e *Entree, s string) {
	for _, x := range e.Sources {
		if x == s {
			return
		}
	}
	e.Sources = append(e.Sources, s)
	sort.Strings(e.Sources)
}

func vide(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && s == ""
}

func maj(e *Entree, o Obs, t int64) {
	if e.Premier == nil || t < *e.Premier {
		e.Premier = &t
	}
	if e.Dernier == nil || t > *e.Dernier {
		tt := t
		e.Dernier = &tt
	}
	if o.Source != "" {
		insereSource(e, o.Source)
	}
	for _, champ := range champsMaj {
		v := o.Vals[champ]
		if vide(v) {
			continue
		}
		if t >= e.t(champ) {
			e.Vals[champ] = v
			e.Ts[champ] = t
		}
	}
	if o.Guild != nil && t >= e.t("guild") {
		e.Vals["guild"] = *o.Guild
		e.Ts["guild"] = t
	}
	nt := int64(-1)
	if e.NomT != nil {
		nt = *e.NomT
	}
	if o.DisplayName != "" && t >= nt {
		dn := o.DisplayName
		e.DisplayName = &dn
		tt := t
		e.NomT = &tt
	}
}

func fusion(dst, src *Entree) {
	if src.Premier != nil && (dst.Premier == nil || *src.Premier < *dst.Premier) {
		v := *src.Premier
		dst.Premier = &v
	}
	if src.Dernier != nil && (dst.Dernier == nil || *src.Dernier > *dst.Dernier) {
		v := *src.Dernier
		dst.Dernier = &v
	}
	for _, s := range src.Sources {
		insereSource(dst, s)
	}
	sort.Strings(dst.Sources)
	for _, champ := range champsFusion {
		v, ok := src.Vals[champ]
		if ok && src.t(champ) > dst.t(champ) {
			dst.Vals[champ] = v
			dst.Ts[champ] = src.t(champ)
		}
	}
}

// Observer ajoute une observation au cumul du périmètre sid.
func (c *Cumul) Observer(sid string, o Obs, t int64, st *Stats) {
	p := c.portee(sid)
	kn := CleNom(o.DisplayName)
	marque := func(cle string) { c.sale[[2]string{sid, cle}] = true; delete(c.supprime, [2]string{sid, cle}) }
	nomSale := func(n string) { c.nomsSales[[2]string{sid, n}] = true }
	if o.GUID != "" {
		guid := o.GUID
		e := p.Persos[guid]
		if e == nil {
			g := guid
			e = nouvelleEntree(&g)
			p.Persos[guid] = e
			st.Nouveaux++
		}
		if e.DisplayName != nil && *e.DisplayName != "" {
			ka := CleNom(*e.DisplayName)
			if ka != kn && p.Noms[ka] == guid {
				delete(p.Noms, ka) // renommage : l'ancien nom ne pointe plus ici
				nomSale(ka)
				st.Renommages++
			}
		}
		if cible, ok := p.Noms[kn]; ok && cible != "" && cible != guid {
			if src, existe := p.Persos[cible]; strings.HasPrefix(cible, "nom:") && existe {
				fusion(e, src) // le /who avait vu ce personnage avant le GUID
				delete(p.Persos, cible)
				c.supprime[[2]string{sid, cible}] = true
				delete(c.sale, [2]string{sid, cible})
				st.Fusions++
				st.Nouveaux--
			} else {
				st.NomsRepris++ // nom repris par un autre GUID
			}
		}
		if kn != "" {
			p.Noms[kn] = guid
			nomSale(kn)
		}
		maj(e, o, t)
		marque(guid)
		return
	}
	if cible, ok := p.Noms[kn]; ok && cible != "" {
		if e, existe := p.Persos[cible]; existe {
			maj(e, o, t)
			marque(cible)
			if !strings.HasPrefix(cible, "nom:") {
				st.WhoRattaches++
			}
			return
		}
	}
	cle := "nom:" + kn
	e := p.Persos[cle]
	if e == nil {
		e = nouvelleEntree(nil)
		p.Persos[cle] = e
		st.Nouveaux++
	}
	p.Noms[kn] = cle
	nomSale(kn)
	maj(e, o, t)
	marque(cle)
}

// Lot : ce que le cumul lit d'un lot valide.
type Lot struct {
	BatchID    string
	ScopeID    string
	ObservedAt int64
	Source     string
	Roster     []RosterPerso
	Who        []WhoPerso
}

type RosterPerso struct {
	GUID, DisplayName, Class, Race string
	Level                          *int
	Zone                           *string
}

type WhoPerso struct {
	Name, ClassLoc, RaceLoc, Guild, Zone string
	Level                                *int
}

// Source : zone, canal, monde ou who, d'après la méthode et la portée.
func Source(method, sourceScope string) string {
	if method == "who_manual" {
		return SrcWho
	}
	switch sourceScope {
	case "realm_channel":
		return SrcCanal
	case "world_channel":
		return SrcMonde
	}
	return SrcZone
}

func lvl(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

// Integrer ajoute les lots d'un fichier (triés par observed_at, tri stable).
func (c *Cumul) Integrer(lots []Lot) Stats {
	var st Stats
	ordre := append([]Lot(nil), lots...)
	sort.SliceStable(ordre, func(i, j int) bool { return ordre[i].ObservedAt < ordre[j].ObservedAt })
	for _, l := range ordre {
		if c.deja[l.BatchID] {
			st.LotsDeja++
			continue
		}
		sid := l.ScopeID
		if sid == "" {
			sid = "?"
		}
		p := c.portee(sid)
		t := l.ObservedAt
		if p.Depuis == nil || t < *p.Depuis {
			tt := t
			p.Depuis = &tt
			c.scopesSal[sid] = true
		}
		if l.Source == SrcWho {
			for _, w := range l.Who {
				g := w.Guild
				c.Observer(sid, Obs{DisplayName: w.Name, Source: SrcWho, Guild: &g, Vals: map[string]any{
					"class_loc": w.ClassLoc, "race_loc": w.RaceLoc, "level": lvl(w.Level), "zone": w.Zone}}, t, &st)
				st.Observations++
			}
		} else {
			for _, r := range l.Roster {
				var zone any
				if r.Zone != nil {
					zone = *r.Zone
				}
				c.Observer(sid, Obs{GUID: r.GUID, DisplayName: r.DisplayName, Source: l.Source, Vals: map[string]any{
					"class": r.Class, "race": r.Race, "level": lvl(r.Level), "zone": zone}}, t, &st)
				st.Observations++
			}
		}
		c.LotsTraites = append(c.LotsTraites, l.BatchID)
		c.deja[l.BatchID] = true
		c.lotsNeufs = append(c.lotsNeufs, l.BatchID)
		st.Lots++
	}
	return st
}

// Resume : les chiffres d'un périmètre (mêmes clés que la référence).
type Resume struct {
	Depuis       *int64         `json:"depuis"`
	Total        int            `json:"total"`
	Actifs30j    int            `json:"actifs_30j"`
	AvecGUID     int            `json:"avec_guid"`
	SeulementWho int            `json:"seulement_who"`
	GuildeConnue int            `json:"guilde_connue"`
	ParSources   map[string]int `json:"par_sources"`
}

func (c *Cumul) Resume(maintenant int64) map[string]Resume {
	out := map[string]Resume{}
	for sid, p := range c.Scopes {
		r := Resume{Depuis: p.Depuis, ParSources: map[string]int{}}
		for _, e := range p.Persos {
			r.Total++
			k := strings.Join(e.Sources, "+")
			r.ParSources[k]++
			if e.Dernier != nil && *e.Dernier >= maintenant-JoursActifs*86400 {
				r.Actifs30j++
			}
			if e.GUID != nil && *e.GUID != "" {
				r.AvecGUID++
			}
			if g, ok := e.Vals["guild"].(string); ok && g != "" {
				r.GuildeConnue++
			}
		}
		r.SeulementWho = r.ParSources[SrcWho]
		out[sid] = r
	}
	return out
}

// Dump : le cumul au format JSON de la référence Python (test différentiel).
func (c *Cumul) Dump() map[string]any {
	scopes := map[string]any{}
	for sid, p := range c.Scopes {
		persos := map[string]any{}
		for k, e := range p.Persos {
			persos[k] = e
		}
		scopes[sid] = map[string]any{"depuis": p.Depuis, "persos": persos, "noms": p.Noms}
	}
	return map[string]any{"version": 1, "lots_traites": c.LotsTraites, "scopes": scopes}
}
