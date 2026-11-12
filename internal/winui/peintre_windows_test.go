//go:build windows

package winui

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"wowsync/internal/i18n"
	"wowsync/internal/vue"
)

var (
	pGetTextFace       = gdi32.NewProc("GetTextFaceW")
	pGetGlyphIndices   = gdi32.NewProc("GetGlyphIndicesW")
	pGetDIBits         = gdi32.NewProc("GetDIBits")
	pCreateCompatDCTst = gdi32.NewProc("CreateCompatibleDC")
	pGdiFlush          = gdi32.NewProc("GdiFlush")
)

// Le texte coréen est dessiné en Malgun Gothic (police livrée avec Windows), qui a
// bien les syllabes affichées : pas de carré vide (« tofu »).
func TestPoliceCoreenne(t *testing.T) {
	dc, _, _ := pCreateCompatDCTst.Call(0)
	if dc == 0 {
		t.Skip("pas de contexte GDI")
	}
	defer pDeleteDC.Call(dc)
	p := nouveauPeintre(96)
	defer p.liberer()
	texte := ""
	defer i18n.Set(i18n.FR)
	i18n.Set(i18n.KO)
	for _, k := range []string{"btn.connect", "btn.send", "win.autostart", "dir.title", "tip.dirpath", "hint.working", "st.allsent"} {
		texte += i18n.T(k)
	}
	texte += i18n.Court(i18n.KO)
	for _, gras := range []bool{false, true} {
		h := p.police(clePolice{taille: 13, gras: gras, coreen: true})
		old, _, _ := pSelectObject.Call(dc, h)
		var face [64]uint16
		pGetTextFace.Call(dc, uintptr(len(face)), uintptr(unsafe.Pointer(&face[0])))
		if got := windowsString(face[:]); got != "Malgun Gothic" {
			t.Errorf("gras=%t : police %q au lieu de Malgun Gothic", gras, got)
		}
		s := []uint16{}
		for _, r := range texte {
			if r < 0x10000 {
				s = append(s, uint16(r))
			}
		}
		idx := make([]uint16, len(s))
		pGetGlyphIndices.Call(dc, uintptr(unsafe.Pointer(&s[0])), uintptr(len(s)), uintptr(unsafe.Pointer(&idx[0])), 1 /*GGI_MARK_NONEXISTING_GLYPHS*/)
		manque := 0
		for i, g := range idx {
			if g == 0xFFFF && s[i] != '\n' {
				manque++
			}
		}
		if manque > 0 {
			t.Errorf("gras=%t : %d caractères sans glyphe en Malgun Gothic", gras, manque)
		}
		pSelectObject.Call(dc, old)
	}
}

func windowsString(u []uint16) string {
	n := 0
	for n < len(u) && u[n] != 0 {
		n++
	}
	r := make([]rune, n)
	for i := 0; i < n; i++ {
		r[i] = rune(u[i])
	}
	return string(r)
}

