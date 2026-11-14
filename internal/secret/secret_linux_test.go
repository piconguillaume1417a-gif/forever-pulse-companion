//go:build linux

package secret

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFallbackVaultWithoutSecretTool(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("PATH", "") // pas de secret-tool : le fichier 0600 prend le relais
	t.Setenv("WOWSYNC_TOKEN", "")

	const jeton = "fpc_AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	if err := Set(jeton); err != nil {
		t.Fatal(err)
	}
	got, err := Get()
	if err != nil || got != jeton {
		t.Fatalf("Get = %q, %v", got, err)
	}
	p := fallbackPath(Target)
	b, err := os.ReadFile(p)
	if err != nil || strings.TrimSpace(string(b)) != jeton {
		t.Fatalf("fichier de secours : %q, %v", b, err)
	}
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("permissions = %v, %v", fi.Mode().Perm(), err)
	}
	if err := Delete(); err != nil {
		t.Fatal(err)
	}
	if _, err := Get(); err != ErrAbsent {
		t.Fatalf("Get après Delete = %v", err)
	}
	if _, err := os.Stat(filepath.Dir(p)); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestFallbackTightensExistingPermissions(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfg)
	t.Setenv("PATH", "")
	t.Setenv("WOWSYNC_TOKEN", "")

	p := fallbackPath(Target)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("ancien"), 0o644); err != nil {
		t.Fatal(err)
	}
	const jeton = "fpc_CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC"
	if err := Set(jeton); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("fichier = %v, %v", fi.Mode().Perm(), err)
	}
	di, err := os.Stat(filepath.Dir(p))
	if err != nil || di.Mode().Perm() != 0o700 {
		t.Fatalf("dossier = %v, %v", di.Mode().Perm(), err)
	}
}

func TestEnvironmentTokenWins(t *testing.T) {
	const jeton = "fpc_BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
	t.Setenv("WOWSYNC_TOKEN", jeton)
	if got, err := Get(); err != nil || got != jeton {
		t.Fatalf("Get = %q, %v", got, err)
	}
}
