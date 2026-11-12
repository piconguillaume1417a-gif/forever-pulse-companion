package i18n

import (
	"reflect"
	"regexp"
	"strconv"
	"testing"
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
