package stats

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"wowsync/internal/lua"
	tl "wowsync/internal/testlua"
)

// Empreinte et charge d'une fiche sans talents, relevées sur la 0.9.0-rc.1
// (1053b7e) avant tout changement : elles ne doivent pas bouger. Le 08/10/2026, le nom
// de la fixture a été remplacé par un nom synthétique avant publication du code ; l'empreinte
// reste SHA-256 de la charge exacte (5cb012cf… avec l'ancien nom).
const (
	chargeRC1    = `{"id":"beta-4619-PVP/Player-4619-00000001","context":"beta-4619-PVP","guid":"Player-4619-00000001","name":"Menthe Vide","realm":null,"class":"HUNTER","race":"Skyborne","faction":"Alliance","sex":3,"level":17,"seen_at":"2026-09-27T10:44:07Z","read_at":"2026-09-27T10:44:07Z","readings":[{"read_at":"2026-09-27T10:44:07Z","level":17,"source":"nameplate","values":{"count":{"60":"1","94":"5"},"copper":{"326":"18815"},"percent":{}}}]}`
	empreinteRC1 = "173990f2b1aaf390aab77f4cf6d4e5aa61501f34b23c72258ead2471bc8e8ddf"
	metaRC1      = `{"file":{"sha256":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","origin":"current","addon_version":"4.1.0","stats_version":4,"locale":"enUS"},"contexts":{"beta-4619-PVP":{"environment":"beta","realm_id":4619,"ruleset":"PVP","region":"90","game_version":"1.60.1","build":70170}},"catalog":{"version":1,"locale":"enUS","build":70170,"exported_at":"2026-10-02T11:20:00Z","categories":[{"id":122,"parent_id":null,"name":"Deaths"}],"statistics":[{"id":60,"category_id":122,"kind":"number","name":"Total deaths"}]}}`
)

var maintenant = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

const taNominal = "1790505847|30|n|1114|21,0,0|1|0|104001:5,104002:3,104007:1@123456"

func baseTalents(persos tl.M) tl.M {
	db := base(persos)
	db["talents_v"] = 1
	return db
}

func ficheTa(ta any) tl.M {
	f := fiche("1790505847|17|n|60:1,94:5,326:18815m")
	if ta != nil {
		f["ta"] = ta
	}
	return f
}

func decodeA(t *testing.T, db tl.M) *Fichier {
	t.Helper()
	f := DecodeAt(table(t, db), maintenant)
	if len(f.Fatales) != 0 {
		t.Fatalf("fatales : %v", f.Fatales)
	}
	return f
}

func talentsJSON(t *testing.T, fi *Fiche) string {
	t.Helper()
	var c map[string]json.RawMessage
	if err := json.Unmarshal(fi.Payload, &c); err != nil {
		t.Fatal(err)
	}
	return string(c["talents"])
}

func TestTalentsNominalEtEntree(t *testing.T) {
	f := decodeA(t, baseTalents(tl.M{"Player-4619-00000001": ficheTa(taNominal)}))
	if len(f.Fiches) != 1 || f.AvecTalents != 1 || f.TalentsIllisibles != 0 || f.TalentsHorsBornes != 0 {
		t.Fatalf("fiches %d, avec talents %d, illisibles %d, hors bornes %d", len(f.Fiches), f.AvecTalents, f.TalentsIllisibles, f.TalentsHorsBornes)
	}
	want := `{"read_at":"2026-09-27T10:44:07Z","level":30,"source":"nameplate","tree_id":1114,"points":[[104001,5,null],[104002,3,null],[104007,1,123456]],"branch_points":[21,0,0],"dominant":1,"off_grid":0}`
	if got := talentsJSON(t, f.Fiches[0]); got != want {
		t.Fatalf("talents :\n%s\nattendu :\n%s", got, want)
	}
	// Clé ajoutée en dernier, après une charge identique à la 0.9.0-rc.1.
	if p := string(f.Fiches[0].Payload); p != chargeRC1[:len(chargeRC1)-1]+`,"talents":`+want+"}" {
		t.Fatalf("charge : %s", p)
	}
	if f.TalentsVersion == nil || *f.TalentsVersion != 1 {
		t.Fatal("talents_version absent")
	}
}

