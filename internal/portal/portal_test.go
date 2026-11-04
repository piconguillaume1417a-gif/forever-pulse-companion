package portal

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodeConnu(t *testing.T) {
	for _, c := range []string{"1", "2", "3", "4", "5", "91", "92", "93", "94", "95", "US", "NA", "KR", "EU", "TW", "CN",
		"us", "na", "kr", "eu", "tw", "cn", "Eu", " 93 ", "\t3\r\n", "  cn"} {
		if !CodeConnu(c) {
			t.Errorf("%q devrait être connu", c)
		}
	}
	for _, c := range []string{"", " ", "0", "6", "90", "96", "110", "111", "UNKNOWN", "USA", "E U", "9 3", "EUW", "test", "public-test"} {
		if CodeConnu(c) {
			t.Errorf("%q devrait être inconnu", c)
		}
	}
}

// La condition de dossier tient en une seule fonction : la retirer se fait ici.
func TestRepliAutorise(t *testing.T) {
	for _, d := range []string{"_forever_", "_FOREVER_", "_Forever_"} {
		if !RepliAutorise(d) {
			t.Errorf("%q devrait être autorisé", d)
		}
	}
	for _, d := range []string{"", "_beta_", "_ptr_", "_xptr_", "_retail_", "_classic_beta_", "_forever_beta_", "forever", "__forever__"} {
		if RepliAutorise(d) {
			t.Errorf("%q ne devrait pas être autorisé", d)
		}
	}
}

func TestDossierJeu(t *testing.T) {
	racine := filepath.Join("C:"+string(filepath.Separator), "Games", "World of Warcraft", "_forever_")
	beta := filepath.Join("C:"+string(filepath.Separator), "Games", "World of Warcraft", "_beta_")
	cas := []struct {
		chemin, racine, nom string
	}{
		{filepath.Join(racine, "WTF", "Account", "123#1", "SavedVariables", "ForeverPulse.lua"), racine, "_forever_"},
		{filepath.Join(racine, "WTF", "Account", "123#1", "SavedVariables", "ForeverPulse.lua.bak"), racine, "_forever_"},
		{filepath.Join(racine, "wtf", "Account", "123#1", "SavedVariables", "ForeverPulse.lua"), racine, "_forever_"},
		{filepath.Join(beta, "WTF", "Account", "123#1", "SavedVariables", "ForeverPulse.lua"), beta, "_beta_"},
		// Un dossier de compte ne peut pas se faire passer pour le dossier de jeu.
		{filepath.Join(beta, "WTF", "Account", "_forever_", "SavedVariables", "ForeverPulse.lua"), beta, "_beta_"},
		{filepath.Join(beta, "WTF", "Account", "WTF", "SavedVariables", "ForeverPulse.lua"), beta, "_beta_"},
		{filepath.Join("C:"+string(filepath.Separator), "Temp", "ForeverPulse.lua"), "", ""},
		{"ForeverPulse.lua", "", ""},
		{"", "", ""},
	}
	for _, c := range cas {
		r, n := DossierJeu(c.chemin)
		if r != c.racine || n != c.nom {
			t.Errorf("%q : obtenu (%q, %q), attendu (%q, %q)", c.chemin, r, n, c.racine, c.nom)
		}
	}
}

// fauxCompte : de fausses lignes de compte, jamais de vraies données.
const fauxCompte = "SET accountName \"FAKE-ACCOUNT-TESTONLY\"\nSET accountList \"FAKE-LIST-TESTONLY!\"\nSET lastCharacterIndex \"7\"\n"

