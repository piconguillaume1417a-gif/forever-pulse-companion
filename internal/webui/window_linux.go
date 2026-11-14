//go:build linux && cgo

// Package webui affiche la fenêtre du compagnon dans WebKitGTK : la mise en page
// vient de internal/vue (opérations de dessin et zones cliquables) et le rendu
// est fait par un canvas HTML. Aucune API Windows n'est utilisée.
package webui

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1
#include <stdlib.h>
#include "webui.h"
*/
import "C"

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"wowsync/internal/i18n"
	"wowsync/internal/icone"
	"wowsync/internal/vue"
)

const AppName = "Forever Pulse Companion"

//go:embed page.html
var pageHTML string

// Garde le fil GTK et le WebView sur le fil principal.
func init() { runtime.LockOSThread() }

type Window struct {
	mu     sync.Mutex
	modele vue.Modele
	occupe map[int]bool
	inter  vue.Interaction
	zones  []vue.Zone
	dark   bool
	view   *C.FpcView
	logo   string

	OnCommand func(id int)
	OnToggle  func(checked bool)
	OnLangue  func(l i18n.Lang)
	OnClose   func()
}

var (
	actifMu sync.Mutex
	actif   *Window

	dispMu  sync.Mutex
	dispID  uint64
	dispFns = map[uint64]func(){}
)

func NewWindow() *Window {
	dark := themeSombre()
	logo := logoDataURI()
	return &Window{
		modele: vue.Modele{},
		occupe: map[int]bool{},
		inter:  vue.Interaction{Survol: -1, Appui: -1, Focus: -1},
		dark:   dark,
		logo:   logo,
	}
}

func (w *Window) Create() error {
	w.mu.Lock()
	html := pageHTML
	dark := w.dark
	w.mu.Unlock()

	titre := C.CString(AppName)
	src := C.CString(html)
	defer C.free(unsafe.Pointer(titre))
	defer C.free(unsafe.Pointer(src))

	p := C.fpc_view_new(titre, C.int(vue.Largeur), C.int(vue.Hauteur(0)), src, cBool(dark))
	if p == nil {
		return fmt.Errorf("WebKitGTK indisponible")
	}
	installerIcone(p)
	w.mu.Lock()
	w.view = p
	w.mu.Unlock()

	actifMu.Lock()
	actif = w
	actifMu.Unlock()
	return nil
}

func installerIcone(p *C.FpcView) {
	var b bytes.Buffer
	if err := png.Encode(&b, icone.Dessine(256, icone.SansPastille)); err != nil {
		return
	}
	C.fpc_set_icon(p, (*C.uchar)(unsafe.Pointer(&b.Bytes()[0])), C.ulong(b.Len()))
}

func cBool(v bool) C.int {
	if v {
		return 1
	}
	return 0
}

func (w *Window) pointeur() *C.FpcView {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.view
}

// Run lance la boucle GTK. ready est appelé une fois la page chargée. hidden
// démarre sans montrer la fenêtre (--tray) ; elle se rouvre au prochain lancement.
func (w *Window) Run(hidden bool, ready func()) error {
	p := w.pointeur()
	if p == nil {
		return fmt.Errorf("fenêtre non créée")
	}
	if hidden {
		C.fpc_hide(p)
	} else {
		C.fpc_present(p)
	}
	if ready != nil {
		go ready()
	}
	C.fpc_run(p)
	w.mu.Lock()
	w.view = nil
	w.mu.Unlock()
	C.fpc_view_free(p)
	actifMu.Lock()
	if actif == w {
		actif = nil
	}
	actifMu.Unlock()
	return nil
}

func (w *Window) Quit() {
	if p := w.pointeur(); p != nil {
		C.fpc_quit(p)
	}
}

func (w *Window) PostShow() { w.callAsync(func() { C.fpc_present(w.pointeur()) }) }
func (w *Window) Show()     { w.callAsync(func() { C.fpc_show(w.pointeur()) }) }
func (w *Window) Hide()     { w.callAsync(func() { C.fpc_hide(w.pointeur()) }) }
func (w *Window) Iconify()  { w.callAsync(func() { C.fpc_iconify(w.pointeur()) }) }

func (w *Window) SetEtat(m vue.Modele) {
	w.mu.Lock()
	// Le réglage « lancer à la connexion » ne fait pas partie de l'état du
	// moteur : il est conservé d'une mise à jour à l'autre, comme sous Windows.
	m.Demarrage = w.modele.Demarrage
	w.modele = m
	w.mu.Unlock()
	w.publishLater()
}

func (w *Window) SetChecked(on bool) {
	w.mu.Lock()
	w.modele.Demarrage = on
	w.mu.Unlock()
	w.publishLater()
}

