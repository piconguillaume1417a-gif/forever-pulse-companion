package vue

import (
	"strings"
	"testing"

	"wowsync/internal/i18n"
)

func modeles() []Modele {
	p := []Portee{{"4619-PVP-Alliance", 51767, "18/09/2026"}, {"4619-PVP-Horde", 12, "21/09/2026"}}
	var out []Modele
	out = append(out, Modele{Version: "0.6.0"}) // démarrage
	for i, code := range []string{"allsent", "pending", "notoken", "token", "schema", "source", "body", "rejected"} {
		m := Modele{Pret: true, Code: code, Message: "message", Couleur: i % 3, Envoyes: 546, Attente: 5,
			Refuses: 2, DernierEnvoi: "x", Version: "0.6.0", Portees: p[:i%3]}
		m.Dossier, m.DossierEtat = `C:\Program Files (x86)\World of Warcraft`, i%3
		out = append(out, m)
	}
	out = append(out, Modele{Pret: true, Code: "allsent", Dossier: `E:\Un dossier très long\Jeux installés ailleurs\Battle.net\World of Warcraft`, DossierEtat: DossierAbsent})
	var six []Portee
	for i := 0; i < 6; i++ {
		six = append(six, Portee{"s", i, "d"})
	}
	out = append(out, Modele{Pret: true, Code: "allsent", Portees: six})
	out = append(out, Modele{Pret: true, Code: "pending", Message: "Auction prices pending", Auctions: true, AuctionSent: 5000, AuctionPending: 2994, AuctionUnproven: 31, Details: []string{"Synthetic auction error"}, Portees: six})
	out = append(out, Modele{Pret: true, Code: "notoken", Connection: i18n.T("connect.pending", "ABCD1234"), Version: "0.9.0-rc.1"})
	out = append(out, Modele{Pret: true, Code: "allsent", Connection: i18n.T("connect.connected", "synthetic@example.invalid"), Version: "0.9.0-rc.1"})
	out = append(out, Modele{Pret: true, Code: "token", Connection: i18n.T("connect.compare"), ConnectionCode: "ABCD1234", Version: "0.10.0-rc.4"})
	return out
}

// Chaque texte existe dans les deux langues, chaque zone tient dans la fenêtre et
// deux zones cliquables ne se chevauchent pas.
func TestMiseEnPage(t *testing.T) {
	defer i18n.Set(i18n.FR)
	for _, l := range i18n.Toutes {
		i18n.Set(l)
		for _, th := range []Theme{Clair(), Sombre()} {
			for _, m := range modeles() {
				ops, zones, w, h := Construire(m, Interaction{Survol: -1, Appui: -1, Focus: Envoyer, FocusVisible: true}, th)
				if w != Largeur || h != HauteurModele(m) {
					t.Fatalf("taille %d×%d", w, h)
				}
				for _, o := range ops {
					if o.Genre == OpTexte && strings.Contains(o.Texte, ".") && !strings.Contains(o.Texte, " ") &&
						strings.IndexAny(o.Texte, "0123456789") < 0 {
						t.Errorf("%s : texte manquant %q", l, o.Texte)
					}
					r := o.R
					if o.Vide {
						r = r.Agrandi(-3)
					}
					if r.X < 0 || r.Y < 0 || r.X+r.W > w || r.Y+r.H > h {
						t.Errorf("%s %s : opération hors fenêtre %+v", l, m.Code, o)
					}
				}
				vus := map[int]bool{}
				for i, a := range zones {
					if vus[a.ID] {
						t.Errorf("zone %d en double", a.ID)
					}
					vus[a.ID] = true
					if a.Bulle == "" || strings.HasPrefix(a.Bulle, "tip.") {
						t.Errorf("%s : infobulle manquante pour %d (%q)", l, a.ID, a.Bulle)
					}
					for _, b := range zones[i+1:] {
						if a.R.X < b.R.X+b.R.W && b.R.X < a.R.X+a.R.W && a.R.Y < b.R.Y+b.R.H && b.R.Y < a.R.Y+a.R.H {
							t.Errorf("zones %d et %d se chevauchent", a.ID, b.ID)
						}
					}
				}
				for _, id := range []int{Envoyer, Installations, Journal, Page, Dossier, Demarrage, Effacer, Desinstaller, Reduire, Quitter, LangueFR, LangueEN, LangueKO} {
					if !vus[id] {
						t.Errorf("commande %d absente", id)
					}
				}
			}
		}
	}
}

