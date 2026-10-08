// Package watch trouve et lit les fichiers de l'addon, sans jamais y toucher.
//
// Découverte : <wow>\_*_\WTF\Account\*\SavedVariables\ForeverPulse.lua et son .bak.
// Le dossier _classic_beta_ est provisoire : rien n'est codé en dur.
//
// Surveillance par sondage (toutes les 5 min par défaut depuis 0.7.3, 30 s en 0.7.2, 10 min de 0.6.0 à 0.7.1) : le jeu
// n'écrit le fichier qu'à la déconnexion et au /reload, un sondage suffit et n'ouvre
// rien tant que la taille ou la date ne changent pas. Un fichier est stable quand
// taille et date sont inchangées depuis 3 s : un changement repéré est donc
// revérifié quelques secondes plus tard (EnAttente), pas au sondage suivant.
package watch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const MaxFile = 64 << 20 // l'addon plafonne à 50 Mo de lignes

// Discover renvoie les ForeverPulse.lua et .bak existants sous le dossier WoW.
func Discover(wowDir string) []string {
	motif := filepath.Join(escape(wowDir), "_*_", "WTF", "Account", "*", "SavedVariables", "ForeverPulse.lua")
	var out []string
	m, _ := filepath.Glob(motif)
	for _, p := range m {
		out = append(out, p)
	}
	b, _ := filepath.Glob(motif + ".bak")
	out = append(out, b...)
	sort.Strings(out)
	return out
}

// L1 revision 2: only the current Auctionator database; never its older .bak.
func DiscoverAuctions(wowDir string) []string {
	m, _ := filepath.Glob(filepath.Join(escape(wowDir), "_*_", "WTF", "Account", "*", "SavedVariables", "Auctionator.lua"))
	sort.Strings(m)
	return m
}
func DiscoverAll(wowDir string) []string {
	return append(Discover(wowDir), DiscoverAuctions(wowDir)...)
}

// PairSignature looks only at metadata; no file is opened during an idle poll.
func PairSignature(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	sig := fmt.Sprintf("%d/%d", st.Size(), st.ModTime().UnixNano())
	n, err := os.Stat(filepath.Join(filepath.Dir(path), "ForeverPulse.lua"))
	if os.IsNotExist(err) {
		return sig + "/absent", nil
	}
	if err != nil {
		return "", err
	}
	return sig + fmt.Sprintf("/%d/%d", n.Size(), n.ModTime().UnixNano()), nil
}

// IsStable also checks that the file has not changed since the last poll.
func (w *Watcher) IsStable(path string, now time.Time) bool {
	st, err := os.Stat(path)
	if os.IsNotExist(err) {
		return true
	} // an absent note proves no market
	e := w.vus[path]
	return err == nil && e != nil && e.size == st.Size() && e.mod.Equal(st.ModTime()) && (e.traite || now.Sub(e.depuis) >= w.Stable)
}

// escape protège les caractères spéciaux de Glob présents dans le chemin de base.
func escape(p string) string {
	r := make([]rune, 0, len(p))
	for _, c := range p {
		if c == '[' || c == '*' || c == '?' {
			r = append(r, '[', c, ']')
			continue
		}
		r = append(r, c)
	}
	return string(r)
}

// Read lit tout le fichier en mémoire, partagé, puis referme aussitôt.
func Read(path string) ([]byte, string, error) {
	f, err := openShared(path)
	if err != nil {
		return nil, "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFile+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > MaxFile {
		return nil, "", fmt.Errorf("fichier de plus de %d Mo", MaxFile>>20)
	}
	sum := sha256.Sum256(data)
	return data, hex.EncodeToString(sum[:]), nil
}

type etat struct {
	size   int64
	mod    time.Time
	depuis time.Time
	traite bool
}

// Watcher suit la taille et la date de chaque fichier.
type Watcher struct {
	Stable time.Duration
	vus    map[string]*etat
}

func New() *Watcher { return &Watcher{Stable: 3 * time.Second, vus: map[string]*etat{}} }

// Ready renvoie les fichiers changés et stables depuis Stable, à traiter.
func (w *Watcher) Ready(paths []string, now time.Time) []string {
	var out []string
	for _, p := range paths {
		st, err := os.Stat(p)
		if err != nil {
			delete(w.vus, p)
			continue
		}
		e := w.vus[p]
		if e == nil || e.size != st.Size() || !e.mod.Equal(st.ModTime()) {
			w.vus[p] = &etat{size: st.Size(), mod: st.ModTime(), depuis: now}
			continue
		}
		if !e.traite && now.Sub(e.depuis) >= w.Stable {
			e.traite = true
			out = append(out, p)
		}
	}
	return out
}

// EnAttente : un fichier a changé et attend d'être stable pour être traité.
func (w *Watcher) EnAttente() bool {
	for _, e := range w.vus {
		if !e.traite {
			return true
		}
	}
	return false
}

// Mark note l'état actuel des fichiers comme déjà traité.
func (w *Watcher) Mark(paths []string) {
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil {
			w.vus[p] = &etat{size: st.Size(), mod: st.ModTime(), traite: true}
		}
	}
}

// Retry remet un fichier en attente (lecture ratée).
func (w *Watcher) Retry(path string) { delete(w.vus, path) }
