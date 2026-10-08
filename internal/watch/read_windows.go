//go:build windows

package watch

import (
	"os"

	"golang.org/x/sys/windows"
)

// openShared ouvre en lecture seule en partageant lecture, écriture ET suppression :
// le jeu peut réécrire le fichier ou le renommer en .bak pendant qu'on le lit,
// le compagnon ne le verrouille jamais.
func openShared(path string) (*os.File, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(h), path), nil
}
