//go:build windows

package winui

import (
	"encoding/binary"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Boîtes de dialogue « modernes » (TaskDialogIndirect, comctl32 v6 déclaré dans le
// manifeste) : une question en gras, le détail dessous, des boutons nommés par
// leur action (« Effacer » / « Annuler ») au lieu de Oui / Non. Si la fonction
// manque, repli sur MessageBox comme en 0.5.0.

var (
	comctl32            = windows.NewLazySystemDLL("comctl32.dll")
	pTaskDialogIndirect = comctl32.NewProc("TaskDialogIndirect")
	pInitCommonControls = comctl32.NewProc("InitCommonControlsEx")
)

const (
	tdWarningIcon = 0xFFFF // MAKEINTRESOURCE(-1)
	tdInfoIcon    = 0xFFFD // MAKEINTRESOURCE(-3)

	tdfAllowCancel      = 0x0008
	tdfRelativeToWindow = 0x1000

	idAction  = 100
	idAnnuler = 101
)

type boutonDlg struct {
	id    int
	texte string
}

// decoupe : « Question ?\n\nDétail » → instruction et contenu.
func decoupe(text string) (string, string) {
	if i := strings.Index(text, "\n\n"); i > 0 {
		return text[:i], text[i+2:]
	}
	return text, ""
}

// dialogue affiche la boîte et renvoie l'identifiant du bouton choisi ; ok = faux
// si TaskDialogIndirect n'a pas pu s'afficher.
func dialogue(parent uintptr, icone uintptr, text string, boutons []boutonDlg, defaut int) (int, bool) {
	if pTaskDialogIndirect.Find() != nil {
		return 0, false
	}
	instruction, contenu := decoupe(text)
	var garde [][]uint16 // les chaînes restent vivantes pendant l'appel
	ptr := func(s string) uint64 {
		if s == "" {
			return 0
		}
		u, _ := windows.UTF16FromString(s)
		garde = append(garde, u)
		return uint64(uintptr(unsafe.Pointer(&u[0])))
	}
	// TASKDIALOG_BUTTON et TASKDIALOGCONFIG sont compactés sur 1 octet (pshpack1.h) :
	// 12 et 160 octets en 64 bits.
	tb := make([]byte, 12*len(boutons))
	for i, b := range boutons {
		binary.LittleEndian.PutUint32(tb[i*12:], uint32(b.id))
		binary.LittleEndian.PutUint64(tb[i*12+4:], ptr(b.texte))
	}
	var c [160]byte
	le := binary.LittleEndian
	le.PutUint32(c[0:], 160)
	le.PutUint64(c[4:], uint64(parent))
	flags := uint32(tdfAllowCancel)
	if parent != 0 {
		flags |= tdfRelativeToWindow
	}
	le.PutUint32(c[20:], flags)
	le.PutUint64(c[28:], ptr(AppName))
	le.PutUint64(c[36:], uint64(icone))
	le.PutUint64(c[44:], ptr(instruction))
	le.PutUint64(c[52:], ptr(contenu))
	le.PutUint32(c[60:], uint32(len(boutons)))
	if len(tb) > 0 {
		le.PutUint64(c[64:], uint64(uintptr(unsafe.Pointer(&tb[0]))))
	}
	le.PutUint32(c[72:], uint32(defaut))
	var choix int32
	r, _, _ := pTaskDialogIndirect.Call(uintptr(unsafe.Pointer(&c[0])), uintptr(unsafe.Pointer(&choix)), 0, 0)
	runtime.KeepAlive(garde)
	runtime.KeepAlive(tb)
	if r != 0 {
		return 0, false
	}
	return int(choix), true
}
