//go:build windows

package winui

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"wowsync/internal/i18n"
	"wowsync/internal/vue"
)

// AppName : le nom affiché partout.
const AppName = "Forever Pulse Companion"

const windowClass = "ForeverPulseCompanionWindow"

// Identifiants des commandes de la fenêtre (mêmes valeurs qu'en 0.5.0).
const (
	CmdEnvoyer      = vue.Envoyer
	CmdColler       = vue.Coller
	CmdJournal      = vue.Journal
	CmdPage         = vue.Page
	CmdDemarrage    = vue.Demarrage
	CmdEffacer      = vue.Effacer
	CmdDesinstaller = vue.Desinstaller
	CmdReduire      = vue.Reduire
	CmdQuitter      = vue.Quitter
)

var (
	pShowWindow       = user32.NewProc("ShowWindow")
	pSendMessage      = user32.NewProc("SendMessageW")
	pMessageBox       = user32.NewProc("MessageBoxW")
	pLoadImage        = user32.NewProc("LoadImageW")
	pLoadCursor       = user32.NewProc("LoadCursorW")
	pSetCursor        = user32.NewProc("SetCursor")
	pAdjustWindowRect = user32.NewProc("AdjustWindowRectEx")
	pGetDC            = user32.NewProc("GetDC")
	pReleaseDC        = user32.NewProc("ReleaseDC")
	pIsWindowVisible  = user32.NewProc("IsWindowVisible")
	pDestroyWindow    = user32.NewProc("DestroyWindow")
	pBeginPaint       = user32.NewProc("BeginPaint")
	pEndPaint         = user32.NewProc("EndPaint")
	pInvalidateRect   = user32.NewProc("InvalidateRect")
	pGetClientRect    = user32.NewProc("GetClientRect")
	pGetWindowRect    = user32.NewProc("GetWindowRect")
	pSetWindowPos     = user32.NewProc("SetWindowPos")
	pSetCapture       = user32.NewProc("SetCapture")
	pReleaseCapture   = user32.NewProc("ReleaseCapture")
	pTrackMouseEvent  = user32.NewProc("TrackMouseEvent")
	pGetKeyState      = user32.NewProc("GetKeyState")
	pSetFocus         = user32.NewProc("SetFocus")
	pSystemParamsInfo = user32.NewProc("SystemParametersInfoW")
	pCreateFont       = gdi32.NewProc("CreateFontW")
	pGetDeviceCaps    = gdi32.NewProc("GetDeviceCaps")
	pSetTextColor     = gdi32.NewProc("SetTextColor")
	dwmapi            = windows.NewLazySystemDLL("dwmapi.dll")
	pDwmSetWindowAttr = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	wsOverlapped   = 0x00000000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsPopup        = 0x80000000
	wsExTopmost    = 0x00000008
	wmClose        = 0x0010
	wmPaint        = 0x000F
	wmEraseBkgnd   = 0x0014
	wmSetCursor    = 0x0020
	wmSettingChg   = 0x001A
	wmNcDestroy    = 0x0082
	wmKeyDown      = 0x0100
	wmMouseMove    = 0x0200
	wmLButtonDown  = 0x0201
	wmMouseLeave   = 0x02A3
	wmCaptureChg   = 0x0215
	wmSize         = 0x0005
	wmSysCommand   = 0x0112
	wmSetIcon      = 0x0080
	wmRefreshWin   = wmApp + 10
	wmRelabel      = wmApp + 11
	scMinimize     = 0xF020
	sizeMinimized  = 1
	swHide         = 0
	swShowNormal   = 1
	swRestore      = 9
	mbYesNo        = 0x4
	mbOK           = 0x0
	mbIconWarning  = 0x30
	mbIconInfo     = 0x40
	mbDefButton2   = 0x100
	mbSetForegr    = 0x10000
	idYes          = 6
	logPixelsY     = 90
	vkTab          = 0x09
	vkReturn       = 0x0D
	vkShift        = 0x10
	vkControl      = 0x11
	vkEscape       = 0x1B
	vkSpace        = 0x20
	vkLeft         = 0x25
	vkUp           = 0x26
	vkRight        = 0x27
	vkDown         = 0x28
	vkV            = 0x56
	idcArrow       = 32512
	idcHand        = 32649
	tmeLeave       = 0x2
	swpNoZOrder    = 0x4
	swpNoActivate  = 0x10
	spiGetWorkArea = 0x30

	ttsAlwaysTip   = 0x01
	ttsNoPrefix    = 0x02
	ttfSubclass    = 0x10
	ttmAddTool     = 0x0432 // TTM_ADDTOOLW
	ttmDelTool     = 0x0433 // TTM_DELTOOLW
	ttmSetMaxWidth = 0x0418
)