func (w *Window) publishLater() { w.callAsync(w.publish) }

func (w *Window) publish() {
	w.mu.Lock()
	m := w.modele
	occ := make(map[int]bool, len(w.occupe))
	for k, v := range w.occupe {
		occ[k] = v
	}
	in := w.inter
	in.Occupe = occ
	dark := w.dark
	w.mu.Unlock()

	theme := vue.Clair()
	if dark {
		theme = vue.Sombre()
	}
	ops, zones, _, h := vue.Construire(m, in, theme)

	w.mu.Lock()
	w.zones = zones
	w.mu.Unlock()

	payload := struct {
		Ops   []vue.Op   `json:"ops"`
		Zones []vue.Zone `json:"zones"`
		W     int        `json:"w"`
		H     int        `json:"h"`
		Dark  bool       `json:"dark"`
	}{ops, zones, vue.Largeur, h, dark}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	js := "window.fpc.render(" + string(b) + ")"
	w.eval(js)
	if p := w.pointeur(); p != nil {
		C.fpc_resize(p, C.int(vue.Largeur), C.int(h))
	}
}

func (w *Window) eval(js string) {
	p := w.pointeur()
	if p == nil {
		return
	}
	c := C.CString(js)
	defer C.free(unsafe.Pointer(c))
	C.fpc_eval(p, c)
}

func (w *Window) callAsync(f func()) {
	dispatch(f)
}

func dispatch(f func()) {
	actifMu.Lock()
	w := actif
	actifMu.Unlock()
	if w == nil {
		return
	}
	p := w.pointeur()
	if p == nil {
		return
	}
	dispMu.Lock()
	dispID++
	id := dispID
	dispFns[id] = f
	dispMu.Unlock()
	C.fpc_dispatch(p, C.guint64(id))
}

func (w *Window) callMain(f func()) {
	done := make(chan struct{})
	dispatch(func() { f(); close(done) })
	<-done
}

func (w *Window) inactif(id int) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, z := range w.zones {
		if z.ID == id {
			return z.Inactif
		}
	}
	return true
}

func (w *Window) activate(id int) {
	if id < 0 || w.inactif(id) {
		return
	}
	switch id {
	case vue.Demarrage:
		w.mu.Lock()
		on := !w.modele.Demarrage
		w.modele.Demarrage = on
		w.mu.Unlock()
		w.publishLater()
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
	case vue.Reduire:
		w.Iconify()
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
		w.publishLater()
		go func() {
			defer func() {
				w.mu.Lock()
				delete(w.occupe, id)
				w.mu.Unlock()
				w.publishLater()
			}()
			w.OnCommand(id)
		}()
	}
}

func (w *Window) ChoisirDossier(titre, initial string) (string, bool) {
	var choisi string
	var ok bool
	t := C.CString(titre)
	i := C.CString(initial)
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(i))
	w.callMain(func() {
		p := C.fpc_choose_folder(w.pointeur(), t, i)
		if p != nil {
			choisi = C.GoString(p)
			C.fpc_free_string(p)
			ok = true
		}
	})
	return choisi, ok
}

func (w *Window) Confirm(text, action string) bool {
	var ok bool
	t := C.CString(text)
	a := C.CString(action)
	defer C.free(unsafe.Pointer(t))
	defer C.free(unsafe.Pointer(a))
	w.callMain(func() { ok = C.fpc_confirm(w.pointeur(), t, a) != 0 })
	return ok
}

func (w *Window) Info(text string) {
	t := C.CString(text)
	defer C.free(unsafe.Pointer(t))
	w.callMain(func() { C.fpc_info(w.pointeur(), t) })
}

func (w *Window) Notify(title, text string, warn bool) {
	args := []string{"-a", AppName}
	if warn {
		args = append(args, "-u", "critical")
	}
	args = append(args, title, text)
	go func() { _ = exec.Command("notify-send", args...).Run() }()
}

// Open ouvre un fichier ou une adresse avec l'application par défaut.
func Open(target string) { go func() { _ = exec.Command("xdg-open", target).Run() }() }

func autostartPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "autostart", "forever-pulse-companion.desktop"), nil
}

func StartWithWindows(on bool) error {
	p, err := autostartPath()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, _ = filepath.Abs(exe)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	contenu := "[Desktop Entry]\nType=Application\nName=" + AppName +
		"\nComment=Forever Pulse Companion\nExec=\"" + strings.ReplaceAll(exe, "\"", "\\\"") + "\" --tray\n" +
		"Terminal=false\nX-GNOME-Autostart-enabled=true\n"
	return os.WriteFile(p, []byte(contenu), 0o600)
}

