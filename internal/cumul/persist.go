package cumul

import "database/sql"

// Changes : ce qui a changé depuis le dernier enregistrement.
type Changes struct {
	Lots            []string
	Scopes          map[string]*int64
	Persos          map[[2]string]*Entree
	PersosSupprimes [][2]string
	Noms            map[[2]string]string // "" = nom retiré
}

// Changements liste les lignes à réécrire.
func (c *Cumul) Changements() Changes {
	ch := Changes{Lots: c.lotsNeufs, Scopes: map[string]*int64{}, Persos: map[[2]string]*Entree{}, Noms: map[[2]string]string{}}
	for sid := range c.scopesSal {
		ch.Scopes[sid] = c.Scopes[sid].Depuis
	}
	for k := range c.sale {
		if p := c.Scopes[k[0]]; p != nil {
			if e := p.Persos[k[1]]; e != nil {
				ch.Persos[k] = e
			}
		}
	}
	for k := range c.supprime {
		ch.PersosSupprimes = append(ch.PersosSupprimes, k)
	}
	for k := range c.nomsSales {
		ch.Noms[k] = ""
		if p := c.Scopes[k[0]]; p != nil {
			ch.Noms[k] = p.Noms[k[1]]
		}
	}
	return ch
}

// Enregistre : les changements sont écrits, on repart d'une page blanche.
func (c *Cumul) Enregistre() { c.resetSale() }

// Chargement depuis la base.
func (c *Cumul) ChargeLots(ids []string) {
	c.LotsTraites = append(c.LotsTraites, ids...)
	for _, id := range ids {
		c.deja[id] = true
	}
}

func (c *Cumul) ChargePortee(sid string, d sql.NullInt64) {
	p := c.portee(sid)
	if d.Valid {
		v := d.Int64
		p.Depuis = &v
	}
}

func (c *Cumul) ChargeEntree(sid, cle string, e *Entree) { c.portee(sid).Persos[cle] = e }

func (c *Cumul) ChargeNom(sid, nom, cle string) { c.portee(sid).Noms[nom] = cle }
