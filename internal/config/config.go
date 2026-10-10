// Package config : %APPDATA%\ForeverPulse\Companion\config.toml.
//
// Un sous-ensemble plat de TOML suffit (clé = valeur, chaînes, booléens) : pas de
// dépendance pour trois réglages.
package config

import (
	"bufio"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Config struct {
	SiteURL          string
	WowDir           string
	StartWithWindows bool
	PollSeconds      int
	Language         string // « fr », « en », ou vide : anglais
	// AutoUpdate : 0.10.0, vérification quotidienne des versions publiées (vrai par
	// défaut ; une clé absente, comme dans les fichiers antérieurs, vaut vrai).
	AutoUpdate bool
}

// Version du format de config.toml. Les fichiers de la version 0.1 (wowsync) n'ont
// pas de clé config_version : leur start_with_windows = false n'était que l'ancienne
// valeur par défaut, jamais un choix (0.1 n'avait pas de case à cocher). Il repasse
// donc à true une fois, puis le choix fait dans la fenêtre est respecté.
//
// Version 3 (0.6.0) : la surveillance passe de 2 s à 10 min. Les fichiers
// antérieurs portent poll_seconds = 2, l'ancien défaut (aucun réglage ne le
// modifiait) : il passe à 600 une fois.
//
// Version 4 (0.6.2, 28/09/2026) : le site est https://forever-pulse.com. L'ancien
// défaut (http://localhost:3000, qui n'était qu'une valeur de développement) passe
// à ce site une fois ; une autre adresse choisie à la main est gardée. L'ancien nom
// foreverpulse.com (sans tiret) est remplacé à chaque lecture : il n'est plus le site.
//
// Version 5 (0.7.2, 03/10/2026) : la surveillance passe de 10 min à 30 s. Un tour
// de sondage ne fait qu'un Glob et quelques Stat, sans ouvrir aucun fichier ; la
// lecture (5 à 11 Mo) n'a lieu qu'après un vrai changement. 600, l'ancien défaut,
// passe à 30 une fois ; une autre valeur choisie à la main est gardée.
//
// Version 6 (0.7.3, 03/10/2026) : le propriétaire fixe la surveillance à 5 min. Les
// anciens défauts 600 (0.6.0 à 0.7.1) et 30 (0.7.2) passent à 300 une fois.
const VersionFormat = 6

// SiteDefaut : adresse du site Forever Pulse (nom de domaine du 28/09/2026).
const SiteDefaut = "https://forever-pulse.com"

// ancienSiteDefaut : défaut des versions 0.1 à 0.6.1.
const ancienSiteDefaut = "http://localhost:3000"

// AncienDomaine dit si l'adresse vise l'ancien nom du site (foreverpulse.com, sans
// tiret, avec ou sans www), qui n'est plus utilisé.
func AncienDomaine(site string) bool {
	u, err := url.Parse(strings.TrimSpace(site))
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	return h == "foreverpulse.com" || h == "www.foreverpulse.com"
}

// Surveillance du fichier : toutes les 5 minutes par défaut, de 1 s à 1 h.
const (
	PollDefaut = 300
	ancienPoll = 600 // défaut des versions 0.6.0 à 0.7.1
	pollDe072  = 30  // défaut de la 0.7.2
	PollMax    = 3600
)

func Defaults() Config {
	// Lancer au démarrage : coché par défaut, décochable dans la fenêtre.
	return Config{SiteURL: SiteDefaut, WowDir: `C:\Program Files (x86)\World of Warcraft`, StartWithWindows: true, PollSeconds: PollDefaut, AutoUpdate: true}
}

// Noms de fichiers dans le dossier de données.
const (
	FichierConfig  = "config.toml"
	FichierBase    = "companion.db"
	FichierJournal = "companion.log"
)

// DataDir : %APPDATA%\ForeverPulse\Companion (ou $XDG_CONFIG_HOME/... hors Windows).
// Un ancien dossier « wowsync » (version 0.1) est renommé la première fois.
func DataDir() (string, error) {
	if d := os.Getenv("WOWSYNC_DATA_DIR"); d != "" {
		return d, nil
	}
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "Companion")
	Migre(filepath.Join(base, "wowsync"), dir)
	return dir, nil
}

// BaseDir : %APPDATA%\ForeverPulse.
func BaseDir() (string, error) {
	base, err := os.UserConfigDir()
	if runtime.GOOS == "windows" {
		if a := os.Getenv("APPDATA"); a != "" {
			base, err = a, nil
		}
	}
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "ForeverPulse"), nil
}

// Migre renomme l'ancien dossier et ses fichiers (wowsync.db, wowsync.log…) s'il
// existe et que le nouveau n'existe pas encore. Sans effet sinon.
func Migre(ancien, nouveau string) {
	if _, err := os.Stat(nouveau); err == nil {
		return
	}
	if _, err := os.Stat(ancien); err != nil {
		return
	}
	if os.Rename(ancien, nouveau) != nil {
		return
	}
	for _, suffixe := range []string{".db", ".db-wal", ".db-shm", ".log", ".log.1", ".log.2"} {
		_ = os.Rename(filepath.Join(nouveau, "wowsync"+suffixe), filepath.Join(nouveau, "companion"+suffixe))
	}
}

