//go:build windows

package config

// Le Compagnon Windows garde exactement ses défauts et ses commentaires de
// config.toml : le code Linux est isolé dans platform_other.go.
func defaultWowDir() string { return `C:\Program Files (x86)\World of Warcraft` }

func defaultAutoUpdate() bool { return true }

func commentaireDemarrage() string {
	return "# Lancer au démarrage de Windows (clé HKCU\\...\\Run, sans droits administrateur)."
}

func commentaireMaj() string {
	return "# Mise à jour automatique : vérification quotidienne des versions signées publiées."
}

// migreCheminWindows : sans effet sous Windows.
func migreCheminWindows(_ string, _ *Config) (bool, error) { return false, nil }
