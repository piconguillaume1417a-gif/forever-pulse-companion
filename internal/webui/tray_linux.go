//go:build linux && cgo

package webui

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"wowsync/internal/icone"
)

// Item : une ligne du menu de l'icône. Action nil = ligne d'information.
type Item struct {
	Key       string
	Label     string
	Action    func()
	Checked   bool
	Toggle    bool
	Separator bool
	Default   bool
}

type pixmap struct {
	Width  int32
	Height int32
	Data   []byte
}

type tooltip struct {
	IconName   string
	IconPixmap []pixmap
	Title      string
	SubTitle   string
}

// Tray : icône StatusNotifierItem et menu com.canonical.dbusmenu.
type Tray struct {
	mu      sync.Mutex
	conn    *dbus.Conn
	service string
	props   *prop.Properties
	menu    *dbusMenu
	icones  [3][]pixmap
	couleur int
	tip     string
	active  bool

	Menu           func() []Item
	OnOpen         func()
	OnBalloonClick func()
}

func NewTray() *Tray {
	t := &Tray{couleur: icone.Vert}
	for i := 0; i < 3; i++ {
		t.icones[i] = pixmapsPour(i)
	}
	return t
}

// Start enregistre l'icône près de la barre. Sans StatusNotifierWatcher, Start
// renvoie une erreur et le compagnon continue sans icône.
func (t *Tray) Start() error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return fmt.Errorf("bus de session : %w", err)
	}
	service := fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())
	if rep, err := conn.RequestName(service, dbus.NameFlagDoNotQueue); err != nil {
		return err
	} else if rep != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("nom d'icône déjà pris")
	}

	item := &statusItem{t: t}
	menu := &dbusMenu{t: t}
	if err := conn.Export(item, "/StatusNotifierItem", "org.kde.StatusNotifierItem"); err != nil {
		return err
	}
	if err := conn.Export(menu, "/MenuBar", "com.canonical.dbusmenu"); err != nil {
		return err
	}
	menuProps, err := prop.Export(conn, "/MenuBar", prop.Map{
		"com.canonical.dbusmenu": {
			"Version":       {Value: uint32(3), Emit: prop.EmitFalse},
			"TextDirection": {Value: "ltr", Emit: prop.EmitFalse},
			"Status":        {Value: "normal", Emit: prop.EmitFalse},
			"IconThemePath": {Value: []string{}, Emit: prop.EmitFalse},
		},
	})
	if err != nil {
		return err
	}
	_ = menuProps

	props, err := prop.Export(conn, "/StatusNotifierItem", prop.Map{
		"org.kde.StatusNotifierItem": {
			"Category":   {Value: "ApplicationStatus", Emit: prop.EmitFalse},
			"Id":         {Value: "forever-pulse-companion", Emit: prop.EmitFalse},
			"Title":      {Value: AppName, Emit: prop.EmitFalse},
			"Status":     {Value: "Active", Emit: prop.EmitFalse},
			"IconName":   {Value: "", Emit: prop.EmitFalse},
			"IconPixmap": {Value: t.icones[0], Emit: prop.EmitTrue},
			"ToolTip":    {Value: tooltip{IconName: "", IconPixmap: t.icones[0], Title: AppName}, Emit: prop.EmitTrue},
			"Menu":       {Value: dbus.ObjectPath("/MenuBar"), Emit: prop.EmitFalse},
			"ItemIsMenu": {Value: false, Emit: prop.EmitFalse},
		},
	})
	if err != nil {
		return err
	}

	t.mu.Lock()
	t.conn, t.service, t.props, t.menu, t.active = conn, service, props, menu, true
	t.mu.Unlock()

	watcher := conn.Object("org.kde.StatusNotifierWatcher", "/StatusNotifierWatcher")
	if err := watcher.Call("org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem", 0, service).Err; err != nil {
		t.Quit()
		return fmt.Errorf("StatusNotifierWatcher : %w", err)
	}
	t.Refresh()
	return nil
}

