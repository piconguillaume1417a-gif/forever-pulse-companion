package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
	"wowsync/internal/app"
	"wowsync/internal/auctions"
	"wowsync/internal/store"
)

func auctionSummary(path string) (auctions.Result, error) {
	r, _, e := app.DecodeAuctionFile(path, time.Now())
	fmt.Printf("prix HdV : %d objets, %d lignes envoyables, %d jours attribués, %d jours sans faction prouvée\n", r.Counts["objets"], r.Counts["lignes"], r.Counts["jours_attribues"], r.Counts["jours_sans_faction_prouvee"])
	// Never print account paths, realm keys or values from private variables.
	fmt.Printf("objets par clé décodée : %v ; versions de royaume : %v\n", r.Objects, r.RealmVersions)
	raw, _ := json.Marshal(r.Counts)
	fmt.Printf("comptages : %s\n", raw)
	return r, e
}
func dryAuctions(st *store.Store, out string) error {
	markets, e := st.AuctionMarkets()
	if e != nil {
		return e
	}
	n := 0
	for _, market := range markets {
		for {
			p, e := st.AuctionNext(market, time.Now())
			if e != nil {
				return e
			}
			if p == nil {
				break
			}
			n++
			var b auctions.Body
			_ = json.Unmarshal(p.Body, &b)
			fmt.Printf("requête HdV %d : %d lignes, %d octets de JSON\n", n, len(b.Rows), len(p.Body))
			if out != "" {
				if e = os.MkdirAll(out, 0700); e != nil {
					return e
				}
				if e = os.WriteFile(filepath.Join(out, fmt.Sprintf("prix-%02d.json", n)), p.Body, 0600); e != nil {
					return e
				}
			}
			if e = st.AuctionAck(p, nil, time.Now()); e != nil {
				return e
			}
		}
	}
	fmt.Printf("%d requêtes HdV construites, aucune postée\n", n)
	return nil
}