func parseValue(v string) (any, error) {
	v = strings.TrimSpace(v)
	switch {
	case v == "true":
		return true, nil
	case v == "false":
		return false, nil
	case strings.HasPrefix(v, "'"):
		end := strings.Index(v[1:], "'")
		if end < 0 {
			return nil, fmt.Errorf("chaîne non fermée")
		}
		return v[1 : 1+end], nil
	case strings.HasPrefix(v, `"`):
		var b strings.Builder
		for i := 1; i < len(v); i++ {
			c := v[i]
			if c == '"' {
				return b.String(), nil
			}
			if c == '\\' && i+1 < len(v) {
				i++
				switch v[i] {
				case 'n':
					b.WriteByte('\n')
				case 't':
					b.WriteByte('\t')
				default:
					b.WriteByte(v[i])
				}
				continue
			}
			b.WriteByte(c)
		}
		return nil, fmt.Errorf("chaîne non fermée")
	}
	if i := strings.IndexByte(v, '#'); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, fmt.Errorf("valeur non comprise : %s", v)
	}
	return n, nil
}

// Load lit le fichier ; absent, il est créé avec les valeurs par défaut.
func Load(path string) (Config, error) {
	c := Defaults()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return c, Save(path, c)
	}
	if err != nil {
		return c, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	n, version := 0, 1
	for sc.Scan() {
		n++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			return c, fmt.Errorf("config.toml ligne %d : « clé = valeur » attendu", n)
		}
		val, err := parseValue(v)
		if err != nil {
			return c, fmt.Errorf("config.toml ligne %d : %v", n, err)
		}
		switch strings.TrimSpace(k) {
		case "site_url":
			c.SiteURL, _ = val.(string)
		case "wow_dir":
			c.WowDir, _ = val.(string)
		case "start_with_windows":
			c.StartWithWindows, _ = val.(bool)
		case "config_version":
			if v, ok := val.(int); ok {
				version = v
			}
		case "language":
			c.Language, _ = val.(string)
		case "auto_update":
			if b, ok := val.(bool); ok {
				c.AutoUpdate = b
			}
		case "poll_seconds":
			if p, ok := val.(int); ok && p >= 1 && p <= PollMax {
				c.PollSeconds = p
			}
		}
	}
	if err := sc.Err(); err != nil {
		return c, err
	}
	if version < VersionFormat || AncienDomaine(c.SiteURL) {
		if version < 2 {
			c.StartWithWindows = true
		}
		if version < 3 || (version < 6 && (c.PollSeconds == ancienPoll || c.PollSeconds == pollDe072)) {
			c.PollSeconds = PollDefaut
		}
		if version < 4 && strings.TrimRight(strings.TrimSpace(c.SiteURL), "/") == ancienSiteDefaut {
			c.SiteURL = SiteDefaut
		}
		if AncienDomaine(c.SiteURL) || strings.TrimSpace(c.SiteURL) == "" {
			c.SiteURL = SiteDefaut
		}
		f.Close()
		return c, Save(path, c)
	}
	return c, nil
}

func quote(s string) string {
	if !strings.Contains(s, "'") && !strings.ContainsAny(s, "\n\r") {
		return "'" + s + "'"
	}
	return strconv.Quote(s)
}

func Save(path string, c Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	body := fmt.Sprintf(`# Forever Pulse Companion
config_version = %d
# Adresse du site qui reçoit les relevés (sans / final) : https://forever-pulse.com
site_url = %s
# Dossier d'installation de World of Warcraft. Les SavedVariables sont cherchées dans
# <wow_dir>\_*_\WTF\Account\*\SavedVariables\ForeverPulse.lua
wow_dir = %s
# Lancer au démarrage de Windows (clé HKCU\...\Run, sans droits administrateur).
start_with_windows = %t
# Intervalle de surveillance du fichier, en secondes (300 par défaut, 3600 au plus).
# Un changement repéré est confirmé 3 s plus tard, puis traité.
poll_seconds = %d
# Langue de l'interface : 'fr', 'en', ou '' pour l'anglais.
language = %s
# Mise à jour automatique : vérification quotidienne des versions signées publiées.
auto_update = %t
`, VersionFormat, quote(c.SiteURL), quote(c.WowDir), c.StartWithWindows, c.PollSeconds, quote(c.Language), c.AutoUpdate)
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// PageCompte : la page du compte sur le site, où le panneau Companion liste les
// installations reliées et permet de les révoquer. /account/companion n'y mène
// plus (redirigée vers /data-sources/contribute depuis la PR #198 du site).
func PageCompte(site string) string {
	return strings.TrimRight(site, "/") + "/account"
}
