package i18n

import (
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
)

// verbe : un verbe de format de fmt (%s, %d, %[2]s, %5.1f…) ou « %% ».
var verbe = regexp.MustCompile(`%(?:\[(\d+)\])?[-+# 0]*\d*(?:\.\d+)?([a-zA-Z%])`)

// arguments : pour chaque argument (1, 2…), les verbes qui le lisent, et le nombre
// de « %% ». Un verbe indexé (%[2]s) déplace le compteur comme fmt.
func arguments(s string) (map[int][]string, int) {
	args, pourcents, n := map[int][]string{}, 0, 1
	for _, m := range verbe.FindAllStringSubmatch(s, -1) {
		if m[2] == "%" {
			pourcents++
			continue
		}
		if m[1] != "" {
			n, _ = strconv.Atoi(m[1])
		}
		args[n] = append(args[n], m[2])
		n++
	}
	return args, pourcents
}

func TestToutesLesClesTraduites(t *testing.T) {
	if len(en) == 0 {
		t.Fatal("table anglaise vide")
	}
	for _, g := range langues {
		for k := range en {
			if g.textes[k] == "" {
				t.Errorf("%s/%s : traduction manquante", g.code, k)
			}
		}
		for k := range g.textes {
			if _, ok := en[k]; !ok {
				t.Errorf("%s/%s : clé absente de l'anglais", g.code, k)
			}
		}
	}
}

func TestMemesVerbesQueLeFrancais(t *testing.T) {
	for k, ref := range fr {
		a, pa := arguments(ref)
		for _, g := range langues {
			b, pb := arguments(g.textes[k])
			if !reflect.DeepEqual(a, b) || pa != pb {
				t.Errorf("%s/%s : %v (%d %%%%) ≠ français %v (%d %%%%)", g.code, k, b, pb, a, pa)
			}
		}
	}
}

func TestTableDesLangues(t *testing.T) {
	if len(Toutes) != len(langues) || Toutes[0] != FR || Toutes[1] != EN {
		t.Fatal("ordre de la liste de choix :", Toutes)
	}
	vus := map[Lang]bool{}
	for _, g := range langues {
		if vus[g.code] || g.nom == "" || g.court == "" || g.textes == nil {
			t.Errorf("ligne invalide : %+v", g.code)
		}
		vus[g.code] = true
		if l, ok := Parse(string(g.code)); !ok || l != g.code || Nom(g.code) != g.nom || Court(g.code) != g.court {
			t.Errorf("%s : Parse/Nom/Court", g.code)
		}
	}
	if Nom("de") != "English" || Court("de") != "EN" {
		t.Error("langue inconnue : nom anglais attendu")
	}
}

func TestChoixEtRepli(t *testing.T) {
	defer Set(FR)
	Set(EN)
	if T("btn.quit") != "Quit" || T("st.pending", 3) != "3 batches waiting to be sent" {
		t.Fatal(T("btn.quit"), T("st.pending", 3))
	}
	Set("de") // inconnue : ignorée
	if Get() != EN {
		t.Fatal(Get())
	}
	Set(FR)
	if T("btn.quit") != "Quitter" || T("inconnue") != "inconnue" {
		t.Fatal(T("btn.quit"))
	}
}

// Une clé absente (ou vide) d'une langue s'affiche en anglais.
func TestRepliAnglais(t *testing.T) {
	defer Set(FR)
	for _, g := range langues {
		if g.code == EN {
			continue
		}
		Set(g.code)
		avant, ok := g.textes["btn.quit"]
		delete(g.textes, "btn.quit")
		absente := T("btn.quit")
		g.textes["btn.quit"] = ""
		vide := T("btn.quit")
		if ok {
			g.textes["btn.quit"] = avant
		}
		if absente != "Quit" || vide != "Quit" {
			t.Errorf("%s : repli %q / %q", g.code, absente, vide)
		}
	}
}

