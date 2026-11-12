package app

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"wowsync/internal/guilds"
	"wowsync/internal/sender"
	tl "wowsync/internal/testlua"
)

func TestGuildFileSharesCensusQueueAndResumes(t *testing.T) {
	b := nouveauBancStats(t)
	b.now = time.Now()
	b.hs.Close()
	var mu sync.Mutex
	acks := map[string]bool{}
	var failed bool
	guildPartsAcked := 0
	total, offline, unknown, maxBody := 0, 0, 0, 0
	b.hs = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path != sender.Route {
			t.Error("unexpected secondary route")
			w.WriteHeader(500)
			return
		}
		zr, e := gzip.NewReader(r.Body)
		if e != nil {
			t.Error(e)
			return
		}
		defer zr.Close()
		var raw json.RawMessage
		if e = json.NewDecoder(zr).Decode(&raw); e != nil {
			t.Error(e)
			return
		}
		maxBody = max(maxBody, len(raw))
		var body struct {
			Batches []struct {
				ID         string          `json:"batch_id"`
				Method     string          `json:"method"`
				Kind       string          `json:"kind"`
				Characters []guilds.Member `json:"characters"`
			} `json:"batches"`
		}
		if e = json.Unmarshal(raw, &body); e != nil {
			t.Error(e)
			return
		}
		for _, part := range body.Batches {
			if part.Method == guilds.Method && guildPartsAcked > 0 && !failed {
				failed = true
				w.WriteHeader(503)
				fmt.Fprint(w, `{"error":"unavailable"}`)
				return
			}
		}
		accepted, duplicates := 0, 0
		for _, part := range body.Batches {
			if acks[part.ID] {
				if part.Method == guilds.Method {
					t.Error("acknowledged guild part was resent")
				}
				duplicates++
				continue
			}
			acks[part.ID] = true
			accepted++
			if part.Method == guilds.Method {
				guildPartsAcked++
				if part.Kind != "membership" || len(part.Characters) > guilds.Chunk {
					t.Error("membership transported as complete census")
				}
				for _, m := range part.Characters {
					total++
					if m.Online == nil {
						unknown++
					} else if !*m.Online {
						offline++
					}
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(202)
		fmt.Fprintf(w, `{"accepted":%d,"duplicates":%d,"rejected":[]}`, accepted, duplicates)
	}))
	defer b.hs.Close()
	b.a.Sender = sender.New(b.hs.URL, Version)
	data := fichierStats(1, 1, nil)
	f, _, _, e := DecodeTout(data, b.now)
	if e != nil {
		t.Fatal(e)
	}
	scope := f.ScopeIDs[0]
	var blocks tl.L
	for start := 0; start < 501; start += 32 {
		var rows []string
		for i := start; i < min(start+32, 501); i++ {
			state := []string{"1", "0", "?"}[i%3]
			rows = append(rows, fmt.Sprintf("Player-4619-%08X|Synthetic %03d|10|MAGE|%s", i+1, i, state))
		}
		blocks = append(blocks, strings.Join(rows, "\n"))
	}
	m := tl.M{"v": 1, "at": b.now.Add(-time.Minute).Unix(), "scope": scope, "guild": "Synthetic Guild", "total": 501, "online": 167,
		"onlineRead": 167, "unknown": 167, "fields": "guid|name|level|class|online", "blocs": blocks}
	path := b.fichier("ForeverPulse.lua", append(data, []byte("\nForeverPulseGuildDB="+tl.Serialise(m))...))
	if added, e := b.a.ProcessFile(context.Background(), path); e != nil || added != 4 {
		t.Fatalf("queue: %d %v", added, e)
	}
	if b.st.GuildCounts()["pending"] != 0 {
		t.Fatal("new files entered old queue")
	}
	c, e := b.st.LoadCumul()
	if e != nil {
		t.Fatal(e)
	}
	p := c.Scopes[scope]
	if p.Persos["Player-4619-00000001"].Dernier == nil || p.Persos["Player-4619-00000002"].Dernier != nil || p.Persos["Player-4619-00000003"].Dernier != nil {
		t.Fatal("membership has incorrect presence")
	}
	if e = b.a.Flush(context.Background(), true); e == nil {
		t.Fatal("expected interruption")
	}
	if e = b.st.Close(); e != nil {
		t.Fatal(e)
	}
	b.ouvre()
	if e = b.a.Flush(context.Background(), true); e != nil {
		t.Fatal(e)
	}
	if added, e := b.a.ProcessFile(context.Background(), path); e != nil || added != 0 {
		t.Fatalf("replay: %d %v", added, e)
	}
	mu.Lock()
	defer mu.Unlock()
	if !failed || total != 501 || offline != 167 || unknown != 167 || maxBody > sender.MaxCensusBody {
		t.Fatalf("total=%d offline=%d unknown=%d max=%d", total, offline, unknown, maxBody)
	}
	t.Logf("501 members, 3 parts, max request %d bytes; resumed after restart", maxBody)
}