func TestTalentsPairesVidesNiveauVideEtSources(t *testing.T) {
	persos := tl.M{
		"Player-4619-00000001": ficheTa("1790505847||t|1114|0,0,0|0|0|"),
		"Player-4619-00000002": ficheTa("1790505847|12|f|1114|0,2,0|2|0|104009:2"),
		"Player-4619-00000003": ficheTa("1790505847|12|m|1114|0,0,1|3|1|104982:1"),
		// Paires dans le désordre : triées, la fiche reste valable.
		"Player-4619-00000004": ficheTa("1790505847|12|n|1114|2,0,0|1|0|104002:1,104001:1"),
	}
	f := decodeA(t, baseTalents(persos))
	if f.AvecTalents != 4 {
		t.Fatalf("avec talents %d", f.AvecTalents)
	}
	if got := talentsJSON(t, f.Fiches[0]); got != `{"read_at":"2026-09-27T10:44:07Z","level":null,"source":"target","tree_id":1114,"points":[],"branch_points":[0,0,0],"dominant":0,"off_grid":0}` {
		t.Fatalf("paires vides : %s", got)
	}
	if got := talentsJSON(t, f.Fiches[1]); !strings.Contains(got, `"source":"focus"`) {
		t.Fatal(got)
	}
	if got := talentsJSON(t, f.Fiches[2]); !strings.Contains(got, `"source":"mouseover"`) || !strings.Contains(got, `"off_grid":1`) {
		t.Fatal(got)
	}
	if got := talentsJSON(t, f.Fiches[3]); !strings.Contains(got, `"points":[[104001,1,null],[104002,1,null]]`) {
		t.Fatal(got)
	}
}

func TestTalentsIllisiblesEtHorsBornes(t *testing.T) {
	futur := maintenant.Add(6 * time.Minute).Unix()
	presque := maintenant.Add(4 * time.Minute).Unix()
	trop := make([]string, 201)
	for i := range trop {
		trop[i] = fmt.Sprintf("%d:1", 104000+i)
	}
	illisibles := []any{
		"illisible", 42, "1790505847|30|n|1114|21,0|1|0|", "1790505847|30|n|1114|21,0,0|1|0|104001:x",
		"1790505847|30|n|1114|21,0,0|1|0|104001:5,", "1790505847|30|N|1114|21,0,0|1|0|", "1790505847|30|n|1114|21,0,0|4|0|",
		"1790505847|30|n|1114|21,0,0|1|0|104001:100", "1790505847|30|n|1114|21,0,0|1|0|104001:5@x",
	}
	horsBornes := []any{
		"1790505847|30|g|1114|21,0,0|1|0|", "1790505847|30||1114|21,0,0|1|0|", "1790505847|30|s|1114|21,0,0|1|0|",
		"1790505847|0|n|1114|21,0,0|1|0|", "1790505847|101|n|1114|21,0,0|1|0|", "1790505847|30|n|0|21,0,0|1|0|",
		"0|30|n|1114|21,0,0|1|0|", fmt.Sprintf("%d|30|n|1114|21,0,0|1|0|", futur),
		"1790505847|30|n|1114|21,0,0|1|201|", "1790505847|30|n|1114|21,0,0|1|0|104001:0",
		"1790505847|30|n|1114|21,0,0|1|0|104001:1,104001:2", "1790505847|30|n|1114|21,0,0|1|0|0:1",
		"1790505847|30|n|1114|21,0,0|1|0|104001:1@0",
		"1790505847|30|n|1114|21,0,0|1|0|" + strings.Join(trop, ","),
	}
	persos := tl.M{}
	n := 0
	for _, ta := range append(append([]any{}, illisibles...), horsBornes...) {
		n++
		persos[fmt.Sprintf("Player-4619-%08d", n)] = ficheTa(ta)
	}
	n++
	persos[fmt.Sprintf("Player-4619-%08d", n)] = ficheTa(fmt.Sprintf("%d|30|n|1114|21,0,0|1|0|", presque))
	f := decodeA(t, baseTalents(persos))
	if len(f.Fiches) != n || f.TalentsIllisibles != len(illisibles) || f.TalentsHorsBornes != len(horsBornes) || f.AvecTalents != 1 {
		t.Fatalf("fiches %d/%d, illisibles %d/%d, hors bornes %d/%d, avec talents %d", len(f.Fiches), n,
			f.TalentsIllisibles, len(illisibles), f.TalentsHorsBornes, len(horsBornes), f.AvecTalents)
	}
	// Les fiches restent envoyées, sans talents, avec la charge d'avant.
	if string(f.Fiches[0].Payload) != chargeRC1 || f.Fiches[0].Empreinte != empreinteRC1 {
		t.Fatalf("charge d'une fiche à ta illisible : %s", f.Fiches[0].Payload)
	}
	for _, fi := range f.Fiches[:n-1] {
		if strings.Contains(string(fi.Payload), "talents") {
			t.Fatalf("talents illisibles ou hors bornes transmis : %s", fi.Payload)
		}
	}
	for _, l := range f.Infos {
		if strings.Contains(l, "Player-") {
			t.Fatalf("un motif porte une identité : %q", l)
		}
	}
}

