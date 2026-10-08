package app

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wowsync/internal/auctions"
	f "wowsync/internal/auctionstest"
	"wowsync/internal/logx"
	"wowsync/internal/sender"
	"wowsync/internal/store"
)

func TestAuctionFilePrivacyAndTruncation(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "_classic_beta_", "WTF", "Account", "SyntheticAccount", "SavedVariables")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(dir, "Auctionator.lua")
	note := filepath.Join(dir, "ForeverPulse.lua")
	data := f.File(f.Prices(110, 0, 105), 8)
	for _, key := range []string{"AUCTIONATOR_POSTING_HISTORY", "AUCTIONATOR_SHOPPING_LISTS", "AUCTIONATOR_RECENT_SEARCHES", "AUCTIONATOR_SELLING_GROUPS", "AUCTIONATOR_CONFIG", "AUCTIONATOR_SAVEDVARS", "AUCTIONATOR_CHARACTER_CONFIG", "AUCTIONATOR_VENDOR_PRICE_CACHE"} {
		data = append(data, []byte(key+" = { n=\"PrivateCanaryNeverSent\" }\n")...)
	}
	if e := os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(note, f.Forever(f.Note), 0600); e != nil {
		t.Fatal(e)
	}
	now := time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)
	r, _, e := DecodeAuctionFile(path, now)
	if e != nil || len(r.Bodies) != 1 {
		t.Fatal(e, r.Counts)
	}
	raw, _ := json.Marshal(r.Bodies[0])
	if bytes.Contains(raw, []byte("PrivateCanaryNeverSent")) || bytes.Contains(raw, []byte("AUCTIONATOR_")) || r.Bodies[0].File.AddonVersion == nil || *r.Bodies[0].File.AddonVersion != "3.11.0" {
		t.Fatal("L2 privacy or addon provenance")
	}
	os.Remove(note)
	r, _, e = DecodeAuctionFile(path, now)
	if e != nil || len(r.Bodies) > 0 || r.Counts["note_absente"] != 1 {
		t.Fatal("J1 note absent", e, r.Counts)
	}
	os.WriteFile(note, []byte("ForeverPulseCensusDB = {"), 0600)
	if _, _, e = DecodeAuctionFile(path, now); e == nil {
		t.Fatal("truncated sibling note accepted")
	}
	os.WriteFile(note, f.Forever(f.Note), 0600)
	os.WriteFile(path, data[:len(data)/2], 0600)
	if _, _, e = DecodeAuctionFile(path, now); e == nil {
		t.Fatal("truncated Auctionator accepted")
	}
}
func TestAuctionsHTTPIsolationAckRetryAndRestart(t *testing.T) {
	for _, code := range []int{200, 403, 404, 409, 429, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "queue.db")
			now := time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)
			calls := 0
			first := true
			hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != sender.RouteAuctions {
					t.Error("wrong route")
				}
				calls++
				gz, e := gzip.NewReader(r.Body)
				if e != nil {
					t.Fatal(e)
				}
				raw, _ := io.ReadAll(gz)
				gz.Close()
				var b auctions.Body
				_ = json.Unmarshal(raw, &b)
				if r.Header.Get("X-Source") != "forever-pulse-auctions" || r.Header.Get("X-Schema") != "1" {
					t.Error("H1 headers")
				}
				w.Header().Set("Content-Type", "application/json")
				if first {
					first = false
					switch code {
					case 200:
						w.WriteHeader(200)
						fmt.Fprint(w, `{"accepted":1,"duplicates":0,"rejected":[]}`)
					case 429:
						w.Header().Set("Retry-After", "1030")
						w.WriteHeader(429)
						fmt.Fprint(w, `{"error":"market_window","retry_after":1030}`)
					default:
						w.WriteHeader(code)
						fmt.Fprintf(w, `{"error":%q}`, map[int]string{403: "source_disabled", 404: "not_found", 409: "market_source_conflict", 500: "internal"}[code])
					}
					return
				}
				w.WriteHeader(202)
				fmt.Fprintf(w, `{"accepted":%d,"duplicates":0,"rejected":[]}`, len(b.Rows))
			}))
			defer hs.Close()
			st, e := store.Open(path)
			if e != nil {
				t.Fatal(e)
			}
			log := &logx.Logger{}
			a, _ := New(st, log, sender.New(hs.URL, Version))
			a.Now = func() time.Time { return now }
			a.Token = func() (string, error) { return jeton, nil }
			m := auctions.Market{ID: "18-beta-90-4619-PVP-Alliance", Project: 18, Environment: "beta", Region: 90, Realm: 4619, Ruleset: "PVP", Faction: "Alliance", Name: "PvP"}
			b := auctions.NewBody(strings.Repeat("a", 64), "3.11.0", Version, m)
			b.Rows = []auctions.Row{{ID: m.ID + "/2589/2467", Item: 2589, Day: 2467, Basis: "auctionator/340/db8/unverified", High: "110"}}
			if _, e = st.AddAuctions(context.Background(), "file", "sig", 1, auctions.Result{Bodies: []auctions.Body{b}}); e != nil {
				t.Fatal(e)
			}
			if e = a.Flush(context.Background(), false); e == nil {
				t.Fatal("unacknowledged upload reported success")
			}
			if st.AuctionCounts()["pending"] != 1 || st.Get("blocked") != "" || st.Get("stats_blocked") != "" {
				t.Fatal("H8 queues lost or other source blocked")
			}
			st.Close()
			st, e = store.Open(path)
			if e != nil {
				t.Fatal(e)
			}
			defer st.Close()
			a, _ = New(st, log, sender.New(hs.URL, Version))
			a.Now = func() time.Time { return now }
			a.Token = func() (string, error) { return jeton, nil }
			if code == 429 {
				now = now.Add(1029 * time.Second)
				_ = a.Flush(context.Background(), true)
				if calls != 1 {
					t.Fatal("H9 manual bypassed persisted Retry-After")
				}
				now = now.Add(771 * time.Second)
			} else {
				now = now.Add(30 * time.Minute)
			}
			if code == 409 {
				_ = a.Flush(context.Background(), false)
				if calls != 1 {
					t.Fatal("H8 conflict retried automatically")
				}
				return
			}
			if e = a.Flush(context.Background(), false); e != nil {
				t.Fatal(e)
			}
			if st.AuctionCounts()["sent"] != 1 || calls != 2 {
				t.Fatal("H7 exact acknowledgement or restart", calls, st.AuctionCounts())
			}
		})
	}
}
func TestAuctionNetworkFailureThenResume(t *testing.T) {
	st, e := store.Open(filepath.Join(t.TempDir(), "queue.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	now := time.Now()
	a, _ := New(st, &logx.Logger{}, sender.New("http://127.0.0.1:1", Version))
	a.Now = func() time.Time { return now }
	a.Token = func() (string, error) { return jeton, nil }
	m := auctions.Market{ID: "18-beta-90-4619-PVP-Alliance"}
	b := auctions.NewBody(strings.Repeat("b", 64), "", Version, m)
	b.Rows = []auctions.Row{{ID: m.ID + "/1/2467", Item: 1, Day: 2467, Basis: "auctionator/340/db8/unverified", High: "1"}}
	if _, e = st.AddAuctions(context.Background(), "f", "s", 1, auctions.Result{Bodies: []auctions.Body{b}}); e != nil {
		t.Fatal(e)
	}
	if e = a.Flush(context.Background(), false); e == nil || st.AuctionCounts()["pending"] != 1 {
		t.Fatal("network lost queue")
	}
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		fmt.Fprint(w, `{"accepted":1,"duplicates":0,"rejected":[]}`)
	}))
	defer hs.Close()
	a.Sender = sender.New(hs.URL, Version)
	now = now.Add(time.Minute)
	if e = a.Flush(context.Background(), false); e != nil || st.AuctionCounts()["sent"] != 1 {
		t.Fatal("network recovery", e)
	}
}