type rect32 struct{ L, T, R, B int32 }

type toolInfo struct {
	CbSize   uint32
	UFlags   uint32
	Hwnd     uintptr
	UID      uintptr
	Rect     rect32
	Hinst    uintptr
	Texte    *uint16
	LParam   uintptr
	Reserved uintptr
}

// Window : la fenêtre du compagnon, dessinée par internal/vue.
//
// Elle n'existe que pendant qu'elle est ouverte : la cacher la détruit (polices,
// GDI+, images, infobulles libérées) et la rouvrir la reconstruit en quelques
// millisecondes. Lancé avec Windows (--tray), le compagnon ne crée aucune fenêtre
// tant qu'on ne clique pas sur l'icône. Fermer, réduire ou Échap la cachent ; le
// compagnon continue près de l'horloge.
type Window struct {
	hwnd atomic.Uintptr
	inst uintptr

	mu     sync.Mutex
	modele vue.Modele
	occupe map[int]bool

	// Fil de l'interface seulement.
	dpi      int
	peintre  *peintre
	zones    []vue.Zone
	inter    vue.Interaction
	suivi    bool
	bulles   uintptr
	outils   []vue.Zone
	sombre   bool
	pos      *[2]int32
	hauteurC int

	OnCommand func(id int)       // appelé dans un fil à part
	OnToggle  func(checked bool) // interrupteur « Lancer au démarrage »
	OnLangue  func(l i18n.Lang)  // FR / EN
}

var currentWindow *Window

func str(s string) uintptr {
	p, _ := windows.UTF16PtrFromString(s)
	return uintptr(unsafe.Pointer(p))
}

func loword(v uintptr) int { return int(int16(v & 0xffff)) }
func hiword(v uintptr) int { return int(int16((v >> 16) & 0xffff)) }

// Create prépare la fenêtre (sur le fil de l'interface) sans l'ouvrir.
// startChecked : état initial de « Lancer au démarrage de Windows ».
func (w *Window) Create(inst uintptr, startChecked bool) error {
	currentWindow = w
	w.inst = inst
	w.mu.Lock()
	w.modele.Demarrage = startChecked
	if w.occupe == nil {
		w.occupe = map[int]bool{}
	}
	w.mu.Unlock()
	w.dpi = 96
	if dc, _, _ := pGetDC.Call(0); dc != 0 {
		if d, _, _ := pGetDeviceCaps.Call(dc, logPixelsY); d >= 96 {
			w.dpi = int(d)
		}
		pReleaseDC.Call(0, dc)
	}
	icc := struct{ Size, ICC uint32 }{8, 0x8 /*ICC_TAB_CLASSES : infobulles*/}
	pInitCommonControls.Call(uintptr(unsafe.Pointer(&icc)))
	big, _, _ := pLoadImage.Call(inst, 1, 1 /*IMAGE_ICON*/, uintptr(32*w.dpi/96), uintptr(32*w.dpi/96), 0)
	small, _, _ := pLoadImage.Call(inst, 1, 1, uintptr(16*w.dpi/96), uintptr(16*w.dpi/96), 0)
	cursor, _, _ := pLoadCursor.Call(0, idcArrow)
	cls, _ := windows.UTF16PtrFromString(windowClass)
	wc := wndClassEx{LpfnWndProc: windows.NewCallback(windowProc), HInstance: inst, LpszClassName: cls,
		HIcon: big, HIconSm: small, HCursor: cursor}
	wc.CbSize = uint32(unsafe.Sizeof(wc))
	if r, _, err := pRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx : %v", err)
	}
	return nil
}

const style = wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox

