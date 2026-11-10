package watch

import (
	"os"
	"path/filepath"
	"testing"
)

func mkdir(t *testing.T, p ...string) string {
	t.Helper()
	d := filepath.Join(p...)
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}

func lua(t *testing.T, wow, client string) {
	t.Helper()
	sv := mkdir(t, wow, client, "WTF", "Account", "ACC", "SavedVariables")
	if err := os.WriteFile(filepath.Join(sv, "ForeverPulse.lua"), []byte("ForeverPulseDB = {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestRacineDepuisSousDossiers(t *testing.T) {
	base := t.TempDir()
	wow := filepath.Join(base, "Jeux", "World of Warcraft")
	lua(t, wow, "_classic_beta_")
	for _, choisi := range []string{
		wow,
		filepath.Join(wow, "_classic_beta_"),
		filepath.Join(wow, "_classic_beta_", "WTF"),
		filepath.Join(wow, "_classic_beta_", "WTF", "Account", "ACC", "SavedVariables"),
		filepath.Join(wow, "_classic_beta_") + `\`,
	} {
		if got := Racine(choisi); got != wow {
			t.Errorf("Racine(%q) = %q, attendu %q", choisi, got, wow)
		}
	}
	if Examine(wow) != DossierPret {
		t.Fatalf("Examine(%q) = %d", wow, Examine(wow))
	}
}

// Un dossier personnel nommé « _jeux_ » n'est pas un client de jeu.
func TestRacineIgnoreUnDossierSoulignéQuelconque(t *testing.T) {
	base := t.TempDir()
	wow := filepath.Join(base, "_jeux_", "World of Warcraft")
	lua(t, wow, "_retail_")
	if got := Racine(wow); got != wow {
		t.Fatalf("Racine = %q, attendu %q", got, wow)
	}
	vide := mkdir(t, base, "_vide_")
	if got := Racine(vide); got != vide {
		t.Fatalf("Racine(vide) = %q", got)
	}
}

func TestExamineEtDetecte(t *testing.T) {
	base := t.TempDir()
	absent := filepath.Join(base, "rien")
	jeu := filepath.Join(base, "A", "World of Warcraft")
	mkdir(t, jeu, "_retail_", "WTF")
	pret := filepath.Join(base, "B", "World of Warcraft")
	lua(t, pret, "_classic_beta_")

	if Examine(absent) != DossierAbsent || Examine(jeu) != DossierSansLua || Examine(pret) != DossierPret {
		t.Fatalf("Examine : %d %d %d", Examine(absent), Examine(jeu), Examine(pret))
	}
	// Le chemin Battle.net désigne le client ; un dossier avec l'addon l'emporte.
	if got := Detecte([]string{absent, filepath.Join(jeu, "_retail_") + `\`, filepath.Join(pret, "_classic_beta_") + `\`}); got != pret {
		t.Fatalf("Detecte = %q, attendu %q", got, pret)
	}
	if got := Detecte([]string{absent, jeu}); got != jeu {
		t.Fatalf("Detecte sans addon = %q, attendu %q", got, jeu)
	}
	if got := Detecte([]string{absent, ""}); got != "" {
		t.Fatalf("Detecte vide = %q", got)
	}
}

func TestCandidatsCourants(t *testing.T) {
	c := CandidatsCourants([]string{"D:"})
	if len(c) == 0 || c[0] != filepath.Join(`D:\`, `Program Files (x86)\World of Warcraft`) {
		t.Fatalf("candidats : %v", c)
	}
}