func TestAuctionsPrivacyInActualPOSTAndChangeNotification(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "_classic_beta_", "SavedVariables")
	os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, "Auctionator.lua")
	data := f.File(f.Prices(110, 0, 105), 8)
	for _, k := range []string{"AUCTIONATOR_POSTING_HISTORY", "AUCTIONATOR_SHOPPING_LISTS", "AUCTIONATOR_RECENT_SEARCHES", "AUCTIONATOR_SELLING_GROUPS", "AUCTIONATOR_CONFIG", "AUCTIONATOR_SAVEDVARS", "AUCTIONATOR_CHARACTER_CONFIG", "AUCTIONATOR_VENDOR_PRICE_CACHE"} {
		data = append(data, []byte(k+" = { canary = \"PersonalPostedBodyCanary\" }\n")...)
	}
	os.WriteFile(path, data, 0600)
	os.WriteFile(filepath.Join(dir, "ForeverPulse.lua"), f.Forever(f.Note), 0600)
	calls := 0
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gz, e := gzip.NewReader(r.Body)
		if e != nil {
			t.Error(e)
			return
		}
		raw, _ := io.ReadAll(gz)
		gz.Close()
		for _, k := range []string{"AUCTIONATOR_", "PersonalPostedBodyCanary", "occurred_at", "SyntheticAccount"} {
			if bytes.Contains(raw, []byte(k)) {
				t.Errorf("L2 forbidden private data in posted body: %s", k)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		fmt.Fprint(w, `{"accepted":1,"duplicates":0,"rejected":[]}`)
	}))
	defer hs.Close()
	st, e := store.Open(filepath.Join(t.TempDir(), "queue.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	a, e := New(st, &logx.Logger{}, sender.New(hs.URL, Version))
	if e != nil {
		t.Fatal(e)
	}
	a.Token = func() (string, error) { return jeton, nil }
	a.Now = func() time.Time { return time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC) }
	ctx := context.Background()
	if _, e = a.ProcessAuctions(ctx, path); e != nil {
		t.Fatal(e)
	}
	changes := 0
	a.OnChange = func() { changes++; _ = a.Etat() }
	if e = a.Flush(ctx, false); e != nil {
		t.Fatal(e)
	}
	if calls != 1 || changes != 1 {
		t.Fatal("POST or UI notification missing", calls, changes)
	}
	if e = a.Flush(ctx, false); e != nil || calls != 1 || changes != 1 {
		t.Fatal("idle flush changed state")
	}
}

