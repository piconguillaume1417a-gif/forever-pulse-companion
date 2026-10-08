package vue

import (
	"strconv"
	"strings"

	"wowsync/internal/i18n"
)

// Largeur de la fenêtre (zone cliente), en pixels logiques (96 ppp).
const Largeur = 440

// Identifiants des zones cliquables. Les commandes gardent les valeurs de la 0.5.0.
const (
	Envoyer = 201 + iota
	Coller
	Journal
	Page
	Demarrage
	Effacer
	Desinstaller
	Reduire
	Quitter
	Langue // ancienne liste ; gardé pour la numérotation
	LangueFR
	LangueEN
	Autres  // ligne « + N autres périmètres » (infobulle seule)
	Details // 0.7.0 : ligne « Dernier envoi » : détail de l'état (infobulle seule)
	PrixDetails
)

// Genre d'opération de dessin.
type Genre int

const (
	OpRect    Genre = iota // rectangle arrondi plein, bordure optionnelle
	OpDegrade              // dégradé vertical Couleur → Couleur2
	OpRond                 // disque
	OpTexte                // texte (une ligne avec « … », ou plusieurs lignes)
	OpGlyphe               // pictogramme de la police Segoe MDL2 Assets
	OpLogo                 // le symbole Forever Pulse (infini et pouls)
)

type Aligne int

const (
	Gauche Aligne = iota
	Centre
	Droite
)

type Rect struct{ X, Y, W, H int }

func (r Rect) Contient(x, y int) bool { return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H }

// Agrandi : r élargi de d pixels de chaque côté.
func (r Rect) Agrandi(d int) Rect { return Rect{r.X - d, r.Y - d, r.W + 2*d, r.H + 2*d} }

type Op struct {
	Genre    Genre
	R        Rect
	Rayon    int
	Couleur  Couleur // remplissage, texte ou glyphe
	Couleur2 Couleur // bas du dégradé
	Bord     Couleur // OpRect : couleur de bordure si Epais > 0
	Epais    int     // OpRect : épaisseur de bordure ; OpRect sans remplissage si Vide
	Vide     bool    // OpRect : bordure seule
	Texte    string  // OpTexte ; OpGlyphe : un caractère
	Taille   int     // taille du texte en pixels logiques
	Gras     bool
	Aligne   Aligne
	Lignes   bool // plusieurs lignes (retour à la ligne automatique)
	Souligne bool
}

// Zone : une surface cliquable, dans l'ordre de passage de la touche Tab.
type Zone struct {
	ID      int
	R       Rect
	Bulle   string // infobulle
	Inactif bool
	Info    bool // infobulle seule : ni clic ni focus
}

// Portee : une ligne du cumul.
type Portee struct {
	Nom    string
	Total  int
	Depuis string
}

// Modele : ce que montre la fenêtre.
type Modele struct {
	Pret         bool // faux tant que le moteur n'a pas donné son premier état
	Couleur      int  // 0 vert, 1 orange, 2 rouge
	Code         string
	Message      string
	Connection   string
	Envoyes      int
	Attente      int
	Refuses      int
	DernierEnvoi string
	Portees      []Portee
	Demarrage    bool
	Version      string
	// 0.7.0 : fichier suivi, dernière lecture, derniers envois confirmés, données
	// en attente (lots et statistiques), dernière erreur. Une ligne par information.
	Details                                      []string
	AuctionSent, AuctionPending, AuctionUnproven int
	Auctions                                     bool
}

// Interaction : survol, appui, focus clavier, commandes en cours.
type Interaction struct {
	Survol, Appui, Focus int
	FocusVisible         bool
	Occupe               map[int]bool
}

// JetonManquant : l'action utile est de coller un jeton, pas d'envoyer.
func (m Modele) JetonManquant() bool { return m.Code == "notoken" || m.Code == "token" }

// MaxPortees : lignes du cumul affichées ; au-delà, une ligne « + N autres ».
const MaxPortees = 4

