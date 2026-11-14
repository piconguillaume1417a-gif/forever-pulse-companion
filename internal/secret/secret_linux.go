//go:build linux

package secret

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Linux : Secret Service (gnome-keyring, KWallet) via secret-tool. Si l'outil
// manque, un fichier 0600 dans le dossier de configuration prend le relais.
// L'environnement WOWSYNC_TOKEN reste prioritaire pour les tests de bout en bout.

func Set(token string) error {
	clean, ok := Normalise(token)
	if !ok {
		return ErrAbsent
	}
	return writeBlob(Target, clean)
}

func Get() (string, error) {
	if t, ok := Normalise(os.Getenv("WOWSYNC_TOKEN")); ok {
		return t, nil
	}
	if t, err := read(Target); err == nil {
		return t, nil
	}
	t, err := read(LegacyTarget)
	if err != nil {
		return "", ErrAbsent
	}
	if Set(t) == nil {
		_ = remove(LegacyTarget)
	}
	return t, nil
}

func Delete() error {
	err := remove(Target)
	if err2 := remove(LegacyTarget); err == nil {
		err = err2
	}
	return err
}

func secretTool() string {
	p, err := exec.LookPath("secret-tool")
	if err != nil {
		return ""
	}
	return p
}

func read(name string) (string, error) {
	if tool := secretTool(); tool != "" {
		out, err := exec.Command(tool, "lookup", "service", "forever-pulse", "account", name).Output()
		if err == nil && strings.TrimSpace(string(out)) != "" {
			return strings.TrimSpace(string(out)), nil
		}
	}
	b, err := os.ReadFile(fallbackPath(name))
	if err != nil {
		return "", ErrAbsent
	}
	return strings.TrimSpace(string(b)), nil
}

func remove(name string) error {
	if tool := secretTool(); tool != "" {
		_ = exec.Command(tool, "clear", "service", "forever-pulse", "account", name).Run()
	}
	if err := os.Remove(fallbackPath(name)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func writeBlob(name, value string) error {
	if tool := secretTool(); tool != "" {
		cmd := exec.Command(tool, "store", "--label", "Forever Pulse Companion", "service", "forever-pulse", "account", name)
		cmd.Stdin = strings.NewReader(value)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	p := fallbackPath(name)
	dir := filepath.Dir(p)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// MkdirAll ne resserre pas un dossier existant, et WriteFile ne change pas
	// le mode d'un fichier déjà présent : les deux sont remis en privé ici.
	if err := os.Chmod(dir, 0o700); err != nil {
		return fmt.Errorf("coffre Linux : %w", err)
	}
	if err := os.WriteFile(p, []byte(value), 0o600); err != nil {
		return fmt.Errorf("coffre Linux : %w", err)
	}
	if err := os.Chmod(p, 0o600); err != nil {
		return fmt.Errorf("coffre Linux : %w", err)
	}
	return nil
}

func fallbackPath(name string) string {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	sum := sha256.Sum256([]byte(name))
	return filepath.Join(base, "ForeverPulse", "Companion", "cles", hex.EncodeToString(sum[:])+".tok")
}