func StartsWithWindows() bool {
	p, err := autostartPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// DossiersBattleNet cherche WoW sous Wine, Steam/Proton et les dossiers usuels.
func DossiersBattleNet() []string {
	var out []string
	home, _ := os.UserHomeDir()
	prefixes := []string{os.Getenv("WINEPREFIX")}
	if home != "" {
		prefixes = append(prefixes, filepath.Join(home, ".wine"))
	}
	steam := []string{
		filepath.Join(home, ".local", "share", "Steam"),
		filepath.Join(home, ".steam", "steam"),
		filepath.Join(home, "Games", "Steam"),
	}
	libraries := append([]string{}, steam...)
	// Bibliothèques Steam supplémentaires.
	for _, s := range steam {
		b, err := os.ReadFile(filepath.Join(s, "steamapps", "libraryfolders.vdf"))
		if err != nil {
			continue
		}
		for _, ligne := range strings.Split(string(b), "\n") {
			if i := strings.Index(ligne, "\"path\""); i >= 0 {
				if j := strings.Index(ligne[i+6:], "\""); j >= 0 {
					reste := ligne[i+6+j+1:]
					if k := strings.Index(reste, "\""); k >= 0 {
						libraries = append(libraries, reste[:k])
					}
				}
			}
		}
	}
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		for _, sub := range []string{
			`drive_c/Program Files (x86)/World of Warcraft`,
			`drive_c/Program Files/World of Warcraft`,
			`drive_c/Program Files (x86)/Blizzard Entertainment/World of Warcraft`,
			`drive_c/Program Files/Blizzard Entertainment/World of Warcraft`,
		} {
			out = append(out, filepath.Join(p, filepath.FromSlash(sub)))
		}
	}
	for _, s := range libraries {
		out = append(out, filepath.Join(s, "steamapps", "common", "World of Warcraft"))
	}
	if home != "" {
		out = append(out, filepath.Join(home, "Games", "World of Warcraft"),
			filepath.Join(home, "Jeux", "World of Warcraft"))
	}
	return out
}

// Lecteurs : les points de montage ne sont pas énumérés ; Wine et Steam suffisent.
func Lecteurs() []string { return nil }

func themeSombre() bool {
	if strings.Contains(strings.ToLower(os.Getenv("GTK_THEME")), "dark") {
		return true
	}
	for _, c := range [][]string{
		{"gsettings", "get", "org.gnome.desktop.interface", "color-scheme"},
		{"gsettings", "get", "org.gnome.desktop.interface", "gtk-theme"},
	} {
		out, err := exec.Command(c[0], c[1:]...).Output()
		if err == nil && strings.Contains(strings.ToLower(string(out)), "dark") {
			return true
		}
	}
	return false
}

func logoDataURI() string {
	img := icone.Logo(304, 180)
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(b.Bytes())
}

//export goWebuiMessage
func goWebuiMessage(cmsg *C.char) {
	actifMu.Lock()
	w := actif
	actifMu.Unlock()
	if w == nil {
		return
	}
	var m struct {
		T            string `json:"t"`
		ID           int    `json:"id"`
		Hover        int    `json:"hover"`
		Press        int    `json:"press"`
		Focus        int    `json:"focus"`
		FocusVisible bool   `json:"focusVisible"`
	}
	if err := json.Unmarshal([]byte(C.GoString(cmsg)), &m); err != nil {
		return
	}
	switch m.T {
	case "ready":
		if w.logo != "" {
			b, _ := json.Marshal(w.logo)
			w.eval("window.fpc.logo(" + string(b) + ")")
		}
		w.publish()
	case "ui":
		w.mu.Lock()
		w.inter.Survol, w.inter.Appui, w.inter.Focus = m.Hover, m.Press, m.Focus
		w.inter.FocusVisible = m.FocusVisible
		w.mu.Unlock()
		w.publishLater()
	case "activate":
		go w.activate(m.ID)
	case "escape":
		w.Iconify()
	}
}

//export goWebuiLoadFinished
func goWebuiLoadFinished() {
	actifMu.Lock()
	w := actif
	actifMu.Unlock()
	if w == nil {
		return
	}
	if w.logo != "" {
		b, _ := json.Marshal(w.logo)
		w.eval("window.fpc.logo(" + string(b) + ")")
	}
	w.publish()
}

//export goWebuiCloseRequested
func goWebuiCloseRequested() C.int {
	actifMu.Lock()
	w := actif
	actifMu.Unlock()
	if w != nil {
		if w.OnClose != nil {
			go w.OnClose()
		} else {
			w.Quit()
		}
	}
	return 1
}

//export goWebuiDispatch
func goWebuiDispatch(id C.guint64) {
	k := uint64(id)
	dispMu.Lock()
	f := dispFns[k]
	delete(dispFns, k)
	dispMu.Unlock()
	if f != nil {
		f()
	}
}
