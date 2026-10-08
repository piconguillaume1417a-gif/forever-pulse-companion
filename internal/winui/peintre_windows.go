//go:build windows

package winui

import (
	"math"
	"unsafe"

	"golang.org/x/sys/windows"

	"wowsync/internal/icone"
	"wowsync/internal/vue"
)

// Le peintre exécute les opérations de internal/vue : formes lissées avec GDI+
// (gdiplus.dll, présent sur tout Windows), textes avec GDI et ClearType. Il
// n'existe que tant que la fenêtre est ouverte ; fermée, elle ne coûte rien.

var (
	gdiplus = windows.NewLazySystemDLL("gdiplus.dll")

	pGdiplusStartup     = gdiplus.NewProc("GdiplusStartup")
	pGdiplusShutdown    = gdiplus.NewProc("GdiplusShutdown")
	pGdipCreateFromHDC  = gdiplus.NewProc("GdipCreateFromHDC")
	pGdipDeleteGraphics = gdiplus.NewProc("GdipDeleteGraphics")
	pGdipSetSmoothing   = gdiplus.NewProc("GdipSetSmoothingMode")
	pGdipSetPixelOffset = gdiplus.NewProc("GdipSetPixelOffsetMode")
	pGdipCreateSolid    = gdiplus.NewProc("GdipCreateSolidFill")
	pGdipCreateLineBrI  = gdiplus.NewProc("GdipCreateLineBrushI")
	pGdipDeleteBrush    = gdiplus.NewProc("GdipDeleteBrush")
	pGdipCreatePen1     = gdiplus.NewProc("GdipCreatePen1")
	pGdipDeletePen      = gdiplus.NewProc("GdipDeletePen")
	pGdipCreatePath     = gdiplus.NewProc("GdipCreatePath")
	pGdipDeletePath     = gdiplus.NewProc("GdipDeletePath")
	pGdipAddPathArc     = gdiplus.NewProc("GdipAddPathArc")
	pGdipAddPathRect    = gdiplus.NewProc("GdipAddPathRectangle")
	pGdipClosePath      = gdiplus.NewProc("GdipClosePathFigure")
	pGdipFillPath       = gdiplus.NewProc("GdipFillPath")
	pGdipDrawPath       = gdiplus.NewProc("GdipDrawPath")
	pGdipFillEllipse    = gdiplus.NewProc("GdipFillEllipse")
	pGdipFillRectI      = gdiplus.NewProc("GdipFillRectangleI")

	pCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	pCreateCompatibleBmp = gdi32.NewProc("CreateCompatibleBitmap")
	pSelectObject        = gdi32.NewProc("SelectObject")
	pDeleteDC            = gdi32.NewProc("DeleteDC")
	pBitBlt              = gdi32.NewProc("BitBlt")
	pSetBkMode           = gdi32.NewProc("SetBkMode")
	pDrawText            = user32.NewProc("DrawTextW")
	pDrawIconEx          = user32.NewProc("DrawIconEx")
	pDestroyIcon         = user32.NewProc("DestroyIcon")
)

// f32 : un REAL de GDI+ passé dans un argument entier. Le runtime Go recopie les
// quatre premiers arguments dans XMM0-3 (convention x64 de Windows) ; au-delà, le
// REAL est lu dans les 4 premiers octets de la case de pile.
func f32(v float64) uintptr { return uintptr(math.Float32bits(float32(v))) }

func argb(c vue.Couleur) uintptr { return uintptr(0xFF000000 | uint32(c)) }

// bgr : COLORREF de GDI.
func bgr(c vue.Couleur) uintptr {
	r, g, b := c.RGB()
	return uintptr(uint32(b)<<16 | uint32(g)<<8 | uint32(r))
}

type clePolice struct {
	taille            int
	gras, sou, glyphe bool
}

type peintre struct {
	dpi     int
	jeton   uintptr // GDI+
	polices map[clePolice]uintptr
	logo    uintptr
	logoPx  int
}

func nouveauPeintre(dpi int) *peintre {
	p := &peintre{dpi: dpi, polices: map[clePolice]uintptr{}}
	in := struct {
		Version                  uint32
		Callback                 uintptr
		SansFil, SansCodecsExtra int32
	}{Version: 1, SansFil: 0}
	if pGdiplusStartup.Find() == nil {
		if r, _, _ := pGdiplusStartup.Call(uintptr(unsafe.Pointer(&p.jeton)), uintptr(unsafe.Pointer(&in)), 0); r != 0 {
			p.jeton = 0
		}
	}
	return p
}

