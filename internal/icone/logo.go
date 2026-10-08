package icone

import (
	"bytes"
	_ "embed"
	"image"
	"image/draw"
	"image/png"
)

// forever.png : le symbole Forever Pulse (infini traversé d'un pouls), détouré du
// logo fourni le 27/09/2026 : le fond noir est devenu transparent (alpha = éclat),
// pour briller sur l'en-tête de la fenêtre. 360 × 214 pixels.
//
//go:embed forever.png
var foreverPNG []byte

// LogoRatio : largeur / hauteur du symbole.
func LogoRatio() float64 {
	c, err := png.DecodeConfig(bytes.NewReader(foreverPNG))
	if err != nil || c.Height == 0 {
		return 1
	}
	return float64(c.Width) / float64(c.Height)
}

// Logo rend le symbole en w×h pixels (réduction par moyenne de surface, en alpha
// prémultiplié : bords et halo sans frange). Décodé à chaque appel : rien ne reste
// en mémoire une fois l'image rendue.
func Logo(w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	src0, err := png.Decode(bytes.NewReader(foreverPNG))
	if err != nil || w <= 0 || h <= 0 {
		return dst
	}
	src := image.NewNRGBA(src0.Bounds())
	draw.Draw(src, src.Bounds(), src0, src0.Bounds().Min, draw.Src)
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	fx, fy := float64(sw)/float64(w), float64(sh)/float64(h)
	for y := 0; y < h; y++ {
		y0, y1 := float64(y)*fy, float64(y+1)*fy
		for x := 0; x < w; x++ {
			x0, x1 := float64(x)*fx, float64(x+1)*fx
			var r, g, b, a, aire float64
			for sy := int(y0); float64(sy) < y1 && sy < sh; sy++ {
				wy := minf(y1, float64(sy+1)) - maxf(y0, float64(sy))
				for sx := int(x0); float64(sx) < x1 && sx < sw; sx++ {
					wx := minf(x1, float64(sx+1)) - maxf(x0, float64(sx))
					p := src.Pix[sy*src.Stride+sx*4:]
					k := wx * wy
					al := float64(p[3]) / 255
					r += float64(p[0]) * al * k
					g += float64(p[1]) * al * k
					b += float64(p[2]) * al * k
					a += al * k
					aire += k
				}
			}
			if a <= 0 || aire <= 0 {
				continue
			}
			o := dst.Pix[y*dst.Stride+x*4:]
			o[0], o[1], o[2] = uint8(r/a+0.5), uint8(g/a+0.5), uint8(b/a+0.5)
			o[3] = uint8(a/aire*255 + 0.5)
		}
	}
	return dst
}

func minf(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
