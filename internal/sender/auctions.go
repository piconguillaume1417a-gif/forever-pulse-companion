package sender

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const RouteAuctions = "/api/ingest/auctions"

func (s *Sender) PostAuctions(ctx context.Context, token string, body []byte) Resultat {
	var b struct {
		Rows []struct {
			ID string `json:"id"`
		} `json:"rows"`
	}
	if json.Unmarshal(body, &b) != nil {
		return Resultat{Verdict: Invalide, Detail: "corps HdV illisible"}
	}
	ids := make([]string, len(b.Rows))
	for i, x := range b.Rows {
		ids[i] = x.ID
	}
	r := s.envoie(ctx, token, body, RouteAuctions, "forever-pulse-auctions", "1", ids)
	if r.Verdict == Accepte {
		for _, x := range r.Reponse.Rejected {
			if x.BatchID != "" || (x.Reason != "no_value" && x.Reason != "invalid_row" && x.Reason != "retention_expired" && x.Reason != "capacity_reached") {
				return Resultat{Verdict: Panne, Detail: "accusé HdV non conforme"}
			}
		}
	}
	return r
}

// H9: no upper clamp may shorten a server's Retry-After. Missing or malformed
// headers use a conservative 30 minutes for this source only.
func auctionRetryAfter(v string, now time.Time) time.Duration {
	if n, e := strconv.ParseInt(strings.TrimSpace(v), 10, 64); e == nil && n >= 0 && n <= int64((1<<63-1)/int64(time.Second)) {
		return max(time.Second, time.Duration(n)*time.Second)
	}
	if t, e := http.ParseTime(v); e == nil {
		return max(time.Second, t.Sub(now))
	}
	return 30 * time.Minute
}
