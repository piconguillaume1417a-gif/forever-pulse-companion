//go:build linux && cgo

package webui

import "testing"

func TestMenuIDsAreStable(t *testing.T) {
	tray := NewTray()
	menu := &dbusMenu{t: tray}
	menu.t.Menu = func() []Item {
		return []Item{{Key: "open", Label: "Open"}, {Key: "quit", Label: "Quit"}}
	}
	menu.renouveler()
	menu.mu.Lock()
	idOpen := menu.cles["open"]
	idQuit := menu.cles["quit"]
	menu.mu.Unlock()

	// Une ligne d'état apparaît : les identifiants des actions ne bougent pas.
	menu.t.Menu = func() []Item {
		return []Item{{Key: "status", Label: "Status"}, {Key: "open", Label: "Open"}, {Key: "quit", Label: "Quit"}}
	}
	menu.renouveler()
	menu.mu.Lock()
	defer menu.mu.Unlock()
	if menu.cles["open"] != idOpen || menu.cles["quit"] != idQuit {
		t.Fatalf("identifiants déplacés : %v %v", menu.cles["open"], menu.cles["quit"])
	}
	if idOpen == idQuit {
		t.Fatal("identifiants dupliqués")
	}
}

func TestPixmapsAreARGB32(t *testing.T) {
	for _, ps := range [][]pixmap{pixmapsPour(0), pixmapsPour(1), pixmapsPour(2)} {
		if len(ps) != 4 {
			t.Fatalf("tailles = %d", len(ps))
		}
		for _, p := range ps {
			if len(p.Data) != int(p.Width*p.Height*4) {
				t.Fatalf("%dx%d : %d octets", p.Width, p.Height, len(p.Data))
			}
		}
	}
}
