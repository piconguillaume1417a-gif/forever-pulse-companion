package schema

// Schéma 5 (addon 3.7.0) : un fichier compact doit produire exactement les mêmes corps
// d'envoi (schéma 4) que le même relevé écrit au schéma 4.

import (
	"encoding/json"
	"strings"
	"testing"
)

func corps(t *testing.T, f *Fichier, i int) string {
	t.Helper()
	l := f.Lots[i]
	if !l.Valide() {
		t.Fatalf("lot %d invalide : %v", i, l.Erreurs)
	}
	b, err := json.Marshal(l.Payload)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSchema5MemeCorpsQueSchema4(t *testing.T) {
	d4 := fichierValide()
	canalDeltaValide(d4)
	d5 := fichierValide()
	canalDeltaValide(d5)
	schema5(d5)
	f4, f5 := analyse(t, d4), analyse(t, d5)
	if len(f5.Lots) != len(f4.Lots)+1 {
		t.Fatalf("%d lots au schéma 5, %d attendus", len(f5.Lots), len(f4.Lots)+1)
	}
	for i := range f4.Lots {
		if a, b := corps(t, f4, i), corps(t, f5, i); a != b {
			t.Fatalf("lot %d : corps différent\n  schéma 4 : %s\n  schéma 5 : %s", i, a, b)
		}
	}
}

func TestSchema5WhoDecode(t *testing.T) {
	d := fichierValide()
	schema5(d)
	f := analyse(t, d)
	p := f.Lots[len(f.Lots)-1].Payload
	b, _ := json.Marshal(p)
	for _, attendu := range []string{`"Guerrier"`, `"Les Trois Fromages"`, `"Kharanos"`, `"Dun Morogh"`} {
		if !strings.Contains(string(b), attendu) {
			t.Fatalf("%s absent du corps /who décodé : %s", attendu, b)
		}
	}
}

func TestSchema5LotSansDictionnaire(t *testing.T) {
	d := fichierValide()
	schema5(d)
	delete(d, "dicts")
	f := analyse(t, d)
	if f.Lots[len(f.Lots)-1].Valide() {
		t.Fatal("lot /who codé sans dictionnaire accepté")
	}
}

// Schéma 6 : noms en indices dans la table partagée, mêmes corps d'envoi qu'au schéma 4.
func TestSchema6MemeCorpsQueSchema4(t *testing.T) {
	d4 := fichierValide()
	canalDeltaValide(d4)
	d6 := fichierValide()
	canalDeltaValide(d6)
	schema6(d6)
	f4, f6 := analyse(t, d4), analyse(t, d6)
	if len(f6.Lots) != len(f4.Lots)+1 {
		t.Fatalf("%d lots au schéma 6, %d attendus", len(f6.Lots), len(f4.Lots)+1)
	}
	for i := range f4.Lots {
		if a, b := corps(t, f4, i), corps(t, f6, i); a != b {
			t.Fatalf("lot %d : corps différent\n  schéma 4 : %s\n  schéma 6 : %s", i, a, b)
		}
	}
	b, _ := json.Marshal(f6.Lots[len(f6.Lots)-1].Payload)
	if !strings.Contains(string(b), `"Guildesynth"`) {
		t.Fatalf("nom /who non décodé : %s", b)
	}
}
