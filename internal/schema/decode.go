// Package schema décode le schéma 4 de ForeverPulseCensusDB.
//
// Portage de forever-pulse-roadmap/outils/schema.py (decode_lot, reconstruit) et
// des contrôles de valide_lots.py qui protègent l'envoi. Chaque lot se décode
// d'après ses propres en-têtes : `fields`, `guid_prefix`, dictionnaires `classes`
// et `races` (indices à partir de 1).
package schema

import (
	"fmt"
	"strconv"
	"strings"

	"wowsync/internal/lua"
)

const (
	ChampsRoster = "id|surname|name|class|race|sex|level"
	ChampsWho    = "name|class_loc|race_loc|level|guild|zone"
)

// Perso : une ligne de roster décodée.
type Perso struct {
	ID          string
	GUID        string
	Name        string
	DisplayName string
	Class       string
	Race        string
	Sex         string
	Level       *int
	Realm       string
}

// WhoLigne : une ligne de /who décodée.
type WhoLigne struct {
	Name     string
	ClassLoc string
	RaceLoc  string
	Level    *int
	Guild    string
	Zone     string
}

// SchemaError : lot non décodable.
type SchemaError struct{ Msg string }

func (e *SchemaError) Error() string { return e.Msg }

func schemaErr(format string, a ...any) error { return &SchemaError{fmt.Sprintf(format, a...)} }

