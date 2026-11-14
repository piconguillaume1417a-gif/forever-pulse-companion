package stats

// Rang JcJ des joueurs croisés (addon ForeverPulse, champ pv d'une fiche).
//
// Une clé facultative de personnages[guid], ignorée par les compagnons
// antérieurs ; la version du stockage reste 4 et aucune version de sous-format
// n'est écrite :
//
//	personnages[guid].pv = "<t>:<rang>:<victoires honorables>"   ex. "1763125200:3:1002"
//
// t : secondes Unix de la lecture ; rang : 0 à 14 ; victoires honorables : total
// à vie, 0 à 2 147 483 647. Comme pour ta, un pv illisible ou hors bornes est
// écarté et compté (Fichier.PvpIllisibles, Fichier.PvpHorsBornes), la fiche part
// sans lui ; il n'est jamais remplacé par zéro.

import (
	"regexp"
	"strconv"
	"time"

	"wowsync/internal/schema"
)

const (
	maxRangPvp      = 14
	maxVictoiresPvp = 2_147_483_647
)

var pvRe = regexp.MustCompile(`^([0-9]{1,12}):([0-9]{1,2}):([0-9]{1,10})$`)

// Pvp : le dernier rang JcJ lu pour un personnage.
type Pvp struct {
	T         int64
	Rang      int
	Victoires int64
}

// decodePvp lit pv. illisible : la valeur n'est pas une chaîne au format ;
// horsBornes : elle le suit mais une valeur sort des bornes du site (date nulle ou
// de plus de 5 min dans le futur, rang au-delà de 14, victoires au-delà de 2^31-1).
func decodePvp(v any, now time.Time) (p *Pvp, illisible, horsBornes bool) {
	s, ok := v.(string)
	if !ok {
		return nil, true, false
	}
	m := pvRe.FindStringSubmatch(s)
	if m == nil {
		return nil, true, false
	}
	p = &Pvp{}
	p.T, _ = strconv.ParseInt(m[1], 10, 64) // 12 chiffres au plus : jamais d'erreur
	if p.T <= 0 || time.Unix(p.T, 0).After(now.Add(avanceMax)) {
		return nil, false, true
	}
	p.Rang, _ = strconv.Atoi(m[2])
	if p.Rang > maxRangPvp {
		return nil, false, true
	}
	p.Victoires, _ = strconv.ParseInt(m[3], 10, 64)
	if p.Victoires > maxVictoiresPvp {
		return nil, false, true
	}
	return p, false, false
}

func (p *Pvp) objet() schema.Obj {
	return schema.Obj{
		{K: "read_at", V: rfc3339(p.T)}, {K: "rank", V: p.Rang}, {K: "honorable_kills", V: p.Victoires},
	}
}
