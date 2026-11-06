package store

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestPendingCapsBatchCountAndKeepsEveryBatchAcrossPages(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	batches := make([]QueuedBatch, 501)
	for i := range batches {
		batches[i] = QueuedBatch{BatchID: fmt.Sprintf("11111111-1111-4111-8111-%012d", i), ScopeID: "4619-PVP-Alliance", ObservedAt: int64(i), Characters: 1, Payload: []byte(`{"synthetic":true}`)}
	}
	f := File{SHA256: strings.Repeat("a", 64), Path: "synthetic.lua", Origin: "current", AddonVersion: "4.6.0", ObserverSessionID: "22222222-2222-4222-8222-222222222222", Scopes: json.RawMessage(`{"4619-PVP-Alliance":{"realm_id":4619}}`), Total: 501}
	if n, err := s.AddFile(context.Background(), f, batches, nil); err != nil || n != 501 {
		t.Fatalf("seed %d %v", n, err)
	}
	_, first, err := s.Pending(1 << 20)
	if err != nil || len(first) != 500 {
		t.Fatalf("firstpage %d %v", len(first), err)
	}
	ids := make([]string, len(first))
	for i, b := range first {
		ids[i] = b.BatchID
	}
	if err := s.MarkSent(ids); err != nil {
		t.Fatal(err)
	}
	_, last, err := s.Pending(1 << 20)
	if err != nil || len(last) != 1 || last[0].BatchID != batches[500].BatchID {
		t.Fatalf("finalpage %d %v", len(last), err)
	}
}

func TestPendingCapsScopesAndKeepsFactionsSeparated(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "queue.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	scopes := map[string]any{}
	batches := make([]QueuedBatch, 17)
	for i := range batches {
		faction := "Alliance"
		if i%2 == 1 {
			faction = "Horde"
		}
		sid := fmt.Sprintf("%d-PVP-%s", 4600+i/2, faction)
		scopes[sid] = map[string]any{"realm_id": 4600 + i/2, "ruleset": "PVP", "faction": faction, "region": "3"}
		batches[i] = QueuedBatch{BatchID: fmt.Sprintf("11111111-1111-4111-8111-%012d", i), ScopeID: sid, ObservedAt: int64(i), Characters: 1, Payload: []byte(`{"synthetic":true}`)}
	}
	raw, _ := json.Marshal(scopes)
	f := File{SHA256: strings.Repeat("a", 64), Path: "synthetic.lua", Origin: "current", AddonVersion: "4.6.0", ObserverSessionID: "22222222-2222-4222-8222-222222222222", Scopes: raw, Total: 17}
	if n, err := s.AddFile(context.Background(), f, batches, nil); err != nil || n != 17 {
		t.Fatalf("seed %d %v", n, err)
	}
	_, first, err := s.Pending(1 << 20)
	if err != nil || len(first) != 16 {
		t.Fatalf("scopepage %d %v", len(first), err)
	}
	ids := make([]string, len(first))
	for i, b := range first {
		if b.ScopeID != batches[i].ScopeID {
			t.Fatal("scope or faction changed")
		}
		ids[i] = b.BatchID
	}
	if err := s.MarkSent(ids); err != nil {
		t.Fatal(err)
	}
	_, last, err := s.Pending(1 << 20)
	if err != nil || len(last) != 1 || last[0].ScopeID != batches[16].ScopeID {
		t.Fatalf("lastscope %d %v", len(last), err)
	}
}
