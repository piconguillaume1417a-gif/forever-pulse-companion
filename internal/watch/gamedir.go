package watch

import (
	"os"
	"path/filepath"
	"strings"
)

// Dossier du jeu (rc.11, demande du propriétaire du 10/11/2026) : World of
// Warcraft n'est pas toujours installé dans C:\Program Files (x86). Le joueur
// choisit le dossier dans la fenêtre ; il peut désigner la racine (« World of
// Warcraft »), le dossier du client (« _classic_beta_ ») ou un dossier plus
// profond (WTF, SavedVariables) : la racine en est déduite.

// clientDir : un dossier de client, « _retail_ », « _classic_beta_ »… qui
// contient WTF ou Interface. Le contenu est vérifié pour ne pas confondre un
// dossier personnel nommé « _jeux_ » avec un client.
func clientDir(p string) bool {
	b := filepath.Base(p)
	if len(b) < 3 || b[0] != '_' || b[len(b)-1] != '_' {
		return false
	}
	for _, s := range []string{"WTF", "Interface"} {
		if st, err := os.Stat(filepath.Join(p, s)); err == nil && st.IsDir() {
			return true
		}
	}
	return false
}

// Racine renvoie la racine World of Warcraft d'un dossier choisi : le parent du
// dossier de client le plus proche, ou le dossier tel quel s'il n'est dans aucun.
func Racine(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	for a := p; ; {
		if clientDir(a) {
			return filepath.Dir(a)
		}
		parent := filepath.Dir(a)
		if parent == a {
			return p
		}
		a = parent
	}
}

// Clients : les dossiers de client présents sous la racine (« _retail_ »…).
func Clients(wowDir string) []string {
	m, _ := filepath.Glob(filepath.Join(escape(wowDir), "_*_"))
	var out []string
	for _, p := range m {
		if clientDir(p) {
			out = append(out, filepath.Base(p))
		}
	}
	return out
}

// Etat du dossier choisi, affiché dans la fenêtre.
type EtatDossier int

const (
	DossierAbsent  EtatDossier = iota // aucun client de jeu sous ce dossier
	DossierSansLua                    // jeu présent, ForeverPulse.lua pas encore écrit
	DossierPret                       // au moins un ForeverPulse.lua trouvé
)

func Examine(wowDir string) EtatDossier {
	if len(Discover(wowDir)) > 0 {
		return DossierPret
	}
	if len(Clients(wowDir)) > 0 {
		return DossierSansLua
	}
	return DossierAbsent
}

// Detecte : premier candidat qui contient un ForeverPulse.lua, sinon le premier
// qui contient un client de jeu, sinon "". Les doublons sont ignorés.
func Detecte(candidats []string) string {
	vus := map[string]bool{}
	var jeu string
	for _, c := range candidats {
		r := Racine(c)
		k := strings.ToLower(r)
		if r == "" || vus[k] {
			continue
		}
		vus[k] = true
		switch Examine(r) {
		case DossierPret:
			return r
		case DossierSansLua:
			if jeu == "" {
				jeu = r
			}
		}
	}
	return jeu
}

// CandidatsCourants : emplacements usuels sur chaque lecteur, après ceux donnés
// par Battle.net (registre, voir winui.DossiersBattleNet).
func CandidatsCourants(lecteurs []string) []string {
	var out []string
	for _, l := range lecteurs {
		for _, s := range []string{
			`Program Files (x86)\World of Warcraft`,
			`Program Files\World of Warcraft`,
			`World of Warcraft`,
			`Games\World of Warcraft`,
			`Jeux\World of Warcraft`,
			`Battle.net\World of Warcraft`,
			`Blizzard\World of Warcraft`,
		} {
			out = append(out, filepath.Join(l+`\`, s))
		}
	}
	return out
}
