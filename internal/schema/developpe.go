package schema

// Schéma 5 (addon Forever Pulse 3.7.0) : lots compacts. Portage de
// forever-pulse-roadmap/outils/schema.py (developpe_lot).
//
//   - `dicts` au niveau du fichier (classes, races, class_loc, race_loc, guilds, zones),
//     partagés par tous les lots ;
//   - un lot sans `fields` a l'en-tête roster par défaut ; sans `kind`, il est complet ;
//     sans `guid_prefix`, le préfixe est « Player-<realm_id du périmètre>- » ; sans
//     `classes` / `races`, il lit les dictionnaires du fichier ;
//   - `rows_count`, `gone_count`, `observer_faction`, `resolved`, `with_level`, `delay_s`
//     ne sont plus écrits ;
//   - lots /who codés (`name|class_loc#|race_loc#|level|guild#|zone#`, « # » = indice
//     dans `dicts`) ;
//   - deltas aussi pour les lots de canaux.
//
// DevelopperLot rend l'équivalent exact au schéma 4 : validation, reconstruction, cumul
// et corps d'envoi ne voient que du schéma 4. Le site reçoit toujours le schéma 4.

import (
	"fmt"
	"strconv"
	"strings"

	"wowsync/internal/lua"
)

// ChampsWhoCodes : en-tête d'un lot /who du schéma 5.
const ChampsWhoCodes = "name|class_loc#|race_loc#|level|guild#|zone#"

// Schéma 6 (addon 3.7.1) : table des noms partagée. `dicts.names` est une liste d'éléments,
// chacun portant un ou plusieurs noms séparés par « \n » ; la table est leur concaténation.
// Colonnes « surname# », « name# » : indice dans cette table. Un lot roster sans `fields` a
// l'en-tête ChampsRosterCodes.
const ChampsRosterCodes = "id|surname#|name#|class|race|sex|level"

// Colonne codée -> dictionnaire.
var dictColonne = map[string]string{"surname#": "names", "name#": "names", "class_loc#": "class_loc",
	"race_loc#": "race_loc", "guild#": "guilds", "zone#": "zones"}

// tableDict : entrées d'un dictionnaire du fichier (les noms dépliés).
func tableDict(db *lua.Table, nom string) []any {
	t := lua.AsTable(lua.AsTable(db.Get("dicts")).Get(nom)).List()
	if nom != "names" {
		return t
	}
	var plat []any
	for _, e := range t {
		s, _ := e.(string)
		for _, n := range strings.Split(s, "\n") {
			if n != "" {
				plat = append(plat, n)
			}
		}
	}
	return plat
}

func copieTable(t *lua.Table) *lua.Table {
	n := &lua.Table{Str: map[string]any{}, Int: map[int64]any{}, Other: map[string]any{}}
	for k, v := range t.Str {
		n.Str[k] = v
	}
	n.Order = append([]string(nil), t.Order...)
	for k, v := range t.Int {
		n.Int[k] = v
	}
	for k, v := range t.Other {
		n.Other[k] = v
	}
	return n
}

func poser(t *lua.Table, k string, v any) {
	if _, ok := t.Str[k]; !ok {
		t.Order = append(t.Order, k)
	}
	t.Str[k] = v
}

func entier(n int) lua.Number { return lua.Number{I: int64(n), IsInt: true} }

// DevelopperLot : équivalent schéma 4 d'un lot (un lot 4 ressort identique).
func DevelopperLot(db, lot *lua.Table) (*lua.Table, error) {
	return developper(db, lot, map[string][]any{})
}

