package schema

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"wowsync/internal/lua"
)

var (
	reGUID    = regexp.MustCompile(`^Player-(\d+)-([0-9A-Fa-f]+)$`)
	rePrefixe = regexp.MustCompile(`^Player-(\d+)-$`)
	reUUID4   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

// LotResultat : verdict et corps d'envoi d'un lot.
type LotResultat struct {
	Index       int // position 1-basée dans le fichier
	BatchID     string
	ScopeID     string
	Method      string
	Kind        string // kind d'origine : full ou delta
	SourceScope string
	ObservedAt  int64
	Erreurs     []string
	Avertis     []string
	Personnages int
	Payload     Obj // nil si le lot est écarté
	Descendants []string
	ZoneName    *string    // zone du lot (lots de zone seulement)
	Roster      []Perso    // effectif reconstitué (lots roster)
	Who         []WhoLigne // lignes décodées (lots /who)
}

// Valide : le lot peut partir.
func (l *LotResultat) Valide() bool { return len(l.Erreurs) == 0 && l.Payload != nil }

// Fichier : le résultat complet d'un ForeverPulseCensusDB.
type Fichier struct {
	Fatales      []string // défauts de fichier : rien n'est envoyé
	Avertis      []string
	Infos        []string
	Schema       int64
	ObserverID   string
	AddonVersion string
	ScopeIDs     []string
	Scopes       map[string]any // table scopes convertie en JSON, telle quelle
	Lots         []*LotResultat
	Etats        map[string]*Etat
}

// LotsValides : les lots envoyables, dans l'ordre du fichier.
func (f *Fichier) LotsValides() []*LotResultat {
	var out []*LotResultat
	for _, l := range f.Lots {
		if l.Valide() {
			out = append(out, l)
		}
	}
	return out
}

func chercheOccurredAt(v any, chemin string, depth int) []string {
	t := lua.AsTable(v)
	if t == nil || depth > lua.MaxDepth {
		return nil
	}
	var out []string
	for _, k := range t.Order {
		if strings.Contains(k, "occurred_at") {
			out = append(out, chemin+"."+k)
		}
		out = append(out, chercheOccurredAt(t.Str[k], chemin+"."+k, depth+1)...)
	}
	keys := make([]int64, 0, len(t.Int))
	for k := range t.Int {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		out = append(out, chercheOccurredAt(t.Int[k], fmt.Sprintf("%s.%d", chemin, k), depth+1)...)
	}
	for k, x := range t.Other {
		if strings.Contains(k, "occurred_at") {
			out = append(out, chemin+"."+k)
		}
		out = append(out, chercheOccurredAt(x, chemin+"."+k, depth+1)...)
	}
	return out
}

// RFC3339 : époque UTC du jeu → horodatage RFC 3339 UTC.
func RFC3339(epoch int64) string { return time.Unix(epoch, 0).UTC().Format("2006-01-02T15:04:05Z") }

func optTime(t *lua.Table, k string) any {
	if n, ok := lua.AsInt(t.Get(k)); ok {
		return RFC3339(n)
	}
	return nil
}

func optInt(t *lua.Table, k string) any {
	if n, ok := lua.AsInt(t.Get(k)); ok {
		return n
	}
	return nil
}

func optStr(t *lua.Table, k string) any {
	if s, ok := t.Get(k).(string); ok {
		return s
	}
	return nil
}

func optBool(t *lua.Table, k string) any {
	if b, ok := t.Get(k).(bool); ok {
		return b
	}
	return nil
}

// ToJSON convertit une valeur Lua en valeur JSON : liste 1..n sans autre clé →
// tableau, table vide → objet vide, sinon objet (clés entières en texte).
func ToJSON(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case string, bool:
		return x
	case lua.Number:
		if x.IsInt {
			return x.I
		}
		return x.F
	case *lua.Table:
		lst := x.List()
		if len(x.Str) == 0 && len(x.Other) == 0 && len(lst) == len(x.Int) && len(lst) > 0 {
			out := make([]any, len(lst))
			for i, e := range lst {
				out[i] = ToJSON(e)
			}
			return out
		}
		o := Obj{}
		keys := append([]string(nil), x.Order...)
		sort.Strings(keys)
		for _, k := range keys {
			o = append(o, KV{k, ToJSON(x.Str[k])})
		}
		ik := make([]int64, 0, len(x.Int))
		for k := range x.Int {
			ik = append(ik, k)
		}
		sort.Slice(ik, func(i, j int) bool { return ik[i] < ik[j] })
		for _, k := range ik {
			o = append(o, KV{strconv.FormatInt(k, 10), ToJSON(x.Int[k])})
		}
		return o
	}
	return nil
}