// Sans jeton, « Connecter » passe en premier (bouton en couleur, premier
// dans l'ordre de Tab).
func TestActionUtileEnPremier(t *testing.T) {
	_, z, _, _ := Construire(Modele{Pret: true, Code: "notoken"}, Interaction{}, Clair())
	if z[0].ID != Page {
		t.Fatalf("première zone %d", z[0].ID)
	}
	_, z, _, _ = Construire(Modele{Pret: true, Code: "allsent"}, Interaction{}, Clair())
	if z[0].ID != Envoyer {
		t.Fatalf("première zone %d", z[0].ID)
	}
}

func TestNombre(t *testing.T) {
	defer i18n.Set(i18n.FR)
	i18n.Set(i18n.FR)
	if Nombre(51767) != "51 767" || Nombre(12) != "12" || Nombre(1234567) != "1 234 567" {
		t.Fatal(Nombre(51767), Nombre(1234567))
	}
	i18n.Set(i18n.EN)
	if Nombre(51767) != "51,767" {
		t.Fatal(Nombre(51767))
	}
	i18n.Set(i18n.KO)
	if Nombre(51767) != "51,767" || Nombre(-1234567) != "-1,234,567" {
		t.Fatal(Nombre(51767), Nombre(-1234567))
	}
}

// Le sélecteur a une case par langue de la table, dans son ordre, et chaque case
// renvoie sa langue.
func TestSelecteurDeLangue(t *testing.T) {
	if len(zonesLangue) != len(i18n.Toutes) {
		t.Fatalf("%d cases pour %d langues", len(zonesLangue), len(i18n.Toutes))
	}
	for i, z := range zonesLangue {
		if z.L != i18n.Toutes[i] {
			t.Errorf("case %d : %s au lieu de %s", i, z.L, i18n.Toutes[i])
		}
		if l, ok := LangueDe(z.ID); !ok || l != z.L {
			t.Errorf("zone %d : %s", z.ID, l)
		}
	}
	if _, ok := LangueDe(Envoyer); ok {
		t.Error("Envoyer n'est pas une langue")
	}
	defer i18n.Set(i18n.FR)
	i18n.Set(i18n.KO)
	ops, zones, _, _ := Construire(Modele{Pret: true, Code: "allsent"}, Interaction{Survol: -1, Appui: -1}, Clair())
	var cases []Rect
	for _, z := range zones {
		if _, ok := LangueDe(z.ID); ok {
			cases = append(cases, z.R)
			if !strings.Contains(z.Bulle, i18n.Nom(i18n.KO)) && z.ID == LangueKO {
				t.Errorf("infobulle %q", z.Bulle)
			}
		}
	}
	if len(cases) != len(i18n.Toutes) {
		t.Fatal(cases)
	}
	for i := 1; i < len(cases); i++ {
		if cases[i].X != cases[i-1].X+cases[i-1].W {
			t.Errorf("cases non contiguës : %+v", cases)
		}
	}
	trouve := false
	for _, o := range ops {
		if o.Genre == OpTexte && o.Texte == "한국어" {
			trouve = true
			if LargeurTexte(o.Texte, o.Taille) > o.R.W {
				t.Errorf("« 한국어 » déborde : %+v", o.R)
			}
		}
	}
	if !trouve {
		t.Error("case « 한국어 » absente")
	}
}