// Le réglage enregistré (config.toml) est lu à l'identique : « fr » et « en » des
// versions précédentes ; vide ou inconnu = anglais.
func TestReglageExistant(t *testing.T) {
	if string(FR) != "fr" || string(EN) != "en" {
		t.Fatal("codes enregistrés modifiés")
	}
	if Choisie("en") != EN || Choisie("fr") != FR {
		t.Fatal("réglage non respecté")
	}
	for _, v := range []string{"", "de", "FR", "fr-FR", "english", " fr"} {
		if Choisie(v) != EN {
			t.Errorf("%q : sans réglage valide, l'interface doit démarrer en anglais", v)
		}
	}
}

func TestVerbesIndexes(t *testing.T) {
	a, _ := arguments("%s : %d")
	b, _ := arguments("%[2]d %[1]s")
	c, _ := arguments("%d : %s")
	if !reflect.DeepEqual(a, b) || reflect.DeepEqual(a, c) {
		t.Fatal(a, b, c)
	}
	if _, p := arguments("100 %%"); p != 1 {
		t.Fatal(p)
	}
}

// K4 : après un verbe de format, la valeur peut finir par une consonne ou une
// voyelle ; une particule variable collée (%s이, %d를, %s로…) serait fausse une
// fois sur deux. Forme double (이(가)) ou tournure sans particule seulement.
var particuleVariable = regexp.MustCompile(`%(?:\[\d+\])?[-+# 0]*\d*(?:\.\d+)?[a-zA-Z](이|가|을|를|은|는|과|와|으로|로|이다|이며|이고)(?:[\s.,!?:;)…”]|$)`)

// K6 : en coréen, latin seulement pour les marques, sigles, noms de fichiers et
// de dossiers du jeu.
var latin = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_.\-/*]*`)

var latinPermis = map[string]bool{
	"Forever": true, "Pulse": true, "Companion": true, "PC": true, "Windows": true, "Esc": true,
	"World": true, "of": true, "Warcraft": true, "ForeverPulse.lua": true, "config.toml": true,
	"wow_dir": true, "_retail_": true, "_classic_beta_": true, "/reload": true, "reload": true,
	"forever-pulse.com": true, "forever-pulse.com/account/companion": true,
	"_": true, "WTF": true, "Account": true, "SavedVariables": true,
}

func TestCoreen(t *testing.T) {
	for k, s := range ko {
		if m := particuleVariable.FindString(s); m != "" {
			t.Errorf("ko/%s : particule variable après un paramètre (%q)", k, m)
		}
		for _, r := range s {
			switch {
			case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana):
				t.Errorf("ko/%s : hanja ou kana %q", k, r)
			case r >= 0x3000 && r <= 0x303F, r >= 0xFF01 && r <= 0xFF60:
				t.Errorf("ko/%s : ponctuation pleine chasse %q", k, r)
			}
		}
		if k == "fmt.date" || k == "fmt.datetime" || k == "fmt.time" {
			continue
		}
		for _, mot := range latin.FindAllString(verbe.ReplaceAllString(strings.ReplaceAll(s, `\`, " "), " "), -1) {
			mot = strings.TrimRight(mot, ".")
			if !latinPermis[mot] && !strings.HasPrefix(mot, "_") {
				t.Errorf("ko/%s : mot latin %q hors liste", k, mot)
			}
		}
	}
}

func TestDatesCoreennes(t *testing.T) {
	defer Set(FR)
	Set(KO)
	d := time.Date(2026, 9, 18, 15, 4, 0, 0, time.UTC)
	if g := d.Format(T("fmt.date")); g != "2026. 9. 18." {
		t.Error(g)
	}
	if g := d.Format(T("fmt.datetime")); g != "9월 18일 15:04" {
		t.Error(g)
	}
	if g := T("st.pending", 3); g != "전송 대기 중인 묶음 3개" {
		t.Error(g)
	}
	if !particuleVariable.MatchString("버전 %s로 업데이트") || particuleVariable.MatchString("%s에 다시 시도") || particuleVariable.MatchString("%s이(가) 준비됨") {
		t.Error("contrôle K4")
	}
}