func (p *peintre) liberer() {
	for _, h := range p.polices {
		pDeleteObject.Call(h)
	}
	p.polices = map[clePolice]uintptr{}
	if p.logo != 0 {
		pDestroyIcon.Call(p.logo)
		p.logo = 0
	}
	if p.jeton != 0 {
		pGdiplusShutdown.Call(p.jeton)
		p.jeton = 0
	}
}

// d : pixels logiques → pixels de l'écran.
func (p *peintre) d(v int) int     { return (v*p.dpi + 48) / 96 }
func (p *peintre) f(v int) float64 { return float64(v) * float64(p.dpi) / 96 }

func (p *peintre) police(k clePolice) uintptr {
	if h, ok := p.polices[k]; ok {
		return h
	}
	face, poids := "Segoe UI", 400
	if k.gras {
		face, poids = "Segoe UI Semibold", 600
	}
	if k.glyphe {
		face, poids = "Segoe MDL2 Assets", 400
	}
	sou := uintptr(0)
	if k.sou {
		sou = 1
	}
	h, _, _ := pCreateFont.Call(uintptr(int32(-p.d(k.taille)))&0xffffffff, 0, 0, 0, uintptr(poids), 0, sou, 0,
		1 /*DEFAULT_CHARSET*/, 0, 0, 5 /*CLEARTYPE_QUALITY*/, 0, str(face))
	p.polices[k] = h
	return h
}

// peindre dessine toute la zone cliente (w×h pixels) dans hdc, via une image en
// mémoire : pas de scintillement.
func (p *peintre) peindre(hdc uintptr, w, h int, ops []vue.Op) {
	mem, _, _ := pCreateCompatibleDC.Call(hdc)
	bmp, _, _ := pCreateCompatibleBmp.Call(hdc, uintptr(w), uintptr(h))
	old, _, _ := pSelectObject.Call(mem, bmp)
	pSetBkMode.Call(mem, 1 /*TRANSPARENT*/)
	var g uintptr
	graphics := func() uintptr {
		if g == 0 && p.jeton != 0 {
			pGdipCreateFromHDC.Call(mem, uintptr(unsafe.Pointer(&g)))
			if g != 0 {
				pGdipSetSmoothing.Call(g, 4 /*AntiAlias*/)
				pGdipSetPixelOffset.Call(g, 4 /*Half*/)
			}
		}
		return g
	}
	// GDI et GDI+ alternent sur la même image : GDI+ est refermé avant chaque texte.
	fermer := func() {
		if g != 0 {
			pGdipDeleteGraphics.Call(g)
			g = 0
		}
	}
	for _, o := range ops {
		switch o.Genre {
		case vue.OpRect:
			p.rect(graphics(), o)
		case vue.OpDegrade:
			if gr := graphics(); gr != 0 {
				p1 := struct{ X, Y int32 }{0, int32(p.d(o.R.Y)) - 1}
				p2 := struct{ X, Y int32 }{0, int32(p.d(o.R.Y+o.R.H)) + 1}
				var br uintptr
				pGdipCreateLineBrI.Call(uintptr(unsafe.Pointer(&p1)), uintptr(unsafe.Pointer(&p2)), argb(o.Couleur), argb(o.Couleur2),
					3 /*TileFlipXY*/, uintptr(unsafe.Pointer(&br)))
				if br != 0 {
					pGdipFillRectI.Call(gr, br, uintptr(p.d(o.R.X)), uintptr(p.d(o.R.Y)), uintptr(p.d(o.R.W)), uintptr(p.d(o.R.H)))
					pGdipDeleteBrush.Call(br)
				}
			}
		case vue.OpRond:
			if gr := graphics(); gr != 0 {
				var br uintptr
				pGdipCreateSolid.Call(argb(o.Couleur), uintptr(unsafe.Pointer(&br)))
				pGdipFillEllipse.Call(gr, br, f32(p.f(o.R.X)), f32(p.f(o.R.Y)), f32(p.f(o.R.W)), f32(p.f(o.R.H)))
				pGdipDeleteBrush.Call(br)
			}
		case vue.OpTexte, vue.OpGlyphe:
			fermer()
			p.texte(mem, o)
		case vue.OpLogo:
			fermer()
			lw, lh := p.d(o.R.W), p.d(o.R.H)
			if p.logo == 0 || p.logoPx != lw {
				if p.logo != 0 {
					pDestroyIcon.Call(p.logo)
				}
				p.logo, p.logoPx = iconFromImage(icone.Logo(lw, lh)), lw
			}
			pDrawIconEx.Call(mem, uintptr(p.d(o.R.X)), uintptr(p.d(o.R.Y)), p.logo, uintptr(lw), uintptr(lh), 0, 0, 3 /*DI_NORMAL*/)
		}
	}
	fermer()
	pBitBlt.Call(hdc, 0, 0, uintptr(w), uintptr(h), mem, 0, 0, 0x00CC0020 /*SRCCOPY*/)
	pSelectObject.Call(mem, old)
	pDeleteObject.Call(bmp)
	pDeleteDC.Call(mem)
}

