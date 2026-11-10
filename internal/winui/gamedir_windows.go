//go:build windows

package winui

import (
	"os"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// DossiersBattleNet : chemins d'installation déclarés par Battle.net, un par
// client (« ...\World of Warcraft\_classic_beta_\ »), lus dans le registre sans
// droits administrateur. watch.Racine en déduit la racine.
func DossiersBattleNet() []string {
	var out []string
	for _, racine := range []string{
		`SOFTWARE\WOW6432Node\Blizzard Entertainment\World of Warcraft`,
		`SOFTWARE\Blizzard Entertainment\World of Warcraft`,
	} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, racine, registry.QUERY_VALUE|registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		if v, _, err := k.GetStringValue("InstallPath"); err == nil && v != "" {
			out = append(out, v)
		}
		noms, _ := k.ReadSubKeyNames(-1)
		for _, n := range noms {
			s, err := registry.OpenKey(k, n, registry.QUERY_VALUE)
			if err != nil {
				continue
			}
			if v, _, err := s.GetStringValue("InstallPath"); err == nil && v != "" {
				out = append(out, v)
			}
			s.Close()
		}
		k.Close()
	}
	return out
}

// Lecteurs : les lecteurs présents, de C: à Z:.
func Lecteurs() []string {
	var out []string
	for c := 'C'; c <= 'Z'; c++ {
		l := string(c) + ":"
		if _, err := os.Stat(l + `\`); err == nil {
			out = append(out, l)
		}
	}
	return out
}

// Sélecteur de dossier de l'Explorateur (IFileOpenDialog avec FOS_PICKFOLDERS).

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	pCoInitializeEx      = ole32.NewProc("CoInitializeEx")
	pCoUninitialize      = ole32.NewProc("CoUninitialize")
	pCoCreateInstance    = ole32.NewProc("CoCreateInstance")
	pCoTaskMemFree       = ole32.NewProc("CoTaskMemFree")
	shell32Dossier       = windows.NewLazySystemDLL("shell32.dll")
	pSHCreateItemFromStr = shell32Dossier.NewProc("SHCreateItemFromParsingName")
)

var (
	clsidFileOpenDialog = windows.GUID{Data1: 0xDC1C5A9C, Data2: 0xE88A, Data3: 0x4DDE, Data4: [8]byte{0xA5, 0xA1, 0x60, 0xF8, 0x2A, 0x20, 0xAE, 0xF7}}
	iidFileOpenDialog   = windows.GUID{Data1: 0xD57C7288, Data2: 0xD4AD, Data3: 0x4768, Data4: [8]byte{0xBE, 0x02, 0x9D, 0x96, 0x95, 0x32, 0xD9, 0x60}}
	iidShellItem        = windows.GUID{Data1: 0x43826D1E, Data2: 0xE718, Data3: 0x42EE, Data4: [8]byte{0xBC, 0x55, 0xA1, 0xE2, 0x61, 0xC3, 0x7B, 0xFE}}
)

const (
	coinitApartment   = 0x2
	coinitNoOLE1DDE   = 0x4
	clsctxInproc      = 0x1
	fosPickFolders    = 0x20
	fosForceFS        = 0x40
	fosNoChangeDir    = 0x8
	fosPathMustExist  = 0x800
	sigdnFileSysPath  = 0x80058000
	hresultCancelled  = 0x800704C7
	vtShow            = 3
	vtSetOptions      = 9
	vtGetOptions      = 10
	vtSetFolder       = 12
	vtSetTitle        = 17
	vtGetResult       = 20
	vtRelease         = 2
	vtShellGetDisplay = 5
)

// comObj : un objet COM, vu par sa table virtuelle (mémoire hors du ramasse-miettes).
type comObj struct{ vtbl *[32]uintptr }

// appel : méthode n de la table virtuelle de l'objet COM obj.
func appel(obj *comObj, n int, args ...uintptr) uintptr {
	r, _, _ := syscall.SyscallN(obj.vtbl[n], append([]uintptr{uintptr(unsafe.Pointer(obj))}, args...)...)
	return r
}

// ChoisirDossier ouvre le sélecteur, placé sur initial s'il existe. ok est faux
// si le joueur annule ou si le sélecteur ne peut pas s'ouvrir.
func (w *Window) ChoisirDossier(titre, initial string) (string, bool) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if r, _, _ := pCoInitializeEx.Call(0, coinitApartment|coinitNoOLE1DDE); int32(r) >= 0 {
		defer pCoUninitialize.Call()
	}
	var dlg *comObj
	if r, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidFileOpenDialog)), 0, clsctxInproc,
		uintptr(unsafe.Pointer(&iidFileOpenDialog)), uintptr(unsafe.Pointer(&dlg))); int32(r) < 0 || dlg == nil {
		return "", false
	}
	defer appel(dlg, vtRelease)
	var opts uint32
	appel(dlg, vtGetOptions, uintptr(unsafe.Pointer(&opts)))
	appel(dlg, vtSetOptions, uintptr(opts|fosPickFolders|fosForceFS|fosPathMustExist|fosNoChangeDir))
	t, _ := windows.UTF16PtrFromString(titre)
	appel(dlg, vtSetTitle, uintptr(unsafe.Pointer(t)))
	if initial != "" {
		if _, err := os.Stat(initial); err == nil {
			p, _ := windows.UTF16PtrFromString(initial)
			var item *comObj
			if r, _, _ := pSHCreateItemFromStr.Call(uintptr(unsafe.Pointer(p)), 0, uintptr(unsafe.Pointer(&iidShellItem)),
				uintptr(unsafe.Pointer(&item))); int32(r) >= 0 && item != nil {
				appel(dlg, vtSetFolder, uintptr(unsafe.Pointer(item)))
				appel(item, vtRelease)
			}
		}
	}
	if r := appel(dlg, vtShow, w.hwnd.Load()); int32(r) < 0 {
		return "", false // annulé (hresultCancelled) ou erreur
	}
	var item *comObj
	if r := appel(dlg, vtGetResult, uintptr(unsafe.Pointer(&item))); int32(r) < 0 || item == nil {
		return "", false
	}
	defer appel(item, vtRelease)
	var chemin *uint16
	if r := appel(item, vtShellGetDisplay, sigdnFileSysPath, uintptr(unsafe.Pointer(&chemin))); int32(r) < 0 || chemin == nil {
		return "", false
	}
	defer pCoTaskMemFree.Call(uintptr(unsafe.Pointer(chemin)))
	return windows.UTF16PtrToString(chemin), true
}