// Nombre : 51 767 (fr) ou 51,767 (en).
func Nombre(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	sep := " "
	if i18n.Get() == i18n.EN {
		sep = ","
	}
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteString(sep)
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// Hauteur de la zone cliente pour un nombre de périmètres donné.
func Hauteur(nPortees int) int {
	_, _, h := mesures(nPortees)
	return h
}

func HauteurModele(m Modele) int {
	h := Hauteur(len(m.Portees))
	if m.Connection != "" {
		h += 42
	}
	return h
}

func lignesCumul(n int) int {
	if n < 1 {
		return 1
	}
	if n > MaxPortees {
		return MaxPortees
	}
	return n
}

// mesures : haut de la carte du cumul, sa hauteur, hauteur totale.
func mesures(nPortees int) (yCumul, hCumul, total int) {
	yCumul = 330
	hCumul = 38 + lignesCumul(nPortees)*28 + 8
	y := yCumul + hCumul + 14 // boutons
	y += 40 + 8 + 34 + 14     // deux rangées de boutons
	y += 52 + 14              // réglages
	return yCumul, hCumul, y + 50
}

// Glyphes (Segoe MDL2 Assets, présente depuis Windows 10).
const (
	GlypheEnvoyer = ""
	GlypheColler  = ""
	GlypheLien    = ""
	GlypheJournal = ""
	GlypheHorloge = ""
	GlypheGlobe   = ""
	GlypheOK      = ""
	GlypheAttente = ""
	GlypheAlerte  = ""
)

// Construire : les opérations de dessin et les zones cliquables, en pixels logiques.
func Construire(m Modele, in Interaction, th Theme) (ops []Op, zones []Zone, largeur, hauteur int) {
	W := Largeur
	yCumul, hCumul, H := mesures(len(m.Portees))
	if m.Connection != "" {
		yCumul += 42
		H += 42
	}
	add := func(o Op) { ops = append(ops, o) }
	rect := func(r Rect, rayon int, fond Couleur) { add(Op{Genre: OpRect, R: r, Rayon: rayon, Couleur: fond}) }
	carte := func(r Rect) {
		add(Op{Genre: OpRect, R: r, Rayon: 10, Couleur: th.Carte, Bord: th.Bord, Epais: 1})
	}
	texte := func(r Rect, s string, taille int, gras bool, c Couleur, a Aligne) {
		add(Op{Genre: OpTexte, R: r, Texte: s, Taille: taille, Gras: gras, Couleur: c, Aligne: a})
	}
	glyphe := func(r Rect, g string, taille int, c Couleur) {
		add(Op{Genre: OpGlyphe, R: r, Texte: g, Taille: taille, Couleur: c, Aligne: Centre})
	}
	etat := func(id int) (survol, appui, occupe bool) {
		return in.Survol == id, in.Appui == id && in.Survol == id, in.Occupe[id]
	}

	// Fond
	rect(Rect{0, 0, W, H}, 0, th.Fond)

	// En-tête : dégradé de la marque, logo, nom, sous-titre, version.
	add(Op{Genre: OpDegrade, R: Rect{0, 0, W, 80}, Couleur: th.EnteteHaut, Couleur2: th.EnteteBas})
	// Symbole du logo Forever Pulse (rapport ≈ 1,68), fond transparent.
	add(Op{Genre: OpLogo, R: Rect{12, 17, 76, 45}})
	titleSize, titleWidth := 20, 248
	if len(m.Version) > 6 {
		titleSize, titleWidth = 18, W-96-40-7*(len(m.Version)+1)
	}
	texte(Rect{96, 16, titleWidth, 28}, "Forever Pulse Companion", titleSize, true, th.EnteteTexte, Gauche)
	texte(Rect{96, 44, W - 96 - 16, 18}, i18n.T("win.subtitle"), 12, false, th.EnteteDoux, Gauche)
	if m.Version != "" {
		v := "v" + m.Version
		pw := 16 + 7*len(v)
		rect(Rect{W - 16 - pw, 20, pw, 20}, 10, Melange(th.EnteteHaut, 0xFFFFFF, 0.14))
		texte(Rect{W - 16 - pw, 20, pw, 20}, v, 11, false, th.EnteteDoux, Centre)
	}

	// Carte d'état : pastille, message, conseil.
	code, msg, coul := m.Code, m.Message, m.Couleur
	if !m.Pret {
		code, msg, coul = "starting", i18n.T("win.starting"), 1
	}
	ce := th.Etat(coul)
	carte(Rect{16, 96, W - 32, 74})
	add(Op{Genre: OpRond, R: Rect{30, 113, 40, 40}, Couleur: Melange(th.Carte, ce, 0.16)})
	g := GlypheOK
	switch {
	case !m.Pret || coul == 1:
		g = GlypheAttente
	case coul == 2:
		g = GlypheAlerte
	}
	glyphe(Rect{30, 113, 40, 40}, g, 18, ce)
	texte(Rect{84, 106, W - 84 - 28, 24}, msg, 15, true, th.Texte, Gauche)
	add(Op{Genre: OpTexte, R: Rect{84, 130, W - 84 - 28, 34}, Texte: i18n.T("hint." + code), Taille: 12,
		Couleur: th.Doux, Lignes: true})

	// Trois compteurs.
	tuiles := []struct {
		n    int
		cle  string
		coul Couleur
	}{
		{m.Envoyes, "tile.sent", th.Texte},
		{m.Attente, "tile.pending", th.Alerte},
		{m.Refuses, "tile.rejected", th.Danger},
	}
	for i, t := range tuiles {
		r := Rect{16 + i*(128+12), 182, 128, 66}
		carte(r)
		c := t.coul
		if t.n == 0 {
			c = th.Texte
		}
		if !m.Pret {
			texte(Rect{r.X + 14, r.Y + 9, r.W - 28, 30}, "–", 24, true, th.Pale, Gauche)
		} else {
			texte(Rect{r.X + 14, r.Y + 9, r.W - 28, 30}, Nombre(t.n), 24, true, c, Gauche)
		}
		texte(Rect{r.X + 14, r.Y + 40, r.W - 28, 16}, i18n.T(t.cle), 12, false, th.Doux, Gauche)
	}

	// Dernier envoi.
	envoi := m.DernierEnvoi
	if envoi == "" {
		envoi = i18n.T("state.nosend")
	}
	glyphe(Rect{16, 257, 18, 20}, GlypheHorloge, 12, th.Pale)
	texte(Rect{38, 257, W - 38 - 16, 20}, envoi, 12, false, th.Doux, Gauche)
	if len(m.Details) > 0 {
		zones = append(zones, Zone{ID: Details, R: Rect{16, 257, W - 32, 20}, Bulle: strings.Join(m.Details, "\n"), Info: true})
	}

	// Cumul par périmètre.
	if m.Auctions {
		texte(Rect{16, 282, W - 32, 20}, i18n.T("win.auctions", m.AuctionSent, m.AuctionPending), 12, false, th.Doux, Gauche)
		texte(Rect{16, 302, W - 32, 20}, i18n.T("win.auctiondays", m.AuctionUnproven), 12, false, th.Doux, Gauche)
		zones = append(zones, Zone{ID: PrixDetails, R: Rect{16, 282, W - 32, 40}, Bulle: strings.Join(m.Details, "\n"), Info: true})
	}
	carte(Rect{16, yCumul, W - 32, hCumul})
	texte(Rect{32, yCumul + 12, W - 64, 18}, i18n.T("win.cumul"), 12, true, th.Doux, Gauche)
	y := yCumul + 38
	if len(m.Portees) == 0 {
		texte(Rect{32, y, W - 64, 28}, i18n.T("state.nocumul"), 13, false, th.Pale, Gauche)
	}
	montrees := m.Portees
	reste := 0
	if len(montrees) > MaxPortees {
		montrees, reste = m.Portees[:MaxPortees-1], len(m.Portees)-(MaxPortees-1)
	}
	for i, p := range montrees {
		if i > 0 {
			rect(Rect{32, y, W - 64, 1}, 0, th.Separateur)
		}
		texte(Rect{32, y, 176, 28}, p.Nom, 13, false, th.Texte, Gauche)
		texte(Rect{208, y, 118, 28}, i18n.T("win.since", p.Depuis), 12, false, th.Pale, Droite)
		texte(Rect{330, y, W - 32 - 16 - 330, 28}, Nombre(p.Total), 15, true, th.Texte, Droite)
		y += 28
	}
	if reste > 0 {
		rect(Rect{32, y, W - 64, 1}, 0, th.Separateur)
		texte(Rect{32, y, W - 64, 28}, i18n.T("win.more", reste), 12, false, th.Pale, Gauche)
		var cachees []string
		for _, p := range m.Portees[MaxPortees-1:] {
			cachees = append(cachees, p.Nom+" : "+Nombre(p.Total)+" ("+i18n.T("win.since", p.Depuis)+")")
		}
		zones = append(zones, Zone{ID: Autres, R: Rect{32, y, W - 64, 28}, Bulle: strings.Join(cachees, "\n"), Info: true})
	}

	// Actions : l'action utile en premier, en couleur.
	if m.Connection != "" {
		texte(Rect{24, yCumul - 42, W - 48, 36}, m.Connection, 12, false, th.Texte, Gauche)
		zones = append(zones, Zone{ID: 990, R: Rect{24, yCumul - 42, W - 48, 36}, Bulle: m.Connection, Info: true})
	}
	yb := yCumul + hCumul + 14
	premier, second := Envoyer, Page
	if m.JetonManquant() {
		premier, second = Page, Envoyer
	}
	bouton := func(id int, r Rect, primaire bool, g, cle, bulle string) {
		survol, appui, occupe := etat(id)
		fond, bord, coulTexte := th.Bouton, th.Bord, th.Texte
		if primaire {
			fond, bord, coulTexte = th.Accent, th.Accent, th.SurAccent
			if appui {
				fond = th.AccentAppui
			} else if survol {
				fond = th.AccentSurvol
			}
		} else if appui {
			fond = th.BoutonAppui
		} else if survol {
			fond = th.BoutonSurvol
		}
		label := i18n.T(cle)
		if occupe {
			label = i18n.T("btn.busy")
			if id == Envoyer {
				label = i18n.T("btn.sending")
			}
			if primaire {
				fond = Melange(th.Accent, th.Fond, 0.35)
			} else {
				coulTexte = th.Pale
			}
		}
		add(Op{Genre: OpRect, R: r, Rayon: 8, Couleur: fond, Bord: bord, Epais: 1})
		if g != "" {
			gc := coulTexte
			if !primaire && !occupe {
				gc = th.Accent
			}
			glyphe(Rect{r.X + 12, r.Y, 20, r.H}, g, 14, gc)
			texte(Rect{r.X + 34, r.Y, r.W - 34 - 10, r.H}, label, 13, primaire, coulTexte, Centre)
		} else {
			texte(r, label, 13, primaire, coulTexte, Centre)
		}
		zones = append(zones, Zone{ID: id, R: r, Bulle: i18n.T(bulle), Inactif: occupe})
	}
	gl := map[int]string{Envoyer: GlypheEnvoyer, Page: ""}
	cles := map[int]string{Envoyer: "btn.send", Page: "btn.connect"}
	bulles := map[int]string{Envoyer: "tip.send", Page: "tip.connect"}
	bw := (W - 32 - 8) / 2
	bouton(premier, Rect{16, yb, bw, 40}, true, gl[premier], cles[premier], bulles[premier])
	bouton(second, Rect{16 + bw + 8, yb, bw, 40}, false, gl[second], cles[second], bulles[second])
	yb += 48
	bouton(Coller, Rect{16, yb, bw, 34}, false, GlypheColler, "btn.paste", "tip.paste")
	bouton(Journal, Rect{16 + bw + 8, yb, bw, 34}, false, GlypheJournal, "btn.log", "tip.log")
	yb += 34 + 14

	// Réglages : interrupteur « Lancer au démarrage » et langue.
	carte(Rect{16, yb, W - 32, 52})
	{
		survol, _, _ := etat(Demarrage)
		piste := th.Piste
		if m.Demarrage {
			piste = th.Accent
			if survol {
				piste = th.AccentSurvol
			}
		} else if survol {
			piste = Melange(th.Piste, th.Texte, 0.12)
		}
		rect(Rect{30, yb + 16, 40, 20}, 10, piste)
		kx := 33
		if m.Demarrage {
			kx = 53
		}
		add(Op{Genre: OpRond, R: Rect{kx, yb + 19, 14, 14}, Couleur: 0xFFFFFF})
		texte(Rect{80, yb, W - 16 - 14 - 88 - 24 - 6 - 80, 52}, i18n.T("win.autostart"), 13, false, th.Texte, Gauche)
		zones = append(zones, Zone{ID: Demarrage, R: Rect{24, yb + 8, 264, 36}, Bulle: i18n.T("tip.autostart")})
	}
	{
		gx := W - 16 - 14 - 88
		glyphe(Rect{gx - 24, yb, 20, 52}, GlypheGlobe, 14, th.Pale)
		rect(Rect{gx, yb + 12, 88, 28}, 8, th.Piste)
		for i, l := range []struct {
			id  int
			nom string
			on  bool
		}{{LangueFR, "FR", i18n.Get() == i18n.FR}, {LangueEN, "EN", i18n.Get() == i18n.EN}} {
			r := Rect{gx + 2 + i*42, yb + 14, 42, 24}
			survol, _, _ := etat(l.id)
			c := th.Doux
			if l.on {
				rect(r, 6, th.Accent)
				c = th.SurAccent
			} else if survol {
				rect(r, 6, th.BoutonSurvol)
				c = th.Texte
			}
			texte(r, l.nom, 12, l.on, c, Centre)
			zones = append(zones, Zone{ID: l.id, R: r, Bulle: i18n.T("tip.lang")})
		}
	}
	yb += 52 + 14

	// Pied : actions rares à gauche (liens), Réduire / Quitter à droite.
	rect(Rect{0, yb, W, H - yb}, 0, th.Pied)
	rect(Rect{0, yb, W, 1}, 0, th.Bord)
	lien := func(id int, x, w int, cle, bulle string) {
		survol, _, occupe := etat(id)
		c := th.Doux
		if survol && !occupe {
			c = th.Danger
		}
		r := Rect{x, yb + 10, w, 30}
		add(Op{Genre: OpTexte, R: r, Texte: i18n.T(cle), Taille: 12, Couleur: c, Souligne: survol && !occupe})
		zones = append(zones, Zone{ID: id, R: r, Bulle: i18n.T(bulle), Inactif: occupe})
	}
	lw := largeurLien(i18n.T("lnk.wipe"))
	lien(Effacer, 16, lw, "lnk.wipe", "tip.wipe")
	lien(Desinstaller, 16+lw+12, largeurLien(i18n.T("lnk.uninstall")), "lnk.uninstall", "tip.uninstall")
	bouton(Reduire, Rect{W - 16 - 80 - 6 - 80, yb + 10, 80, 30}, false, "", "btn.minimize", "tip.minimize")
	bouton(Quitter, Rect{W - 16 - 80, yb + 10, 80, 30}, false, "", "btn.quit", "tip.quit")

	// Focus clavier : anneau autour de la zone.
	if in.FocusVisible {
		for _, z := range zones {
			if z.ID == in.Focus && !z.Info {
				add(Op{Genre: OpRect, R: z.R.Agrandi(3), Rayon: 10, Vide: true, Bord: th.Accent, Epais: 2})
			}
		}
	}
	return ops, zones, W, H
}

// largeurLien : estimation large (Segoe UI 12 px ≈ 6,3 px par caractère).
func largeurLien(s string) int { return len([]rune(s))*13/2 + 4 }
