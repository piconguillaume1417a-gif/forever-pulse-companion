package store

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wowsync/internal/auctions"
)

func auctionFixture(n int, sha string) auctions.Result {
	m := auctions.Market{ID: "18-beta-90-4619-PVP-Alliance", Project: 18, Environment: "beta", Region: 90, Realm: 4619, Ruleset: "PVP", Faction: "Alliance", Name: "PvP"}
	b := auctions.NewBody(sha, "3.11.0", "0.8.0", m)
	for i := 1; i <= n; i++ {
		b.Rows = append(b.Rows, auctions.Row{ID: fmt.Sprintf("%s/%d/2467", m.ID, i), Item: int64(i), Day: 2467, Basis: "auctionator/340/db8/unverified", High: "110", Current: "105"})
	}
	return auctions.Result{Bodies: []auctions.Body{b}, Counts: map[string]int{"jours_attribues": 1}}
}
func TestAuctionQueueIdempotenceOlderLossAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { s.Close() }()
	ctx := context.Background()
	now := time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)
	r := auctionFixture(1, strings.Repeat("a", 64))
	market := r.Bodies[0].Market.ID
	add := func(fresh int64, want int) {
		t.Helper()
		n, e := s.AddAuctions(ctx, "file", "signature", fresh, r)
		if e != nil || n != want {
			t.Fatalf("add %d %v expected %d", n, e, want)
		}
	}
	add(100, 1)
	add(100, 0)
	p, e := s.AuctionNext(market, now)
	if e != nil || p == nil {
		t.Fatal(e)
	}
	if e = s.AuctionStart(market, now); e != nil {
		t.Fatal(e)
	}
	if e = s.AuctionDelay(market, now.Add(1030*time.Second), ""); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if s.AuctionGate(market, now.Add(1029*time.Second)) {
		t.Fatal("H9 persisted retry shortened")
	}
	if !s.AuctionGate(market, now.Add(1800*time.Second)) {
		t.Fatal("H9 retry never expires")
	}
	p2, e := s.AuctionNext(market, now)
	if e != nil || p2.ID != p.ID || string(p2.Body) != string(p.Body) {
		t.Fatal("H9 snapshot lost on restart")
	}
	if e = s.AuctionAck(p, nil, now); e != nil {
		t.Fatal(e)
	}
	add(101, 0)
	r.Bodies[0].Rows[0].Current = ""
	add(102, 0)
	r.Bodies[0].Rows[0].High = "120"
	add(99, 0)
	add(103, 1)
	if s.AuctionGate(market, now.Add(1799*time.Second)) {
		t.Fatal("H9 new update before 30 minutes")
	}
	p, e = s.AuctionNext(market, now.Add(1800*time.Second))
	if e != nil || p == nil {
		t.Fatal(e)
	}
	// A newer candidate arriving while a request is in flight remains pending.
	r.Bodies[0].Rows[0].High = "130"
	r.Bodies[0].File.SHA = strings.Repeat("b", 64)
	r.Bodies[0].Update = auctions.Hash([]byte(r.Bodies[0].File.SHA + "|" + market))
	add(104, 1)
	if e = s.AuctionAck(p, nil, now.Add(1800*time.Second)); e != nil {
		t.Fatal(e)
	}
	if s.AuctionCounts()["pending"] != 1 {
		t.Fatal("in-flight ack consumed changed candidate")
	}
}
func TestAuctionPartsLimitsAndCatalogAcknowledgement(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r := auctionFixture(5001, strings.Repeat("c", 64))
	locale := "enUS"
	r.Bodies[0].Catalog = &auctions.Catalog{Version: 1, Locale: &locale, Items: []auctions.Item{{ID: 2589, Name: "Linen Cloth"}}}
	market := r.Bodies[0].Market.ID
	ctx := context.Background()
	now := time.Now().Truncate(time.Second)
	if _, e = s.AddAuctions(ctx, "file", "sig", 1, r); e != nil {
		t.Fatal(e)
	}
	n, lines, items := 0, 0, 0
	for {
		p, e := s.AuctionNext(market, now)
		if e != nil {
			t.Fatal(e)
		}
		if p == nil {
			break
		}
		var b auctions.Body
		if e = json.Unmarshal(p.Body, &b); e != nil {
			t.Fatal(e)
		}
		if len(b.Rows) > 5000 || len(p.Body) > auctions.MaxBody {
			t.Fatal("H3 H5 limits")
		}
		lines += len(b.Rows)
		if b.Catalog != nil {
			items += len(b.Catalog.Items)
		}
		n++
		if e = s.AuctionAck(p, nil, now); e != nil {
			t.Fatal(e)
		}
	}
	if n != 2 || lines != 5001 || items != 1 {
		t.Fatalf("chunk counts %d %d %d", n, lines, items)
	}
	if n, e := s.AddAuctions(ctx, "file", "sig2", 2, r); e != nil || n != 0 {
		t.Fatal("H4 catalog idempotence", n, e)
	}
}
func TestAuctionRejectionsAndOtherMarkets(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r := auctionFixture(3, strings.Repeat("d", 64))
	now := time.Now().Truncate(time.Second)
	market := r.Bodies[0].Market.ID
	if _, e = s.AddAuctions(context.Background(), "file", "sig", 1, r); e != nil {
		t.Fatal(e)
	}
	p, e := s.AuctionNext(market, now)
	if e != nil {
		t.Fatal(e)
	}
	rejects := map[string]string{r.Bodies[0].Rows[0].ID: "invalid_row", r.Bodies[0].Rows[1].ID: "retention_expired", r.Bodies[0].Rows[2].ID: "capacity_reached"}
	if e = s.AuctionAck(p, rejects, now); e != nil {
		t.Fatal(e)
	}
	c := s.AuctionCounts()
	if c["pending"] != 1 || c["rejected"] != 1 || c["expired"] != 1 {
		t.Fatal("H7 rejection states", c)
	}
	if s.AuctionGate(market, now.Add(6*time.Hour-time.Second)) || !s.AuctionGate(market, now.Add(6*time.Hour)) {
		t.Fatal("H7 capacity retry")
	}
	r.Bodies[0].Market.ID = "18-beta-90-4613-PVP-Horde"
	r.Bodies[0].Rows = nil
	r.Bodies[0].Catalog = &auctions.Catalog{Version: 1, Items: []auctions.Item{{ID: 1, Name: "Synthetic"}}}
	if _, e = s.AddAuctions(context.Background(), "other", "sig", 2, r); e != nil {
		t.Fatal(e)
	}
	if !s.AuctionGate(r.Bodies[0].Market.ID, now) {
		t.Fatal("H9 one market delays another")
	}
}

