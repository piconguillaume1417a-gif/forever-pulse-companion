//go:build linux

package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLinuxDefaults(t *testing.T) {
	c := Defaults()
	if c.AutoUpdate {
		t.Fatal("la mise à jour automatique doit rester désactivée sous Linux")
	}
	if strings.HasPrefix(c.WowDir, `C:\`) || !strings.HasSuffix(c.WowDir, filepath.Join("Games", "World of Warcraft")) {
		t.Fatalf("WowDir = %q", c.WowDir)
	}
}

func TestLoadReplacesWindowsWowDir(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	body := "config_version = 6\nsite_url = 'https://forever-pulse.com'\n" +
		"wow_dir = 'C:\\Program Files (x86)\\World of Warcraft'\n" +
		"start_with_windows = true\npoll_seconds = 300\nlanguage = ''\nauto_update = true\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if c.WowDir != Defaults().WowDir || strings.HasPrefix(c.WowDir, `C:\`) {
		t.Fatalf("WowDir = %q", c.WowDir)
	}
	b, err := os.ReadFile(p)
	if err != nil || !strings.Contains(string(b), Defaults().WowDir) {
		t.Fatalf("config.toml non réécrit : %s, %v", b, err)
	}
}

func TestStartAtLoginPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.toml")
	c, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	c.StartWithWindows = false
	c.Language = "fr"
	if err := Save(p, c); err != nil {
		t.Fatal(err)
	}
	d, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if d.StartWithWindows || d.Language != "fr" {
		t.Fatalf("réglages perdus : %+v", d)
	}
}