func TestAuctionRouteFailureDoesNotDelayCensusOrStats(t *testing.T) {
	b := nouveauBancStats(t)
	ctx := context.Background()
	m := auctions.Market{ID: "18-beta-90-4619-PVP-Alliance"}
	body := auctions.NewBody(strings.Repeat("c", 64), "", Version, m)
	body.Rows = []auctions.Row{{ID: m.ID + "/1/2467", Item: 1, Day: 2467, Basis: "auctionator/340/db8/unverified", High: "1"}}
	if _, e := b.st.AddAuctions(ctx, "prices", "sig", 1, auctions.Result{Bodies: []auctions.Body{body}}); e != nil {
		t.Fatal(e)
	}
	_ = b.a.Flush(ctx, false) // synthetic server has no auction route (404)
	p := b.fichier("ForeverPulse.lua", fichierStats(123, 1790505847, map[string][]string{"00000001": {"1790505847|20|n|60:7"}}))
	if _, e := b.a.ProcessFile(ctx, p); e != nil {
		t.Fatal(e)
	}
	if e := b.a.Flush(ctx, false); e != nil {
		t.Fatal(e)
	}
	if b.srv.appelsLots != 1 || b.srv.appelsStats != 1 || b.st.AuctionCounts()["pending"] != 1 {
		t.Fatal("H8 prices delayed other flows")
	}
}

func TestUnsupportedAuctionFormatRemainsVisibleAcrossAccounts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "_classic_beta_")
	os.MkdirAll(dir, 0700)
	path := filepath.Join(dir, "Auctionator.lua")
	os.WriteFile(path, f.File(f.Prices(110, 0, 105), 9), 0600)
	st, e := store.Open(filepath.Join(t.TempDir(), "queue.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	a, e := New(st, &logx.Logger{}, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.ProcessAuctions(context.Background(), path); e == nil {
		t.Fatal("unsupported DB accepted")
	}
	other := filepath.Join(dir, "other", "Auctionator.lua")
	os.MkdirAll(filepath.Dir(other), 0700)
	os.WriteFile(other, f.File(f.Prices(110, 0, 105), 8), 0600)
	if _, e = a.ProcessAuctions(context.Background(), other); e != nil {
		t.Fatal(e)
	}
	if len(st.AuctionReadErrors()) != 1 {
		t.Fatal("other account cleared format error")
	}
	os.WriteFile(path, f.File(f.Prices(110, 0, 105), 8), 0600)
	if _, e = a.ProcessAuctions(context.Background(), path); e != nil {
		t.Fatal(e)
	}
	if len(st.AuctionReadErrors()) != 0 {
		t.Fatal("corrected format error not cleared")
	}
}