func developper(db, lot *lua.Table, cache map[string][]any) (*lua.Table, error) {
	l := copieTable(lot)
	dicts := lua.AsTable(db.Get("dicts"))
	switch lua.Str(l.Get("method")) {
	case "channel_roster":
		if lua.Str(l.Get("fields")) == "" {
			if v, ok := lua.AsInt(db.Get("schema")); ok && v >= 6 {
				poser(l, "fields", ChampsRosterCodes)
			} else {
				poser(l, "fields", ChampsRoster)
			}
		}
		for _, k := range []string{"classes", "races"} {
			if !l.Has(k) {
				if d := dicts.Get(k); d != nil {
					poser(l, k, d)
				}
			}
		}
		if lua.Str(l.Get("guid_prefix")) == "" {
			sc := lua.AsTable(lua.AsTable(db.Get("scopes")).Get(lua.Str(l.Get("scope_id"))))
			if r, ok := lua.AsInt(sc.Get("realm_id")); ok {
				poser(l, "guid_prefix", fmt.Sprintf("Player-%d-", r))
			}
		}
	}
	if m := lua.Str(l.Get("method")); m == "channel_roster" || m == "who_manual" {
		if err := decoderColonnes(db, l, cache); err != nil {
			return nil, err
		}
	}
	if lua.Str(l.Get("kind")) == "" {
		poser(l, "kind", "full")
	}
	if !l.Has("rows_count") {
		rows, _ := l.Get("rows").(string)
		poser(l, "rows_count", entier(len(Lignes(rows))))
	}
	if g, _ := l.Get("gone").(string); g != "" && !l.Has("gone_count") {
		poser(l, "gone_count", entier(len(Lignes(g))))
	}
	return l, nil
}

// DevelopperLots : tous les lots du fichier au schéma 4, dans l'ordre. Un lot qui ne se
// développe pas est rendu tel quel, son erreur dans `errs` (même indice).
func DevelopperLots(db *lua.Table) ([]*lua.Table, map[int]error) {
	var out []*lua.Table
	errs := map[int]error{}
	cache := map[string][]any{} // dictionnaires dépliés une fois par fichier
	for i, v := range lua.AsTable(db.Get("batches")).List() {
		t := lua.AsTable(v)
		if t == nil {
			out = append(out, &lua.Table{Str: map[string]any{}, Int: map[int64]any{}})
			continue
		}
		d, err := developper(db, t, cache)
		if err != nil {
			errs[i] = err
			out = append(out, t)
			continue
		}
		out = append(out, d)
	}
	return out, errs
}

// decoderColonnes remplace les indices des colonnes « …# » par leurs valeurs et retire les « # »
// de l'en-tête (schémas 5 et 6).
func decoderColonnes(db, l *lua.Table, cache map[string][]any) error {
	noms := strings.Split(lua.Str(l.Get("fields")), "|")
	codes := map[int][]any{}
	for j, c := range noms {
		d, ok := dictColonne[c]
		if !ok {
			continue
		}
		t, vu := cache[d]
		if !vu {
			t = tableDict(db, d)
			cache[d] = t
		}
		codes[j] = t
	}
	if len(codes) == 0 {
		return nil
	}
	var sortie []string
	for numero, ligne := range Lignes(lua.Str(l.Get("rows"))) {
		parts := strings.Split(ligne, "|")
		if len(parts) != len(noms) {
			return schemaErr("ligne %d : %d champs au lieu de %d", numero+1, len(parts), len(noms))
		}
		for j, t := range codes {
			if parts[j] == "" {
				continue
			}
			i, err := strconv.Atoi(parts[j])
			if err != nil || !isDigits(parts[j]) {
				return schemaErr("ligne %d : indice %s non entier « %s »", numero+1, noms[j], parts[j])
			}
			if i < 1 || i > len(t) {
				return schemaErr("ligne %d : indice %s hors dictionnaire : %d (taille %d)", numero+1, noms[j], i, len(t))
			}
			v, ok := t[i-1].(string)
			if !ok {
				return schemaErr("dictionnaire %s non textuel", dictColonne[noms[j]])
			}
			parts[j] = v
		}
		sortie = append(sortie, strings.Join(parts, "|"))
	}
	en := make([]string, len(noms))
	for j, c := range noms {
		en[j] = strings.TrimSuffix(c, "#")
	}
	poser(l, "rows", strings.Join(sortie, "\n"))
	poser(l, "fields", strings.Join(en, "|"))
	return nil
}
