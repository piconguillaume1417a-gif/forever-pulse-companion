// Package portal : le repli de région de la 0.10.0-rc.8.
//
// L'addon écrit la région que le jeu lui donne. Un code connu du site part tel quel,
// le site le traduit. Un code inconnu ou vide est remplacé, à l'envoi seulement, par
// la valeur de la ligne « SET portal » de WTF/Config.wtf, et seulement pour un fichier
// du dossier de jeu DossierRepli. Sans valeur claire, l'envoi reste inchangé et le
// refus du site aussi.
//
// Config.wtf porte aussi le nom du compte. Ce paquet ne rend donc qu'une valeur prise
// dans une table fixe et un statut, jamais une ligne ni un morceau du fichier, et il
// n'a pas d'erreur : aucun texte d'erreur ne peut en recopier le contenu.
package portal

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"wowsync/internal/schema"
	"wowsync/internal/watch"
)

// DossierRepli : le seul dossier de jeu où le repli s'applique. Sur _beta_, _ptr_ ou
// _xptr_, 90 et 110 seraient sinon réattribués à une région de sortie, ce qui
// mélangerait deux populations. Décision prise par défaut, à confirmer par le
// propriétaire : pour la retirer, c'est ici et dans RepliAutorise que ça se passe.
const DossierRepli = "_forever_"

// RepliAutorise dit si le repli s'applique aux fichiers de ce dossier de jeu.
func RepliAutorise(dossier string) bool { return strings.EqualFold(dossier, DossierRepli) }

const (
	// TailleMax : un Config.wtf réel fait quelques Ko. Au-delà, rien n'est lu.
	TailleMax = 256 << 10
	// tamponLigne : une ligne plus longue n'est pas une ligne « SET portal » ; elle est
	// sautée sans être gardée.
	tamponLigne = 4 << 10
)

// Statut : le résultat d'une lecture, sans aucun contenu du fichier.
type Statut int

const (
	Resolu    Statut = iota // une valeur claire : US, KR, EU, TW ou CN
	Absent                  // pas de Config.wtf
	TropGros                // plus de TailleMax
	Illisible               // ouverture ou lecture impossible, ou pas un fichier ordinaire
	PasClair                // pas de ligne portal, une autre valeur, ou deux valeurs différentes
	Ecarte                  // dossier de jeu autre que DossierRepli : Config.wtf n'est pas ouvert
)

// CodeConnu : la liste que le site traduit, en miroir de private.census_region_code
// (candidate census-region-aliases) : 1 à 5, 91 à 95, US, NA, KR, EU, TW, CN,
// insensible à la casse et aux espaces. Une valeur connue n'est jamais remplacée.
func CodeConnu(code string) bool {
	switch strings.ToUpper(strings.TrimSpace(code)) {
	case "1", "2", "3", "4", "5", "91", "92", "93", "94", "95", "US", "NA", "KR", "EU", "TW", "CN":
		return true
	}
	return false
}

// DossierJeu déduit d'un fichier sous <wow>\<client>\WTF\… le dossier <client> et son
// chemin. Le premier parent nommé WTF dont le propre parent a la forme _xxx_ fait foi :
// un dossier de compte qui s'appellerait _forever_ ou WTF ne peut pas tromper la règle.
// Rien de reconnu : deux chaînes vides.
func DossierJeu(fichier string) (racine, nom string) {
	for p := filepath.Dir(fichier); ; p = filepath.Dir(p) {
		parent := filepath.Dir(p)
		if parent == p {
			return "", ""
		}
		if strings.EqualFold(filepath.Base(p), "WTF") {
			n := filepath.Base(parent)
			if len(n) > 2 && strings.HasPrefix(n, "_") && strings.HasSuffix(n, "_") {
				return parent, n
			}
		}
	}
}

// PourFichier rend la valeur du portail du dossier de jeu d'un fichier de l'addon, lue
// au moment de l'appel, sans cache. Un dossier non autorisé n'ouvre aucun fichier.
func PourFichier(fichier string) (string, Statut) {
	racine, nom := DossierJeu(fichier)
	if !RepliAutorise(nom) {
		return "", Ecarte
	}
	return Lire(filepath.Join(racine, "WTF", "Config.wtf"))
}

// Lire lit un Config.wtf en partage, ligne par ligne, sans rien garder d'autre que la
// valeur de portal.
func Lire(chemin string) (string, Statut) {
	f, err := watch.OpenShared(chemin)
	if err != nil {
		if os.IsNotExist(err) {
			return "", Absent
		}
		return "", Illisible
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		return "", Illisible
	}
	if st.Size() > TailleMax {
		return "", TropGros
	}
	return analyse(io.LimitReader(f, TailleMax+1))
}