// taille : dimensions extérieures de la fenêtre pour une zone cliente logique.
func (w *Window) taille(hauteur int) (int, int) {
	rc := rect32{0, 0, int32(vue.Largeur * w.dpi / 96), int32(hauteur * w.dpi / 96)}
	pAdjustWindowRect.Call(uintptr(unsafe.Pointer(&rc)), style, 0, 0)
	return int(rc.R - rc.L), int(rc.B - rc.T)
}

// coinHorloge : en bas à droite de l'espace de travail, du côté de la barre des
// tâches (en haut ou à gauche si elle y est).
func coinHorloge(ww, wh int) (int, int) {
	var wa rect32
	pSystemParamsInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&wa)), 0)
	const marge = 12
	x, y := int(wa.R)-ww-marge, int(wa.B)-wh-marge
	if wa.L > 0 {
		x = int(wa.L) + marge
	}
	if wa.T > 0 {
		y = int(wa.T) + marge
	}
	if x < int(wa.L) {
		x = int(wa.L)
	}
	if y < int(wa.T) {
		y = int(wa.T)
	}
	return x, y
}

func (w *Window) creer() bool {
	w.mu.Lock()
	height := vue.HauteurModele(w.modele)
	w.mu.Unlock()
	w.hauteurC = height
	ww, wh := w.taille(w.hauteurC)
	x, y := coinHorloge(ww, wh)
	if w.pos != nil {
		x, y = int(w.pos[0]), int(w.pos[1])
	}
	cls, _ := windows.UTF16PtrFromString(windowClass)
	h, _, _ := pCreateWindowEx.Call(0, uintptr(unsafe.Pointer(cls)), str(AppName), style,
		uintptr(x), uintptr(y), uintptr(ww), uintptr(wh), 0, 0, w.inst, 0)
	if h == 0 {
		return false
	}
	w.hwnd.Store(h)
	w.peintre = nouveauPeintre(w.dpi)
	w.inter = vue.Interaction{Survol: -1, Appui: -1, Focus: -1}
	w.outils = nil
	w.sombre = themeSombre()
	w.barreTitre()
	w.bulles, _, _ = pCreateWindowEx.Call(wsExTopmost, str("tooltips_class32"), 0, wsPopup|ttsAlwaysTip|ttsNoPrefix,
		0x80000000, 0x80000000, 0x80000000, 0x80000000, h, 0, w.inst, 0)
	if w.bulles != 0 {
		pSendMessage.Call(w.bulles, ttmSetMaxWidth, 0, uintptr(320*w.dpi/96))
	}
	return true
}

// barreTitre : barre de titre sombre, et sur Windows 11 de la couleur de l'en-tête.
func (w *Window) barreTitre() {
	h := w.hwnd.Load()
	if h == 0 || pDwmSetWindowAttr.Find() != nil {
		return
	}
	set := func(attr uintptr, v uint32) {
		pDwmSetWindowAttr.Call(h, attr, uintptr(unsafe.Pointer(&v)), 4)
	}
	set(20, 1) // DWMWA_USE_IMMERSIVE_DARK_MODE
	set(35, uint32(bgr(vue.Nuit)))
	set(36, 0xFFFFFF) // DWMWA_TEXT_COLOR
}

// themeSombre : réglage « Mode d'application » de Windows.
func themeSombre() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err == nil && v == 0
}

func (w *Window) theme() vue.Theme {
	if w.sombre {
		return vue.Sombre()
	}
	return vue.Clair()
}

// construire : mise en page de l'état courant.
func (w *Window) construire() ([]vue.Op, []vue.Zone, int) {
	w.mu.Lock()
	m := w.modele
	occ := make(map[int]bool, len(w.occupe))
	for k, v := range w.occupe {
		occ[k] = v
	}
	w.mu.Unlock()
	in := w.inter
	in.Occupe = occ
	ops, zones, _, h := vue.Construire(m, in, w.theme())
	return ops, zones, h
}

func (w *Window) invalider() {
	if h := w.hwnd.Load(); h != 0 {
		pInvalidateRect.Call(h, 0, 0)
	}
}

