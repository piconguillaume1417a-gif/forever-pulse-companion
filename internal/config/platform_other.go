//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"strings"
)

func defaultWowDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join("Games", "World of Warcraft")
	}
	return filepath.Join(home, "Games", "World of Warcraft")
}

// La mise à jour automatique reste Windows seulement dans cette version.
func defaultAutoUpdate() bool { return false }

func commentaireDemarrage() string {
	return "# Lancer à la connexion (XDG autostart sous Linux, clé HKCU sous Windows)."
}

func commentaireMaj() string {
	return "# Mise à jour automatique : désactivée sous Linux dans cette version (Windows seulement)."
}

// migreCheminWindows remplace une fois un dossier « C:\... » hérité d'un
// config.toml Windows par le défaut de la plateforme.
func migreCheminWindows(_ string, c *Config) (bool, error) {
	chemin := strings.TrimSpace(c.WowDir)
	if !strings.HasPrefix(chemin, `C:\`) && !strings.HasPrefix(chemin, `c:\`) {
		return false, nil
	}
	c.WowDir = defaultWowDir()
	return true, nil
}
