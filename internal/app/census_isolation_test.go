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
	"strings"
	"testing"

	"wowsync/internal/sender"
	"wowsync/internal/store"
)

// Adds an independent, strict census refusal in front of the existing synthetic
// census/statistics receiver. The real credential getter is always replaced by
// nouveauBancStats; every request stays on an httptest numeric loopback server.
func censusFaultServer(t *testing.T, b *bancStats, refuse func([]byte) (int, string)) *int {
	t.Helper()
	b.hs.Close()
	calls := new(int)
	b.hs = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == sender.Route {
			*calls++
			compressed, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			zr, err := gzip.NewReader(bytes.NewReader(compressed))
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			body, err := io.ReadAll(zr)
			_ = zr.Close()
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(compressed))
			if code, reason := refuse(body); code != 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(code)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": reason})
				return
			}
		}
		b.srv.ServeHTTP(w, r)
	}))
	t.Cleanup(b.hs.Close)
	b.a.Sender = sender.New(b.hs.URL, Version)
	return calls
}

func censusWithStatistics(t *testing.T, seed, lots int) []byte {
	t.Helper()
	stats := fichierStats(seed, 1790505847, map[string][]string{"00000001": {"1790505847|20|s|60:1,1488:2"}})
	i := bytes.Index(stats, []byte("ForeverPulseStatsDB ="))
	if i < 0 {
		t.Fatal("fixture missing statistics")
	}
	return append(fixture(lots, 3, seed), stats[i:]...)
}

func TestCensusInvalidBodyIsolatesOneBatchAndStillSendsStatistics(t *testing.T) {
	b := nouveauBancStats(t)
	const bad = "00000001-0000-4000-8000-000000000001"
	calls := censusFaultServer(t, b, func(body []byte) (int, string) {
		if bytes.Contains(body, []byte(bad)) {
			return 400, "invalid_body"
		}
		return 0, ""
	})
	data := censusWithStatistics(t, 1, 3)
	if _, err := b.a.ProcessFile(context.Background(), b.fichier("mixed.lua", data)); err != nil {
		t.Fatal(err)
	}
	if err := b.a.Flush(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	e := b.a.Etat()
	if e.Pending != 0 || e.Rejected != 1 || e.Sent != 2 || e.StatsPending != 0 || e.StatsSent != 1 || e.StatsRejected != 0 {
		t.Fatalf("invalid batch damaged independent queues: %+v", e)
	}
	if b.st.Get("blocked") != "" || b.st.Get("stats_blocked") != "" {
		t.Fatal("isolated invalid_body left a global block")
	}
	if e.Code == "allsent" {
		t.Fatal("rejected census was presented as entirely sent")
	}
	if len(b.srv.lots) != 2 || b.srv.lots[bad] || len(b.srv.fiches) != 1 {
		t.Fatal("synthetic receiver did not get exactly valid census and statistics")
	}
	before := *calls
	if _, err := b.a.ProcessFile(context.Background(), b.fichier("mixed.bak", append(data, '\n'))); err != nil {
		t.Fatal(err)
	}
	if err := b.a.Flush(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if *calls != before {
		t.Fatal("already confirmed or individually rejected census was needlessly retransmitted")
	}
}

func TestCensusGlobalAuthAndSourceRefusalsSurviveRestart(t *testing.T) {
	for _, tc := range []struct {
		status          int
		reason, blocked string
	}{
		{401, "token_required", BlocJeton}, {401, "token_invalid", BlocJeton}, {401, "token_revoked", BlocJeton}, {403, "token_revoked", BlocJeton},
		{403, "source_disabled", BlocSource}, {400, "invalid_source", BlocSource}, {400, "invalid_content_type", BlocSchema}, {400, "invalid_encoding", BlocSchema},
	} {
		t.Run(fmt.Sprintf("%d_%s", tc.status, tc.reason), func(t *testing.T) {
			b := nouveauBancStats(t)
			calls := censusFaultServer(t, b, func([]byte) (int, string) { return tc.status, tc.reason })
			if _, err := b.a.ProcessFile(context.Background(), b.fichier("blocked.lua", censusWithStatistics(t, 1, 3))); err != nil {
				t.Fatal(err)
			}
			if err := b.a.Flush(context.Background(), false); err == nil {
				t.Fatal("global refusal ignored")
			}
			e := b.a.Etat()
			if e.Pending != 3 || e.Rejected != 0 || e.Sent != 0 || e.StatsPending != 1 || e.StatsSent != 0 || b.st.Get("blocked") != tc.blocked {
				t.Fatalf("global refusal lost data or failed to block: %+v block=%q", e, b.st.Get("blocked"))
			}
			if err := b.st.Close(); err != nil {
				t.Fatal(err)
			}
			b.ouvre()
			_ = b.a.Flush(context.Background(), false)
			if *calls != 1 {
				t.Fatal("restart retried a globally forbidden source/token")
			}
		})
	}
}

func TestCensusUnknown400And403PreservePendingDataWithoutFalseGlobalBlock(t *testing.T) {
	for _, status := range []int{400, 403} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			b := nouveauBancStats(t)
			censusFaultServer(t, b, func([]byte) (int, string) { return status, "synthetic_unknown_failure" })
			if _, err := b.a.ProcessFile(context.Background(), b.fichier("unknown.lua", censusWithStatistics(t, 1, 3))); err != nil {
				t.Fatal(err)
			}
			if err := b.a.Flush(context.Background(), false); err == nil {
				t.Fatal("unknown refusal ignored")
			}
			e := b.a.Etat()
			if e.Pending != 3 || e.Rejected != 0 || e.StatsPending != 1 || b.st.Get("blocked") != "" {
				t.Fatalf("unknown refusal destroyed data or invented a source/auth decision: %+v", e)
			}
			if strings.Contains(b.st.Get("last_error"), jeton) {
				t.Fatal("error exposed credential")
			}
		})
	}
}

