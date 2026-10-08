// Package vue : la mise en page de la fenêtre de Forever Pulse Companion, sans
// dépendance à Windows.
//
// Construire transforme l'état affiché en une liste d'opérations de dessin
// (rectangles arrondis, dégradés, textes, glyphes) et en zones cliquables. Le
// peintre Windows (internal/winui) les exécute avec GDI+ et GDI ; l'outil
// outils/apercu les exécute hors Windows pour produire des aperçus PNG.
package vue

// Couleur : 0xRRGGBB.
type Couleur uint32

func (c Couleur) RGB() (r, g, b uint8) { return uint8(c >> 16), uint8(c >> 8), uint8(c) }

// Melange : a vers b, t entre 0 et 1.
func Melange(a, b Couleur, t float64) Couleur {
	ar, ag, ab := a.RGB()
	br, bg, bb := b.RGB()
	m := func(x, y uint8) Couleur { return Couleur(float64(x) + (float64(y)-float64(x))*t + 0.5) }
	return m(ar, br)<<16 | m(ag, bg)<<8 | m(ab, bb)
}

// Theme : la palette. Les couleurs de marque (bleu nuit → violet, or) viennent de
// l'icône ; le reste suit le thème clair ou sombre de Windows.
type Theme struct {
	Sombre bool

	Fond, Carte, Bord, Separateur     Couleur
	Texte, Doux, Pale                 Couleur
	Accent, AccentSurvol, AccentAppui Couleur
	SurAccent                         Couleur
	Bouton, BoutonSurvol, BoutonAppui Couleur
	Piste                             Couleur // interrupteur éteint, fond du choix de langue
	Pied                              Couleur // bande du bas
	Succes, Alerte, Danger            Couleur

	EnteteHaut, EnteteBas   Couleur
	EnteteTexte, EnteteDoux Couleur
	Or                      Couleur
}

// Couleurs de la barre de titre (Windows 11) : prolongent l'en-tête.
const (
	Nuit  Couleur = 0x131A45
	Prune Couleur = 0x33256E
)

func Clair() Theme {
	return Theme{
		Fond: 0xF3F4F9, Carte: 0xFFFFFF, Bord: 0xE1E4EE, Separateur: 0xEEF0F5,
		Texte: 0x1A1D2B, Doux: 0x585E73, Pale: 0x8A90A4,
		Accent: 0x5646D6, AccentSurvol: 0x4A3BC4, AccentAppui: 0x3F31AE, SurAccent: 0xFFFFFF,
		Bouton: 0xFFFFFF, BoutonSurvol: 0xF0F1F7, BoutonAppui: 0xE5E7F0,
		Piste: 0xE6E8F0, Pied: 0xEBEDF4,
		Succes: 0x23964A, Alerte: 0xD97A00, Danger: 0xCC3333,
		EnteteHaut: Nuit, EnteteBas: Prune, EnteteTexte: 0xFFFFFF, EnteteDoux: 0xC5C8E8,
		Or: 0xDEB252,
	}
}

func Sombre() Theme {
	return Theme{
		Sombre: true,
		Fond:   0x15161C, Carte: 0x1F2029, Bord: 0x2E3040, Separateur: 0x292B37,
		Texte: 0xECEDF4, Doux: 0xA7ABBE, Pale: 0x797E93,
		Accent: 0x7466F0, AccentSurvol: 0x8578F5, AccentAppui: 0x6556E0, SurAccent: 0xFFFFFF,
		Bouton: 0x272936, BoutonSurvol: 0x303345, BoutonAppui: 0x383B50,
		Piste: 0x343748, Pied: 0x1A1B22,
		Succes: 0x3CC06A, Alerte: 0xF2A23A, Danger: 0xF06464,
		EnteteHaut: Nuit, EnteteBas: Prune, EnteteTexte: 0xFFFFFF, EnteteDoux: 0xC5C8E8,
		Or: 0xDEB252,
	}
}

// Etat : vert, orange, rouge (même ordre que app.Couleur et l'icône).
func (t Theme) Etat(c int) Couleur {
	switch c {
	case 1:
		return t.Alerte
	case 2:
		return t.Danger
	}
	return t.Succes
}