func analyse(r io.Reader) (string, Statut) {
	br := bufio.NewReaderSize(r, tamponLigne)
	lu, premiere, longue, valeur := 0, true, false, ""
	for {
		ligne, err := br.ReadSlice('\n')
		lu += len(ligne)
		if lu > TailleMax {
			return "", TropGros
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			longue, premiere = true, false
			continue
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return "", Illisible
		}
		if !longue {
			if premiere {
				ligne = bytes.TrimPrefix(ligne, []byte("\xef\xbb\xbf"))
			}
			if brut, oui := valeurPortal(ligne); oui {
				v := regionFixe(brut)
				if v == "" || (valeur != "" && valeur != v) {
					return "", PasClair
				}
				valeur = v
			}
		}
		longue, premiere = false, false
		if err != nil {
			break
		}
	}
	if valeur == "" {
		return "", PasClair
	}
	return valeur, Resolu
}

// valeurPortal : la valeur brute d'une ligne « SET portal … », sans ses guillemets.
func valeurPortal(ligne []byte) ([]byte, bool) {
	l := bytes.TrimSpace(ligne)
	i := bytes.IndexAny(l, " \t")
	if i < 0 || !bytes.EqualFold(l[:i], []byte("SET")) {
		return nil, false
	}
	l = bytes.TrimLeft(l[i:], " \t")
	i = bytes.IndexAny(l, " \t")
	if i < 0 || !bytes.EqualFold(l[:i], []byte("portal")) {
		return nil, false
	}
	v := bytes.TrimLeft(l[i:], " \t")
	if n := len(v); n >= 2 && (v[0] == '"' || v[0] == '\'') && v[n-1] == v[0] {
		v = v[1 : n-1]
	}
	return v, true
}

// regionFixe : la seule sortie possible de ce paquet, prise dans une table fixe. Le
// résultat n'est jamais un morceau du fichier. Toute autre valeur (test, public-test…)
// rend une chaîne vide.
func regionFixe(brut []byte) string {
	switch string(bytes.ToUpper(brut)) {
	case "US":
		return "US"
	case "KR":
		return "KR"
	case "EU":
		return "EU"
	case "TW":
		return "TW"
	case "CN":
		return "CN"
	}
	return ""
}

// RepliObjets applique le repli à un objet JSON { nom : { …, "region": … } } : les
// périmètres d'un fichier, les contextes de ses statistiques. Un membre marqué beta ou
// ptr n'est jamais une cible : il garde sa valeur, donc le refus du site, car le dossier
// _forever_ a aussi servi à la bêta et ces observations ne doivent pas entrer dans une
// région de sortie. valeur n'est appelée que si au moins une cible existe. Un objet sans
// remplacement est rendu octet pour octet ; seuls les membres remplacés changent.
func RepliObjets(objets []byte, valeur func() (string, bool)) ([]byte, bool) {
	ms, err := schema.Membres(objets)
	if err != nil {
		return objets, false
	}
	type cible struct {
		i, j int
		in   []schema.Membre
	}
	var cibles []cible
	for i, m := range ms {
		in, err := schema.Membres(m.V)
		if err != nil {
			continue
		}
		j, connu, test := -1, false, false
		for k, x := range in {
			switch x.K {
			case "region":
				j, connu = k, regionConnue(x.V)
			case "environment":
				test = environnementTest(x.V)
			}
		}
		if !connu && !test {
			cibles = append(cibles, cible{i, j, in})
		}
	}
	if len(cibles) == 0 {
		return objets, false
	}
	v, ok := valeur()
	if !ok {
		return objets, false
	}
	remplace, err := json.Marshal(v)
	if err != nil {
		return objets, false
	}
	for _, c := range cibles {
		if c.j >= 0 {
			c.in[c.j].V = remplace
		} else {
			c.in = append(c.in, schema.Membre{K: "region", V: remplace})
		}
		ms[c.i].V = schema.Assemble(c.in)
	}
	return schema.Assemble(ms), true
}

// RepliMeta applique le repli aux contextes de l'en-tête d'un fichier de statistiques.
func RepliMeta(meta []byte, valeur func() (string, bool)) []byte {
	ms, err := schema.Membres(meta)
	if err != nil {
		return meta
	}
	for i, m := range ms {
		if m.K != "contexts" {
			continue
		}
		out, change := RepliObjets(m.V, valeur)
		if !change {
			return meta
		}
		ms[i].V = out
		return schema.Assemble(ms)
	}
	return meta
}

// environnementTest : l'environnement écrit par l'addon (IsTestBuild) vaut beta, ou ptr
// pour un client de test. Absent ou live : le membre reste éligible au repli.
func environnementTest(brut json.RawMessage) bool {
	var v string
	if err := json.Unmarshal(brut, &v); err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "beta", "ptr":
		return true
	}
	return false
}

// regionConnue lit une région telle qu'écrite dans le JSON : un texte ou un nombre.
// Null, absente ou d'un autre type vaut « vide ».
func regionConnue(brut json.RawMessage) bool {
	d := json.NewDecoder(bytes.NewReader(brut))
	d.UseNumber()
	var v any
	if err := d.Decode(&v); err != nil {
		return false
	}
	switch x := v.(type) {
	case string:
		return CodeConnu(x)
	case json.Number:
		return CodeConnu(x.String())
	}
	return false
}
