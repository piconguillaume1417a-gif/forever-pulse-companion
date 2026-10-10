// apercu écrit en JSON les opérations de dessin de la fenêtre pour quelques états
// types (thème clair et sombre, français et anglais). outils/apercu/rendu.py les
// transforme en images PNG, pour relire la mise en page hors Windows.
//
//	go run ./outils/apercu > apercu.json && python outils/apercu/rendu.py apercu.json dossier
package main

import (
	"encoding/json"
	"image/png"
	"os"

	"wowsync/internal/icone"

	"wowsync/internal/i18n"
	"wowsync/internal/vue"
)

type scene struct {
	Nom     string
	Largeur int
	Hauteur int
	Ops     []vue.Op
	Zones   []vue.Zone
}

func main() {
	portees := []vue.Portee{{Nom: "4619-PVP-Alliance", Total: 51767, Depuis: "18/09/2026"}, {Nom: "4619-PVP-Horde", Total: 2310, Depuis: "21/09/2026"}}
	base := vue.Modele{Pret: true, Version: "0.9.0-rc.2", Demarrage: true, Envoyes: 546, DernierEnvoi: "Dernier envoi : 27/09 15:04, 12 lots, 3456 personnages", Portees: portees}
	cas := []struct {
		nom   string
		lang  i18n.Lang
		th    vue.Theme
		m     func(vue.Modele) vue.Modele
		inter vue.Interaction
	}{
		{"00-connection-pending-fr", i18n.FR, vue.Clair(), func(m vue.Modele) vue.Modele {
			// Installation neuve : association en cours, code affiché en grand.
			m.Code, m.Message, m.Couleur = "notoken", "Companion non connecté", 2
			m.Connection, m.ConnectionCode = vue.Connexion("notoken", "pending", "0E26CF55")
			return m
		}, vue.Interaction{}},
		{"00-connection-connected-en", i18n.EN, vue.Sombre(), func(m vue.Modele) vue.Modele {
			m.Code, m.Message = "allsent", "Everything has been sent"
			m.Connection = i18n.T("connect.connected", "synthetic@example.invalid")
			return m
		}, vue.Interaction{}},
		{"01-tout-envoye-clair-en", i18n.EN, vue.Clair(), func(m vue.Modele) vue.Modele {
			m.Couleur, m.Code, m.Message = 0, "allsent", "Everything has been sent"
			m.DernierEnvoi = "Last upload: Sep 27, 15:04, 12 batches, 3456 characters"
			return m
		}, vue.Interaction{Survol: vue.Envoyer}},
		{"02-en-attente-sombre", i18n.FR, vue.Sombre(), func(m vue.Modele) vue.Modele {
			m.Couleur, m.Code, m.Message, m.Attente, m.Refuses = 1, "pending", "5 lots en attente d'envoi, nouvel essai à 15:32", 5, 2
			return m
		}, vue.Interaction{Occupe: map[int]bool{vue.Envoyer: true}, Survol: vue.Effacer}},
		{"03-sans-jeton-clair", i18n.FR, vue.Clair(), func(m vue.Modele) vue.Modele {
			m.Couleur, m.Code, m.Message, m.Attente, m.Envoyes, m.DernierEnvoi = 2, "notoken", "Companion déconnecté", 546, 0, ""
			m.Demarrage = false
			return m
		}, vue.Interaction{FocusVisible: true, Focus: vue.Coller}},
		{"04-demarrage-sombre-en", i18n.EN, vue.Sombre(), func(m vue.Modele) vue.Modele {
			m.Pret, m.Portees, m.DernierEnvoi = false, nil, ""
			return m
		}, vue.Interaction{}},
		{"05-six-portees-clair-en", i18n.EN, vue.Clair(), func(m vue.Modele) vue.Modele {
			m.Couleur, m.Code, m.Message = 0, "allsent", "Everything has been sent"
			m.DernierEnvoi = "Last upload: Sep 27, 15:04, 12 batches, 3456 characters"
			for i := 0; i < 4; i++ {
				m.Portees = append(m.Portees, vue.Portee{Nom: "4620-PVE-Horde", Total: 1234 * (i + 1), Depuis: "Sep 20, 2026"})
			}
			return m
		}, vue.Interaction{Survol: vue.LangueFR}},
	}
	if len(os.Args) > 1 { // image du logo, pour le rendu
		if f, err := os.Create(os.Args[1]); err == nil {
			_ = png.Encode(f, icone.Logo(304, 180))
			f.Close()
		}
	}
	var out []scene
	for _, c := range cas {
		i18n.Set(c.lang)
		ops, zones, w, h := vue.Construire(c.m(base), c.inter, c.th)
		out = append(out, scene{c.nom, w, h, ops, zones})
	}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", " ")
	_ = e.Encode(out)
}
