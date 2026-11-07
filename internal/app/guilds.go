package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
	"wowsync/internal/guilds"
	"wowsync/internal/sender"
)

// Called under the same upload mutex, after census and statistics. One snapshot
// per flush keeps this bounded even after a long offline period.
func (a *App) envoieGuild(ctx context.Context, manual bool) (bool, error) {
	if manual {
		a.prochainGuild = time.Time{}
		_ = a.Store.Set("guild_blocked", "")
	}
	if a.Store.Get("blocked") != "" || a.Store.Get("guild_blocked") != "" || a.Now().Before(a.prochainGuild) {
		return false, nil
	}
	g, e := a.Store.GuildPending()
	if e != nil {
		return false, e
	}
	if g == nil {
		return false, nil
	}
	token, e := a.Token()
	if e != nil {
		_ = a.Store.Set("blocked", BlocJeton)
		return true, e
	}
	body, e := json.Marshal(guilds.Body{Schema: 1, Companion: Version, Snapshots: []guilds.Snapshot{*g}})
	if e != nil {
		return true, e
	}
	r := a.Sender.PostGuild(ctx, token, body, []string{g.ID})
	a.Log.Printf("guild roster upload: %d members, HTTP %d", g.Total, r.Status)
	switch r.Verdict {
	case sender.Accepte:
		state, reason := "sent", ""
		if len(r.Reponse.Rejected) > 0 {
			state = "rejected"
			reason = r.Reponse.Rejected[0].Reason
		}
		a.echecsGuild = 0
		return true, a.Store.MarkGuild(g, state, reason)
	case sender.JetonRefuse:
		_ = a.Store.Set("blocked", BlocJeton)
	case sender.SourceCoupee:
		_ = a.Store.Set("guild_blocked", BlocSource)
	case sender.Incompatible:
		_ = a.Store.Set("guild_blocked", BlocSchema)
	case sender.Invalide, sender.TropGros:
		return true, a.Store.MarkGuild(g, "rejected", fmt.Sprintf("http_%d", r.Status))
	case sender.Quota:
		a.prochainGuild = a.Now().Add(r.RetryAfter)
	default:
		a.echecsGuild++
		a.prochainGuild = a.Now().Add(sender.Backoff(a.echecsGuild))
	}
	return true, fmt.Errorf("guild roster upload pending (HTTP %d)", r.Status)
}