func (t *Tray) Active() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.active
}

func (t *Tray) Quit() {
	t.mu.Lock()
	conn, service, active := t.conn, t.service, t.active
	t.conn, t.active = nil, false
	t.mu.Unlock()
	if conn == nil {
		return
	}
	if active {
		_, _ = conn.ReleaseName(service)
	}
	_ = conn.Close()
}

// Set change la pastille d'état et l'infobulle.
func (t *Tray) Set(couleur int, tip string) {
	if couleur < 0 || couleur > 2 {
		couleur = 0
	}
	t.mu.Lock()
	deja := t.couleur == couleur && t.tip == tip
	t.couleur, t.tip = couleur, tip
	props, conn, active := t.props, t.conn, t.active
	icons := t.icones[couleur]
	t.mu.Unlock()
	if !active || deja {
		return
	}
	if props != nil {
		props.SetMust("org.kde.StatusNotifierItem", "IconPixmap", icons)
		props.SetMust("org.kde.StatusNotifierItem", "ToolTip", tooltip{IconName: "", IconPixmap: icons, Title: AppName, SubTitle: tip})
	}
	if conn != nil {
		_ = conn.Emit("/StatusNotifierItem", "org.kde.StatusNotifierItem.NewIcon")
		_ = conn.Emit("/StatusNotifierItem", "org.kde.StatusNotifierItem.NewToolTip")
	}
	t.Refresh()
}

// Refresh force la relecture du menu (état, autostart coché, version prête).
func (t *Tray) Refresh() {
	t.mu.Lock()
	conn, menu, active := t.conn, t.menu, t.active
	t.mu.Unlock()
	if !active || conn == nil || menu == nil {
		return
	}
	rev := menu.renouveler()
	_ = conn.Emit("/MenuBar", "com.canonical.dbusmenu.LayoutUpdated", rev, int32(0))
}

func (t *Tray) Notify(title, text string, warn bool) {
	args := []string{"-a", AppName}
	if warn {
		args = append(args, "-u", "critical")
	}
	args = append(args, title, text)
	go func() { _ = exec.Command("notify-send", args...).Run() }()
}

func (t *Tray) open() {
	if t.OnOpen != nil {
		go t.OnOpen()
	}
}

type statusItem struct{ t *Tray }

func (s *statusItem) Activate(x, y int32) *dbus.Error {
	s.t.open()
	return nil
}

func (s *statusItem) SecondaryActivate(x, y int32) *dbus.Error { return nil }
func (s *statusItem) ContextMenu(x, y int32) *dbus.Error       { return nil }

func (s *statusItem) Scroll(delta int32, orientation string) *dbus.Error { return nil }

// dbusMenu expose com.canonical.dbusmenu. Les identifiants sont reconstruits à
// chaque état ; le menu est relu par l'hôte à chaque LayoutUpdated.
type dbusMenu struct {
	t  *Tray
	mu sync.Mutex

	revision uint32
	ids      map[int32]Item
	cles     map[string]int32
	suivant  int32
	layout   dbusLayout
}

type dbusLayout struct {
	ID         int32
	Properties map[string]dbus.Variant
	Children   []dbus.Variant
}

type groupProps struct {
	ID    int32
	Props map[string]dbus.Variant
}

type event struct {
	ID        int32
	EventID   string
	Data      dbus.Variant
	Timestamp uint32
}

func (m *dbusMenu) renouveler() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.revision++
	if m.t.Menu == nil {
		m.ids = nil
		m.layout = dbusLayout{ID: 0, Properties: map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}}
		return m.revision
	}
	items := m.t.Menu()
	m.ids = make(map[int32]Item, len(items))
	if m.cles == nil {
		m.cles = map[string]int32{}
	}
	var enfants []dbus.Variant
	for _, it := range items {
		cle := it.Key
		if cle == "" {
			cle = it.Label
			if it.Separator {
				cle = "separator"
			}
		}
		id, ok := m.cles[cle]
		if !ok {
			m.suivant++
			id = m.suivant
			m.cles[cle] = id
		}
		m.ids[id] = it
		layout := dbusLayout{ID: id, Properties: proprietes(it)}
		enfants = append(enfants, dbus.MakeVariant(layout))
	}
	m.layout = dbusLayout{ID: 0, Properties: map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}, Children: enfants}
	return m.revision
}

