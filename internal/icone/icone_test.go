package icone

import "testing"

func TestDessine(t *testing.T) {
	for _, n := range []int{16, 32, 256} {
		for _, s := range []int{SansPastille, Vert, Orange, Rouge} {
			img := Dessine(n, s)
			if img.Bounds().Dx() != n || img.NRGBAAt(0, 0).A != 0 || img.NRGBAAt(n/2, n/3).A != 255 {
				t.Fatalf("icône %d/%d mal formée", n, s)
			}
		}
	}
	// La pastille rouge colore bien le coin bas droit.
	p := Dessine(32, Rouge).NRGBAAt(26, 26)
	if p.R < 150 || p.G > 100 {
		t.Fatalf("pastille : %v", p)
	}
}

func TestLogoForever(t *testing.T) {
	r := LogoRatio()
	if r < 1.5 || r > 1.9 {
		t.Fatalf("rapport %v", r)
	}
	img := Logo(74, 44)
	if img.Bounds().Dx() != 74 || img.Bounds().Dy() != 44 {
		t.Fatal(img.Bounds())
	}
	// Le pouls traverse le centre : pixel lumineux et opaque ; les coins sont transparents.
	vus := 0
	for x := 30; x < 44; x++ {
		for y := 5; y < 40; y++ {
			if p := img.NRGBAAt(x, y); p.A > 200 && p.B > 200 {
				vus++
			}
		}
	}
	if vus == 0 || img.NRGBAAt(0, 0).A > 40 || img.NRGBAAt(73, 43).A > 40 {
		t.Fatalf("rendu inattendu : %d pixels du pouls, coins %v %v", vus, img.NRGBAAt(0, 0), img.NRGBAAt(73, 43))
	}
}

// 0.6.1 : l'icône porte le symbole officiel (pouls lumineux au centre) sur une
// tuile sombre aux coins arrondis transparents.
func TestIconeSymbole(t *testing.T) {
	for _, n := range []int{16, 32, 256} {
		img := Dessine(n, SansPastille)
		c := img.NRGBAAt(n/2, n/2)
		if c.A != 255 || c.B < 150 {
			t.Fatalf("%d px : centre %v, le pouls devrait y briller", n, c)
		}
		if f := img.NRGBAAt(n/2, n/8); f.A != 255 || f.B > 120 || f.R > 60 {
			t.Fatalf("%d px : tuile %v, attendue sombre", n, f)
		}
		if img.NRGBAAt(0, 0).A != 0 || img.NRGBAAt(n-1, 0).A != 0 {
			t.Fatalf("%d px : coins non transparents", n)
		}
	}
}
