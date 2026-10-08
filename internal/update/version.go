package update

import (
	"fmt"
	"strconv"
	"strings"
)

// Plus dit si la version a est strictement plus récente que b.
//
// Formes rencontrées : « 0.8.0 », « 0.9.0-rc.2 », « 0.10.0 ». Une forme inconnue
// est une erreur, jamais « plus récente » : en cas de doute, on ne remplace pas.
func Plus(a, b string) (bool, error) {
	va, err := lireVersion(a)
	if err != nil {
		return false, err
	}
	vb, err := lireVersion(b)
	if err != nil {
		return false, err
	}
	return compare(va, vb) > 0, nil
}

// version : MAJEUR.MINEUR.CORRECTIF, et rc > 0 pour « -rc.N » (0 : version finale).
type version struct {
	num [3]int
	rc  int
}

func lireVersion(s string) (version, error) {
	var v version
	coeur, pre, avecPre := strings.Cut(strings.TrimSpace(s), "-")
	parts := strings.Split(coeur, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("version %q : MAJEUR.MINEUR.CORRECTIF attendu", s)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return v, fmt.Errorf("version %q : nombre attendu", s)
		}
		v.num[i] = n
	}
	if avecPre {
		n, err := strconv.Atoi(strings.TrimPrefix(pre, "rc."))
		if !strings.HasPrefix(pre, "rc.") || err != nil || n < 1 || strings.TrimPrefix(pre, "rc.") != strconv.Itoa(n) {
			return v, fmt.Errorf("version %q : seul « -rc.N » est accepté", s)
		}
		v.rc = n
	}
	return v, nil
}

// compare renvoie un nombre < 0 si a est plus ancienne que b, 0 si elles sont
// égales, > 0 si a est plus récente.
func compare(a, b version) int {
	for i := range a.num {
		if a.num[i] != b.num[i] {
			return a.num[i] - b.num[i]
		}
	}
	// À numéros égaux, la finale (rc 0) passe après toutes ses candidates.
	switch {
	case a.rc == b.rc:
		return 0
	case a.rc == 0:
		return 1
	case b.rc == 0:
		return -1
	}
	return a.rc - b.rc
}
