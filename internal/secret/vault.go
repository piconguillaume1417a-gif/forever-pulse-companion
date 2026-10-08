package secret

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// Vault preserves the production credential. Alternate data directories and
// sites get distinct targets; an isolated test never falls back to production.
type Vault struct{ target string }

func ForContext(dir, site string, isolated bool) Vault {
	target := Target
	site = strings.TrimRight(site, "/")
	if isolated || site != "https://forever-pulse.com" {
		path, _ := filepath.Abs(dir)
		sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(path)) + "\x00" + site))
		target += "/isolated/" + hex.EncodeToString(sum[:16])
	}
	return Vault{target: target}
}
func (v Vault) Get() (string, error) {
	if v.target == Target {
		return Get()
	}
	t, err := read(v.target)
	if err != nil {
		return "", err
	}
	if _, ok := Normalise(t); !ok {
		return "", ErrAbsent
	}
	return t, nil
}
func (v Vault) Set(t string) error {
	clean, ok := Normalise(t)
	if !ok {
		return ErrAbsent
	}
	return writeBlob(v.target, clean)
}
func (v Vault) ReadConnection() (string, error) { return read(v.target + "/Connection") }
func (v Vault) WriteConnection(s string) error  { return writeBlob(v.target+"/Connection", s) }
func (v Vault) DeleteConnection() error         { return remove(v.target + "/Connection") }
