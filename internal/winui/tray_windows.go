//go:build windows

// Package winui : la fenêtre de Forever Pulse Companion, l'icône près de
// l'horloge et son menu, en Win32 direct.
//
// Pourquoi pas fyne.io/systray : les registres de modules étaient inaccessibles au
// moment de l'écriture, et Shell_NotifyIconW + un menu contextuel tiennent en
// quelques centaines de lignes sur golang.org/x/sys, déjà requis par SQLite.
package winui

import (
	"fmt"
	"image"
	"os"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"wowsync/internal/icone"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	pRegisterClassEx    = user32.NewProc("RegisterClassExW")
	pCreateWindowEx     = user32.NewProc("CreateWindowExW")
	pDefWindowProc      = user32.NewProc("DefWindowProcW")
	pGetMessage         = user32.NewProc("GetMessageW")
	pTranslateMessage   = user32.NewProc("TranslateMessage")
	pDispatchMessage    = user32.NewProc("DispatchMessageW")
	pPostMessage        = user32.NewProc("PostMessageW")
	pPostQuitMessage    = user32.NewProc("PostQuitMessage")
	pCreatePopupMenu    = user32.NewProc("CreatePopupMenu")
	pAppendMenu         = user32.NewProc("AppendMenuW")
	pTrackPopupMenu     = user32.NewProc("TrackPopupMenu")
	pDestroyMenu        = user32.NewProc("DestroyMenu")
	pGetCursorPos       = user32.NewProc("GetCursorPos")
	pSetForegroundWin   = user32.NewProc("SetForegroundWindow")
	pCreateIconIndir    = user32.NewProc("CreateIconIndirect")
	pGetSystemMetrics   = user32.NewProc("GetSystemMetrics")
	pShellNotifyIcon    = shell32.NewProc("Shell_NotifyIconW")
	pShellExecute       = shell32.NewProc("ShellExecuteW")
	pCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	pCreateBitmap       = gdi32.NewProc("CreateBitmap")
	pDeleteObject       = gdi32.NewProc("DeleteObject")
	pCreateMutex        = kernel32.NewProc("CreateMutexW")
	pGetModuleHandle    = kernel32.NewProc("GetModuleHandleW")
	pFindWindow         = user32.NewProc("FindWindowW")
	pAllowSetForeground = user32.NewProc("AllowSetForegroundWindow")
	pSetMenuDefaultItem = user32.NewProc("SetMenuDefaultItem")
)

const (
	wmCommand      = 0x0111
	wmDestroy      = 0x0002
	wmNull         = 0x0000
	wmLButtonUp    = 0x0202
	wmRButtonUp    = 0x0205
	wmApp          = 0x8000
	wmTray         = wmApp + 1
	wmRefresh      = wmApp + 2
	wmNotify       = wmApp + 3
	wmShow         = wmApp + 4
	wmQuit         = wmApp + 5
	wmLButtonDbl   = 0x0203
	trayClass      = "ForeverPulseCompanionTray"
	ninBalloonClic = 0x0405

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10
	niifInfo   = 0x01
	niifWarn   = 0x02
	// 0.6.1 : notifications à l'icône Forever Pulse (hBalloonIcon), en grand format.
	niifUser      = 0x04
	niifLargeIcon = 0x20

	mfString    = 0x0000
	mfGrayed    = 0x0001
	mfChecked   = 0x0008
	mfSeparator = 0x0800

	tpmReturnCmd   = 0x0100
	tpmNoNotify    = 0x0080
	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020

	smCxSmIcon       = 49
	smCxIcon         = 11
	wsExToolWindow   = 0x00000080
	errAlreadyExists = 183
)

type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         windows.GUID
	HBalloonIcon     uintptr
}

type wndClassEx struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type point struct{ X, Y int32 }