// TestApercuGDI : avec WOWSYNC_APERCU_GDI=<dossier>, écrit en PNG la fenêtre
// dessinée par le vrai peintre (GDI, GDI+, ClearType), hors écran : aucune fenêtre,
// aucun Companion lancé, aucune donnée lue. Scènes synthétiques.
func TestApercuGDI(t *testing.T) {
	dir := os.Getenv("WOWSYNC_APERCU_GDI")
	if dir == "" {
		t.Skip("WOWSYNC_APERCU_GDI absent : rendu non demandé")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Comme le fil de l'interface : GDI garde ses opérations en lot par fil.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer i18n.Set(i18n.FR)
	jour := func(j int) string { return time.Date(2026, 9, j, 12, 0, 0, 0, time.UTC).Format(i18n.T("fmt.date")) }
	base := func() vue.Modele {
		portees := []vue.Portee{{Nom: "4619-PVP-Alliance", Total: 51767, Depuis: jour(18)}, {Nom: "4619-PVP-Horde", Total: 2310, Depuis: jour(21)}}
		return vue.Modele{Pret: true, Version: "0.10.0-rc.17", Demarrage: true, Envoyes: 546, Portees: portees,
			Dossier: "C:/Program Files (x86)/World of Warcraft", DossierEtat: vue.DossierPret,
			Details: []string{"synthétique"}}
	}
	type scene struct {
		nom   string
		lang  i18n.Lang
		th    vue.Theme
		m     func() vue.Modele
		inter vue.Interaction
	}
	aucune := vue.Interaction{Survol: -1, Appui: -1, Focus: -1}
	scenes := []scene{
		{"ko-01-tout-envoye-clair", i18n.KO, vue.Clair(), func() vue.Modele {
			m := base()
			m.Code, m.Message = "allsent", i18n.T("st.allsent")
			envoi := time.Date(2026, 9, 27, 15, 4, 0, 0, time.UTC).Format(i18n.T("fmt.datetime"))
			m.DernierEnvoi = i18n.T("st.lastsend", envoi, "12", vue.Nombre(3456))
			return m
		}, vue.Interaction{Survol: vue.Envoyer, Appui: -1, Focus: -1}},
		{"ko-02-en-attente-sombre", i18n.KO, vue.Sombre(), func() vue.Modele {
			m := base()
			m.Couleur, m.Code, m.Attente, m.Refuses = 1, "pending", 5, 2
			m.Message = i18n.T("st.pending", 5) + i18n.T("st.retry", "15:32")
			return m
		}, vue.Interaction{Survol: vue.Effacer, Appui: -1, Focus: -1, Occupe: map[int]bool{vue.Envoyer: true}}},
		{"ko-03-connexion-en-cours-clair", i18n.KO, vue.Clair(), func() vue.Modele {
			m := base()
			m.Couleur, m.Code, m.Message, m.Envoyes = 2, "notoken", i18n.T("st.notoken"), 0
			m.Connection, m.ConnectionCode = vue.Connexion("notoken", "pending", "0E26CF55")
			return m
		}, aucune},
		{"ko-04-connecte-sombre", i18n.KO, vue.Sombre(), func() vue.Modele {
			m := base()
			m.Code, m.Message = "working", i18n.T("st.working")
			m.Connection, _ = vue.Connexion("working", "connected", "synthetic@example.invalid")
			return m
		}, aucune},
		{"ko-05-dossier-introuvable-clair", i18n.KO, vue.Clair(), func() vue.Modele {
			m := base()
			m.Code, m.Message = "allsent", i18n.T("st.allsent")
			m.DossierEtat = vue.DossierAbsent
			return m
		}, vue.Interaction{Survol: vue.LangueKO, Appui: -1, Focus: -1}},
		{"ko-06-sans-lua-hdv-sombre", i18n.KO, vue.Sombre(), func() vue.Modele {
			m := base()
			m.Couleur, m.Code, m.Message = 1, "pending", i18n.T("st.auctionpending", 2994)
			m.DossierEtat, m.Auctions, m.AuctionSent, m.AuctionPending, m.AuctionUnproven = vue.DossierSansLua, true, 5000, 2994, 31
			return m
		}, aucune},
		{"ko-07-refus-six-portees-clair", i18n.KO, vue.Clair(), func() vue.Modele {
			m := base()
			m.Couleur, m.Code, m.Message, m.Refuses = 2, "body", i18n.T("st.body"), 3
			for i := 0; i < 4; i++ {
				m.Portees = append(m.Portees, vue.Portee{Nom: "4620-PVE-Horde", Total: 1234 * (i + 1), Depuis: jour(20)})
			}
			return m
		}, vue.Interaction{Survol: -1, Appui: -1, Focus: vue.Installations, FocusVisible: true}},
		{"ko-08-jeton-refuse-sombre", i18n.KO, vue.Sombre(), func() vue.Modele {
			m := base()
			m.Couleur, m.Code, m.Message = 2, "token", i18n.T("st.token")
			m.Connection, _ = vue.Connexion("token", "", "")
			return m
		}, aucune},
		{"ko-09-demarrage-clair", i18n.KO, vue.Clair(), func() vue.Modele {
			m := base()
			m.Pret, m.Portees, m.Demarrage = false, nil, false
			return m
		}, aucune},
		// Le sélecteur à trois cases dans les deux autres langues.
		{"fr-01-tout-envoye-clair", i18n.FR, vue.Clair(), func() vue.Modele {
			m := base()
			m.Code, m.Message = "allsent", i18n.T("st.allsent")
			return m
		}, aucune},
		{"en-01-tout-envoye-sombre", i18n.EN, vue.Sombre(), func() vue.Modele {
			m := base()
			m.Code, m.Message = "allsent", i18n.T("st.allsent")
			return m
		}, vue.Interaction{Survol: vue.LangueKO, Appui: -1, Focus: -1}},
	}
	for _, dpi := range []int{96, 144} {
		for _, s := range scenes {
			i18n.Set(s.lang)
			ops, _, w, h := vue.Construire(s.m(), s.inter, s.th)
			img := rendre(t, dpi, w, h, ops)
			nom := filepath.Join(dir, fmt.Sprintf("%s-%dppp.png", s.nom, dpi))
			f, err := os.Create(nom)
			if err != nil {
				t.Fatal(err)
			}
			if err := png.Encode(f, img); err != nil {
				t.Fatal(err)
			}
			f.Close()
		}
	}
}

// rendre : peint les opérations dans une image en mémoire et la relit (GetDIBits).
func rendre(t *testing.T, dpi, wl, hl int, ops []vue.Op) image.Image {
	p := nouveauPeintre(dpi)
	defer p.liberer()
	w, h := p.d(wl), p.d(hl)
	ecran, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, ecran)
	dc, _, _ := pCreateCompatDCTst.Call(ecran)
	defer pDeleteDC.Call(dc)
	bmp, _, _ := pCreateCompatibleBmp.Call(ecran, uintptr(w), uintptr(h))
	defer pDeleteObject.Call(bmp)
	old, _, _ := pSelectObject.Call(dc, bmp)
	p.peindre(dc, w, h, ops)
	pGdiFlush.Call()
	pSelectObject.Call(dc, old)
	type entete struct {
		Size                         uint32
		Width, Height                int32
		Planes, BitCount             uint16
		Compression, SizeImage       uint32
		XPels, YPels, Used, Importnt int32
	}
	bi := struct {
		H   entete
		Pal [4]byte
	}{H: entete{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}}
	buf := make([]byte, w*h*4)
	if n, _, _ := pGetDIBits.Call(dc, bmp, 0, uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bi)), 0); int(n) != h {
		t.Fatalf("GetDIBits : %d lignes sur %d", n, h)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		img.Set(i%w, i/w, color.RGBA{buf[i*4+2], buf[i*4+1], buf[i*4], 255})
	}
	return img
}

