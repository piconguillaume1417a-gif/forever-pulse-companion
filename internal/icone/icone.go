// Package icone dessine l'icône de Forever Pulse Companion : le symbole officiel
// Forever Pulse (infini traversé d'un pouls, forever.png) posé sur une tuile
// arrondie bleu nuit → prune, comme l'en-tête de la fenêtre, avec une pastille
// d'état optionnelle (vert : tout est envoyé, orange : en attente, rouge : action
// nécessaire).
//
// Pur Go : utilisé à l'exécution pour l'icône près de l'horloge et celle des
// notifications, et par outils/mkres pour l'icône de l'exécutable (barre des
// tâches, Explorateur, fenêtre).
//
// 0.6.1 : remplace l'ancien disque bleu à anneau doré dessiné à la main.
package icone

import (
	"image"
	"image/color"
	"math"
)

const (
	SansPastille = -1
	Vert         = 0
	Orange       = 1
	Rouge        = 2
)

var pastilles = [3][3]float64{{46, 160, 67}, {240, 140, 0}, {211, 47, 47}}

// Couleurs de la tuile : celles de l'en-tête (vue.Nuit 0x131A45 → vue.Prune
// 0x33256E), assombries pour que le symbole ressorte comme sur le logo d'origine
// (fond noir).
var (
	tuileHaut = [3]float64{10, 14, 40}
	tuileBas  = [3]float64{36, 22, 72}
	tuileBord = [3]float64{90, 120, 230}
)

// dansTuile : couverture (0 ou 1) d'un point par le carré arrondi de côté n.
func dansTuile(px, py, n, rayon float64) bool {
	cx := math.Max(rayon, math.Min(n-rayon, px))
	cy := math.Max(rayon, math.Min(n-rayon, py))
	return math.Hypot(px-cx, py-cy) <= rayon
}

// distBord : distance d'un point intérieur au bord du carré arrondi.
func distBord(px, py, n, rayon float64) float64 {
	cx := math.Max(rayon, math.Min(n-rayon, px))
	cy := math.Max(rayon, math.Min(n-rayon, py))
	if cx == px && cy == py {
		return math.Min(math.Min(px, n-px), math.Min(py, n-py))
	}
	return rayon - math.Hypot(px-cx, py-cy)
}

// Dessine rend l'icône en n×n pixels (tuile suréchantillonnée 4×4, symbole réduit
// par moyenne de surface).
func Dessine(n int, statut int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, n, n))
	const ss = 4
	fn := float64(n)
	rayon := fn * 0.22
	bord := math.Max(1, fn/64) // liseré bleu discret : la tuile se détache d'une barre sombre

	// 1. La tuile.
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			var R, G, B, A float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					px, py := float64(x)+(float64(sx)+0.5)/ss, float64(y)+(float64(sy)+0.5)/ss
					if !dansTuile(px, py, fn, rayon) {
						continue
					}
					t := py / fn
					c := [3]float64{}
					for i := range c {
						c[i] = tuileHaut[i] + (tuileBas[i]-tuileHaut[i])*t
					}
					if distBord(px, py, fn, rayon) < bord {
						for i := range c {
							c[i] = c[i]*0.55 + tuileBord[i]*0.45
						}
					}
					R, G, B, A = R+c[0], G+c[1], B+c[2], A+1
				}
			}
			if A > 0 {
				img.SetNRGBA(x, y, color.NRGBA{uint8(R/A + 0.5), uint8(G/A + 0.5), uint8(B/A + 0.5), uint8(255*A/(ss*ss) + 0.5)})
			}
		}
	}

	// 2. Le symbole, centré. Aux petites tailles il occupe toute la largeur utile
	// et son éclat est renforcé, sinon le halo se fond dans la tuile.
	largeur := 0.86
	gain := 1.0
	switch {
	case n <= 16:
		largeur, gain = 0.98, 2.2
	case n <= 24:
		largeur, gain = 0.94, 1.8
	case n <= 48:
		largeur, gain = 0.90, 1.35
	}
	lw := int(math.Round(fn * largeur))
	lh := int(math.Round(float64(lw) / LogoRatio()))
	if lw < 1 || lh < 1 {
		return img
	}
	sym := Logo(lw, lh)
	ox, oy := (n-lw)/2, (n-lh)/2
	for y := 0; y < lh; y++ {
		for x := 0; x < lw; x++ {
			s := sym.NRGBAAt(x, y)
			if s.A == 0 {
				continue
			}
			a := math.Min(1, float64(s.A)/255*gain)
			d := img.NRGBAAt(ox+x, oy+y)
			if d.A == 0 {
				continue // hors de la tuile (coins arrondis)
			}
			mix := func(sc, dc uint8) uint8 { return uint8(float64(sc)*a + float64(dc)*(1-a) + 0.5) }
			img.SetNRGBA(ox+x, oy+y, color.NRGBA{mix(s.R, d.R), mix(s.G, d.G), mix(s.B, d.B), d.A})
		}
	}

	// 3. La pastille d'état, en bas à droite, cerclée de blanc.
	if statut >= 0 && statut < len(pastilles) {
		pr := math.Max(fn*0.19, 3.3) // au moins ~6,5 px de diamètre : ronde même en 16 px
		pcx, pcy := fn-pr-0.5, fn-pr-0.5
		liser := math.Max(1, fn*0.04)
		for y := int(pcy - pr - 1); y <= int(pcy+pr+1) && y < n; y++ {
			for x := int(pcx - pr - 1); x <= int(pcx+pr+1) && x < n; x++ {
				if x < 0 || y < 0 {
					continue
				}
				var R, G, B, A float64
				for sy := 0; sy < ss; sy++ {
					for sx := 0; sx < ss; sx++ {
						px, py := float64(x)+(float64(sx)+0.5)/ss, float64(y)+(float64(sy)+0.5)/ss
						d := math.Hypot(px-pcx, py-pcy)
						if d > pr {
							continue
						}
						c := pastilles[statut]
						if d > pr-liser {
							c = [3]float64{255, 255, 255}
						}
						R, G, B, A = R+c[0], G+c[1], B+c[2], A+1
					}
				}
				if A == 0 {
					continue
				}
				a := A / (ss * ss)
				d := img.NRGBAAt(x, y)
				da := float64(d.A) / 255
				oa := a + da*(1-a)
				mix := func(sc float64, dc uint8) uint8 {
					return uint8((sc/A*a+float64(dc)*da*(1-a))/oa + 0.5)
				}
				img.SetNRGBA(x, y, color.NRGBA{mix(R, d.R), mix(G, d.G), mix(B, d.B), uint8(oa*255 + 0.5)})
			}
		}
	}
	return img
}
