//go:build linux && cgo

package webui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"wowsync/internal/vue"
)

func TestLogoDataURI(t *testing.T) {
	u := logoDataURI()
	if !strings.HasPrefix(u, "data:image/png;base64,") {
		t.Fatalf("logo = %.40q", u)
	}
	if len(u) < 1000 {
		t.Fatalf("logo trop court : %d octets", len(u))
	}
}

func TestEditeurTerminal(t *testing.T) {
	dir := t.TempDir()
	terminal := filepath.Join(dir, "xdg-terminal-exec")
	if err := os.WriteFile(terminal, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("VISUAL", "nvim -R")
	t.Setenv("EDITOR", "nano")
	got, args, ok := editeurTerminal("/tmp/companion.log")
	if !ok || got != terminal || strings.Join(args, " ") != "nvim -R /tmp/companion.log" {
		t.Fatalf("éditeur = %q %q %t", got, args, ok)
	}
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if _, _, ok := editeurTerminal("/tmp/companion.log"); ok {
		t.Fatal("aucun éditeur ne doit produire de commande")
	}
}

func TestDossiersBattleNetWine(t *testing.T) {
	prefix := t.TempDir()
	t.Setenv("WINEPREFIX", prefix)
	var trouve bool
	for _, d := range DossiersBattleNet() {
		if d == filepath.Join(prefix, "drive_c", "Program Files (x86)", "World of Warcraft") {
			trouve = true
		}
	}
	if !trouve {
		t.Fatal("préfixe Wine absent des candidats")
	}
}

func TestAutostart(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	if StartsWithWindows() {
		t.Fatal("autostart présent avant activation")
	}
	if err := StartWithWindows(true); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(cfg, "autostart", "forever-pulse-companion.desktop")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "--tray") {
		t.Fatalf("autostart sans --tray :\n%s", b)
	}
	if !StartsWithWindows() {
		t.Fatal("autostart non détecté")
	}
	if err := StartWithWindows(false); err != nil {
		t.Fatal(err)
	}
	if StartsWithWindows() {
		t.Fatal("autostart encore présent")
	}
}

func TestThemeSombreGTK(t *testing.T) {
	t.Setenv("GTK_THEME", "Adwaita:dark")
	if !themeSombre() {
		t.Fatal("GTK_THEME sombre non détecté")
	}
	t.Setenv("GTK_THEME", "Adwaita:light")
	// gsettings peut encore annoncer un thème sombre sur la machine de test.
	_ = themeSombre()
}

func TestSetEtatPreservesStartAtLogin(t *testing.T) {
	w := NewWindow()
	w.SetChecked(true)
	w.SetEtat(vue.Modele{Version: "1", Message: "x"})
	w.mu.Lock()
	on := w.modele.Demarrage
	w.mu.Unlock()
	if !on {
		t.Fatal("SetEtat a effacé « lancer à la connexion »")
	}
}