func TestAuctionWithdrawalOnAmbiguousDayAndStrictDeadline(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "queue.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	r := auctionFixture(1, strings.Repeat("e", 64))
	m := r.Bodies[0].Market.ID
	now := time.Unix(1791057600, 900000000)
	if _, e = s.AddAuctions(context.Background(), "source", "sig", 1, r); e != nil {
		t.Fatal(e)
	}
	p, e := s.AuctionNext(m, now)
	if e != nil || p == nil {
		t.Fatal(e)
	}
	if e = s.AuctionDelay(m, now.Add(1030*time.Second), ""); e != nil {
		t.Fatal(e)
	}
	if s.AuctionGate(m, now.Add(1030*time.Second-time.Nanosecond)) {
		t.Fatal("H9 deadline truncated to whole seconds")
	}
	if _, e = s.AddAuctions(context.Background(), "source", "newnote", 1, auctions.Result{Counts: map[string]int{"jour_multi_marches": 1}}); e != nil {
		t.Fatal(e)
	}
	if s.AuctionCounts()["pending"] != 0 {
		t.Fatal("J4 ambiguous day left pending")
	}
	if p, e = s.AuctionNext(m, now); e != nil || p != nil {
		t.Fatal("J4 ambiguous snapshot survived")
	}
	if n, e := s.AddAuctions(context.Background(), "source", "proven", 2, r); e != nil || n != 1 {
		t.Fatal("J4 withdrawn day cannot return after proof", n, e)
	}
	if e = s.SuspendAuctions("source"); e != nil || s.AuctionCounts()["pending"] != 0 {
		t.Fatal("truncated read left pending", e)
	}
}