type msg struct {
	HWnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

// Couleur de l'icône (même ordre que app.Couleur).
const (
	Vert = iota
	Orange
	Rouge
)

// Item : une ligne du menu. Action nil = ligne d'information grisée.
type Item struct {
	Label     string
	Action    func()
	Checked   bool
	Separator bool
	Default   bool // en gras : l'action du clic gauche
}

// Tray : l'icône et son menu.
type Tray struct {
	hwnd           uintptr
	icons          [3]uintptr
	bulles         [2]uintptr // icône des notifications : normale, avertissement (pastille rouge)
	mu             sync.Mutex
	couleur        int
	tip            string
	applique       bool // l'icône affichée correspond à couleur et tip
	Menu           func() []Item
	OnBalloonClick func()
	OnOpen         func() // clic gauche ou double-clic sur l'icône
	actions        map[uintptr]func()
}

// SingleInstance : faux si le compagnon tourne déjà pour cet utilisateur ; dans ce
// cas sa fenêtre est ramenée au premier plan.
func SingleInstance() bool {
	name, _ := windows.UTF16PtrFromString(`Local\ForeverPulseCompanion`)
	h, _, err := pCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return false
	}
	if errno, ok := err.(windows.Errno); ok && errno == errAlreadyExists {
		// 0.6.0 : la fenêtre n'existe que lorsqu'elle est ouverte ; la demande passe
		// par la fenêtre de l'icône, qui existe toujours.
		pAllowSetForeground.Call(^uintptr(0))
		postToRunning(wmShow, 0)
		return false
	}
	return true
}

// QuitRunning demande au compagnon déjà lancé de se fermer proprement (icône
// retirée, base fermée). Faux s'il ne tourne pas.
func QuitRunning() bool { return postToRunning(wmQuit, 0) }

// MenuRunning ouvre le menu de l'icône du compagnon lancé, comme un clic droit
// (diagnostic : vérifier le menu sans cliquer dans la zone de notification).
func MenuRunning() bool {
	pAllowSetForeground.Call(^uintptr(0)) // ASFW_ANY : le compagnon peut passer au premier plan
	return postToRunning(wmTray, wmRButtonUp)
}

func postToRunning(m, lparam uintptr) bool {
	cls, _ := windows.UTF16PtrFromString(trayClass)
	w, _, _ := pFindWindow.Call(uintptr(unsafe.Pointer(cls)), 0)
	if w == 0 {
		return false
	}
	pPostMessage.Call(w, m, 0, lparam)
	return true
}

func utf16Into(dst []uint16, s string) {
	u, _ := windows.UTF16FromString(s)
	if len(u) > len(dst) {
		u = u[:len(dst)]
		u[len(u)-1] = 0
	}
	copy(dst, u)
}

var current *Tray

