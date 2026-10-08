package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"
	"wowsync/internal/auctions"
	"wowsync/internal/i18n"
	"wowsync/internal/lua"
	"wowsync/internal/sender"
	"wowsync/internal/watch"
)

func AuctionPath(path string) bool { return strings.EqualFold(filepath.Base(path), "Auctionator.lua") }
func ClientFolder(path string) string {
	for p := filepath.Dir(path); ; p = filepath.Dir(p) {
		base := filepath.Base(p)
		if strings.HasPrefix(base, "_") && strings.HasSuffix(base, "_") {
			return base
		}
		if p == filepath.Dir(p) {
			return ""
		}
	}
}

// DecodeAuctionFile reads only the sibling ForeverPulse.lua, never another
// account's notes or Auctionator's backup. All errors are fixed, count-only text.
func DecodeAuctionFile(path string, now time.Time) (auctions.Result, int64, error) {
	r := auctions.Result{Counts: map[string]int{}}
	if !AuctionPath(path) {
		return r, 0, errors.New("fichier de prix non pris en charge")
	}
	before, e := os.Stat(path)
	if e != nil {
		return r, 0, errors.New("fichier Auctionator inaccessible")
	}
	data, sha, e := watch.Read(path)
	if e != nil {
		return r, 0, errors.New("lecture Auctionator impossible")
	}
	notePath := filepath.Join(filepath.Dir(path), "ForeverPulse.lua")
	var hdv *lua.Table
	var catalog *auctions.Catalog
	addon := ""
	nb, ne := os.Stat(notePath)
	note, _, e := watch.Read(notePath)
	if e == nil {
		v, e := lua.Parse(note, map[string]bool{"ForeverPulseCensusDB": true, "ForeverPulseStatsDB": true})
		if e != nil {
			return r, 0, errors.New("note Forever Pulse illisible")
		}
		census := lua.AsTable(v["ForeverPulseCensusDB"])
		hdv = lua.AsTable(census.Get("hdv"))
		addon = lua.Str(lua.AsTable(census.Get("addon")).Get("version"))
		catalog = auctions.ReadCatalog(lua.AsTable(lua.AsTable(v["ForeverPulseStatsDB"]).Get("objets")))
	} else if !os.IsNotExist(e) {
		return r, 0, errors.New("lecture de la note impossible")
	}
	after, e := os.Stat(path)
	if e != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return r, 0, errors.New("fichier Auctionator en cours d'écriture")
	}
	if ne == nil {
		na, e := os.Stat(notePath)
		if e != nil || nb.Size() != na.Size() || !nb.ModTime().Equal(na.ModTime()) {
			return r, 0, errors.New("note en cours d'écriture")
		}
	}
	r, e = auctions.Decode(data, sha, auctions.ReadNotes(hdv, ClientFolder(path)), catalog, addon, Version, now)
	return r, before.ModTime().UnixNano(), e
}
func (a *App) ProcessAuctions(ctx context.Context, path string) (int, error) {

	id := auctions.Hash([]byte(filepath.Clean(path)))
	sig, e := watch.PairSignature(path)
	if e != nil {
		return 0, errors.New("fichier Auctionator inaccessible")
	}
	if a.Store.AuctionFileKnown(id, sig) {
		return 0, nil
	}
	defer debug.FreeOSMemory()
	r, fresh, e := DecodeAuctionFile(path, a.Now())
	if e != nil {
		_ = a.Store.Set("auction_read_error/"+id, e.Error())
		if suspend := a.Store.SuspendAuctions(id); suspend != nil {
			return 0, suspend
		}
		return 0, e
	}
	a.mu.Lock()
	added, e := a.Store.AddAuctions(ctx, id, sig, fresh, r)
	a.mu.Unlock()
	if e != nil {
		return 0, e
	}
	raw, _ := json.Marshal(r.Counts)
	a.Log.Printf("prix HdV : %s ; %d lignes nouvelles ou modifiées en file", raw, added)
	_ = a.Store.Set("auction_read_error/"+id, "")
	if r.Counts["format_non_pris_en_charge"] > 0 {
		_ = a.Store.Set("auction_read_error/"+id, "format Auctionator non pris en charge")
	}
	a.changed()
	return added, nil
}
func (a *App) envoieAuctions(ctx context.Context, manual bool) (attempted bool, err error) {
	if manual {
		if e := a.Store.AuctionUnblock(); e != nil {
			return attempted, e
		}
	}
	markets, e := a.Store.AuctionMarkets()
	if e != nil {
		return attempted, e
	}
	var last error
	for _, m := range markets {
		for a.Store.AuctionGate(m, a.Now()) {
			p, e := a.Store.AuctionNext(m, a.Now())
			if e != nil {
				return attempted, e
			}
			if p == nil {
				break
			}
			token, e := a.Token()
			if e != nil {
				last = errors.New("jeton absent pour les prix HdV")
				break
			}
			if e = a.Store.AuctionStart(m, a.Now()); e != nil {
				return attempted, e
			}
			attempted = true
			r := a.Sender.PostAuctions(ctx, token, p.Body)
			if r.Verdict == sender.Accepte {
				rejected := map[string]string{}
				for _, x := range r.Reponse.Rejected {
					rejected[x.ID] = x.Reason
				}
				if e = a.Store.AuctionAck(p, rejected, a.Now()); e != nil {
					return attempted, e
				}
				_ = a.Store.Set("auctions_last_send", a.Now().Format(time.RFC3339))
				_ = a.Store.Set("auctions_error", "")
				continue
			}
			until := a.Now().Add(sender.Backoff(a.Store.AuctionFailures(m) + 1))
			blocked := ""
			switch r.Verdict {
			case sender.Quota:
				until = a.Now().Add(r.RetryAfter)
			case sender.Incompatible, sender.Invalide:
				blocked = r.Detail
				if r.Reponse.Error == "unsupported_schema" {
					blocked = "unsupported_schema"
				}
			case sender.JetonRefuse:
				blocked = "jeton refusé"
			case sender.TropGros:
				if e = a.Store.AuctionSplit(p); e == nil {
					continue
				}
				blocked = "ligne HdV trop grande"
			}
			if e = a.Store.AuctionDelay(m, until, blocked); e != nil {
				return attempted, e
			}
			last = fmt.Errorf("prix HdV : %s", r.Detail)
			_ = a.Store.Set("auctions_error", last.Error())
			break
		}
	}
	return attempted, last
}
func (a *App) auctionDetails() []string {
	c := a.Store.AuctionCounts()
	d := a.Store.AuctionDayCounts()
	out := []string{i18n.T("det.auctions", c["sent"], c["pending"], d["jours_sans_faction_prouvee"])}
	raw, _ := json.Marshal(d)
	out = append(out, i18n.T("det.auctiondays", string(raw)))
	for _, msg := range a.Store.AuctionReadErrors() {
		out = append(out, i18n.T("det.auctionerror", msg))
	}
	for _, key := range []string{"auctions_error"} {
		if e := a.Store.Get(key); e != "" {
			out = append(out, i18n.T("det.auctionerror", e))
		}
	}
	if date := a.Store.Get("auctions_last_send"); date != "" {
		out = append(out, i18n.T("det.auctionsent", date))
	}
	return out
}
