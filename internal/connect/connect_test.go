package connect

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type memoryVault struct {
	s    string
	fail bool
}

func (v *memoryVault) ReadConnection() (string, error) {
	if v.s == "" {
		return "", errors.New("absent")
	}
	return v.s, nil
}
func (v *memoryVault) WriteConnection(s string) error {
	if v.fail {
		return errors.New("vault")
	}
	v.s = s
	return nil
}
func (v *memoryVault) DeleteConnection() error { v.s = ""; return nil }
func TestApprovalStoredBeforeAckAndResume(t *testing.T) {
	v := &memoryVault{}
	saved := ""
	ack := 0
	starts := 0
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		w.Header().Set("Content-Type", "application/json")
		switch in["operation"] {
		case "start":
			starts++
			if !strings.Contains(v.s, in["verifier"]) {
				t.Error("verifier not persisted before network")
			}
			_ = json.NewEncoder(w).Encode(Result{State: "pending", Interval: 5, Code: Code(in["verifier"]), Expires: time.Now().Add(14 * time.Minute), URI: server.URL + "/account/companion/connect?request=" + in["request_id"]})
		case "poll":
			_ = json.NewEncoder(w).Encode(Result{State: "approved", Token: "fpc_" + strings.Repeat("A", 43), Account: "SYNTHETIC account"})
		case "ack":
			if saved == "" || !strings.Contains(v.s, `"completed":true`) {
				t.Error("ACK before durable save")
			}
			ack++
			_ = json.NewEncoder(w).Encode(Result{State: "acknowledged"})
		}
	}))
	defer server.Close()
	c := Client{Site: server.URL, Vault: v, SaveToken: func(s string) error { saved = s; return nil }}
	if e := c.Start(context.Background()); e != nil {
		t.Fatal(e)
	}
	if ack != 1 || starts != 1 || saved == "" {
		t.Fatal("missing authorization")
	}
	var record Record
	_ = json.Unmarshal([]byte(v.s), &record)
	if record.Verifier != "" || !record.Completed {
		t.Fatal("verifier retained after ACK")
	}
	if e := c.Resume(context.Background()); e != nil {
		t.Fatal(e)
	}
	if starts != 1 {
		t.Fatal("connected resume started a new request")
	}
}
func TestLostAckResumesWithoutNewToken(t *testing.T) {
	r := Record{ID: "17000000-0000-4000-8000-000000000001", Verifier: strings.Repeat("A", 43), Expires: time.Now().Add(time.Minute), Completed: true, Account: "SYNTHETIC"}
	b, _ := json.Marshal(r)
	v := &memoryVault{s: string(b)}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in["operation"] != "ack" {
			t.Error("restart must retry ACK only")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"acknowledged"}`))
	}))
	defer s.Close()
	c := Client{Site: s.URL, Vault: v, SaveToken: func(string) error { t.Fatal("unexpected token replacement"); return nil }}
	if e := c.Resume(context.Background()); e != nil {
		t.Fatal(e)
	}
	if strings.Contains(v.s, "verifier") {
		t.Fatal("verifier not cleared")
	}
}
func TestNoRedirectAndNoRawHTTPError(t *testing.T) {
	hit := false
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer other.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusTemporaryRedirect)
	}))
	defer s.Close()
	c := Client{Site: s.URL}
	_, e := c.call(context.Background(), Record{}, "poll")
	if e == nil || e.Error() != "network" || hit {
		t.Fatal("redirect or unsafe error")
	}
}
func TestVaultFailurePreventsStart(t *testing.T) {
	c := Client{Site: "https://example.invalid", Vault: &memoryVault{fail: true}}
	if e := c.Start(context.Background()); e == nil || e.Error() != "vault" {
		t.Fatal("must fail before network")
	}
}
func TestCancelledAndExpiredRequestsForgetVerifier(t *testing.T) {
	for _, code := range []string{"access_denied", "expired_token"} {
		t.Run(code, func(t *testing.T) {
			v := &memoryVault{}
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(Result{Error: code})
			}))
			defer s.Close()
			c := Client{Site: s.URL, Vault: v}
			if e := c.Start(context.Background()); e != nil {
				t.Fatal(e)
			}
			if v.s != "" {
				t.Fatal("verifier retained")
			}
		})
	}
}

func TestOfflineExpiryAndShutdownRemainDistinct(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer s.Close()
	for _, shutdown := range []bool{false, true} {
		v := &memoryVault{}
		state := ""
		c := Client{Site: s.URL, Vault: v, Status: func(next, _ string) { state = next }}
		expires := time.Now().Add(50 * time.Millisecond)
		ctx, cancel := context.WithCancel(context.Background())
		if shutdown {
			expires = time.Now().Add(time.Minute)
			cancel()
		}
		if err := c.save(Record{ID: "17000000-0000-4000-8000-000000000001", Verifier: strings.Repeat("A", 43), Expires: expires}); err != nil {
			t.Fatal(err)
		}
		err := c.Resume(ctx)
		cancel()
		if shutdown {
			if !errors.Is(err, context.Canceled) || v.s == "" || state == "expired" {
				t.Fatal("shutdown must preserve the pending request for restart")
			}
		} else if err != nil || state != "expired" || v.s != "" {
			t.Fatal("offline deadline must expire and forget the request")
		}
	}
}

// The pairing code reaches the window before the browser opens, and it is the
// code the server returned (the one the website page shows).
func TestPendingCodeShownBeforeBrowserOpens(t *testing.T) {
	v := &memoryVault{}
	var events []string
	serverCode := ""
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		w.Header().Set("Content-Type", "application/json")
		switch in["operation"] {
		case "start":
			serverCode = Code(in["verifier"])
			_ = json.NewEncoder(w).Encode(Result{State: "pending", Interval: 5, Code: serverCode, Expires: time.Now().Add(14 * time.Minute), URI: server.URL + "/account/companion/connect?request=" + in["request_id"]})
		default:
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(Result{Error: "access_denied"})
		}
	}))
	defer server.Close()
	c := Client{Site: server.URL, Vault: v, SaveToken: func(string) error { return nil },
		Open:   func(string) { events = append(events, "open") },
		Status: func(state, detail string) { events = append(events, state+":"+detail) }}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if e := c.Start(ctx); e != nil {
		t.Fatal(e)
	}
	if len(events) < 2 || events[0] != "pending:"+serverCode || events[1] != "open" || serverCode == "" {
		t.Fatalf("code must be reported before the browser opens, got %v", events)
	}
}
