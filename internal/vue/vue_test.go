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
	for i, code := range []string{"allsent", "pending", "notoken", "token", "schema", "source", "body"} {
		m := Modele{Pret: true, Code: code, Message: "message", Couleur: i % 3, Envoyes: 546, Attente: 5,
			Refuses: 2, DernierEnvoi: "x", Version: "0.6.0", Portees: p[:i%3]}
		out = append(out, m)
	}
	var six []Portee
	for i := 0; i < 6; i++ {
		six = append(six, Portee{"s", i, "d"})
	}
	out = append(out, Modele{Pret: true, Code: "allsent", Portees: six})
	out = append(out, Modele{Pret: true, Code: "pending", Message: "Auction prices pending", Auctions: true, AuctionSent: 5000, AuctionPending: 2994, AuctionUnproven: 31, Details: []string{"Synthetic auction error"}, Portees: six})
	out = append(out, Modele{Pret: true, Code: "notoken", Connection: i18n.T("connect.pending", "ABCD1234"), Version: "0.9.0-rc.1"})
	out = append(out, Modele{Pret: true, Code: "allsent", Connection: i18n.T("connect.connected", "synthetic@example.invalid"), Version: "0.9.0-rc.1"})
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
				for _, id := range []int{Envoyer, Coller, Journal, Page, Demarrage, Effacer, Desinstaller, Reduire, Quitter, LangueFR, LangueEN} {
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
}

func TestMelange(t *testing.T) {
	if Melange(0x000000, 0xFFFFFF, 0.5) != 0x808080 || Melange(0x123456, 0x654321, 0) != 0x123456 {
		t.Fatal(Melange(0x000000, 0xFFFFFF, 0.5))
	}
}