func (w *Window) peindre(hwnd uintptr) {
	var ps struct {
		Hdc       uintptr
		Erase     int32
		Rc        rect32
		Restore   int32
		IncUpdate int32
		Reserved  [32]byte
	}
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	var rc rect32
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	ops, zones, _ := w.construire()
	w.zones = zones
	if w.peintre != nil {
		w.peintre.peindre(hdc, int(rc.R), int(rc.B), ops)
	}
	pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	w.majBulles(hwnd)
}

// majBulles : une infobulle par zone ; refaites seulement si les zones ont bougé
// ou changé de texte (langue, jeton manquant, nombre de périmètres).
func (w *Window) majBulles(hwnd uintptr) {
	if w.bulles == 0 || reflect.DeepEqual(w.outils, w.zones) {
		return
	}
	for _, z := range w.outils {
		ti := toolInfo{Hwnd: hwnd, UID: uintptr(z.ID)}
		ti.CbSize = uint32(unsafe.Sizeof(ti))
		pSendMessage.Call(w.bulles, ttmDelTool, 0, uintptr(unsafe.Pointer(&ti)))
	}
	for _, z := range w.zones {
		if z.Bulle == "" {
			continue
		}
		t, _ := windows.UTF16PtrFromString(z.Bulle)
		ti := toolInfo{UFlags: ttfSubclass, Hwnd: hwnd, UID: uintptr(z.ID), Hinst: w.inst, Texte: t,
			Rect: rect32{int32(z.R.X * w.dpi / 96), int32(z.R.Y * w.dpi / 96), int32((z.R.X + z.R.W) * w.dpi / 96), int32((z.R.Y + z.R.H) * w.dpi / 96)}}
		ti.CbSize = uint32(unsafe.Sizeof(ti))
		pSendMessage.Call(w.bulles, ttmAddTool, 0, uintptr(unsafe.Pointer(&ti)))
	}
	w.outils = append([]vue.Zone(nil), w.zones...)
}

// zoneA : la zone cliquable sous le point (pixels de l'écran), ou -1.
func (w *Window) zoneA(x, y int) int {
	lx, ly := x*96/w.dpi, y*96/w.dpi
	for _, z := range w.zones {
		if !z.Info && z.R.Contient(lx, ly) {
			return z.ID
		}
	}
	return -1
}

func (w *Window) inactif(id int) bool {
	for _, z := range w.zones {
		if z.ID == id {
			return z.Inactif
		}
	}
	return true
}

// focusables : zones parcourues par Tab, dans l'ordre de l'écran.
func (w *Window) focusables() []int {
	var ids []int
	for _, z := range w.zones {
		if !z.Info {
			ids = append(ids, z.ID)
		}
	}
	return ids
}

func (w *Window) deplacerFocus(pas int) {
	ids := w.focusables()
	if len(ids) == 0 {
		return
	}
	i := -1
	for k, id := range ids {
		if id == w.inter.Focus {
			i = k
		}
	}
	if i < 0 {
		if pas > 0 {
			i = -1
		} else {
			i = 0
		}
	}
	w.inter.Focus = ids[(i+pas+len(ids))%len(ids)]
	w.inter.FocusVisible = true
	w.invalider()
}

// activer : clic ou Entrée sur une zone (fil de l'interface).
func (w *Window) activer(id int) {
	if id < 0 || w.inactif(id) {
		return
	}
	switch id {
	case vue.Reduire:
		w.Hide()
	case vue.Demarrage:
		w.mu.Lock()
		on := !w.modele.Demarrage
		w.modele.Demarrage = on
		w.mu.Unlock()
		w.invalider()
		if w.OnToggle != nil {
			go w.OnToggle(on)
		}
	case vue.LangueFR, vue.LangueEN:
		l := i18n.FR
		if id == vue.LangueEN {
			l = i18n.EN
		}
		if l != i18n.Get() && w.OnLangue != nil {
			go w.OnLangue(l)
		}
	default:
		if w.OnCommand == nil {
			return
		}
		w.mu.Lock()
		if w.occupe[id] {
			w.mu.Unlock()
			return
		}
		w.occupe[id] = true
		w.mu.Unlock()
		w.invalider()
		go func() {
			defer func() {
				w.mu.Lock()
				delete(w.occupe, id)
				w.mu.Unlock()
				if h := w.hwnd.Load(); h != 0 {
					pPostMessage.Call(h, wmRefreshWin, 0, 0)
				}
			}()
			w.OnCommand(id)
		}()
	}
}

