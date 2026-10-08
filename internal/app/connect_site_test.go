package app

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"wowsync/internal/connect"
	"wowsync/internal/logx"
	"wowsync/internal/sender"
	"wowsync/internal/store"
)

// Opt-in test invoked by the site's owned PostgreSQL harness. The vault is
// in memory and the input Lua is fabricated; no installed app or credential.
type testConnectionVault struct{ s string }

func (v *testConnectionVault) ReadConnection() (string, error) {
	if v.s == "" {
		return "", errors.New("absent")
	}
	return v.s, nil
}
func (v *testConnectionVault) WriteConnection(s string) error { v.s = s; return nil }
func (v *testConnectionVault) DeleteConnection() error        { v.s = ""; return nil }
func TestConnectActualLocalSite(t *testing.T) {
	site := os.Getenv("PULSE_COMPANION_TEST_SITE")
	if site == "" {
		t.Skip("requires owned local site/SQL harness")
	}
	u, e := url.Parse(site)
	if e != nil || u.Hostname() != "127.0.0.1" || u.Scheme != "http" {
		t.Fatal("non-local test site refused")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	dir := t.TempDir()
	st, e := store.Open(filepath.Join(dir, "companion.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	log, e := logx.Open(filepath.Join(dir, "companion.log"))
	if e != nil {
		t.Fatal(e)
	}
	defer log.Close()
	a, e := New(st, log, sender.New(site, Version))
	if e != nil {
		t.Fatal(e)
	}
	token := ""
	a.Token = func() (string, error) {
		if token == "" {
			return "", errors.New("absent")
		}
		return token, nil
	}
	a.SaveToken = func(s string) error { token = s; return nil }
	c := connect.Client{Site: site, Vault: &testConnectionVault{}, SaveToken: a.SetToken, Open: func(uri string) {
		q, _ := url.Parse(uri)
		r, e := http.Get(site + "/test-approve?request=" + q.Query().Get("request"))
		if e != nil {
			t.Error("test approval transport failed")
			return
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Error("test approval failed")
		}
	}}
	if e = c.Start(ctx); e != nil {
		t.Fatal(e)
	}
	if token == "" {
		t.Fatal("authorization missing")
	}
	now := time.Now().Add(-time.Minute).Unix()
	data := fichierStats(64, int(now), map[string][]string{"00CD8642": {fmt.Sprintf("%d|20|t|60:7", now)}})
	data = []byte(strings.ReplaceAll(strings.ReplaceAll(string(data), "1790180000", fmt.Sprint(now)), "1790179995", fmt.Sprint(now-5)))
	path := filepath.Join(dir, "ForeverPulse.lua")
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = a.ProcessFile(ctx, path); e != nil {
		t.Fatal(e)
	}
	if e = a.Flush(ctx, true); e != nil {
		t.Fatal(e)
	}
	state := a.Etat()
	if state.Pending != 0 || state.StatsPending != 0 || state.Sent != 1 || state.StatsSent != 1 {
		t.Fatalf("wrong queue counts: census %d/%d stats %d/%d", state.Sent, state.Pending, state.StatsSent, state.StatsPending)
	}
	if _, e = a.ProcessFile(ctx, path); e != nil {
		t.Fatal(e)
	}
	if e = a.Flush(ctx, true); e != nil {
		t.Fatal(e)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "companion.log"))
	if strings.Contains(string(raw), token) {
		t.Fatal("credential in log")
	}
	r, e := http.Get(site + "/test-revoke")
	if e != nil {
		t.Fatal("revocation transport")
	}
	r.Body.Close()
	// A fresh valid batch reaches authentication, remains queued on 401, and
	// changes the real application's state to the reconnection prompt.
	data = []byte(strings.ReplaceAll(string(data), "00000064-", "00000065-"))
	if e = os.WriteFile(path, data, 0600); e != nil {
		t.Fatal(e)
	}
	if _, e = a.ProcessFile(ctx, path); e != nil {
		t.Fatal(e)
	}
	if e = a.Flush(ctx, true); e == nil || a.Etat().Code != "token" || a.Etat().Pending != 1 {
		t.Fatal("revocation did not preserve queue and require reconnection")
	}
}