func TestTalentsVersionAbsenteOuInconnue(t *testing.T) {
	db := base(tl.M{"Player-4619-00000001": ficheTa(taNominal)})
	db["arbres"] = tl.I{1114: arbreNominal()}
	f := decodeA(t, db)
	if f.AvecTalents != 0 || f.TalentsVersion != nil || len(f.Arbres) != 0 || f.Fiches[0].Empreinte != empreinteRC1 {
		t.Fatalf("talents_v absent : %+v", f)
	}
	db["talents_v"] = 2
	f = decodeA(t, db)
	if f.AvecTalents != 0 || f.TalentsVersion != nil || len(f.Arbres) != 0 || f.Fiches[0].Empreinte != empreinteRC1 {
		t.Fatalf("talents_v inconnu : %+v", f)
	}
}

// Une fiche sans ta garde exactement la charge et l'empreinte de la 0.9.0-rc.1,
// même quand le fichier porte talents_v et un catalogue d'arbres ; un ta qui
// change change l'empreinte.
func TestTalentsEmpreinte(t *testing.T) {
	sans := decodeA(t, baseTalents(tl.M{"Player-4619-00000001": ficheTa(nil)}))
	if string(sans.Fiches[0].Payload) != chargeRC1 || sans.Fiches[0].Empreinte != empreinteRC1 {
		t.Fatalf("fiche sans talents modifiée : %s %s", sans.Fiches[0].Payload, sans.Fiches[0].Empreinte)
	}
	db := baseTalents(tl.M{"Player-4619-00000001": ficheTa(nil)})
	db["arbres"] = tl.I{1114: arbreNominal()}
	if f := decodeA(t, db); f.Fiches[0].Empreinte != empreinteRC1 {
		t.Fatal("le catalogue des arbres change l'empreinte d'une fiche sans talents")
	}
	un := decodeA(t, baseTalents(tl.M{"Player-4619-00000001": ficheTa(taNominal)}))
	deux := decodeA(t, baseTalents(tl.M{"Player-4619-00000001": ficheTa(taNominal)}))
	trois := decodeA(t, baseTalents(tl.M{"Player-4619-00000001": ficheTa(strings.Replace(taNominal, "104002:3", "104002:2", 1))}))
	quatre := decodeA(t, baseTalents(tl.M{"Player-4619-00000001": ficheTa(strings.Replace(taNominal, "1790505847", "1790600000", 1))}))
	if un.Fiches[0].Empreinte != deux.Fiches[0].Empreinte {
		t.Fatal("empreinte instable")
	}
	if un.Fiches[0].Empreinte == empreinteRC1 || un.Fiches[0].Empreinte == trois.Fiches[0].Empreinte || un.Fiches[0].Empreinte == quatre.Fiches[0].Empreinte {
		t.Fatal("un relevé de talents nouveau ou modifié ne change pas l'empreinte")
	}
	// La date du relevé de talents compte dans la fraîcheur (relecture d'un .bak).
	if quatre.Fiches[0].Fraicheur() != 1790600000 {
		t.Fatalf("fraîcheur %d", quatre.Fiches[0].Fraicheur())
	}
}

