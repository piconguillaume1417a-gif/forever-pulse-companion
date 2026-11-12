// Package i18n : textes de l'interface, une table par langue.
//
// Le journal technique et les sorties de la ligne de commande restent en français ;
// tout ce que la fenêtre, le menu et les notifications affichent passe par T.
//
// Ajouter une langue : un fichier textes_<code>.go (mêmes clés que textes_en.go,
// mêmes verbes de format dans le même ordre) et une ligne dans langues. Une clé
// absente d'une langue s'affiche en anglais.
package i18n

import (
	"fmt"
	"sync/atomic"
)

type Lang string

const (
	FR Lang = "fr"
	EN Lang = "en"
)

// langue : une ligne de la table des langues.
type langue struct {
	code   Lang
	nom    string            // nom de la langue dans cette langue (liste de choix)
	court  string            // libellé du sélecteur de la fenêtre
	textes map[string]string // clé → texte ; clé absente = anglais
}

// langues : dans l'ordre de la liste de choix. L'anglais sert de repli.
var langues = []langue{
	{FR, "Français", "FR", fr},
	{EN, "English", "EN", en},
}

// parCode : index de langues ; Toutes : les codes, dans l'ordre de la liste de choix.
var (
	parCode = map[Lang]*langue{}
	Toutes  []Lang
)

var cur atomic.Value

func init() {
	for i := range langues {
		parCode[langues[i].code] = &langues[i]
		Toutes = append(Toutes, langues[i].code)
	}
	cur.Store(FR)
}

// Parse : un code de la table (« fr », « en »…) ; toute autre valeur est refusée.
func Parse(s string) (Lang, bool) {
	if _, ok := parCode[Lang(s)]; ok {
		return Lang(s), true
	}
	return "", false
}

// Set choisit la langue ; une valeur inconnue est ignorée.
func Set(l Lang) {
	if _, ok := Parse(string(l)); ok {
		cur.Store(l)
	}
}

func Get() Lang { return cur.Load().(Lang) }

// Choisie : la langue du réglage si elle est valide, sinon l'anglais (0.6.0 : la
// fenêtre, le menu et les notifications démarrent en anglais tant qu'aucune langue
// n'a été choisie ; le journal et la ligne de commande restent en français).
func Choisie(reglage string) Lang {
	if l, ok := Parse(reglage); ok {
		return l
	}
	return EN
}

// texte : le texte de la clé dans la langue l, sinon en anglais.
func texte(l Lang, key string) (string, bool) {
	if g, ok := parCode[l]; ok {
		if s, ok := g.textes[key]; ok && s != "" {
			return s, true
		}
	}
	s, ok := en[key]
	return s, ok
}

// T renvoie le texte de la clé dans la langue choisie (anglais si la clé y
// manque), formaté avec args. Une clé inconnue est renvoyée telle quelle.
func T(key string, args ...any) string {
	s, ok := texte(Get(), key)
	if !ok {
		return key
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// Nom : le nom d'une langue dans cette langue (liste de choix) ; anglais si inconnue.
func Nom(l Lang) string {
	if g, ok := parCode[l]; ok {
		return g.nom
	}
	return parCode[EN].nom
}

// Court : le libellé de la langue dans le sélecteur de la fenêtre (« FR », « EN »…).
func Court(l Lang) string {
	if g, ok := parCode[l]; ok {
		return g.court
	}
	return parCode[EN].court
}
