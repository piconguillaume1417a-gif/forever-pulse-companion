package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"wowsync/internal/sender"
)

// A small request can exceed the server's 500-batch ceiling without reaching
// its byte ceiling. All traffic stays on nouveauBanc's httptest loopback server.
func TestCensus501SmallBatchesRemainDeliverable(t *testing.T) {
	b := nouveauBanc(t)
	b.now = b.now.Add(7 * 24 * time.Hour)
	b.srv.brut = []http.HandlerFunc{func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Batches []json.RawMessage `json:"batches"`
		}
		if err := json.Unmarshal(b.srv.corps[len(b.srv.corps)-1], &body); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		if len(body.Batches) > 500 {
			w.WriteHeader(400)
			fmt.Fprint(w, `{"error":"invalid_body"}`)
			return
		}
		w.WriteHeader(202)
		fmt.Fprintf(w, `{"accepted":%d,"duplicates":0,"rejected":[]}`, len(body.Batches))
	}}
	n, err := b.a.ProcessFile(context.Background(), b.fichier("synthetic-501.lua", fixture(501, 1, 1)))
	if err != nil || n != 501 {
		t.Fatalf("seed: %d %v", n, err)
	}
	sendErr := b.a.Flush(context.Background(), true)
	e := b.a.Etat()
	if e.Rejected != 0 || e.Sent != 501 || e.Pending != 0 {
		t.Fatalf("valid small batches became undeliverable: sent=%d pending=%d rejected=%d blocked=%q error=%v", e.Sent, e.Pending, e.Rejected, b.st.Get("blocked"), sendErr)
	}
}

func TestCensusPacketBoundaryAndRestart(t *testing.T) {
	for _, count := range []int{500, 501, 1201} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			b := nouveauBancStats(t)
			b.now = b.now.Add(14 * 24 * time.Hour)
			calls, fail := 0, true
			censusFaultServer(t, b, func(raw []byte) (int, string) {
				calls++
				var body struct {
					Scopes  map[string]json.RawMessage `json:"scopes"`
					Batches []struct {
						Scope string `json:"scope_id"`
						At    string `json:"observed_at"`
					} `json:"batches"`
				}
				if err := json.Unmarshal(raw, &body); err != nil {
					t.Error(err)
				}
				if len(body.Batches) > 500 || len(body.Scopes) > 16 || len(raw) > sender.MaxCensusBody {
					t.Errorf("packet exceeded contract: %d batches, %d scopes, %d bytes", len(body.Batches), len(body.Scopes), len(raw))
				}
				used := map[string]bool{}
				for i, batch := range body.Batches {
					used[batch.Scope] = true
					if body.Scopes[batch.Scope] == nil {
						t.Error("missing scope")
					}
					if i > 0 && body.Batches[i-1].At > batch.At {
						t.Error("observation order changed")
					}
				}
				if len(used) != len(body.Scopes) {
					t.Error("unused scopes sent")
				}
				if fail && calls == 2 {
					return 503, "retry_later"
				}
				return 0, ""
			})
			if n, err := b.a.ProcessFile(context.Background(), b.fichier("many.lua", fixture(count, 1, 9))); err != nil || n != count {
				t.Fatalf("seed %d %v", n, err)
			}
			err := b.a.Flush(context.Background(), false)
			if count > 500 {
				if err == nil || b.a.Etat().Sent != 500 || b.a.Etat().Pending != count-500 || b.a.Etat().Rejected != 0 {
					t.Fatalf("interrupted state: %+v err=%v", b.a.Etat(), err)
				}
				if err := b.st.Close(); err != nil {
					t.Fatal(err)
				}
				b.ouvre()
				fail = false
				if err := b.a.Flush(context.Background(), false); err != nil {
					t.Fatal(err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if e := b.a.Etat(); e.Sent != count || e.Pending != 0 || e.Rejected != 0 || len(b.srv.lots) != count {
				t.Fatalf("incomplete delivery: %+v unique=%d", e, len(b.srv.lots))
			}
		})
	}
}

func TestCensusCommitBeforeLostResponseReplaysAfterRestart(t *testing.T) {
	b := nouveauBancStats(t)
	b.hs.Close()
	lost := true
	b.hs = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorded := httptest.NewRecorder()
		b.srv.ServeHTTP(recorded, r)
		if lost && r.URL.Path == sender.Route {
			lost = false
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":"retry_later"}`)
			return
		}
		for k, v := range recorded.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(recorded.Code)
		_, _ = w.Write(recorded.Body.Bytes())
	}))
	t.Cleanup(b.hs.Close)
	b.a.Sender = sender.New(b.hs.URL, Version)
	if _, err := b.a.ProcessFile(context.Background(), b.fichier("lost.lua", fixture(3, 5, 1))); err != nil {
		t.Fatal(err)
	}
	if err := b.a.Flush(context.Background(), false); err == nil {
		t.Fatal("lost acknowledgement accepted")
	}
	if e := b.a.Etat(); e.Pending != 3 || e.Sent != 0 || e.Rejected != 0 || len(b.srv.lots) != 3 {
		t.Fatalf("after lost acknowledgement: %+v", e)
	}
	if err := b.st.Close(); err != nil {
		t.Fatal(err)
	}
	b.ouvre()
	if err := b.a.Flush(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if e := b.a.Etat(); e.Pending != 0 || e.Sent != 3 || e.Rejected != 0 || len(b.srv.lots) != 3 {
		t.Fatalf("replay lost or duplicated data: %+v", e)
	}
}

func TestCensusManyInvalidBatchesBoundRequestsAndPreserveRest(t *testing.T) {
	b := nouveauBancStats(t)
	calls := censusFaultServer(t, b, func([]byte) (int, string) { return 400, "invalid_body" })
	if _, err := b.a.ProcessFile(context.Background(), b.fichier("invalid.lua", fixture(100, 1, 1))); err != nil {
		t.Fatal(err)
	}
	if err := b.a.Flush(context.Background(), false); err == nil {
		t.Fatal("expected bounded pass")
	}
	e := b.a.Etat()
	if *calls != 32 || e.Pending == 0 || e.Sent != 0 || e.Pending+e.Rejected != 100 || b.st.Get("blocked") != "" {
		t.Fatalf("unbounded/lost state: calls=%d %+v", *calls, e)
	}
	if err := b.a.Flush(context.Background(), false); err != nil || *calls != 32 {
		t.Fatal("resumed before backoff")
	}
	if err := b.st.Close(); err != nil {
		t.Fatal(err)
	}
	b.ouvre()
	if e2 := b.a.Etat(); e2.Pending != e.Pending || e2.Rejected != e.Rejected {
		t.Fatal("restart changed pending/refused decisions")
	}
}

func TestCensus413SplitsWholeBatchesAndContinues(t *testing.T) {
	b := nouveauBancStats(t)
	censusFaultServer(t, b, func(raw []byte) (int, string) {
		var body struct {
			Batches []json.RawMessage `json:"batches"`
		}
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Error(err)
		}
		if len(body.Batches) > 1 {
			return 413, "payload_too_large"
		}
		return 0, ""
	})
	if _, err := b.a.ProcessFile(context.Background(), b.fichier("smaller-packets.lua", fixture(5, 3, 1))); err != nil {
		t.Fatal(err)
	}
	if err := b.a.Flush(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if e := b.a.Etat(); e.Sent != 5 || e.Pending != 0 || e.Rejected != 0 || len(b.srv.lots) != 5 {
		t.Fatalf("413 lost batches: %+v", e)
	}
}