func arbreNominal() tl.M {
	return tl.M{"v": 1, "t": 1790940000, "build": 70170, "locale": "enUS", "c": "PRIEST",
		"n": strings.Join([]string{
			"104001|1020|2130|5|1|14522|Unbreakable Will",
			"104007|5020|2130|1|2|15237|Holy | Nova|200:15237;201:",
			"104982|102800|900|1|3||",
			"104002|2820|2730|3|1|14523|Silent Resolve",
		}, "\n"),
		"hg": "104982"}
}

func TestArbresNominalEtHorsGrille(t *testing.T) {
	db := baseTalents(tl.M{"Player-4619-00000001": ficheTa(taNominal)})
	db["arbres"] = tl.I{1114: arbreNominal(), 1091: tl.M{"v": 1, "t": 1790940001, "n": "1|0|0|1|1|0|"}}
	f := decodeA(t, db)
	if f.ArbresIgnores != 0 || len(f.Arbres) != 2 {
		t.Fatalf("arbres %d, ignorés %d, infos %v", len(f.Arbres), f.ArbresIgnores, f.Infos)
	}
	meta, err := f.Meta(strings.Repeat("a", 64), "current", "4.2.0")
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(meta, &m); err != nil {
		t.Fatal(err)
	}
	want := `[{"tree_id":1091,"class":null,"build":null,"locale":null,"exported_at":"2026-10-02T11:20:01Z","off_grid":[],"nodes":[{"id":1,"x":0,"y":0,"max_ranks":1,"branch":1,"spell_id":0,"name":null,"entries":null}]},` +
		`{"tree_id":1114,"class":"PRIEST","build":70170,"locale":"enUS","exported_at":"2026-10-02T11:20:00Z","off_grid":[104982],"nodes":[` +
		`{"id":104001,"x":1020,"y":2130,"max_ranks":5,"branch":1,"spell_id":14522,"name":"Unbreakable Will","entries":null},` +
		`{"id":104002,"x":2820,"y":2730,"max_ranks":3,"branch":1,"spell_id":14523,"name":"Silent Resolve","entries":null},` +
		`{"id":104007,"x":5020,"y":2130,"max_ranks":1,"branch":2,"spell_id":15237,"name":"Holy | Nova","entries":[[200,15237],[201,null]]},` +
		`{"id":104982,"x":102800,"y":900,"max_ranks":1,"branch":3,"spell_id":null,"name":null,"entries":null}]}]`
	if string(m["talent_trees"]) != want {
		t.Fatalf("talent_trees :\n%s\nattendu :\n%s", m["talent_trees"], want)
	}
	if !strings.HasSuffix(string(m["file"]), `"locale":"enUS","talents_version":1}`) {
		t.Fatalf("file : %s", m["file"])
	}
}