func touche(vk uintptr) bool {
	r, _, _ := pGetKeyState.Call(vk)
	return r&0x8000 != 0
}

func windowProc(hwnd, m, wparam, lparam uintptr) uintptr {
	w := currentWindow
	switch m {
	case wmPaint:
		w.peindre(hwnd)
		return 0
	case wmEraseBkgnd:
		return 1
	case wmMouseMove:
		if !w.suivi {
			tme := struct {
				Size, Flags uint32
				Hwnd        uintptr
				Hover       uint32
			}{Flags: tmeLeave, Hwnd: hwnd}
			tme.Size = uint32(unsafe.Sizeof(tme))
			pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			w.suivi = true
		}
		if z := w.zoneA(loword(lparam), hiword(lparam)); z != w.inter.Survol {
			w.inter.Survol = z
			w.invalider()
		}
		return 0
	case wmMouseLeave:
		w.suivi = false
		if w.inter.Survol != -1 {
			w.inter.Survol = -1
			w.invalider()
		}
		return 0
	case wmLButtonDown:
		w.inter.Appui = w.zoneA(loword(lparam), hiword(lparam))
		w.inter.FocusVisible = false
		if w.inter.Appui >= 0 {
			w.inter.Focus = w.inter.Appui
			pSetCapture.Call(hwnd)
		}
		w.invalider()
		return 0
	case wmLButtonUp:
		appui := w.inter.Appui
		w.inter.Appui = -1
		pReleaseCapture.Call()
		w.invalider()
		if appui >= 0 && appui == w.zoneA(loword(lparam), hiword(lparam)) {
			w.activer(appui)
		}
		return 0
	case wmCaptureChg:
		if w.inter.Appui != -1 {
			w.inter.Appui = -1
			w.invalider()
		}
		return 0
	case wmSetCursor:
		if lparam&0xffff == 1 /*HTCLIENT*/ {
			c := uintptr(idcArrow)
			if id := w.inter.Survol; id >= 0 && !w.inactif(id) {
				c = idcHand
			}
			h, _, _ := pLoadCursor.Call(0, c)
			pSetCursor.Call(h)
			return 1
		}
	case wmKeyDown:
		switch wparam {
		case vkTab:
			if touche(vkShift) {
				w.deplacerFocus(-1)
			} else {
				w.deplacerFocus(1)
			}
		case vkRight, vkDown:
			w.deplacerFocus(1)
		case vkLeft, vkUp:
			w.deplacerFocus(-1)
		case vkReturn, vkSpace:
			if w.inter.Focus >= 0 {
				w.activer(w.inter.Focus)
			}
		case vkEscape:
			w.Hide()
		case vkV:
			if touche(vkControl) {
				w.activer(vue.Coller)
			}
		}
		return 0
	case wmSettingChg:
		// Passage du thème clair au sombre (ou l'inverse) pendant que la fenêtre est ouverte.
		if s := themeSombre(); s != w.sombre {
			w.sombre = s
			w.invalider()
		}
	case wmClose:
		w.Hide()
		return 0
	case wmSysCommand:
		// Bouton « — » de la barre de titre (et Windows+Bas) : la fenêtre part
		// directement près de l'horloge, sans passer par la barre des tâches.
		if wparam&0xFFF0 == scMinimize {
			w.Hide()
			return 0
		}
	case wmSize:
		if wparam == sizeMinimized {
			w.Hide()
			return 0
		}
	case wmRelabel:
		w.invalider()
		return 0
	case wmRefreshWin:
		w.ajusterHauteur(hwnd)
		w.invalider()
		return 0
	case wmNcDestroy:
		if w.peintre != nil {
			w.peintre.liberer()
			w.peintre = nil
		}
		w.bulles, w.outils, w.zones, w.suivi = 0, nil, nil, false
		w.hwnd.Store(0)
	}
	r, _, _ := pDefWindowProc.Call(hwnd, m, wparam, lparam)
	return r
}

