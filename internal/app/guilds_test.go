package app

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
	"wowsync/internal/guilds"
	"wowsync/internal/sender"
	tl "wowsync/internal/testlua"
)

func TestGuildFileAndSequentialUploadRetry(t *testing.T) {
	b := nouveauBancStats(t)
	b.now = time.Now()
	b.hs.Close()
	calls := []string{}
	guildCalls := 0
	b.hs = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		if r.URL.Path != sender.RouteGuild {
			b.srv.ServeHTTP(w, r)
			return
		}
		guildCalls++
		zr, e := gzip.NewReader(r.Body)
		if e != nil {
			t.Error(e)
			return
		}
		defer zr.Close()
		var body guilds.Body
		if e = json.NewDecoder(zr).Decode(&body); e != nil {
			t.Error(e)
			return
		}
		if len(body.Snapshots) != 1 || body.Snapshots[0].Total != 2 || *body.Snapshots[0].Members[1].Online {
			t.Error("lost full/offline roster")
		}
		w.Header().Set("Content-Type", "application/json")
		if guildCalls == 1 {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":"unavailable"}`)
			return
		}
		w.WriteHeader(202)
		fmt.Fprint(w, `{"accepted":1,"duplicates":0,"rejected":[]}`)
	}))
	b.a.Sender = sender.New(b.hs.URL, Version)
	data := fichierStats(1, 1, nil)
	// The synthetic census fixture's scope is used explicitly, as the addon does.
	f, _, _, e := DecodeTout(data, b.now)
	if e != nil {
		t.Fatal(e)
	}
	scope := f.ScopeIDs[0]
	m := tl.M{"v": 1, "at": b.now.Add(-time.Minute).Unix(), "scope": scope, "guild": "Synthetic Guild", "total": 2, "online": 1, "onlineRead": 1, "unknown": 0, "fields": "guid|name|level|class|online", "blocs": tl.L{"Player-9998-0001|Synthetic One|10|MAGE|1\nPlayer-9998-0002|Synthetic Two|11|DRUID|0"}}
	data = append(data, []byte("\nForeverPulseGuildDB="+tl.Serialise(m))...)
	path := filepath.Join(t.TempDir(), "ForeverPulse.lua")
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = b.a.ProcessFile(context.Background(), path); e != nil {
		t.Fatal(e)
	}
	if b.st.GuildCounts()["pending"] != 1 {
		t.Fatal("not queued", b.st.Get("guild_note"), scope, f.Scopes)
	}
	if e = b.a.Flush(context.Background(), true); e == nil {
		t.Fatal("expected outage")
	}
	if b.st.GuildCounts()["pending"] != 1 {
		t.Fatal("outage lost snapshot")
	}
	if calls[len(calls)-1] != sender.RouteGuild {
		t.Fatal("guild must follow other uploads", calls)
	}
	if e = b.a.Flush(context.Background(), true); e != nil {
		t.Fatal(e)
	}
	if b.st.GuildCounts()["sent"] != 1 {
		t.Fatal("missing exact ack")
	}
	if _, e = b.a.ProcessFile(context.Background(), path); e != nil {
		t.Fatal(e)
	}
	if b.st.GuildCounts()["pending"] != 0 {
		t.Fatal("requeued unchanged file")
	}
}
