// Package secret garde le jeton de compagnon.
//
// Windows : Gestionnaire d'identifiants (identifiant générique « ForeverPulse/Companion »,
// l'ancien « ForeverPulse/wowsync » de la version 0.1 est relu puis migré).
// Ailleurs (tests seulement) : variable d'environnement WOWSYNC_TOKEN.
// Le jeton n'est jamais écrit dans un journal, un fichier de configuration ou un commit.
package secret

import (
	"errors"
	"regexp"
	"strings"
)

const (
	Target       = "ForeverPulse/Companion"
	LegacyTarget = "ForeverPulse/wowsync"
)

var ErrAbsent = errors.New("aucun jeton enregistré")

var reToken = regexp.MustCompile(`^fpc_[A-Za-z0-9_-]{43}$`)

// Normalise vérifie le format fpc_<32 octets base64url> et retire les blancs.
func Normalise(s string) (string, bool) {
	s = strings.TrimSpace(s)
	return s, reToken.MatchString(s)
}
