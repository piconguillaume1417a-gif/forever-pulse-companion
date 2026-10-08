package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestAllerRetour(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	c, err := Load(p) // absent : créé avec les défauts
	if err != nil || c.WowDir != `C:\Program Files (x86)\World of Warcraft` {
		t.Fatal(c, err)
	}
	c.SiteURL, c.StartWithWindows, c.WowDir, c.Language = "https://exemple.test", true, `D:\Jeux\World of Warcraft`, "en"
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	d, err := Load(p)
	if err != nil || d != c {
		t.Fatal(d, err)
	}
}

func TestDemarrageCocheParDefautEtMigration(t *testing.T) {
	if !Defaults().StartWithWindows {
		t.Fatal("lancer au démarrage doit être coché par défaut")
	}
	base := t.TempDir()
	ancien, nouveau := filepath.Join(base, "wowsync"), filepath.Join(base, "Companion")
	_ = os.MkdirAll(ancien, 0o700)
	_ = os.WriteFile(filepath.Join(ancien, "wowsync.db"), []byte("x"), 0o600)
	Migre(ancien, nouveau)
	if _, err := os.Stat(filepath.Join(nouveau, FichierBase)); err != nil {
		t.Fatal("base non migrée")
	}
	Migre(ancien, nouveau) // idempotent
}

// 0.6.0, 0.7.2 puis 0.7.3 : les anciens défauts (2 s, 600 s, 30 s) passent à 300 s une
// fois, sans toucher au reste ; un choix fait ensuite est gardé.
func TestSurveillanceCinqMinutes(t *testing.T) {
	if Defaults().PollSeconds != 300 {
		t.Fatal(Defaults().PollSeconds)
	}
	p := filepath.Join(t.TempDir(), "config.toml")
	_ = os.WriteFile(p, []byte("config_version = 2\nsite_url = 'https://exemple.test'\nstart_with_windows = false\npoll_seconds = 2\nlanguage = 'fr'\n"), 0o600)
	c, err := Load(p)
	if err != nil || c.PollSeconds != 300 || c.StartWithWindows || c.Language != "fr" || c.SiteURL != "https://exemple.test" {
		t.Fatal(c, err)
	}
	for _, k := range []struct{ avant, apres int }{{600, 300}, {30, 300}, {1800, 1800}, {5, 5}} {
		q := filepath.Join(t.TempDir(), "config.toml")
		_ = os.WriteFile(q, []byte(fmt.Sprintf("config_version = 5\nsite_url = 'https://exemple.test'\nstart_with_windows = false\npoll_seconds = %d\n", k.avant)), 0o600)
		if d, err := Load(q); err != nil || d.PollSeconds != k.apres || d.StartWithWindows {
			t.Fatal("version 5", k, d, err)
		}
		if d, err := Load(q); err != nil || d.PollSeconds != k.apres { // relu au format 6 : tel quel
			t.Fatal("relecture", k, d, err)
		}
	}
	c.PollSeconds = 1800
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	if d, err := Load(p); err != nil || d.PollSeconds != 1800 {
		t.Fatal("réglage non respecté", d, err)
	}
}

func TestConfig01DemarrageRecoche(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	// Fichier écrit par la version 0.1 : pas de config_version, false par défaut.
	_ = os.WriteFile(p, []byte("# wowsync\nsite_url = 'https://exemple.test'\nstart_with_windows = false\npoll_seconds = 2\n"), 0o600)
	c, err := Load(p)
	if err != nil || !c.StartWithWindows || c.SiteURL != "https://exemple.test" {
		t.Fatal(c, err)
	}
	// Décoché ensuite dans la fenêtre : le choix est conservé.
	c.StartWithWindows = false
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	if d, err := Load(p); err != nil || d.StartWithWindows {
		t.Fatal("choix décoché non respecté", d, err)
	}
}

// 0.6.2 (28/09/2026) : le site est https://forever-pulse.com. Un fichier des versions
// précédentes qui portait l'ancien défaut (localhost:3000) y passe une fois ; l'ancien nom
// foreverpulse.com est toujours remplacé ; une autre adresse choisie est gardée.
func TestSiteForeverPulseCom(t *testing.T) {
	if Defaults().SiteURL != "https://forever-pulse.com" {
		t.Fatal(Defaults().SiteURL)
	}
	cas := []struct{ version, avant, apres string }{
		{"3", "http://localhost:3000", "https://forever-pulse.com"},
		{"3", "http://localhost:3000/", "https://forever-pulse.com"},
		{"2", "http://localhost:3000", "https://forever-pulse.com"},
		{"3", "https://foreverpulse.com", "https://forever-pulse.com"},
		{"4", "https://www.ForeverPulse.com/", "https://forever-pulse.com"},
		{"3", "https://exemple.test", "https://exemple.test"},
		{"3", "http://localhost:3001", "http://localhost:3001"},
		{"4", "http://localhost:3000", "http://localhost:3000"}, // choisi après la 0.6.2 : gardé
		{"4", "https://forever-pulse.com", "https://forever-pulse.com"},
	}
	for _, k := range cas {
		p := filepath.Join(t.TempDir(), "config.toml")
		_ = os.WriteFile(p, []byte("config_version = "+k.version+"\nsite_url = '"+k.avant+"'\nstart_with_windows = false\npoll_seconds = 1800\nlanguage = 'fr'\n"), 0o600)
		c, err := Load(p)
		if err != nil || c.SiteURL != k.apres || c.Language != "fr" || c.StartWithWindows {
			t.Fatal(k, c, err)
		}
		if k.version == "4" || k.version == "3" {
			if c.PollSeconds != 1800 {
				t.Fatal("poll_seconds changé", k, c.PollSeconds)
			}
		}
		d, err := Load(p) // réécrit au format 4 : relu tel quel
		if err != nil || d != c {
			t.Fatal("relecture", k, d, err)
		}
	}
	if AncienDomaine("https://forever-pulse.com") || !AncienDomaine("http://foreverpulse.com/api") {
		t.Fatal("AncienDomaine")
	}
}
