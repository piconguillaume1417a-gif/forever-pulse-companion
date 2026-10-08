//go:build windows

package secret

import (
	"errors"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	advapi32   = windows.NewLazySystemDLL("advapi32.dll")
	credWrite  = advapi32.NewProc("CredWriteW")
	credRead   = advapi32.NewProc("CredReadW")
	credDelete = advapi32.NewProc("CredDeleteW")
	credFree   = advapi32.NewProc("CredFree")
)

const (
	credTypeGeneric         = 1
	credPersistLocalMachine = 2
)

type credential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         uintptr
	TargetAlias        *uint16
	UserName           *uint16
}

func Set(token string) error {
	t, ok := Normalise(token)
	if !ok {
		return errors.New("jeton au mauvais format")
	}
	return writeBlob(Target, t)
}

func writeBlob(name, value string) error {
	if len(value) == 0 || len(value) > 2500 {
		return errors.New("invalid credential size")
	}
	target, _ := windows.UTF16PtrFromString(name)
	user, _ := windows.UTF16PtrFromString("ForeverPulseCompanion")
	blob := []byte(value)
	c := credential{Type: credTypeGeneric, TargetName: target, UserName: user, Persist: credPersistLocalMachine,
		CredentialBlobSize: uint32(len(blob)), CredentialBlob: &blob[0]}
	r, _, err := credWrite.Call(uintptr(unsafe.Pointer(&c)), 0)
	for i := range blob {
		blob[i] = 0
	}
	if r == 0 {
		return err
	}
	return nil
}

func read(name string) (string, error) {
	target, _ := windows.UTF16PtrFromString(name)
	var p *credential
	r, _, _ := credRead.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0, uintptr(unsafe.Pointer(&p)))
	if r == 0 || p == nil {
		return "", ErrAbsent
	}
	defer credFree.Call(uintptr(unsafe.Pointer(p)))
	b := unsafe.Slice(p.CredentialBlob, p.CredentialBlobSize)
	return string(b), nil
}

func remove(name string) error {
	target, _ := windows.UTF16PtrFromString(name)
	r, _, err := credDelete.Call(uintptr(unsafe.Pointer(target)), credTypeGeneric, 0)
	if r == 0 {
		return err
	}
	return nil
}

// Get lit le jeton ; un jeton rangé par la version 0.1 est déplacé sous le nouveau nom.
func Get() (string, error) {
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

// Delete retire le jeton (nouveau et ancien noms). Absent : pas une erreur.
func Delete() error {
	_ = remove(LegacyTarget)
	if err := remove(Target); err != nil && err != windows.ERROR_NOT_FOUND {
		return err
	}
	return nil
}
