package stats

import (
	"encoding/json"
	"strings"
	"testing"

	"wowsync/internal/lua"
	tl "wowsync/internal/testlua"
)

// table : sérialise une table Lua de test et la relit avec le vrai analyseur.
func table(t *testing.T, db tl.M) *lua.Table {
	t.Helper()
	v, err := lua.Parse([]byte(Variable+" = "+tl.Serialise(db)+"\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return lua.AsTable(v[Variable])
}

func base(persos tl.M) tl.M {
	return tl.M{"schema": SchemaStockage, "version": 4, "locale": "enUS",
		"ctx": tl.L{"beta-4619-PVP", "beta-4620-NORMAL"},
		"contextes": tl.M{
			"beta-4619-PVP":    tl.M{"environment": "beta", "realm_id": 4619, "ruleset": "PVP", "region": "90", "game_version": "1.60.1", "build": 70170},
			"beta-4620-NORMAL": tl.M{"environment": "beta", "realm_id": 4620, "ruleset": "NORMAL"},
		},
		"personnages": persos}
}

func fiche(v ...string) tl.M {
	l := tl.L{}
	for _, g := range v {
		l = append(l, g)
	}
	return tl.M{"n": "Menthe Vide", "c": "HUNTER", "ra": "Skyborne", "f": "A", "s": 3, "l": 17, "x": 1,
		"d": 1790505847, "o": 1790505847, "v": l}
}

type corps struct {
	ID       string  `json:"id"`
	Context  string  `json:"context"`
	GUID     string  `json:"guid"`
	Name     string  `json:"name"`
	Realm    *string `json:"realm"`
	Faction  *string `json:"faction"`
	Level    *int    `json:"level"`
	SeenAt   *string `json:"seen_at"`
	ReadAt   *string `json:"read_at"`
	Readings []struct {
		ReadAt string                       `json:"read_at"`
		Level  *int                         `json:"level"`
		Source string                       `json:"source"`
		Values map[string]map[string]string `json:"values"`
	} `json:"readings"`
}

func decodeCorps(t *testing.T, fi *Fiche) corps {
	t.Helper()
	var c corps
	if err := json.Unmarshal(fi.Payload, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// Un zéro écrit est une valeur ; une statistique absente n'est pas transmise ; une
// valeur illisible est écartée et comptée, jamais remplacée par zéro.
func TestZeroAbsenceEtIllisible(t *testing.T) {
	f := Decode(table(t, base(tl.M{
		"Player-4619-00AA0104": fiche("1790505847|17|n|60:0,94:5,326:18815m,500:72.5p,777:abc,888:,999:1x,1000:-3"),
	})))
	if len(f.Fatales) != 0 || len(f.Fiches) != 1 {
		t.Fatalf("fatales %v, fiches %d", f.Fatales, len(f.Fiches))
	}
	fi := f.Fiches[0]
	if fi.Valeurs != 5 || fi.Illisibles != 3 {
		t.Fatalf("valeurs %d, illisibles %d", fi.Valeurs, fi.Illisibles)
	}
	c := decodeCorps(t, fi)
	v := c.Readings[0].Values
	if v["count"]["60"] != "0" {
		t.Fatalf("le zéro écrit doit être transmis : %v", v["count"])
	}
	if v["count"]["94"] != "5" || v["count"]["1000"] != "-3" || v["copper"]["326"] != "18815" || v["percent"]["500"] != "72.5" {
		t.Fatalf("valeurs : %v", v)
	}
	for _, unite := range []string{"count", "copper", "percent"} {
		if _, ok := v[unite]; !ok {
			t.Fatalf("unité %s absente du corps", unite)
		}
		for _, id := range []string{"95", "777", "888", "999"} {
			if _, ok := v[unite][id]; ok {
				t.Fatalf("statistique %s transmise alors qu'elle est absente ou illisible", id)
			}
		}
	}
	if c.ID != "beta-4619-PVP/Player-4619-00AA0104" || c.Context != "beta-4619-PVP" || c.Realm != nil ||
		c.Faction == nil || *c.Faction != "Alliance" || c.Readings[0].Source != "nameplate" ||
		c.Readings[0].ReadAt != "2026-09-27T10:44:07Z" || *c.Readings[0].Level != 17 {
		t.Fatalf("fiche : %+v", c)
	}
}

func TestNombreExact(t *testing.T) {
	bons := map[string]string{"0": "0", "-0": "0", "007": "7", "12345678901234567890": "12345678901234567890",
		"1.5": "1.5", "1.5e+16": "15000000000000000", "-12.25": "-12.25", "1e3": "1000"}
	for in, want := range bons {
		if got, ok := nombre(in); !ok || got != want {
			t.Errorf("nombre(%q) = %q, %v ; attendu %q", in, got, ok, want)
		}
	}
	for _, in := range []string{"", "abc", "1e400", "NaN", "Inf", "--1", "1,5", "0x10", " 1"} {
		if got, ok := nombre(in); ok {
			t.Errorf("nombre(%q) accepté : %q", in, got)
		}
	}
}

func TestFichesEcarteesSansBloquerLesAutres(t *testing.T) {
	mauvaisNiveau := fiche("1790505847|17|n|60:1")
	mauvaisNiveau["l"] = 250
	sansContexte := fiche("1790505847|17|n|60:1")
	sansContexte["x"] = 9
	sansNom := fiche("1790505847|17|n|60:1")
	delete(sansNom, "n")
	f := Decode(table(t, base(tl.M{
		"Player-4619-00000001": fiche("1790505847|17|n|60:1"),
		"Player-4619-00000002": mauvaisNiveau,
		"Player-4619-00000003": sansContexte,
		"Player-4619-00000004": sansNom,
		"pas-un-guid":          fiche("1790505847|17|n|60:1"),
		"Player-4619-00000005": fiche(), // aucune valeur : rien à envoyer, pas une erreur
		"Player-4619-00000006": fiche("entete illisible", "1790505847|17|n|"),
	})))
	if len(f.Fiches) != 1 || f.Fiches[0].GUID != "Player-4619-00000001" {
		t.Fatalf("fiches gardées : %d", len(f.Fiches))
	}
	if f.Total != 7 || f.SansValeur != 2 || len(f.Ecartees) != 4 {
		t.Fatalf("total %d, sans valeur %d, écartées %v", f.Total, f.SansValeur, f.Ecartees)
	}
	for _, m := range f.Ecartees {
		if strings.Contains(m, "Player-") || strings.Contains(m, "Menthe") {
			t.Fatalf("un motif porte une identité : %q", m)
		}
	}
}

func TestPlusieursRelevesEtContextes(t *testing.T) {
	autre := fiche("1790600000|20|t|60:9", "1790505847|17|n|94:5,60:1")
	autre["x"] = 2
	autre["r"] = "ClassicBetaPvP2"
	f := Decode(table(t, base(tl.M{"Player-4613-00AA0105": autre})))
	if len(f.Fiches) != 1 {
		t.Fatal("fiche absente")
	}
	fi := f.Fiches[0]
	// 60 figure dans deux groupes : seul le plus récent compte, l'autre est compté illisible.
	if fi.Valeurs != 2 || fi.Illisibles != 1 || len(fi.Releves) != 2 || fi.Releves[0].T != 1790600000 {
		t.Fatalf("valeurs %d, illisibles %d, relevés %+v", fi.Valeurs, fi.Illisibles, fi.Releves)
	}
	c := decodeCorps(t, fi)
	if c.Context != "beta-4620-NORMAL" || c.Realm == nil || *c.Realm != "ClassicBetaPvP2" || c.Readings[0].Source != "target" {
		t.Fatalf("fiche : %+v", c)
	}
	// Seul le contexte utilisé est transmis ; ses champs absents sont nuls, jamais devinés.
	if len(f.Contextes) != 1 || f.Contextes[0].K != "beta-4620-NORMAL" {
		t.Fatalf("contextes : %+v", f.Contextes)
	}
	meta, err := f.Meta(strings.Repeat("a", 64), "current", "3.9.8")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		File     map[string]any            `json:"file"`
		Contexts map[string]map[string]any `json:"contexts"`
		Catalog  any                       `json:"catalog"`
	}
	if err := json.Unmarshal(meta, &m); err != nil {
		t.Fatal(err)
	}
	ctx := m.Contexts["beta-4620-NORMAL"]
	if ctx["realm_id"] != float64(4620) || ctx["ruleset"] != "NORMAL" || ctx["region"] != nil || ctx["build"] != nil {
		t.Fatalf("contexte : %v", ctx)
	}
	if m.Catalog != nil || m.File["stats_version"] != float64(4) || m.File["locale"] != "enUS" {
		t.Fatalf("meta : %s", meta)
	}
	if fi.Fraicheur() != 1790600000 {
		t.Fatalf("fraîcheur %d", fi.Fraicheur())
	}
}

func TestCatalogue(t *testing.T) {
	db := base(tl.M{"Player-4619-00000001": fiche("1790505847|17|n|60:1")})
	db["cat"] = tl.M{"v": 1, "t": 1790940000, "locale": "enUS", "build": 70170,
		"cats":  "122|-1|Deaths\n140|-1|Wealth\n125|122|Dungeons and Raids\nmal formée",
		"stats": "60|122|n|Total deaths\n328|140|n|Total gold acquired\n1458|128|t|Creature type killed the most\nx|1|n|ignorée"}
	f := Decode(table(t, db))
	meta, _ := f.Meta(strings.Repeat("a", 64), "current", "3.9.8")
	var m struct {
		Catalog struct {
			Version    int    `json:"version"`
			Locale     string `json:"locale"`
			Build      int    `json:"build"`
			ExportedAt string `json:"exported_at"`
			Categories []struct {
				ID       int    `json:"id"`
				ParentID *int   `json:"parent_id"`
				Name     string `json:"name"`
			} `json:"categories"`
			Statistics []struct {
				ID         int    `json:"id"`
				CategoryID int    `json:"category_id"`
				Kind       string `json:"kind"`
				Name       string `json:"name"`
			} `json:"statistics"`
		} `json:"catalog"`
	}
	if err := json.Unmarshal(meta, &m); err != nil {
		t.Fatal(err)
	}
	c := m.Catalog
	if c.Version != 1 || c.Locale != "enUS" || c.Build != 70170 || len(c.Categories) != 3 || len(c.Statistics) != 3 {
		t.Fatalf("catalogue : %+v", c)
	}
	if c.Categories[0].ParentID != nil || c.Categories[2].ParentID == nil || *c.Categories[2].ParentID != 122 {
		t.Fatalf("parents : %+v", c.Categories)
	}
	if c.Statistics[0].Name != "Total deaths" || c.Statistics[0].Kind != "number" || c.Statistics[2].Kind != "text" {
		t.Fatalf("statistiques : %+v", c.Statistics)
	}

	// Version inconnue : rien n'est transmis, rien n'est deviné.
	db["cat"].(tl.M)["v"] = 2
	f = Decode(table(t, db))
	if f.Catalogue != nil {
		t.Fatal("catalogue d'une version inconnue transmis")
	}
}

// Forme exacte écrite par l'addon 3.9.9 (StatsCatalogue.lua, C.Exporter) : parent vide pour
// une catégorie racine, « | » possible dans le dernier champ, accents conservés.
func TestCatalogueAddon399(t *testing.T) {
	db := base(tl.M{"Player-4619-00000001": fiche("1790505847|17|n|60:1")})
	db["cat"] = tl.M{"v": 1, "t": 1790940000, "locale": "frFR", "build": 70170,
		"cats":  "122||Morts\n21||Joueur contre joueur\n124|21|Champs de bataille",
		"stats": "60|122|n|Morts au total\n1491|21|n|Victoires | honorables, étape\n1458|122|t|Type de créature le plus tué"}
	f := Decode(table(t, db))
	meta, _ := f.Meta(strings.Repeat("a", 64), "current", "3.9.9")
	var m struct {
		Catalog struct {
			Locale     string `json:"locale"`
			Categories []struct {
				ID       int  `json:"id"`
				ParentID *int `json:"parent_id"`
			} `json:"categories"`
			Statistics []struct {
				ID   int    `json:"id"`
				Kind string `json:"kind"`
				Name string `json:"name"`
			} `json:"statistics"`
		} `json:"catalog"`
	}
	if err := json.Unmarshal(meta, &m); err != nil {
		t.Fatal(err)
	}
	c := m.Catalog
	if c.Locale != "frFR" || len(c.Categories) != 3 || len(c.Statistics) != 3 {
		t.Fatalf("catalogue : %+v", c)
	}
	if c.Categories[0].ParentID != nil || c.Categories[2].ParentID == nil || *c.Categories[2].ParentID != 21 {
		t.Fatalf("parents : %+v", c.Categories)
	}
	if c.Statistics[1].Name != "Victoires | honorables, étape" || c.Statistics[2].Kind != "text" {
		t.Fatalf("statistiques : %+v", c.Statistics)
	}
}

func TestSchemaOuVersionInconnus(t *testing.T) {
	db := base(tl.M{"Player-4619-00000001": fiche("1790505847|17|n|60:1")})
	db["version"] = 3
	if f := Decode(table(t, db)); len(f.Fatales) != 1 || len(f.Fiches) != 0 {
		t.Fatalf("version 3 : %+v", f)
	}
	db["version"], db["schema"] = 4, "autre"
	if f := Decode(table(t, db)); len(f.Fatales) != 1 || len(f.Fiches) != 0 {
		t.Fatalf("schéma inconnu : %+v", f)
	}
	if f := Decode(nil); f.Present || len(f.Fatales) != 0 {
		t.Fatal("variable absente : ni présente ni fatale")
	}
}

func TestEmpreinteStableEtSensible(t *testing.T) {
	un := Decode(table(t, base(tl.M{"Player-4619-00000001": fiche("1790505847|17|n|60:1,94:5")})))
	deux := Decode(table(t, base(tl.M{"Player-4619-00000001": fiche("1790505847|17|n|94:5,60:1")})))
	trois := Decode(table(t, base(tl.M{"Player-4619-00000001": fiche("1790505847|17|n|60:2,94:5")})))
	if un.Fiches[0].Empreinte != deux.Fiches[0].Empreinte {
		t.Fatal("l'ordre des valeurs dans le fichier change l'empreinte")
	}
	if un.Fiches[0].Empreinte == trois.Fiches[0].Empreinte {
		t.Fatal("une valeur modifiée ne change pas l'empreinte")
	}
}