func wndProc(hwnd, m, wparam, lparam uintptr) uintptr {
	t := current
	switch m {
	case wmTray:
		switch lparam & 0xffff {
		case wmLButtonUp, wmLButtonDbl:
			if t.OnOpen != nil {
				t.OnOpen()
			}
		case wmRButtonUp:
			t.showMenu()
		case ninBalloonClic:
			if t.OnBalloonClick != nil {
				t.OnBalloonClick()
			}
		}
		return 0
	case wmRefresh:
		t.apply()
		return 0
	case wmShow:
		if currentWindow != nil {
			currentWindow.Show()
		}
		return 0
	case wmQuit:
		t.remove()
		pPostQuitMessage.Call(0)
		return 0
	case wmDestroy:
		t.remove()
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProc.Call(hwnd, m, wparam, lparam)
	return r
}

// Run crée l'icône et fait tourner la boucle de messages jusqu'à Quit.
// onUI est appelé sur le fil de l'interface (pour créer la fenêtre), puis ready
// dans un fil à part une fois l'icône en place.
func (t *Tray) Run(onUI func(inst uintptr), ready func()) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	current = t
	inst, _, _ := pGetModuleHandle.Call(0)
	cls, _ := windows.UTF16PtrFromString(trayClass)
	wc := wndClassEx{LpfnWndProc: windows.NewCallback(wndProc), HInstance: inst, LpszClassName: cls}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx : %v", err)
	}
	title, _ := windows.UTF16PtrFromString(AppName)
	// Fenêtre de premier niveau jamais affichée (et non « message-only ») : le menu
	// du clic droit a besoin d'une vraie fenêtre au premier plan, sinon il ne
	// s'ouvre pas ou ne se referme plus.
	h, _, err := pCreateWindowEx.Call(wsExToolWindow, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(title)), 0,
		0, 0, 0, 0, 0, 0, inst, 0)
	if h == 0 {
		return fmt.Errorf("CreateWindowEx : %v", err)
	}
	t.hwnd = h
	size := 16
	if s, _, _ := pGetSystemMetrics.Call(smCxSmIcon); s > 0 {
		size = int(s)
	}
	for c := Vert; c <= Rouge; c++ {
		t.icons[c] = iconFromImage(icone.Dessine(size, c))
	}
	grand := 32
	if s, _, _ := pGetSystemMetrics.Call(smCxIcon); s > 0 {
		grand = int(s)
	}
	t.bulles[0] = iconFromImage(icone.Dessine(grand, icone.SansPastille))
	t.bulles[1] = iconFromImage(icone.Dessine(grand, icone.Rouge))
	nid := t.base()
	nid.UFlags = nifMessage | nifIcon | nifTip
	nid.UCallbackMessage = wmTray
	nid.HIcon = t.icons[t.couleur]
	utf16Into(nid.SzTip[:], t.tipText())
	if r, _, err := pShellNotifyIcon.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r == 0 {
		return fmt.Errorf("Shell_NotifyIcon : %v", err)
	}
	if onUI != nil {
		onUI(inst)
	}
	if ready != nil {
		go ready()
	}
	var m msg
	for {
		r, _, _ := pGetMessage.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			return nil
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessage.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func (t *Tray) base() notifyIconData {
	var nid notifyIconData
	nid.CbSize = uint32(unsafe.Sizeof(nid))
	nid.HWnd = t.hwnd
	nid.UID = 1
	return nid
}

func (t *Tray) tipText() string {
	if t.tip == "" {
		return AppName
	}
	return AppName + " — " + t.tip
}

// Set change la couleur et l'infobulle (depuis n'importe quel fil).
//
// 0.6.0 : rien n'est envoyé à l'Explorateur si l'icône et l'infobulle n'ont pas changé.
func (t *Tray) Set(couleur int, tip string) {
	t.mu.Lock()
	inchange := t.applique && t.couleur == couleur && t.tip == tip
	t.couleur, t.tip = couleur, tip
	t.mu.Unlock()
	if !inchange && t.hwnd != 0 {
		pPostMessage.Call(t.hwnd, wmRefresh, 0, 0)
	}
}

func (t *Tray) apply() {
	t.mu.Lock()
	couleur, tip := t.couleur, t.tipText()
	t.applique = true
	t.mu.Unlock()
	nid := t.base()
	nid.UFlags = nifIcon | nifTip
	nid.HIcon = t.icons[couleur]
	utf16Into(nid.SzTip[:], tip)
	pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

// Notify affiche une notification Windows près de l'horloge.
func (t *Tray) Notify(title, text string, warn bool) {
	nid := t.base()
	nid.UFlags = nifInfo
	utf16Into(nid.SzInfoTitle[:], title)
	utf16Into(nid.SzInfo[:], text)
	nid.DwInfoFlags = niifInfo
	if warn {
		nid.DwInfoFlags = niifWarn
	}
	// Symbole Forever Pulse à la place du « i » / « ! » de Windows ; repli sur ces
	// derniers si l'icône n'a pas pu être créée.
	b := t.bulles[0]
	if warn {
		b = t.bulles[1]
	}
	if b != 0 {
		nid.HBalloonIcon = b
		nid.DwInfoFlags = niifUser | niifLargeIcon
	}
	pShellNotifyIcon.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func (t *Tray) remove() {
	nid := t.base()
	pShellNotifyIcon.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
}

// Quit retire l'icône et termine Run. Utilisable depuis n'importe quel fil : la
// demande est postée au fil de l'interface (PostQuitMessage ne vaut que pour le fil appelant).
func (t *Tray) Quit() {
	if t.hwnd != 0 {
		pPostMessage.Call(t.hwnd, wmQuit, 0, 0)
	}
}

func (t *Tray) showMenu() {
	if t.Menu == nil {
		return
	}
	hm, _, _ := pCreatePopupMenu.Call()
	defer pDestroyMenu.Call(hm)
	t.actions = map[uintptr]func(){}
	id := uintptr(100)
	for _, it := range t.Menu() {
		if it.Separator {
			pAppendMenu.Call(hm, mfSeparator, 0, 0)
			continue
		}
		label, _ := windows.UTF16PtrFromString(strings.ReplaceAll(it.Label, "&", "&&"))
		flags := uintptr(mfString)
		if it.Action == nil {
			flags |= mfGrayed
		}
		if it.Checked {
			flags |= mfChecked
		}
		pAppendMenu.Call(hm, flags, id, uintptr(unsafe.Pointer(label)))
		if it.Action != nil {
			t.actions[id] = it.Action
		}
		if it.Default {
			pSetMenuDefaultItem.Call(hm, id, 0)
		}
		id++
	}
	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	pSetForegroundWin.Call(t.hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(hm, tpmReturnCmd|tpmNoNotify|tpmRightButton|tpmBottomAlign,
		uintptr(pt.X), uintptr(pt.Y), 0, t.hwnd, 0)
	pPostMessage.Call(t.hwnd, wmNull, 0, 0)
	if f := t.actions[cmd]; f != nil {
		go f()
	}
}

// iconFromImage fabrique une icône Windows 32 bits (alpha) à partir d'une image,
// carrée ou non (le symbole de l'en-tête est plus large que haut).
func iconFromImage(img *image.NRGBA) uintptr {
	size, haut := img.Bounds().Dx(), img.Bounds().Dy()
	type bitmapInfoHeader struct {
		BiSize          uint32
		BiWidth         int32
		BiHeight        int32
		BiPlanes        uint16
		BiBitCount      uint16
		BiCompression   uint32
		BiSizeImage     uint32
		BiXPelsPerMeter int32
		BiYPelsPerMeter int32
		BiClrUsed       uint32
		BiClrImportant  uint32
	}
	bi := bitmapInfoHeader{BiWidth: int32(size), BiHeight: -int32(haut), BiPlanes: 1, BiBitCount: 32}
	bi.BiSize = uint32(unsafe.Sizeof(bi))
	var bits unsafe.Pointer
	color, _, _ := pCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if color == 0 || bits == nil {
		return 0
	}
	px := unsafe.Slice((*byte)(bits), size*haut*4)
	for i := 0; i < size*haut; i++ {
		px[i*4], px[i*4+1], px[i*4+2], px[i*4+3] = img.Pix[i*4+2], img.Pix[i*4+1], img.Pix[i*4], img.Pix[i*4+3]
	}
	maskBits := make([]byte, ((size+15)/16*2)*haut)
	mask, _, _ := pCreateBitmap.Call(uintptr(size), uintptr(haut), 1, 1, uintptr(unsafe.Pointer(&maskBits[0])))
	type iconInfo struct {
		FIcon    int32
		XHotspot uint32
		YHotspot uint32
		HbmMask  uintptr
		HbmColor uintptr
	}
	ii := iconInfo{FIcon: 1, HbmMask: mask, HbmColor: color}
	h, _, _ := pCreateIconIndir.Call(uintptr(unsafe.Pointer(&ii)))
	pDeleteObject.Call(color)
	pDeleteObject.Call(mask)
	return h
}

// Open ouvre un fichier ou une adresse avec l'application par défaut.
func Open(target string) {
	verb, _ := windows.UTF16PtrFromString("open")
	p, _ := windows.UTF16PtrFromString(target)
	pShellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(p)), 0, 0, 1)
}

const (
	runKey    = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue  = "ForeverPulseCompanion"
	runLegacy = "wowsync" // version 0.1
)

// StartWithWindows écrit ou retire la valeur « ForeverPulseCompanion » de
// HKCU\...\Run (sans droits administrateur). Lancé ainsi, le compagnon démarre
// discrètement, icône seule, sans ouvrir sa fenêtre (--tray).
func StartWithWindows(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	_ = k.DeleteValue(runLegacy)
	if !on {
		if err := k.DeleteValue(runValue); err != nil && err != registry.ErrNotExist {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(runValue, `"`+exe+`" --tray`)
}

// StartsWithWindows : la valeur existe-t-elle ?
func StartsWithWindows() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValue)
	return err == nil
}