// Un texte de plusieurs lignes tient en entier dans sa case à chaque échelle de
// Windows : avec DT_EDITCONTROL, une dernière ligne partielle n'est pas dessinée
// (« Lancer au démarrage de W… » à 144 ppp, rc.17).
func TestLignesEntieresAChaqueEchelle(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer i18n.Set(i18n.FR)
	dc, _, _ := pCreateCompatDCTst.Call(0)
	if dc == 0 {
		t.Skip("pas de contexte GDI")
	}
	defer pDeleteDC.Call(dc)
	modeles := []vue.Modele{
		{Pret: true, Code: "allsent", Message: "m", Demarrage: true, Dossier: "D:/WoW", DossierEtat: vue.DossierPret},
		{Pret: true, Code: "notoken", Message: "m", Dossier: "D:/WoW"},
		{Pret: true, Code: "working", Message: "m", Dossier: "D:/WoW"},
		{Pret: true, Code: "body", Message: "m", Dossier: "D:/WoW"},
		{Pret: false},
	}
	for _, l := range i18n.Toutes {
		i18n.Set(l)
		ms := append([]vue.Modele{}, modeles...)
		c, code := vue.Connexion("notoken", "pending", "0E26CF55")
		ms = append(ms, vue.Modele{Pret: true, Code: "notoken", Connection: c, ConnectionCode: code},
			vue.Modele{Pret: true, Code: "token", Connection: i18n.T("connect.revoked", "")})
		for _, dpi := range []int{96, 120, 144, 168, 192} {
			p := nouveauPeintre(dpi)
			for _, m := range ms {
				ops, _, _, _ := vue.Construire(m, vue.Interaction{Survol: -1, Appui: -1, Focus: -1}, vue.Clair())
				for _, o := range ops {
					if o.Genre != vue.OpTexte || !o.Lignes {
						continue
					}
					h := p.police(clePolice{taille: o.Taille, gras: o.Gras, coreen: vue.Coreen(o.Texte)})
					old, _, _ := pSelectObject.Call(dc, h)
					texte := o.Texte
					largeur := p.d(o.R.X+o.R.W) - p.d(o.R.X)
					if vue.Coreen(texte) {
						texte = p.couper(dc, texte, largeur)
					}
					s, _ := windows.UTF16FromString(texte)
					rc := struct{ L, T, R, B int32 }{0, 0, int32(largeur), 0}
					pDrawText.Call(dc, uintptr(unsafe.Pointer(&s[0])), ^uintptr(0), uintptr(unsafe.Pointer(&rc)),
						dtWordBreak|dtNoPrefix|dtEditControl|0x400 /*DT_CALCRECT*/)
					pSelectObject.Call(dc, old)
					if dispo := p.d(o.R.Y+o.R.H) - p.d(o.R.Y); int(rc.B) > dispo {
						t.Errorf("%s %d ppp : %q demande %d px, case de %d px", l, dpi, o.Texte, rc.B, dispo)
					}
				}
			}
			p.liberer()
		}
	}
}
