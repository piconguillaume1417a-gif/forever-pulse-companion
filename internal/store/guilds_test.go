package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wowsync/internal/guilds"
)

func TestGuildQueueChronologyPersistenceAndAck(t *testing.T) {
	path := filepath.Join(t.TempDir(), "companion.db")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	g := guilds.Snapshot{ID: strings.Repeat("a", 64), ScopeID: "9998-PVP-Alliance", Scope: guilds.Scope{Realm: 9998, Ruleset: "PVP", Faction: "Alliance", Region: "3"}, Guild: "Synthetic Guild", ObservedAt: time.Now().Add(-time.Hour).UTC().Format(time.RFC3339), Total: 1}
	if e = s.AddGuild(context.Background(), "first", &g); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	p, e := s.GuildPending()
	if e != nil || p == nil || p.ID != g.ID {
		t.Fatal("pending snapshot did not survive restart", e)
	}
	newer := g
	newer.ID = strings.Repeat("b", 64)
	newer.ObservedAt = time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	if e = s.AddGuild(context.Background(), "newer", &newer); e != nil {
		t.Fatal(e)
	}
	if e = s.MarkGuild(&g, "sent", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.AddGuild(context.Background(), "backup", &g); e != nil {
		t.Fatal(e)
	}
	p, e = s.GuildPending()
	if e != nil || p == nil || p.ID != newer.ID {
		t.Fatal("old ack or backup replaced newer pending snapshot", e)
	}
	if e = s.MarkGuild(&newer, "sent", ""); e != nil {
		t.Fatal(e)
	}
	if e = s.AddGuild(context.Background(), "same in another file", &newer); e != nil {
		t.Fatal(e)
	}
	if s.GuildCounts()["pending"] != 0 || s.GuildCounts()["sent"] != 1 {
		t.Fatal("same snapshot requeued")
	}
	if e = s.Wipe(); e != nil {
		t.Fatal(e)
	}
	if len(s.GuildCounts()) != 0 || s.GuildFileKnown("first") {
		t.Fatal("wipe retains guild data")
	}
}