func TestCensusBodyCarriesOnlyReferencedScopesWithoutMixingFactions(t *testing.T) {
	f := &store.File{SHA256: strings.Repeat("a", 64), Origin: "current", AddonVersion: "4.6.0", Scopes: json.RawMessage(`{"4619-PVP-Alliance":{"realm_id":4619,"faction":"Alliance","region":"3"},"4620-RP_PVP-Horde":{"realm_id":4620,"faction":"Horde","region":"3"},"9999-NORMAL-Horde":{"realm_id":9999,"faction":"Horde","region":"1"}}`)}
	batches := []store.QueuedBatch{
		{BatchID: "11111111-1111-4111-8111-111111111111", ScopeID: "4619-PVP-Alliance", Payload: []byte(`{"batch_id":"11111111-1111-4111-8111-111111111111","scope_id":"4619-PVP-Alliance","characters":[{"guid":"Player-4619-00000001"}]}`)},
		{BatchID: "22222222-2222-4222-8222-222222222222", ScopeID: "4620-RP_PVP-Horde", Payload: []byte(`{"batch_id":"22222222-2222-4222-8222-222222222222","scope_id":"4620-RP_PVP-Horde","characters":[{"guid":"Player-4620-00000002"}]}`)},
	}
	raw, err := Body(f, batches)
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Scopes  map[string]map[string]any `json:"scopes"`
		Batches []json.RawMessage         `json:"batches"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Scopes) != 2 || body.Scopes["4619-PVP-Alliance"]["faction"] != "Alliance" || body.Scopes["4620-RP_PVP-Horde"]["faction"] != "Horde" {
		t.Fatal("scope pruning changed identities or mixed factions")
	}
	if len(body.Batches) != 2 {
		t.Fatal("batches lost")
	}
	for i := range batches {
		if !bytes.Equal(body.Batches[i], batches[i].Payload) {
			t.Fatal("scope pruning modified batch payload")
		}
	}
	if !bytes.Contains(f.Scopes, []byte("9999-NORMAL-Horde")) {
		t.Fatal("Body modified stored original scope provenance")
	}
}