func sexe(s string) (any, bool) {
	if s == "" {
		return nil, true
	}
	if !isDigits(s) {
		return nil, false
	}
	n, _ := strconv.Atoi(s)
	return n, true
}

func niveauOK(p *int) bool { return p == nil || (*p >= 1 && *p <= 100) }

// Analyse valide un ForeverPulseCensusDB et prépare les corps d'envoi.
// `maintenant` sert au contrôle « au plus 5 minutes dans le futur » du site.
func Analyse(db *lua.Table, maintenant time.Time) *Fichier {
	f := &Fichier{Scopes: map[string]any{}}
	if db == nil {
		f.Fatales = append(f.Fatales, "ni ForeverPulseCensusDB dans ce fichier")
		return f
	}
	sch, ok := lua.AsInt(db.Get("schema"))
	f.Schema = sch
	// 0.4.0 : schéma 5 (addon 3.7.0, lots compacts) accepté ; ses lots sont développés
	// en schéma 4 avant tout contrôle (DevelopperLots). Le site reçoit toujours du 4.
	if !ok || sch < 4 || sch > 6 {
		f.Fatales = append(f.Fatales, fmt.Sprintf("schema = %v, attendu 4, 5 ou 6", db.Get("schema")))
	}
	obs := lua.AsTable(db.Get("observer"))
	f.ObserverID = lua.Str(obs.Get("id"))
	if !reUUID4.MatchString(f.ObserverID) {
		f.Fatales = append(f.Fatales, fmt.Sprintf("observer.id n'est pas un UUID v4 : %q", f.ObserverID))
	}
	f.AddonVersion = lua.Str(lua.AsTable(db.Get("addon")).Get("version"))

	scopes := lua.AsTable(db.Get("scopes"))
	if scopes == nil || len(scopes.Str) == 0 {
		f.Fatales = append(f.Fatales, "aucun périmètre : la table scopes est absente ou vide")
	}
	if scopes != nil {
		f.ScopeIDs = append(f.ScopeIDs, scopes.Order...)
		sort.Strings(f.ScopeIDs)
		for _, sid := range f.ScopeIDs {
			sc := lua.AsTable(scopes.Str[sid])
			if sc == nil {
				f.Fatales = append(f.Fatales, fmt.Sprintf("périmètre %q : ce n'est pas une table", sid))
				continue
			}
			if _, ok := lua.AsInt(sc.Get("realm_id")); !ok {
				f.Avertis = append(f.Avertis, fmt.Sprintf("périmètre %s : realm_id absent ou non entier", sid))
			}
			if lua.Str(sc.Get("ruleset")) == "" {
				f.Avertis = append(f.Avertis, fmt.Sprintf("périmètre %s : mode de jeu inconnu", sid))
			}
			f.Scopes[sid] = ToJSON(sc)
			f.Infos = append(f.Infos, fmt.Sprintf("périmètre %s : %s [%v] mode %s, région %s, %s", sid,
				lua.Str(sc.Get("realm_name")), ToJSON(sc.Get("realm_id")), lua.Str(sc.Get("ruleset")),
				lua.Str(sc.Get("region")), lua.Str(sc.Get("faction"))))
		}
	}

	// occurred_at : interdit partout. Hors des lots, c'est tout le fichier qui tombe.
	for _, k := range db.Order {
		if k == "batches" {
			continue
		}
		if strings.Contains(k, "occurred_at") {
			f.Fatales = append(f.Fatales, "champ interdit occurred_at : ."+k)
		}
		if found := chercheOccurredAt(db.Str[k], "."+k, 0); len(found) > 0 {
			f.Fatales = append(f.Fatales, "champ interdit occurred_at : "+strings.Join(found, ", "))
		}
	}

	lots, devErrs := DevelopperLots(db)
	if len(lots) == 0 {
		f.Fatales = append(f.Fatales, "aucun lot")
		return f
	}

	etats, chaineErr := Reconstruit(lots)
	f.Etats = etats
	vus := map[string]bool{}
	var precedent *int64
	limite := maintenant.Add(5 * time.Minute).Unix()

	for i, lot := range lots {
		kind := lua.Str(lot.Get("kind"))
		if kind == "" {
			kind = "full"
		}
		method := lua.Str(lot.Get("method"))
		r := &LotResultat{Index: i + 1, BatchID: lua.Str(lot.Get("batch_id")), ScopeID: lua.Str(lot.Get("scope_id")),
			Method: method, Kind: kind, SourceScope: lua.Str(lot.Get("source_scope"))}
		f.Lots = append(f.Lots, r)
		errf := func(format string, a ...any) { r.Erreurs = append(r.Erreurs, fmt.Sprintf(format, a...)) }
		warn := func(format string, a ...any) { r.Avertis = append(r.Avertis, fmt.Sprintf(format, a...)) }

		if e, ok := devErrs[i]; ok {
			errf("%v", e)
		}
		if found := chercheOccurredAt(lot, "", 0); len(found) > 0 {
			errf("champ interdit occurred_at : %s", strings.Join(found, ", "))
		}
		var sc *lua.Table
		if scopes != nil {
			sc = lua.AsTable(scopes.Str[r.ScopeID])
		}
		if sc == nil {
			errf("scope_id %q absent de la table scopes", r.ScopeID)
		}
		realmAttendu, realmOK := lua.AsInt(sc.Get("realm_id"))

		switch {
		case !reUUID4.MatchString(r.BatchID):
			errf("batch_id n'est pas un UUID v4 : %q", r.BatchID)
		case vus[r.BatchID]:
			errf("batch_id en double %s", r.BatchID)
		default:
			vus[r.BatchID] = true
		}
		if method != "channel_roster" && method != "who_manual" {
			errf("méthode inconnue %q", method)
		}
		at, atOK := lua.AsInt(lot.Get("observed_at"))
		r.ObservedAt = at
		if !atOK || at < 1_600_000_000 {
			errf("observed_at invalide %v", ToJSON(lot.Get("observed_at")))
		} else {
			if precedent != nil && at < *precedent {
				warn("observed_at antérieur au lot précédent")
			}
			if at > limite {
				errf("observed_at plus de 5 minutes dans le futur")
			}
			a := at
			precedent = &a
		}
		champs, _ := lot.Get("fields").(string)
		if champs == "" {
			errf("en-tête fields absent")
			continue
		}
		rowsStr, _ := lot.Get("rows").(string)
		corps := Lignes(rowsStr)
		nb, nbOK := lua.AsInt(lot.Get("rows_count"))
		if !nbOK || int(nb) != len(corps) {
			errf("rows_count %v ≠ %d lignes dans rows", ToJSON(lot.Get("rows_count")), len(corps))
		}
		read, readOK := lua.AsInt(lot.Get("read"))

		if method == "who_manual" {
			if champs != ChampsWho {
				errf("en-tête who inattendu « %s »", champs)
			}
			if !readOK || int(read) != len(corps) {
				errf("read %v ≠ %d lignes", ToJSON(lot.Get("read")), len(corps))
			}
			if lot.Has("zone_count") {
				errf("un lot /who ne peut pas porter de zone_count")
			}
			if kind != "full" {
				errf("un lot /who doit être complet, pas « %s »", kind)
			}
			lignes, err := DecodeWho(lot)
			if err != nil {
				errf("%v", err)
			}
			chars := make([]any, 0, len(lignes))
			for n, w := range lignes {
				if !niveauOK(w.Level) {
					errf("ligne %d : niveau hors bornes 1 à 100", n+1)
					break
				}
				var lv any
				if w.Level != nil {
					lv = *w.Level
				}
				chars = append(chars, Obj{{"name", w.Name}, {"class_loc", w.ClassLoc}, {"race_loc", w.RaceLoc},
					{"level", lv}, {"guild", w.Guild}, {"zone", w.Zone}})
			}
			r.Personnages = len(chars)
			r.Who = lignes
			if len(r.Erreurs) == 0 {
				o := Obj{{"batch_id", r.BatchID}, {"scope_id", r.ScopeID}, {"method", method},
					{"kind", "full"}, {"reconstructed_from_delta", false},
					{"observed_at", RFC3339(at)}, {"requested_at", optTime(lot, "requested_at")},
					{"closed_at", optTime(lot, "closed_at")}, {"close_reason", optStr(lot, "close_reason")},
					{"trigger", optStr(lot, "trigger")}, {"read", read},
					{"queries_sent", optInt(lot, "queries_sent")}, {"truncated", optBool(lot, "truncated")},
					{"characters", chars}}
				r.Payload = o
			}
			continue
		}

		// --- lots roster
		if !strings.HasPrefix(champs, ChampsRoster) {
			errf("en-tête roster inattendu « %s »", champs)
		}
		prefixe, _ := lot.Get("guid_prefix").(string)
		if m := rePrefixe.FindStringSubmatch(prefixe); m == nil {
			errf("guid_prefix mal formé %q", prefixe)
		} else if realmOK {
			if n, _ := strconv.ParseInt(m[1], 10, 64); n != realmAttendu {
				warn("guid_prefix %s ≠ realm_id du périmètre (%d)", prefixe, realmAttendu)
			}
		}
		persos, err := DecodeLot(lot)
		if err != nil {
			errf("%v", err)
			continue
		}
		ids := map[string]bool{}
		for _, p := range persos {
			ids[p.ID] = true
		}
		if len(ids) != len(persos) {
			errf("%d identifiants en double dans rows", len(persos)-len(ids))
		}
		doublons := int64(0)
		if v := lot.Get("duplicates"); v != nil {
			d, ok := lua.AsInt(v)
			if !ok || d < 0 {
				errf("duplicates invalide %v", ToJSON(v))
			} else {
				doublons = d
			}
		}
		distincts := read - doublons
		zc, zcOK := lua.AsInt(lot.Get("zone_count"))
		portee := r.SourceScope
		zone := false
		switch {
		case portee == "realm_channel" || portee == "world_channel":
			for _, interdit := range []string{"zone_count", "zone_name", "ui_map_id", "partial"} {
				if lot.Has(interdit) {
					errf("un lot de canal ne peut pas porter %s — il ne décrit pas une zone", interdit)
				}
			}
			if lua.Str(lot.Get("channel_name")) == "" {
				errf("lot de canal sans channel_name")
			}
			if portee == "realm_channel" && lot.Get("channel_opt_in") != true {
				errf("lot de canal sans channel_opt_in = true")
			}
			if portee == "world_channel" {
				if lot.Get("channel_opt_in") != false {
					errf("canal rejoint d'office sans channel_opt_in = false")
				}
				if c := lua.Str(lot.Get("channel_category")); c == "" || c == "CHANNEL_CATEGORY_CUSTOM" {
					errf("canal rejoint d'office de catégorie %q — CHANNEL_CATEGORY_CUSTOM est un realm_channel", c)
				}
			}
			// 0.4.0 : un lot de canal peut être un delta (addon 3.7.0) ; il part reconstitué.
		case lot.Has("source_scope"):
			errf("source_scope inconnu %v", ToJSON(lot.Get("source_scope")))
		default:
			zone = true
			partial := lot.Get("partial") == true
			switch {
			case !zcOK:
				errf("lot roster sans zone_count")
			case !readOK:
				errf("read absent")
			case read != zc && !partial:
				errf("read %d ≠ zone_count %d sans partial = true", read, zc)
			case read != zc:
				warn("lot partiel, %d lus sur %d annoncés", read, zc)
			}
			uim, uimOK := lua.AsInt(lot.Get("ui_map_id"))
			if (!uimOK || uim == 0) && lot.Get("ui_map_id_absent") != true {
				errf("ni ui_map_id ni ui_map_id_absent")
			}
		}
		if !readOK {
			if !zone {
				errf("read absent")
			}
		} else if kind == "full" {
			if int64(len(corps)) != distincts {
				errf("lot complet, %d lignes pour read = %d%s", len(corps), read, sDoublons(doublons))
			}
		} else {
			inch, ok := lua.AsInt(lot.Get("unchanged"))
			if !ok {
				errf("delta sans unchanged")
			} else if inch+int64(len(corps)) != distincts {
				errf("unchanged %d + %d émises ≠ read %d%s", inch, len(corps), read, sDoublons(doublons))
			}
			g := map[string]bool{}
			for _, x := range Lignes(lua.Str(lot.Get("gone"))) {
				g[x] = true
			}
			if v := lot.Get("gone_count"); v != nil {
				if n, _ := lua.AsInt(v); int(n) != len(g) {
					errf("gone_count %v ≠ %d identifiants", ToJSON(v), len(g))
				}
			}
			commun := 0
			for x := range g {
				if ids[x] {
					commun++
				}
			}
			if commun > 0 {
				errf("%d identifiants à la fois partis et émis", commun)
			}
		}
		if e, ok := chaineErr[r.BatchID]; ok {
			errf("chaîne de deltas : %v", e)
		}
		etat := etats[r.BatchID]
		if etat != nil && readOK && int64(etat.Len()) != distincts {
			errf("effectif reconstitué %d ≠ read %d%s", etat.Len(), read, sDoublons(doublons))
		}
		if etat == nil {
			if _, ok := chaineErr[r.BatchID]; !ok {
				errf("effectif non reconstitué")
			}
			continue
		}
		// Contrôles alignés sur la route du site : GUID, niveau 1-100, sexe entier.
		chars := make([]any, 0, etat.Len())
		for _, p := range etat.Persos() {
			if !reGUID.MatchString(p.GUID) {
				errf("GUID mal formé « %s »", p.GUID)
				break
			}
			if !niveauOK(p.Level) {
				errf("niveau hors bornes 1 à 100")
				break
			}
			sx, ok := sexe(p.Sex)
			if !ok {
				errf("sexe non entier « %s »", p.Sex)
				break
			}
			var lv any
			if p.Level != nil {
				lv = *p.Level
			}
			chars = append(chars, Obj{{"guid", p.GUID}, {"name", p.Name}, {"display_name", p.DisplayName},
				{"class", p.Class}, {"race", p.Race}, {"sex", sx}, {"level", lv}, {"realm", p.Realm}})
		}
		r.Personnages = len(chars)
		r.Roster = etat.Persos()
		if zn, ok := lot.Get("zone_name").(string); ok && zone {
			r.ZoneName = &zn
		}
		if len(r.Erreurs) > 0 {
			continue
		}
		var src any
		if portee != "" {
			src = portee
		}
		var base any
		if kind == "delta" {
			base = lua.Str(lot.Get("base_batch_id"))
		}
		ci, ciOK := lua.AsInt(lot.Get("chain_index"))
		var chain any
		if ciOK {
			chain = ci
		} else if !zone {
			chain = int64(0)
		}
		o := Obj{{"batch_id", r.BatchID}, {"scope_id", r.ScopeID}, {"method", method}, {"source_scope", src},
			{"kind", "full"}, {"reconstructed_from_delta", kind == "delta"}, {"base_batch_id", base},
			{"chain_index", chain}, {"observed_at", RFC3339(at)}, {"requested_at", optTime(lot, "requested_at")}}
		if zone {
			var uim any
			if n, ok := lua.AsInt(lot.Get("ui_map_id")); ok && n != 0 {
				uim = n
			}
			o = append(o, KV{"zone_name", optStr(lot, "zone_name")}, KV{"ui_map_id", uim})
			if lot.Get("ui_map_id_absent") == true {
				o = append(o, KV{"ui_map_id_absent", true})
			}
			o = append(o, KV{"zone_count", zc})
		}
		o = append(o, KV{"read", read}, KV{"duplicates", doublons})
		if zone {
			o = append(o, KV{"partial", lot.Get("partial") == true})
		}
		o = append(o, KV{"trigger", optStr(lot, "trigger")}, KV{"channel_name", optStr(lot, "channel_name")})
		if !zone {
			o = append(o, KV{"channel_category", optStr(lot, "channel_category")},
				KV{"channel_opt_in", lot.Get("channel_opt_in") == true}, KV{"observer_zone", optStr(lot, "observer_zone")})
		}
		o = append(o, KV{"characters", chars})
		r.Payload = o
	}

	// Pas de demi-chaîne : un delta dont un ancêtre est écarté est écarté aussi.
	parID := map[string]*LotResultat{}
	for _, l := range f.Lots {
		if _, dup := parID[l.BatchID]; !dup {
			parID[l.BatchID] = l
		}
	}
	for changed := true; changed; {
		changed = false
		for i, l := range f.Lots {
			if l.Kind != "delta" || len(l.Erreurs) > 0 {
				continue
			}
			base := parID[lua.Str(lots[i].Get("base_batch_id"))]
			if base == nil || len(base.Erreurs) > 0 {
				l.Erreurs = append(l.Erreurs, "base écartée : la chaîne entière est écartée")
				l.Payload = nil
				if base != nil {
					base.Descendants = append(base.Descendants, l.BatchID)
				}
				changed = true
			}
		}
	}
	return f
}

func sDoublons(d int64) string {
	if d == 0 {
		return ""
	}
	return fmt.Sprintf(" − %d doublons", d)
}