// arbresDe : décode un fichier dont le catalogue porte l'arbre 1114 donné et un
// arbre 1091 valable ; rend le fichier et l'arbre 1114 (nil s'il est écarté).
func arbresDe(t *testing.T, a tl.M) (*Fichier, map[string]json.RawMessage) {
	t.Helper()
	db := baseTalents(tl.M{"Player-4619-00000001": ficheTa(taNominal)})
	db["arbres"] = tl.I{1114: a, 1091: tl.M{"v": 1, "t": 1790940001, "n": "1|0|0|1|1|0|"}}
	f := decodeA(t, db)
	if f.AvecTalents != 1 {
		t.Fatalf("talents de la fiche perdus : %+v", f)
	}
	meta, _ := f.Meta(strings.Repeat("a", 64), "current", "4.2.0")
	var m struct {
		Arbres []map[string]json.RawMessage `json:"talent_trees"`
	}
	if err := json.Unmarshal(meta, &m); err != nil {
		t.Fatal(err)
	}
	for _, x := range m.Arbres {
		if string(x["tree_id"]) == "1114" {
			return f, x
		}
	}
	return f, nil
}

// Contrat §7.2 : un nœud illisible, hors bornes ou en double est écarté et compté,
// l'arbre et ses autres nœuds restent ; bornes communes au site exactement.
func TestArbresNoeudEcarteArbreGarde(t *testing.T) {
	const bon = "104001|1020|2130|5|1|14522|Bon"
	cas := map[string]struct {
		ligne  string
		ecarte int
	}{
		"branche 4":          {"104002|1020|2130|5|4|14522|X", 1},
		"branche 0":          {"104002|1020|2130|5|0|14522|X", 1},
		"x hors bornes":      {"104002|10000001|2130|5|1|14522|X", 1},
		"y hors bornes":      {"104002|1020|-10000001|5|1|14522|X", 1},
		"rangs 0":            {"104002|1020|2130|0|1|14522|X", 1},
		"rangs 100":          {"104002|1020|2130|100|1|14522|X", 1},
		"nœud 0":             {"0|1020|2130|5|1|14522|X", 1},
		"nœud 2^31":          {"2147483648|1020|2130|5|1|14522|X", 1},
		"sort 2^31":          {"104002|1020|2130|5|1|2147483648|X", 1},
		"sort négatif":       {"104002|1020|2130|5|1|-1|X", 1},
		"entrée 0":           {"104002|1020|2130|5|1|1|X|0:5;2:6", 1},
		"entrée 2^31":        {"104002|1020|2130|5|1|1|X|2147483648:5", 1},
		"sort d'entrée 2^31": {"104002|1020|2130|5|1|1|X|1:2147483648", 1},
		"neuf entrées":       {"104002|1020|2130|5|1|1|X|1:1;2:2;3:3;4:4;5:5;6:6;7:7;8:8;9:9", 1},
		"nom trop long":      {"104002|1020|2130|5|1|1|" + strings.Repeat("é", 201), 1},
		"nom avec contrôle":  {"104002|1020|2130|5|1|1|a\tb", 1},
		"ligne courte":       {"104002|1020|2130|5|1", 1},
		"texte":              {"abc", 1},
		"nœud en double":     {"104002|1020|2130|5|1|1|X\n104002|1020|2130|5|1|1|Y", 2},
		"double du bon nœud": {"104001|1020|2130|5|1|1|Y", 2},
	}
	for nom, c := range cas {
		a := arbreNominal()
		a["n"] = bon + "\n" + c.ligne
		f, x := arbresDe(t, a)
		if nom == "double du bon nœud" {
			// Toutes les lignes du nœud ambigu sont écartées : plus aucun nœud, arbre écarté.
			if x != nil || f.ArbresIgnores != 1 || f.NoeudsIgnores != 2 {
				t.Errorf("%s : arbre %s, arbres écartés %d, nœuds %d", nom, x["nodes"], f.ArbresIgnores, f.NoeudsIgnores)
			}
			continue
		}
		if x == nil || f.ArbresIgnores != 0 || f.NoeudsIgnores != c.ecarte || len(f.Arbres) != 2 {
			t.Errorf("%s : arbre %v, arbres écartés %d, nœuds écartés %d", nom, x != nil, f.ArbresIgnores, f.NoeudsIgnores)
			continue
		}
		if string(x["nodes"]) != `[{"id":104001,"x":1020,"y":2130,"max_ranks":5,"branch":1,"spell_id":14522,"name":"Bon","entries":null}]` {
			t.Errorf("%s : nœuds %s", nom, x["nodes"])
		}
	}

	// Bornes atteintes, pas dépassées.
	a := arbreNominal()
	a["build"] = 2147483647
	a["hg"] = "2147483647,abc,0,104982"
	a["n"] = "2147483647|10000000|-10000000|99|3|2147483647|Max|1:2147483647;2147483647:0;3:\n1|0|0|1|1|0|"
	f, x := arbresDe(t, a)
	if x == nil || f.NoeudsIgnores != 2 {
		t.Fatalf("bornes : arbre %v, nœuds écartés %d", x != nil, f.NoeudsIgnores)
	}
	if string(x["build"]) != "2147483647" || string(x["off_grid"]) != "[104982,2147483647]" ||
		string(x["nodes"]) != `[{"id":1,"x":0,"y":0,"max_ranks":1,"branch":1,"spell_id":0,"name":null,"entries":null},`+
			`{"id":2147483647,"x":10000000,"y":-10000000,"max_ranks":99,"branch":3,"spell_id":2147483647,"name":"Max","entries":[[1,2147483647],[2147483647,0],[3,null]]}]` {
		t.Fatalf("bornes : %s %s %s", x["build"], x["off_grid"], x["nodes"])
	}

	// Champs facultatifs de l'arbre hors bornes : null, l'arbre et ses nœuds restent.
	a = arbreNominal()
	a["build"], a["c"], a["locale"] = 2147483648, "priest", "enUS-enUS-x"
	f, x = arbresDe(t, a)
	if x == nil || string(x["build"]) != "null" || string(x["class"]) != "null" || string(x["locale"]) != "null" ||
		f.NoeudsIgnores != 0 || f.ArbresIgnores != 0 || !strings.Contains(string(x["nodes"]), `"id":104982`) {
		t.Fatalf("champs facultatifs : %v", x)
	}
}

