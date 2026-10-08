// apercu-icone : planche de l'icône (16 à 256 px, avec et sans pastille) sur fond
// clair et sombre, en taille réelle et agrandie ×8 pour les petites tailles.
//
//	go run ./outils/apercu-icone planche.png
package main

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"

	"wowsync/internal/icone"
)

func main() {
	out := "apercu-icone.png"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	fonds := []color.NRGBA{{243, 243, 243, 255}, {32, 32, 32, 255}}
	W, H := 1400, 2*520
	img := image.NewNRGBA(image.Rect(0, 0, W, H))
	for i, f := range fonds {
		draw.Draw(img, image.Rect(0, i*520, W, (i+1)*520), &image.Uniform{f}, image.Point{}, draw.Src)
		y0 := i*520 + 20
		// Taille réelle.
		x := 20
		for _, n := range []int{16, 20, 24, 32, 40, 48, 64, 256} {
			draw.Draw(img, image.Rect(x, y0, x+n, y0+n), icone.Dessine(n, icone.SansPastille), image.Point{}, draw.Over)
			x += n + 16
		}
		// Pastilles, taille réelle 16/24/32.
		x += 10
		for _, n := range []int{16, 24, 32} {
			for s := icone.Vert; s <= icone.Rouge; s++ {
				draw.Draw(img, image.Rect(x, y0, x+n, y0+n), icone.Dessine(n, s), image.Point{}, draw.Over)
				x += n + 8
			}
		}
		// Agrandi ×8 : 16, 24, 32 avec pastille orange.
		x, y := 20, y0+280
		for _, c := range []struct{ n, s int }{{16, icone.SansPastille}, {16, icone.Orange}, {24, icone.SansPastille}, {32, icone.Vert}} {
			src := icone.Dessine(c.n, c.s)
			k := 7
			if c.n == 32 {
				k = 6
			}
			r := image.Rect(x, y, x+c.n*k, y+c.n*k)
			for yy := 0; yy < c.n*k; yy++ {
				for xx := 0; xx < c.n*k; xx++ {
					p := src.NRGBAAt(xx/k, yy/k)
					img.Set(r.Min.X+xx, r.Min.Y+yy, over(p, img.NRGBAAt(r.Min.X+xx, r.Min.Y+yy)))
				}
			}
			x += c.n*k + 20
		}
	}
	f, _ := os.Create(out)
	defer f.Close()
	_ = png.Encode(f, img)
}

func over(s, d color.NRGBA) color.NRGBA {
	a := float64(s.A) / 255
	m := func(x, y uint8) uint8 { return uint8(float64(x)*a + float64(y)*(1-a)) }
	return color.NRGBA{m(s.R, d.R), m(s.G, d.G), m(s.B, d.B), 255}
}