// ajusterHauteur : la carte du cumul grandit avec le nombre de périmètres.
func (w *Window) ajusterHauteur(hwnd uintptr) {
	w.mu.Lock()
	h := vue.HauteurModele(w.modele)
	w.mu.Unlock()
	if h == w.hauteurC {
		return
	}
	w.hauteurC = h
	var wr rect32
	pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr)))
	ww, wh := w.taille(h)
	var wa rect32
	pSystemParamsInfo.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&wa)), 0)
	y := wr.T
	if y+int32(wh) > wa.B {
		y = wa.B - int32(wh)
	}
	if y < wa.T {
		y = wa.T
	}
	pSetWindowPos.Call(hwnd, 0, uintptr(wr.L), uintptr(y), uintptr(ww), uintptr(wh), swpNoZOrder|swpNoActivate)
}

// Show ouvre la fenêtre au premier plan (fil de l'interface ; ailleurs : PostShow).
func (w *Window) Show() {
	if w.inst == 0 {
		return
	}
	h := w.hwnd.Load()
	if h == 0 {
		if !w.creer() {
			return
		}
		h = w.hwnd.Load()
	}
	pShowWindow.Call(h, swRestore)
	pShowWindow.Call(h, swShowNormal)
	pSetForegroundWin.Call(h)
	pSetFocus.Call(h)
}

// Hide ferme la fenêtre, sans notification : le compagnon reste près de l'horloge.
// La position est retenue pour la prochaine ouverture.
func (w *Window) Hide() {
	h := w.hwnd.Load()
	if h == 0 {
		return
	}
	var wr rect32
	if r, _, _ := pGetWindowRect.Call(h, uintptr(unsafe.Pointer(&wr))); r != 0 {
		w.pos = &[2]int32{wr.L, wr.T}
	}
	pShowWindow.Call(h, swHide)
	pDestroyWindow.Call(h)
}

// Relabel redessine la fenêtre dans la langue choisie (depuis n'importe quel fil).
func (w *Window) Relabel() {
	if h := w.hwnd.Load(); h != 0 {
		pPostMessage.Call(h, wmRelabel, 0, 0)
	}
}

// PostShow : demande l'ouverture depuis un autre fil (via la fenêtre de l'icône,
// qui existe toujours).
func (w *Window) PostShow() {
	if current != nil && current.hwnd != 0 {
		pPostMessage.Call(current.hwnd, wmShow, 0, 0)
	}
}

// SetEtat met à jour ce que montre la fenêtre (depuis n'importe quel fil). Fenêtre
// fermée : l'état est seulement retenu, rien n'est dessiné.
func (w *Window) SetEtat(m vue.Modele) {
	w.mu.Lock()
	m.Demarrage = w.modele.Demarrage
	w.modele = m
	w.mu.Unlock()
	if h := w.hwnd.Load(); h != 0 {
		pPostMessage.Call(h, wmRefreshWin, 0, 0)
	}
}

// SetChecked règle l'interrupteur « Lancer au démarrage ».
func (w *Window) SetChecked(on bool) {
	w.mu.Lock()
	w.modele.Demarrage = on
	w.mu.Unlock()
	if h := w.hwnd.Load(); h != 0 {
		pPostMessage.Call(h, wmRefreshWin, 0, 0)
	}
}

// Confirm : question avec un bouton d'action (« Effacer », « Désinstaller ») et
// « Annuler », choisi par défaut.
func (w *Window) Confirm(text, action string) bool {
	if r, ok := dialogue(w.hwnd.Load(), tdWarningIcon, text,
		[]boutonDlg{{idAction, action}, {idAnnuler, i18n.T("dlg.cancel")}}, idAnnuler); ok {
		return r == idAction
	}
	r, _, _ := pMessageBox.Call(w.hwnd.Load(), str(text), str(AppName), mbYesNo|mbIconWarning|mbDefButton2|mbSetForegr)
	return r == idYes
}

// Info : message avec un bouton OK.
func (w *Window) Info(text string) {
	if _, ok := dialogue(w.hwnd.Load(), tdInfoIcon, text, []boutonDlg{{idAction, "OK"}}, idAction); ok {
		return
	}
	pMessageBox.Call(w.hwnd.Load(), str(text), str(AppName), mbOK|mbIconInfo|mbSetForegr)
}