func ecrit(t *testing.T, contenu string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "Config.wtf")
	if err := os.WriteFile(p, []byte(contenu), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLireFormes(t *testing.T) {
	cas := []struct {
		nom, contenu, valeur string
		statut               Statut
	}{
		{"nominal", fauxCompte + `SET portal "EU"` + "\n", "EU", Resolu},
		{"crlf", strings.ReplaceAll(fauxCompte, "\n", "\r\n") + `SET portal "KR"` + "\r\n", "KR", Resolu},
		{"bom sur la ligne portal", "\xef\xbb\xbfSET portal \"TW\"\r\n" + fauxCompte, "TW", Resolu},
		{"bom ailleurs", "\xef\xbb\xbf" + fauxCompte + "SET portal \"TW\"\n", "TW", Resolu},
		{"casse de SET et portal", fauxCompte + `set PORTAL "US"`, "US", Resolu},
		{"casse de la valeur", `Set Portal "cn"`, "CN", Resolu},
		{"sans guillemets", `SET portal EU`, "EU", Resolu},
		{"apostrophes", `SET portal 'EU'`, "EU", Resolu},
		{"tabulations et espaces", "  SET\t portal \t \"EU\"  \n", "EU", Resolu},
		{"sans fin de ligne", fauxCompte + `SET portal "EU"`, "EU", Resolu},
		{"répétée à l'identique", "SET portal \"EU\"\nSET portal \"eu\"\n", "EU", Resolu},
		{"aucune ligne portal", fauxCompte, "", PasClair},
		{"fichier vide", "", "", PasClair},
		{"valeur vide", `SET portal ""`, "", PasClair},
		{"sans valeur", `SET portal`, "", PasClair},
		{"test", `SET portal "test"`, "", PasClair},
		{"public-test", `SET portal "public-test"`, "", PasClair},
		{"NA n'est pas un portail", `SET portal "NA"`, "", PasClair},
		{"code numérique", `SET portal "93"`, "", PasClair},
		{"guillemets déséquilibrés", `SET portal "EU`, "", PasClair},
		{"guillemets mélangés", `SET portal "EU'`, "", PasClair},
		{"valeur suivie de texte", `SET portal "EU" "KR"`, "", PasClair},
		{"deux valeurs différentes", "SET portal \"EU\"\nSET portal \"KR\"\n", "", PasClair},
		{"une valeur puis une autre inconnue", "SET portal \"EU\"\nSET portal \"test\"\n", "", PasClair},
		{"autre nom de variable", `SET portalX "EU"`, "", PasClair},
		{"commande autre que SET", `GET portal "EU"`, "", PasClair},
		{"ligne commentée", `# SET portal "EU"`, "", PasClair},
		{"utf-16", "S\x00E\x00T\x00 \x00p\x00o\x00r\x00t\x00a\x00l\x00 \x00E\x00U\x00", "", PasClair},
		{"fin d'une ligne trop longue", strings.Repeat("x", tamponLigne*3) + ` SET portal "EU"` + "\n", "", PasClair},
		{"ligne suivant une ligne trop longue", strings.Repeat("x", tamponLigne*3) + "\nSET portal \"EU\"\n", "EU", Resolu},
	}
	for _, c := range cas {
		t.Run(c.nom, func(t *testing.T) {
			v, st := Lire(ecrit(t, c.contenu))
			if v != c.valeur || st != c.statut {
				t.Fatalf("obtenu (%q, %d), attendu (%q, %d)", v, st, c.valeur, c.statut)
			}
		})
	}
}

func TestLireAbsentTropGrosIllisible(t *testing.T) {
	if v, st := Lire(filepath.Join(t.TempDir(), "Config.wtf")); st != Absent || v != "" {
		t.Fatalf("absent : (%q, %d)", v, st)
	}
	gros := `SET portal "EU"` + "\n" + strings.Repeat("SET x \"y\"\n", TailleMax/10+1)
	if v, st := Lire(ecrit(t, gros)); st != TropGros || v != "" {
		t.Fatalf("trop gros : (%q, %d)", v, st)
	}
	// À la limite exacte, le fichier est encore lu.
	juste := `SET portal "EU"` + "\n"
	juste += strings.Repeat("#", TailleMax-len(juste))
	if v, st := Lire(ecrit(t, juste)); st != Resolu || v != "EU" {
		t.Fatalf("à la limite : (%q, %d)", v, st)
	}
	// Un dossier à la place du fichier.
	d := filepath.Join(t.TempDir(), "Config.wtf")
	if err := os.Mkdir(d, 0o700); err != nil {
		t.Fatal(err)
	}
	if v, st := Lire(d); (st != Illisible && st != Absent) || v != "" {
		t.Fatalf("dossier : (%q, %d)", v, st)
	}
}

// Un fichier qui grandit pendant la lecture reste borné.
func TestAnalyseBornee(t *testing.T) {
	if _, st := analyse(strings.NewReader(strings.Repeat("a\n", TailleMax))); st != TropGros {
		t.Fatalf("statut %d", st)
	}
}

type lecteurPanne struct{ err error }

func (l lecteurPanne) Read([]byte) (int, error) { return 0, l.err }

// L'erreur de lecture n'a aucun chemin vers l'appelant : rien que le statut.
func TestAnalyseErreur(t *testing.T) {
	if v, st := analyse(lecteurPanne{errors.New("FAKE-ACCOUNT-TESTONLY")}); st != Illisible || v != "" {
		t.Fatalf("(%q, %d)", v, st)
	}
}

// PourFichier sur un dossier autre que _forever_ n'ouvre pas Config.wtf, même lisible.
func TestPourFichierDossier(t *testing.T) {
	base := t.TempDir()
	prepare := func(dossier, contenu string) string {
		wtf := filepath.Join(base, dossier, "WTF")
		sv := filepath.Join(wtf, "Account", "123#1", "SavedVariables")
		if err := os.MkdirAll(sv, 0o700); err != nil {
			t.Fatal(err)
		}
		if contenu != "" {
			if err := os.WriteFile(filepath.Join(wtf, "Config.wtf"), []byte(contenu), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		return filepath.Join(sv, "ForeverPulse.lua")
	}
	if v, st := PourFichier(prepare("_forever_", fauxCompte+`SET portal "EU"`)); st != Resolu || v != "EU" {
		t.Fatalf("_forever_ : (%q, %d)", v, st)
	}
	if v, st := PourFichier(prepare("_FOREVER2_", `SET portal "EU"`)); st != Ecarte || v != "" {
		t.Fatalf("autre nom : (%q, %d)", v, st)
	}
	for _, d := range []string{"_beta_", "_ptr_", "_xptr_", "_classic_beta_"} {
		if v, st := PourFichier(prepare(d, `SET portal "EU"`)); st != Ecarte || v != "" {
			t.Fatalf("%s : (%q, %d)", d, v, st)
		}
	}
	if v, st := PourFichier(prepare("_forever_", "")); st != Resolu || v != "EU" {
		t.Fatalf("relecture : (%q, %d)", v, st)
	}
	if err := os.Remove(filepath.Join(base, "_forever_", "WTF", "Config.wtf")); err != nil {
		t.Fatal(err)
	}
	// Aucun cache : le fichier retiré n'a plus de valeur.
	if v, st := PourFichier(filepath.Join(base, "_forever_", "WTF", "Account", "123#1", "SavedVariables", "ForeverPulse.lua")); st != Absent || v != "" {
		t.Fatalf("après retrait : (%q, %d)", v, st)
	}
	if v, st := PourFichier(filepath.Join(base, "ForeverPulse.lua")); st != Ecarte || v != "" {
		t.Fatalf("hors dossier : (%q, %d)", v, st)
	}
}

func TestRepliObjets(t *testing.T) {
	eu := func() (string, bool) { return "EU", true }
	rien := func() (string, bool) { return "", false }
	jamais := func() (string, bool) {
		t.Fatal("valeur lue alors que toutes les régions sont connues")
		return "", false
	}

	// Une région connue : le même octet, sans lecture, sous toutes ses formes.
	connus := `{"a":{"realm_id":1,"region":"93"},"b":{"region":" eu "},"c":{"region":3},"d":{"region":"Cn"}}`
	if out, ch := RepliObjets([]byte(connus), jamais); ch || string(out) != connus {
		t.Fatalf("connus : %s", out)
	}
	// Un inconnu parmi des connus : seul lui change, à sa place.
	mele := `{"a":{"realm_id":1,"region":"93","x":"é<&>"},"b":{"realm_id":2,"region":"110","faction":"Horde"}}`
	attendu := `{"a":{"realm_id":1,"region":"93","x":"é<&>"},"b":{"realm_id":2,"region":"EU","faction":"Horde"}}`
	if out, ch := RepliObjets([]byte(mele), eu); !ch || string(out) != attendu {
		t.Fatalf("mêlé : %s", out)
	}
	for _, brut := range []string{`""`, `"  "`, `null`, `"90"`, `90`, `true`, `{}`} {
		avant := `{"a":{"region":` + brut + `,"z":1}}`
		if out, ch := RepliObjets([]byte(avant), eu); !ch || string(out) != `{"a":{"region":"EU","z":1}}` {
			t.Fatalf("%s : %s", brut, out)
		}
		if out, ch := RepliObjets([]byte(avant), rien); ch || string(out) != avant {
			t.Fatalf("%s sans valeur : %s", brut, out)
		}
	}
	// Région absente : ajoutée en fin d'objet, ou laissée absente sans valeur.
	if out, ch := RepliObjets([]byte(`{"a":{"realm_id":1}}`), eu); !ch || string(out) != `{"a":{"realm_id":1,"region":"EU"}}` {
		t.Fatalf("absente : %s", out)
	}
	if out, ch := RepliObjets([]byte(`{"a":{"realm_id":1}}`), rien); ch || string(out) != `{"a":{"realm_id":1}}` {
		t.Fatalf("absente sans valeur : %s", out)
	}
	for _, x := range []string{`{}`, `{"a":1}`, `{"a":null}`, `{"a":[1]}`, `pas du json`, ``} {
		if out, ch := RepliObjets([]byte(x), jamais); ch || string(out) != x {
			t.Fatalf("%q : %s", x, out)
		}
	}
	// Un membre de test n'est jamais une cible : pas de lecture s'il est seul, et il garde
	// sa valeur à côté d'un membre live remplacé. Absent ou live reste éligible.
	for _, env := range []string{`"beta"`, `"BETA"`, `" ptr "`} {
		seul := `{"a":{"environment":` + env + `,"region":"90"}}`
		if out, ch := RepliObjets([]byte(seul), jamais); ch || string(out) != seul {
			t.Fatalf("%s seul : %s", env, out)
		}
		deux := `{"a":{"environment":` + env + `,"region":"90"},"b":{"environment":"live","region":"96"},"c":{"region":"96"}}`
		attenduDeux := `{"a":{"environment":` + env + `,"region":"90"},"b":{"environment":"live","region":"EU"},"c":{"region":"EU"}}`
		if out, ch := RepliObjets([]byte(deux), eu); !ch || string(out) != attenduDeux {
			t.Fatalf("%s avec live : %s", env, out)
		}
	}
	// La valeur n'est lue qu'une fois par objet, quel que soit le nombre d'inconnus.
	n := 0
	un := func() (string, bool) { n++; return "KR", true }
	if out, _ := RepliObjets([]byte(`{"a":{"region":"90"},"b":{"region":"96"}}`), un); n != 1 || string(out) != `{"a":{"region":"KR"},"b":{"region":"KR"}}` {
		t.Fatalf("%d lectures : %s", n, out)
	}
}

func TestRepliMeta(t *testing.T) {
	eu := func() (string, bool) { return "EU", true }
	meta := `{"file":{"sha256":"a"},"contexts":{"x":{"environment":"live","region":"90"}},"catalog":null,"talent_trees":[]}`
	attendu := `{"file":{"sha256":"a"},"contexts":{"x":{"environment":"live","region":"EU"}},"catalog":null,"talent_trees":[]}`
	if out := RepliMeta([]byte(meta), eu); string(out) != attendu {
		t.Fatalf("%s", out)
	}
	connu := strings.ReplaceAll(meta, `"90"`, `"93"`)
	if out := RepliMeta([]byte(connu), func() (string, bool) { t.Fatal("lecture inutile"); return "", false }); string(out) != connu {
		t.Fatalf("%s", out)
	}
	if out := RepliMeta([]byte(meta), func() (string, bool) { return "", false }); string(out) != meta {
		t.Fatalf("%s", out)
	}
	for _, x := range []string{`{}`, `{"file":{}}`, `pas du json`} {
		if out := RepliMeta([]byte(x), eu); string(out) != x {
			t.Fatalf("%q : %s", x, out)
		}
	}
}
