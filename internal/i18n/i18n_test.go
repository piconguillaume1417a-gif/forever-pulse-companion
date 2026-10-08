package i18n

import (
	"regexp"
	"testing"
)

var verbe = regexp.MustCompile(`%[a-z]`)

func TestToutesLesClesTraduites(t *testing.T) {
	for k, m := range msgs {
		if m[0] == "" || m[1] == "" {
			t.Errorf("%s : traduction manquante", k)
		}
		a, b := verbe.FindAllString(m[0], -1), verbe.FindAllString(m[1], -1)
		if len(a) != len(b) {
			t.Errorf("%s : %v ≠ %v", k, a, b)
			continue
		}
		for i := range a {
			if a[i] != b[i] {
				t.Errorf("%s : %v ≠ %v", k, a, b)
			}
		}
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
	if Choisie("en") != EN || Choisie("fr") != FR {
		t.Fatal("réglage non respecté")
	}
	if Choisie("") != EN || Choisie("de") != EN {
		t.Fatal("sans réglage valide, l'interface doit démarrer en anglais")
	}
}
