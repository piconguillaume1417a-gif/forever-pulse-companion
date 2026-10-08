//go:build !windows

package secret

import (
	"errors"
	"os"
)

// Hors Windows, le jeton ne vient que de l'environnement (tests de bout en bout).
func Set(token string) error { return errors.New("hors Windows : utilisez WOWSYNC_TOKEN") }

func Get() (string, error) {
	t, ok := Normalise(os.Getenv("WOWSYNC_TOKEN"))
	if !ok {
		return "", ErrAbsent
	}
	return t, nil
}

func Delete() error                      { return nil }
func read(name string) (string, error)   { return "", ErrAbsent }
func remove(name string) error           { return nil }
func writeBlob(name, value string) error { return errors.New("Windows vault unavailable") }