// rect : rectangle arrondi plein, avec bordure éventuelle (tracée à l'intérieur).
func (p *peintre) rect(g uintptr, o vue.Op) {
	if g == 0 {
		return
	}
	x, y, w, h, r := p.f(o.R.X), p.f(o.R.Y), p.f(o.R.W), p.f(o.R.H), p.f(o.Rayon)
	if !o.Vide {
		path := chemin(x, y, w, h, r)
		var br uintptr
		pGdipCreateSolid.Call(argb(o.Couleur), uintptr(unsafe.Pointer(&br)))
		pGdipFillPath.Call(g, br, path)
		pGdipDeleteBrush.Call(br)
		pGdipDeletePath.Call(path)
	}
	if o.Epais > 0 {
		e := math.Max(1, math.Round(p.f(o.Epais)))
		path := chemin(x+e/2, y+e/2, w-e, h-e, math.Max(0, r-e/2))
		var pen uintptr
		pGdipCreatePen1.Call(argb(o.Bord), f32(e), 2 /*UnitPixel*/, uintptr(unsafe.Pointer(&pen)))
		if pen != 0 {
			pGdipDrawPath.Call(g, pen, path)
			pGdipDeletePen.Call(pen)
		}
		pGdipDeletePath.Call(path)
	}
}

func chemin(x, y, w, h, r float64) uintptr {
	var path uintptr
	pGdipCreatePath.Call(0, uintptr(unsafe.Pointer(&path)))
	if r <= 0.5 {
		pGdipAddPathRect.Call(path, f32(x), f32(y), f32(w), f32(h))
		return path
	}
	d := math.Min(2*r, math.Min(w, h))
	pGdipAddPathArc.Call(path, f32(x), f32(y), f32(d), f32(d), f32(180), f32(90))
	pGdipAddPathArc.Call(path, f32(x+w-d), f32(y), f32(d), f32(d), f32(270), f32(90))
	pGdipAddPathArc.Call(path, f32(x+w-d), f32(y+h-d), f32(d), f32(d), f32(0), f32(90))
	pGdipAddPathArc.Call(path, f32(x), f32(y+h-d), f32(d), f32(d), f32(90), f32(90))
	pGdipClosePath.Call(path)
	return path
}

const (
	dtCenter      = 0x1
	dtRight       = 0x2
	dtVCenter     = 0x4
	dtWordBreak   = 0x10
	dtSingleLine  = 0x20
	dtNoPrefix    = 0x800
	dtEditControl = 0x2000
	dtEndEllipsis = 0x8000
)

func (p *peintre) texte(hdc uintptr, o vue.Op) {
	h := p.police(clePolice{taille: o.Taille, gras: o.Gras, sou: o.Souligne, glyphe: o.Genre == vue.OpGlyphe})
	old, _, _ := pSelectObject.Call(hdc, h)
	pSetTextColor.Call(hdc, bgr(o.Couleur))
	rc := struct{ L, T, R, B int32 }{int32(p.d(o.R.X)), int32(p.d(o.R.Y)), int32(p.d(o.R.X + o.R.W)), int32(p.d(o.R.Y + o.R.H))}
	flags := uintptr(dtNoPrefix | dtEndEllipsis)
	if o.Lignes {
		flags |= dtWordBreak | dtEditControl
	} else {
		flags |= dtSingleLine | dtVCenter
	}
	switch o.Aligne {
	case vue.Centre:
		flags |= dtCenter
	case vue.Droite:
		flags |= dtRight
	}
	s, _ := windows.UTF16FromString(o.Texte)
	pDrawText.Call(hdc, uintptr(unsafe.Pointer(&s[0])), ^uintptr(0), uintptr(unsafe.Pointer(&rc)), flags)
	pSelectObject.Call(hdc, old)
}