// Lignes découpe une chaîne `rows` / `gone` (vide = aucune ligne).
func Lignes(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func nomAffiche(surname, name string) string {
	if surname == "" {
		return name
	}
	if strings.HasPrefix(surname, "=") {
		return surname[1:]
	}
	if name != "" {
		return name + " " + surname
	}
	return surname
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

func stringList(v any) ([]string, error) {
	t := lua.AsTable(v)
	var out []string
	for _, x := range t.List() {
		s, ok := x.(string)
		if !ok {
			return nil, schemaErr("dictionnaire non textuel")
		}
		out = append(out, s)
	}
	return out, nil
}

// DecodeLot renvoie les personnages d'un lot roster (pour un delta : arrivées et
// changements seulement).
func DecodeLot(lot *lua.Table) ([]Perso, error) {
	champs := lua.Str(lot.Get("fields"))
	noms := strings.Split(champs, "|")
	idx := map[string]int{}
	for i, n := range noms {
		idx[n] = i
	}
	if _, ok := idx["id"]; !ok {
		return nil, schemaErr("en-tête fields sans colonne id")
	}
	prefixe := lua.Str(lot.Get("guid_prefix"))
	classes, err := stringList(lot.Get("classes"))
	if err != nil {
		return nil, err
	}
	races, err := stringList(lot.Get("races"))
	if err != nil {
		return nil, err
	}
	rows, ok := lot.Get("rows").(string)
	if !ok && lot.Get("rows") != nil {
		return nil, schemaErr("rows n'est pas une chaîne")
	}
	col := func(parts []string, n string) string {
		if i, ok := idx[n]; ok {
			return parts[i]
		}
		return ""
	}
	dico := func(t []string, v, quoi string) (string, error) {
		if v == "" {
			return "", nil
		}
		i, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return "", schemaErr("index de %s non entier : %q", quoi, v)
		}
		if i < 1 || i > len(t) {
			return "", schemaErr("index de %s hors dictionnaire : %d (taille %d)", quoi, i, len(t))
		}
		return t[i-1], nil
	}
	var out []Perso
	for numero, ligne := range Lignes(rows) {
		parts := strings.Split(ligne, "|")
		if len(parts) != len(noms) {
			apercu := ligne
			if len(apercu) > 70 {
				apercu = apercu[:70]
			}
			return nil, schemaErr("ligne %d : %d champs au lieu de %d — %s", numero+1, len(parts), len(noms), apercu)
		}
		ident := col(parts, "id")
		guid := prefixe + ident
		if strings.Contains(ident, "-") {
			guid = ident
		}
		niveau := col(parts, "level")
		var lv *int
		if niveau != "" {
			if !isDigits(niveau) {
				return nil, schemaErr("ligne %d : niveau non entier « %s »", numero+1, niveau)
			}
			n, _ := strconv.Atoi(niveau)
			lv = &n
		}
		cl, err := dico(classes, col(parts, "class"), "classe")
		if err != nil {
			return nil, err
		}
		ra, err := dico(races, col(parts, "race"), "race")
		if err != nil {
			return nil, err
		}
		out = append(out, Perso{
			ID: ident, GUID: guid, Name: col(parts, "name"),
			DisplayName: nomAffiche(col(parts, "surname"), col(parts, "name")),
			Class:       cl, Race: ra, Sex: col(parts, "sex"), Level: lv, Realm: col(parts, "realm"),
		})
	}
	return out, nil
}

// DecodeWho décode un lot who_manual. Plus strict que la référence : chaque ligne
// doit avoir exactement six champs, sinon un « | » s'est glissé dans un nom.
func DecodeWho(lot *lua.Table) ([]WhoLigne, error) {
	champs := lua.Str(lot.Get("fields"))
	if champs != ChampsWho {
		return nil, schemaErr("en-tête who inattendu « %s »", champs)
	}
	var out []WhoLigne
	for numero, ligne := range Lignes(lua.Str(lot.Get("rows"))) {
		p := strings.Split(ligne, "|")
		if len(p) != 6 {
			return nil, schemaErr("ligne %d : %d champs au lieu de 6", numero+1, len(p))
		}
		var lv *int
		if p[3] != "" {
			if !isDigits(p[3]) {
				return nil, schemaErr("ligne %d : niveau non entier « %s »", numero+1, p[3])
			}
			n, _ := strconv.Atoi(p[3])
			lv = &n
		}
		out = append(out, WhoLigne{Name: p[0], ClassLoc: p[1], RaceLoc: p[2], Level: lv, Guild: p[4], Zone: p[5]})
	}
	return out, nil
}

// Etat : effectif reconstitué d'un lot, dans l'ordre d'insertion (comme un dict
// Python : une réaffectation garde la place, une suppression puis un ajout va à la fin).
type Etat struct {
	keys  []string
	pos   map[string]int
	vals  map[string]Perso
	alive int
}

func newEtat() *Etat { return &Etat{pos: map[string]int{}, vals: map[string]Perso{}} }

func (e *Etat) clone() *Etat {
	n := newEtat()
	for _, k := range e.keys {
		if _, ok := e.vals[k]; ok {
			n.set(k, e.vals[k])
		}
	}
	return n
}

func (e *Etat) set(k string, p Perso) {
	if _, ok := e.vals[k]; !ok {
		e.pos[k] = len(e.keys)
		e.keys = append(e.keys, k)
		e.alive++
	}
	e.vals[k] = p
}

func (e *Etat) del(k string) {
	if _, ok := e.vals[k]; ok {
		delete(e.vals, k)
		delete(e.pos, k)
		e.alive--
	}
}

// Len : nombre de personnages.
func (e *Etat) Len() int { return e.alive }

// Persos : les personnages dans l'ordre d'insertion.
func (e *Etat) Persos() []Perso {
	out := make([]Perso, 0, e.alive)
	for i, k := range e.keys {
		if p, ok := e.vals[k]; ok && e.pos[k] == i {
			out = append(out, p)
		}
	}
	return out
}

// Reconstruit applique les chaînes de deltas. Contrairement à la référence, qui
// s'arrête à la première erreur, chaque lot a son résultat : un delta dont la base
// manque ou échoue est en erreur, ses descendants aussi (pas de demi-chaîne).
func Reconstruit(lots []*lua.Table) (map[string]*Etat, map[string]error) {
	parID := map[string]*lua.Table{}
	for _, l := range lots {
		parID[lua.Str(l.Get("batch_id"))] = l
	}
	etats := map[string]*Etat{}
	errs := map[string]error{}
	enCours := map[string]bool{}
	var etat func(l *lua.Table) (*Etat, error)
	etat = func(l *lua.Table) (*Etat, error) {
		bid := lua.Str(l.Get("batch_id"))
		if e, ok := etats[bid]; ok {
			return e, nil
		}
		if err, ok := errs[bid]; ok {
			return nil, err
		}
		if enCours[bid] {
			return nil, schemaErr("lot %s : chaîne de deltas en boucle", bid)
		}
		enCours[bid] = true
		defer delete(enCours, bid)
		var courant *Etat
		if lua.Str(l.Get("kind")) == "delta" {
			baseID := lua.Str(l.Get("base_batch_id"))
			base, ok := parID[baseID]
			if !ok {
				return nil, schemaErr("lot %s : base %s absente du fichier", bid, baseID)
			}
			ci, _ := lua.AsInt(l.Get("chain_index"))
			bci, _ := lua.AsInt(base.Get("chain_index"))
			if ci != bci+1 {
				return nil, schemaErr("lot %s : chain_index %v ne suit pas celui de sa base (%v)", bid, l.Get("chain_index"), base.Get("chain_index"))
			}
			b, err := etat(base)
			if err != nil {
				errs[baseID] = err
				return nil, schemaErr("lot %s : base %s en erreur (%v)", bid, baseID, err)
			}
			courant = b.clone()
			for _, pid := range Lignes(lua.Str(l.Get("gone"))) {
				courant.del(pid)
			}
		} else {
			courant = newEtat()
		}
		persos, err := DecodeLot(l)
		if err != nil {
			return nil, err
		}
		for _, p := range persos {
			courant.set(p.ID, p)
		}
		etats[bid] = courant
		return courant, nil
	}
	for _, l := range lots {
		if lua.Str(l.Get("method")) != "channel_roster" {
			continue
		}
		bid := lua.Str(l.Get("batch_id"))
		if _, err := etat(l); err != nil {
			errs[bid] = err
		}
	}
	return etats, errs
}