// En coréen, chaque libellé d'une ligne tient dans sa case (estimation large :
// 1 cadratin par syllabe hangul) ; seuls les textes de données (chemins, messages)
// peuvent finir en « … ».
func TestCoreenTientDansLaFenetre(t *testing.T) {
	defer i18n.Set(i18n.FR)
	for _, l := range []i18n.Lang{i18n.KO} {
		i18n.Set(l)
		donnees := map[string]bool{}
		for _, m := range modeles() {
			donnees[m.Message], donnees[m.Dossier], donnees[m.DernierEnvoi], donnees[m.Connection] = true, true, true, true
			for _, p := range m.Portees {
				donnees[p.Nom] = true
			}
			ops, _, _, _ := Construire(m, Interaction{Survol: -1, Appui: -1}, Clair())
			for _, o := range ops {
				if o.Genre != OpTexte || o.Lignes || donnees[o.Texte] || o.Texte == "Forever Pulse Companion" || strings.HasPrefix(o.Texte, "v") {
					continue
				}
				if w := LargeurTexte(o.Texte, o.Taille); w > o.R.W {
					t.Errorf("%s : %q (%d px estimés) dépasse sa case de %d px", l, o.Texte, w, o.R.W)
				}
			}
		}
	}
}

func TestLargeurTexte(t *testing.T) {
	if largeurLien("Effacer les données…") != len([]rune("Effacer les données…"))*13/2+4 {
		t.Error("estimation latine modifiée")
	}
	if LargeurTexte("한국어", 12) != 36 || !Coreen("Forever Pulse에 연결") || Coreen("Connect") {
		t.Error(LargeurTexte("한국어", 12))
	}
}

func TestMelange(t *testing.T) {
	if Melange(0x000000, 0xFFFFFF, 0.5) != 0x808080 || Melange(0x123456, 0x654321, 0) != 0x123456 {
		t.Fatal(Melange(0x000000, 0xFFFFFF, 0.5))
	}
}

// Installation neuve : le premier envoi sans jeton pose le blocage « token ».
// Une association en cours garde pourtant son code à l'écran ; hors association,
// le blocage s'affiche comme avant.
func TestConnexionGardeLeCodeMalgreLeBlocageJeton(t *testing.T) {
	for _, l := range i18n.Toutes {
		i18n.Set(l)
		texte, code := Connexion("token", "pending", "ABCD1234")
		if code != "ABCD1234" || texte != i18n.T("connect.compare") {
			t.Fatalf("%s : code masqué (%q, %q)", l, texte, code)
		}
		if texte, code = Connexion("notoken", "pending", "ABCD1234"); code != "ABCD1234" {
			t.Fatalf("%s : code absent (%q)", l, texte)
		}
		if texte, code = Connexion("token", "", ""); texte != i18n.T("connect.revoked", "") || code != "" {
			t.Fatalf("%s : blocage hors association (%q)", l, texte)
		}
		if texte, code = Connexion("token", "connected", "synthetic@example.invalid"); texte != i18n.T("connect.revoked", "") || code != "" {
			t.Fatalf("%s : jeton refusé après association (%q)", l, texte)
		}
		if texte, code = Connexion("allsent", "network", ""); texte != i18n.T("connect.network", "") || code != "" {
			t.Fatalf("%s : état réseau (%q)", l, texte)
		}
	}
	i18n.Set(i18n.FR)
}

// Le bouton « Changer… » passe en couleur quand le jeu est introuvable.
func TestDossierIntrouvableEnCouleur(t *testing.T) {
	th := Clair()
	for _, c := range []struct {
		etat     int
		primaire bool
	}{{DossierAbsent, true}, {DossierSansLua, false}, {DossierPret, false}} {
		ops, zones, _, _ := Construire(Modele{Pret: true, Code: "allsent", Dossier: "D:/WoW", DossierEtat: c.etat}, Interaction{Survol: -1, Appui: -1}, th)
		var r Rect
		for _, z := range zones {
			if z.ID == Dossier {
				r = z.R
			}
		}
		if r.W == 0 {
			t.Fatal("bouton Dossier absent")
		}
		coul := false
		for _, o := range ops {
			if o.Genre == OpRect && o.R == r {
				coul = o.Couleur == th.Accent
			}
		}
		if coul != c.primaire {
			t.Errorf("état %d : bouton en couleur = %t", c.etat, coul)
		}
	}
}