func TestArbresEcartes(t *testing.T) {
	cas := map[string]func(a tl.M){
		"version inconnue":   func(a tl.M) { a["v"] = 2 },
		"sans version":       func(a tl.M) { delete(a, "v") },
		"sans date":          func(a tl.M) { delete(a, "t") },
		"aucun nœud":         func(a tl.M) { a["n"] = "" },
		"aucun nœud valable": func(a tl.M) { a["n"] = "104001|1020|2130|5|4|14522|X\nabc" },
		"trop de nœuds": func(a tl.M) {
			l := make([]string, 501)
			for i := range l {
				l[i] = fmt.Sprintf("%d|1020|2130|1|1|1|N", 104000+i)
			}
			a["n"] = strings.Join(l, "\n")
		},
	}
	for nom, change := range cas {
		a := arbreNominal()
		change(a)
		f, x := arbresDe(t, a)
		if x != nil || f.ArbresIgnores != 1 || len(f.Arbres) != 1 {
			t.Errorf("%s : arbres %d, ignorés %d", nom, len(f.Arbres), f.ArbresIgnores)
		}
	}
	// 500 nœuds : la borne est atteinte, pas dépassée.
	l := make([]string, 500)
	for i := range l {
		l[i] = fmt.Sprintf("%d|1020|2130|1|1|1|N", 104000+i)
	}
	db := baseTalents(tl.M{"Player-4619-00000001": ficheTa(taNominal)})
	db["arbres"] = tl.I{1114: tl.M{"v": 1, "t": 1790940000, "n": strings.Join(l, "\n")}}
	if f := decodeA(t, db); len(f.Arbres) != 1 || f.ArbresIgnores != 0 {
		t.Fatalf("500 nœuds : %d arbres, %d ignorés", len(f.Arbres), f.ArbresIgnores)
	}
	// Doublon de tree_id : la clé texte « "1114" » à côté de [1114] est écartée et
	// comptée, l'arbre entier [1114] part une seule fois.
	src := Variable + " = " + tl.Serialise(baseTalents(tl.M{"Player-4619-00000001": ficheTa(taNominal)}))
	src = strings.TrimSuffix(src, "}") + "\t[\"arbres\"] = {\n\t\t[1114] = " + tl.Serialise(arbreNominal()) +
		",\n\t\t[\"1114\"] = " + tl.Serialise(arbreNominal()) + ",\n\t},\n}\n"
	v, err := lua.Parse([]byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	f := DecodeAt(lua.AsTable(v[Variable]), maintenant)
	if len(f.Arbres) != 1 || f.ArbresIgnores != 1 {
		t.Fatalf("tree_id en double : %d arbres, %d écartés", len(f.Arbres), f.ArbresIgnores)
	}
	// Plus de 64 arbres : catalogue ignoré, compté ; les fiches et leurs talents partent.
	trop := tl.I{}
	for i := 1; i <= 65; i++ {
		trop[i] = tl.M{"v": 1, "t": 1790940000, "n": "1|0|0|1|1|0|"}
	}
	db["arbres"] = trop
	if f := decodeA(t, db); len(f.Arbres) != 0 || f.ArbresIgnores != 65 || f.AvecTalents != 1 {
		t.Fatalf("65 arbres : %d gardés, %d ignorés", len(f.Arbres), f.ArbresIgnores)
	}
}

// Forme du schéma 1 : file.talents_version et talent_trees retirés, le reste
// identique octet pour octet à la 0.9.0-rc.1 ; une meta antérieure reçoit les
// valeurs par défaut au schéma 2.
func TestCorpsSchema(t *testing.T) {
	db := base(tl.M{"Player-4619-00000001": fiche("1790505847|17|n|60:1,94:5,326:18815m")})
	db["cat"] = tl.M{"v": 1, "t": 1790940000, "locale": "enUS", "build": 70170, "cats": "122|-1|Deaths", "stats": "60|122|n|Total deaths"}
	f := decodeA(t, db)
	meta, _ := f.Meta(strings.Repeat("a", 64), "current", "4.1.0")
	if v1, err := CorpsSchema(meta, 1); err != nil || string(v1) != metaRC1 {
		t.Fatalf("schéma 1 :\n%s\n%v", v1, err)
	}
	v2, err := CorpsSchema(meta, 2)
	if err != nil || string(v2) != string(meta) {
		t.Fatalf("schéma 2 : %s", v2)
	}
	if !strings.Contains(string(v2), `"locale":"enUS","talents_version":null}`) || !strings.HasSuffix(string(v2), `,"talent_trees":[]}`) {
		t.Fatalf("schéma 2 sans talents : %s", v2)
	}
	ancienne, err := CorpsSchema([]byte(metaRC1), 2)
	if err != nil || string(ancienne) != string(meta) {
		t.Fatalf("meta 0.9.0-rc.1 au schéma 2 :\n%s\n%s", ancienne, meta)
	}
	if v1, _ := CorpsSchema([]byte(metaRC1), 1); string(v1) != metaRC1 {
		t.Fatal("meta 0.9.0-rc.1 modifiée au schéma 1")
	}
	avec := decodeA(t, baseTalents(tl.M{"Player-4619-00000001": ficheTa(taNominal)}))
	p1, err := FicheSchema(avec.Fiches[0].Payload, 1)
	if err != nil || string(p1) != chargeRC1 {
		t.Fatalf("fiche au schéma 1 : %s", p1)
	}
	if p2, _ := FicheSchema(avec.Fiches[0].Payload, 2); string(p2) != string(avec.Fiches[0].Payload) {
		t.Fatal("fiche modifiée au schéma 2")
	}
}