func (m *dbusMenu) GetLayout(parentID int32, depth int32, props []string) (uint32, dbusLayout, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.layout.Properties == nil {
		m.revision++
		m.layout = dbusLayout{ID: 0, Properties: map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")}}
	}
	if parentID != 0 {
		return m.revision, dbusLayout{ID: parentID}, nil // pas de sous-menu
	}
	layout := m.layout
	if depth == 0 {
		layout.Children = nil
	}
	return m.revision, layout, nil
}

func (m *dbusMenu) GetGroupProperties(ids []int32, props []string) ([]groupProps, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(ids) == 0 {
		ids = []int32{0}
		if m.ids != nil {
			for id := range m.ids {
				ids = append(ids, id)
			}
		}
	}
	out := make([]groupProps, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			out = append(out, groupProps{ID: 0, Props: m.layout.Properties})
			continue
		}
		if it, ok := m.ids[id]; ok {
			out = append(out, groupProps{ID: id, Props: proprietes(it)})
		}
	}
	return out, nil
}

func (m *dbusMenu) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if id == 0 {
		return m.layout.Properties[name], nil
	}
	it, ok := m.ids[id]
	if !ok {
		return dbus.MakeVariant(""), nil
	}
	return proprietes(it)[name], nil
}

func proprietes(it Item) map[string]dbus.Variant {
	p := map[string]dbus.Variant{
		"label":   dbus.MakeVariant(it.Label),
		"enabled": dbus.MakeVariant(it.Action != nil),
		"visible": dbus.MakeVariant(true),
	}
	if it.Separator {
		p["type"] = dbus.MakeVariant("separator")
	} else {
		p["type"] = dbus.MakeVariant("standard")
		if it.Default {
			p["is-default"] = dbus.MakeVariant(true)
		}
	}
	if it.Toggle && !it.Separator {
		p["toggle-type"] = dbus.MakeVariant("checkmark")
		if it.Checked {
			p["toggle-state"] = dbus.MakeVariant(int32(1))
		} else {
			p["toggle-state"] = dbus.MakeVariant(int32(0))
		}
	}
	return p
}

func (m *dbusMenu) AboutToShow(id int32) (bool, *dbus.Error) { return false, nil }

func (m *dbusMenu) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return nil, nil, nil
}

func (m *dbusMenu) Event(id int32, eventID string, data dbus.Variant, timestamp uint32) *dbus.Error {
	if eventID != "clicked" {
		return nil
	}
	m.mu.Lock()
	it, ok := m.ids[id]
	m.mu.Unlock()
	if ok && it.Action != nil {
		go func() {
			it.Action()
			m.t.Refresh()
		}()
	}
	return nil
}

func (m *dbusMenu) EventGroup(events []event) ([]bool, *dbus.Error) {
	res := make([]bool, len(events))
	for i, e := range events {
		_ = m.Event(e.ID, e.EventID, e.Data, e.Timestamp)
		res[i] = true
	}
	return res, nil
}

func pixmapsPour(couleur int) []pixmap {
	tailles := []int32{22, 32, 48, 64}
	out := make([]pixmap, 0, len(tailles))
	for _, n := range tailles {
		img := icone.Dessine(int(n), couleur)
		data := make([]byte, 0, int(n)*int(n)*4)
		for y := 0; y < int(n); y++ {
			for x := 0; x < int(n); x++ {
				c := img.NRGBAAt(x, y)
				// ARGB32, ordre réseau.
				data = append(data, c.A, c.R, c.G, c.B)
			}
		}
		out = append(out, pixmap{Width: n, Height: n, Data: data})
	}
	return out
}
