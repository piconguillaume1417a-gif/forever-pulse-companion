package lua

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func parseOne(t *testing.T, src string) any {
	t.Helper()
	v, err := Parse([]byte("X = "+src), nil)
	if err != nil {
		t.Fatalf("%q : %v", src, err)
	}
	return v["X"]
}

func TestChaines(t *testing.T) {
	cas := map[string]string{
		`"a|b|c"`:                  "a|b|c",
		`"ligne 1\nligne 2"`:       "ligne 1\nligne 2",
		`"il dit \"bonjour\""`:     `il dit "bonjour"`,
		`"barre \\ oblique"`:       `barre \ oblique`,
		`"\65\066\0677"`:           "ABC7",
		`"原形 이정 Рексар"`:           "原形 이정 Рексар",
		`'simple'`:                 "simple",
		`"tab\tret\r"`:             "tab\tret\r",
		`"=한글 이름|한글|2|1|3|8\n00A"`: "=한글 이름|한글|2|1|3|8\n00A",
		`"\195\169t\195\169"`:      "été", // \ddd = octets UTF-8
		`"inconnu \q"`:             "inconnu q",
		"\"suite \\\nligne\"":      "suite \nligne",
	}
	for src, want := range cas {
		if got := parseOne(t, src); got != want {
			t.Errorf("%s → %q, attendu %q", src, got, want)
		}
	}
}

func TestNombresEtMots(t *testing.T) {
	if n := parseOne(t, "42").(Number); !n.IsInt || n.I != 42 {
		t.Error("entier")
	}
	if n := parseOne(t, "-7").(Number); !n.IsInt || n.I != -7 {
		t.Error("négatif")
	}
	if n := parseOne(t, "1.07").(Number); n.IsInt || n.F != 1.07 {
		t.Error("décimal")
	}
	if n := parseOne(t, "1e3").(Number); n.IsInt || n.F != 1000 {
		t.Error("exposant")
	}
	if n := parseOne(t, "0x1F").(Number); !n.IsInt || n.I != 31 {
		t.Error("hexadécimal")
	}
	if n := parseOne(t, ".5").(Number); n.F != 0.5 {
		t.Error(".5")
	}
	if parseOne(t, "true") != true || parseOne(t, "false") != false || parseOne(t, "nil") != nil {
		t.Error("mots")
	}
}

func TestTables(t *testing.T) {
	src := `{
		-- commentaire
		["batch_id"] = "x", --[[ long
		commentaire ]]
		[1] = "explicite",
		"positionnel 1", "positionnel 2";
		nom = 3,
		[2.0] = "deux",
		[1.5] = "décimal",
		["vide"] = {},
		["liste"] = { "Human", "Skyborne", "NightElf", },
		["nil"] = nil,
	}`
	tb := AsTable(parseOne(t, src))
	if tb.Get("batch_id") != "x" || tb.Get("nom") != (Number{I: 3, IsInt: true}) {
		t.Fatal("clés chaînes / identifiant")
	}
	// setdefault : la clé explicite [1] gagne sur la position 1 ; [2.0] = clé 2 gagne aussi.
	if tb.Int[1] != "explicite" || tb.Int[2] != "deux" {
		t.Errorf("positions : %v", tb.Int)
	}
	if tb.Other["1.5"] != "décimal" {
		t.Error("clé décimale")
	}
	if l := AsTable(tb.Get("liste")).List(); len(l) != 3 || l[1] != "Skyborne" {
		t.Errorf("liste : %v", l)
	}
	if v, ok := tb.Str["nil"]; !ok || v != nil || tb.Has("nil") {
		t.Error("nil explicite conservé comme absent")
	}
}

func TestErreursPropres(t *testing.T) {
	cas := []string{
		`X = { ["a"] = "non fermée`,
		`X = { ["a"] = 1,`,
		`X = { ["a"] = "\`,
		`X = { ["a"] = print("x") }`,
		`X = { ["a"] = 1 } Y`,
		`X = "\300"`,
		`X = @`,
		`= 3`,
	}
	for _, src := range cas {
		_, err := Parse([]byte(src), nil)
		var pe *ParseError
		if err == nil || !errors.As(err, &pe) {
			t.Errorf("%q : erreur attendue, obtenu %v", src, err)
		}
	}
}

func TestProfondeurBornee(t *testing.T) {
	src := "X = " + strings.Repeat("{", 100000) + strings.Repeat("}", 100000)
	_, err := Parse([]byte(src), nil)
	if err == nil || !strings.Contains(err.Error(), "imbrication") {
		t.Fatalf("profondeur non bornée : %v", err)
	}
}

func TestVariableIgnoree(t *testing.T) {
	v, err := Parse([]byte("ForeverPulseCensusDB = { [\"a\"] = 1 }\nForeverPulseScanDB = { [\"b\"] = { \"x\" } }\n"),
		map[string]bool{"ForeverPulseCensusDB": true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := v["ForeverPulseScanDB"]; ok || v["ForeverPulseCensusDB"] == nil {
		t.Fatal("ForeverPulseScanDB ne doit jamais être construit")
	}
	// Mais un ScanDB cassé rend quand même le fichier illisible : pas de demi-lecture.
	if _, err := Parse([]byte("ForeverPulseCensusDB = {}\nForeverPulseScanDB = { \"x\""), map[string]bool{"ForeverPulseCensusDB": true}); err == nil {
		t.Fatal("fichier tronqué accepté")
	}
}

// Les fichiers réels du 23/09 (jamais commités : testdata/reel est ignoré par git).
func TestFichiersReels(t *testing.T) {
	for _, p := range []string{"../../testdata/reel/ForeverPulse.lua", "../../testdata/reel/ForeverPulse.lua.bak"} {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Skipf("%s absent : %v", p, err)
		}
		v, err := Parse(data, map[string]bool{"ForeverPulseCensusDB": true})
		if err != nil {
			t.Fatalf("%s : %v", p, err)
		}
		db := AsTable(v["ForeverPulseCensusDB"])
		if n, _ := AsInt(db.Get("schema")); n != 4 || len(AsTable(db.Get("batches")).List()) == 0 {
			t.Fatalf("%s : schéma ou lots absents", p)
		}
		// Tronqué à toutes les tailles : jamais de panique, toujours une erreur.
		for _, cut := range []int{10, 100, len(data) / 3, len(data) / 2, len(data) - 3} {
			if _, err := Parse(data[:cut], nil); err == nil {
				t.Errorf("%s tronqué à %d accepté", p, cut)
			}
		}
	}
}
