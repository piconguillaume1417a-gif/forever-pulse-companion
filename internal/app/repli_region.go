package app

import (
	"encoding/json"
	"time"

	"wowsync/internal/portal"
	"wowsync/internal/store"
)

// Remise en file unique de la 0.10.0-rc.8.
const (
	// CleRemiseRegions : clé meta versionnée. Une version future en pose une nouvelle
	// si une autre région arrive après celle-ci.
	CleRemiseRegions = "region_requeue_v1"
	// BorneRemise : au plus autant de lignes par table. Le surplus reste refusé.
	BorneRemise = 50_000
)

// PlancherRegions : 3 novembre 2026, 12:00 Paris, le observed_not_before des politiques
// de la sortie.
var PlancherRegions = time.Date(2026, 11, 3, 11, 0, 0, 0, time.UTC)

// RemiseEnFileRegions remet en attente, une seule fois, ce que le site a refusé en
// source_disabled depuis le plancher. À appeler une fois au démarrage du moteur, avant
// le premier envoi. Une erreur ne consomme pas la clé : le prochain démarrage
// recommencera.
//
// Ne publier une version qui l'appelle qu'une fois les candidates d'alias et de la
// Chine vérifiées en PROD : plus tôt, ces lots seraient refusés de nouveau et la clé
// serait consommée.
func (a *App) RemiseEnFileRegions() {
	a.remiseRegions(PlancherRegions.Unix(), BorneRemise)
}

func (a *App) remiseRegions(plancher int64, borne int) {
	a.mu.Lock()
	b, err := a.Store.RemiseEnFile(CleRemiseRegions, plancher, borne)
	a.mu.Unlock()
	if err != nil {
		a.Log.Printf("remise en file unique non faite : erreur de la base locale, nouvel essai au prochain démarrage")
		return
	}
	if b.DejaFaite {
		return
	}
	a.Log.Printf("remise en file unique (%s) : %d lots et %d fiches de statistiques refusés en %s remis en attente",
		CleRemiseRegions, b.Lots, b.Fiches, store.RaisonRemise)
	if b.LotsLaisses > 0 || b.FichesLaisses > 0 {
		a.Log.Printf("remise en file unique : borne de %d lignes dépassée, %d lots et %d fiches laissés refusés",
			borne, b.LotsLaisses, b.FichesLaisses)
	}
	if b.Lots > 0 || b.Fiches > 0 {
		a.changed()
	}
}

// valeurPortal : la lecture à la demande pour un corps d'envoi. Config.wtf est relu au
// plus une fois par corps, et pas du tout si aucun membre n'est une cible du repli. Le journal
// n'en dit que « portal utilisé : EU » ou « portal non résolu », une fois par changement.
func (a *App) valeurPortal(chemin string) func() (string, bool) {
	return func() (string, bool) {
		v, st := portal.PourFichier(chemin)
		ok := st == portal.Resolu
		a.muPortal.Lock()
		defer a.muPortal.Unlock()
		if !ok {
			v = ""
		}
		if !a.portalNote || a.portalVu != v {
			a.portalNote, a.portalVu = true, v
			if ok {
				a.Log.Printf("portal utilisé : %s", v)
			} else {
				a.Log.Printf("portal non résolu")
			}
		}
		return v, ok
	}
}

// ScopesEnvoi : les périmètres d'un fichier tels qu'ils partent. La valeur stockée
// (files.scopes) ne change jamais : un lot déjà en base profite du repli à l'envoi.
func (a *App) ScopesEnvoi(f *store.File) json.RawMessage {
	out, _ := portal.RepliObjets(f.Scopes, a.valeurPortal(f.Path))
	return out
}

// MetaEnvoi : l'en-tête des statistiques d'un fichier tel qu'il part. La valeur stockée
// (stats_files.meta) ne change jamais.
func (a *App) MetaEnvoi(chemin string, meta []byte) []byte {
	return portal.RepliMeta(meta, a.valeurPortal(chemin))
}
