package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Installation (programme fermé, base et journal déjà fermés) :
//
//  1. la copie vérifiée est écrite à côté du programme : <exe>.new ;
//  2. <exe> devient <exe>.old (Windows permet de renommer un programme lancé,
//     pas de l'effacer) ;
//  3. <exe>.new devient <exe> ; en cas d'échec, <exe>.old reprend sa place ;
//  4. essai.json note « de → vers » : la nouvelle version compte ses démarrages.
//
// La nouvelle version confirme l'essai quand sa première passe de surveillance est
// terminée (fichiers lus, file ouverte, envoi tenté) : <exe>.old et essai.json sont
// alors retirés. Si elle démarre MaxTentatives fois sans y parvenir, elle remet
// <exe>.old en place, note sa version dans refusees.txt et relance l'ancienne.
// L'entrée « Lancer au démarrage » vise toujours le même chemin.
//
// La base companion.db n'est jamais copiée ni restaurée : un retour arrière garde la
// file la plus récente et ses accusés (règle du retour arrière de la 0.9.0).

// Lance vérifie que Windows accepte d'exécuter la copie (Smart App Control peut
// bloquer un exécutable non signé inconnu, sans prévenir) et qu'elle est bien la
// version annoncée. Remplaçable par les tests.
var Lance = func(path, version string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "--version").Output()
	if err != nil {
		return fmt.Errorf("Windows refuse de lancer la nouvelle version : %v", err)
	}
	if v := strings.TrimSpace(string(out)); v != version {
		return fmt.Errorf("la nouvelle version se déclare %q au lieu de %q", v, version)
	}
	return nil
}

// MaxTentatives : démarrages sans confirmation avant le retour arrière.
const MaxTentatives = 3

// Essai : installation en attente de confirmation.
type Essai struct {
	De         string `json:"from"`
	Vers       string `json:"to"`
	Tentatives int    `json:"attempts"`
}

func lireEssai(dir string) (Essai, bool) {
	var e Essai
	b, err := os.ReadFile(filepath.Join(dir, fichierEssai))
	if err != nil || json.Unmarshal(b, &e) != nil {
		return e, false
	}
	return e, true
}

func ecrireEssai(dir string, e Essai) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return ecrit(filepath.Join(dir, fichierEssai), b)
}

// Installe remplace exe par la version prête dans dir. version : celle en cours.
func Installe(exe, dir, version string, m Manifeste, pret string) error {
	b, err := os.ReadFile(pret)
	if err != nil {
		return err
	}
	if err := Conforme(b, m); err != nil {
		return err
	}
	if err := ecrit(exe+".new", b); err != nil {
		return err
	}
	// Essai de lancement à son emplacement final, avant de toucher au programme en
	// service : une version bloquée est refusée et l'actuelle continue de tourner.
	if err := Lance(exe+".new", m.Version); err != nil {
		_ = os.Remove(exe + ".new")
		_ = refuse(dir, m.Version)
		Nettoie(dir)
		return err
	}
	if err := os.Remove(exe + ".old"); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = os.Remove(exe + ".new")
		return err
	}
	if err := os.Rename(exe, exe+".old"); err != nil {
		_ = os.Remove(exe + ".new")
		return err
	}
	if err := os.Rename(exe+".new", exe); err != nil {
		if rerr := os.Rename(exe+".old", exe); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	if err := ecrireEssai(dir, Essai{De: version, Vers: m.Version}); err != nil {
		return err
	}
	Nettoie(dir)
	return nil
}

// Demarre compte un démarrage de la version en cours si elle est à l'essai. Vrai :
// trop de démarrages sans confirmation, il faut appeler Retablit.
func Demarre(dir, version string) (Essai, bool) {
	e, ok := lireEssai(dir)
	if !ok || e.Vers != version {
		return e, false
	}
	e.Tentatives++
	_ = ecrireEssai(dir, e)
	return e, e.Tentatives > MaxTentatives
}

// Confirme termine l'essai de la version en cours : l'ancienne copie et le marqueur
// sont retirés. Renvoie l'essai confirmé (pour l'annoncer), s'il y en avait un.
func Confirme(exe, dir, version string) (Essai, bool) {
	e, ok := lireEssai(dir)
	if !ok || e.Vers != version {
		return e, false
	}
	if err := os.Remove(exe + ".old"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return e, false // garder le marqueur : nouvel essai au prochain démarrage
	}
	_ = os.Remove(filepath.Join(dir, fichierEssai))
	return e, true
}

// Retablit remet l'ancienne version en place et refuse durablement celle en cours.
// Le programme en cours est seulement renommé en <exe>.refusee (retiré par Menage).
func Retablit(exe, dir string, e Essai) error {
	if _, err := os.Stat(exe + ".old"); err != nil {
		return err
	}
	_ = os.Remove(exe + ".refusee")
	if err := os.Rename(exe, exe+".refusee"); err != nil {
		return err
	}
	if err := os.Rename(exe+".old", exe); err != nil {
		_ = os.Rename(exe+".refusee", exe)
		return err
	}
	_ = refuse(dir, e.Vers)
	_ = os.Remove(filepath.Join(dir, fichierEssai))
	Nettoie(dir)
	return nil
}

// Menage retire une version refusée laissée par Retablit.
func Menage(exe string) { _ = os.Remove(exe + ".refusee") }
