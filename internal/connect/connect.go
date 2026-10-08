// Package connect implements browser approval with bounded polling. Credentials
// remain in the Windows vault; HTTP errors are never echoed (they may be HTML).
package connect

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"wowsync/internal/secret"
	"wowsync/internal/sender"
)

type Vault interface {
	ReadConnection() (string, error)
	WriteConnection(string) error
	DeleteConnection() error
}
type Record struct {
	ID        string    `json:"id"`
	Verifier  string    `json:"verifier,omitempty"`
	Expires   time.Time `json:"expires"`
	Account   string    `json:"account,omitempty"`
	Completed bool      `json:"completed,omitempty"`
}
type Result struct {
	State          string    `json:"state"`
	Error          string    `json:"error"`
	Interval       int       `json:"interval"`
	Expires        time.Time `json:"expires_at"`
	URI            string    `json:"verification_uri"`
	Code           string    `json:"user_code"`
	Token          string    `json:"token"`
	Account        string    `json:"account"`
	InstallationID string    `json:"installation_id"`
}
type Client struct {
	Site      string
	HTTP      *http.Client
	Vault     Vault
	SaveToken func(string) error
	Open      func(string)
	Status    func(state, detail string)
}

func (c *Client) report(state, detail string) {
	if c.Status != nil {
		c.Status(state, detail)
	}
}
func (c *Client) load() (Record, error) {
	s, e := c.Vault.ReadConnection()
	if e != nil {
		return Record{}, e
	}
	var r Record
	e = json.Unmarshal([]byte(s), &r)
	return r, e
}
func (c *Client) save(r Record) error {
	b, e := json.Marshal(r)
	if e != nil {
		return e
	}
	return c.Vault.WriteConnection(string(b))
}
func Code(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return strings.ToUpper(hex.EncodeToString(h[:4]))
}
func (c *Client) call(ctx context.Context, r Record, operation string) (Result, error) {
	endpoint, e := sender.CibleRoute(c.Site, "/api/companion/device")
	if e != nil {
		return Result{}, errors.New("invalid_site")
	}
	b, _ := json.Marshal(map[string]string{"operation": operation, "request_id": r.ID, "verifier": r.Verifier, "label": "Windows PC"})
	req, e := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(b))
	if e != nil {
		return Result{}, errors.New("network")
	}
	req.Header.Set("Content-Type", "application/json")
	hc := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if c.HTTP != nil {
		*hc = *c.HTTP
		hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		hc.Timeout = 15 * time.Second
	}
	resp, e := hc.Do(req)
	if e != nil {
		return Result{}, errors.New("network")
	}
	defer resp.Body.Close()
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") || resp.StatusCode >= 500 || resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return Result{}, errors.New("network")
	}
	raw, e := io.ReadAll(io.LimitReader(resp.Body, 4097))
	if e != nil || len(raw) > 4096 {
		return Result{}, errors.New("network")
	}
	var out Result
	if json.Unmarshal(raw, &out) != nil {
		return out, errors.New("network")
	}
	if resp.StatusCode == 404 {
		return out, errors.New("unavailable")
	}
	if out.Error == "" && resp.StatusCode != 200 {
		return out, errors.New("network")
	}
	return out, nil
}

func (c *Client) Resume(ctx context.Context) error {
	r, e := c.load()
	if e != nil {
		return nil
	}
	if r.Verifier == "" {
		if r.Completed {
			c.report("connected", r.Account)
		}
		return nil
	}
	return c.run(ctx, r, false)
}
func (c *Client) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if r, e := c.load(); e == nil && r.Verifier != "" && time.Now().Before(r.Expires) {
		return c.run(ctx, r, true)
	}
	var id [16]byte
	var v [32]byte
	if _, e := rand.Read(id[:]); e != nil {
		return e
	}
	if _, e := rand.Read(v[:]); e != nil {
		return e
	}
	id[6] = (id[6] & 15) | 64
	id[8] = (id[8] & 63) | 128
	r := Record{ID: fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:]), Verifier: base64.RawURLEncoding.EncodeToString(v[:]), Expires: time.Now().Add(15 * time.Minute)}
	// Persist BEFORE start so a lost response/restart repeats the same request.
	if e := c.save(r); e != nil {
		return errors.New("vault")
	}
	return c.run(ctx, r, true)
}
func (c *Client) Cancel(ctx context.Context) error {
	r, e := c.load()
	if e != nil || r.Verifier == "" {
		return nil
	}
	out, e := c.call(ctx, r, "cancel")
	if e != nil {
		c.report("network", "")
		return e
	}
	if out.State != "cancelled" && out.Error != "expired_token" && out.Error != "access_denied" {
		return errors.New("unavailable")
	}
	if e = c.Vault.DeleteConnection(); e != nil {
		return errors.New("vault")
	}
	c.report("cancelled", "")
	return nil
}
func wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
func (c *Client) run(ctx context.Context, r Record, open bool) error {
	ctx, cancel := context.WithDeadline(ctx, r.Expires)
	defer cancel()
	interval := 5 * time.Second
	started := false
	for time.Now().Before(r.Expires) {
		if ctx.Err() != nil {
			if !time.Now().Before(r.Expires) {
				break
			}
			return ctx.Err()
		}
		op := "poll"
		if !started {
			op = "start"
		}
		if r.Completed {
			op = "ack"
		}
		out, e := c.call(ctx, r, op)
		if e != nil {
			c.report(e.Error(), "")
			if !wait(ctx, interval) {
				if !time.Now().Before(r.Expires) {
					break
				}
				return ctx.Err()
			}
			interval = min(60*time.Second, interval*2)
			continue
		}
		switch out.Error {
		case "access_denied", "expired_token", "invalid_request":
			// An ACK may have committed before its response was lost. The credential
			// was durably saved first, so completed records can safely finish locally.
			if r.Completed && out.Error == "invalid_request" {
				r.Verifier = ""
				if e = c.save(r); e != nil {
					return errors.New("vault")
				}
				c.report("connected", r.Account)
				return nil
			}
			_ = c.Vault.DeleteConnection()
			c.report(map[string]string{"access_denied": "cancelled", "expired_token": "expired", "invalid_request": "expired"}[out.Error], "")
			return nil
		case "authorization_pending", "slow_down":
			if out.Interval < 5 || out.Interval > 60 {
				return errors.New("network")
			}
			interval = time.Duration(out.Interval) * time.Second
			c.report("pending", Code(r.Verifier))
		case "":
			if op == "start" {
				expected := strings.TrimRight(c.Site, "/") + "/account/companion/connect?request=" + r.ID
				u, e := url.Parse(out.URI)
				if e != nil || u.String() != expected || out.Code != Code(r.Verifier) || out.Expires.IsZero() || out.Expires.After(r.Expires.Add(30*time.Second)) {
					return errors.New("network")
				}
				r.Expires = minTime(r.Expires, out.Expires)
				if e = c.save(r); e != nil {
					return errors.New("vault")
				}
				started = true
				if open && c.Open != nil {
					c.Open(expected)
					open = false
				}
				c.report("pending", out.Code)
			} else if op == "ack" && out.State == "acknowledged" {
				r.Verifier = ""
				if e = c.save(r); e != nil {
					return errors.New("vault")
				}
				c.report("connected", r.Account)
				return nil
			} else if op == "poll" && out.State == "approved" {
				if _, ok := secret.Normalise(out.Token); !ok || out.Account == "" || len(out.Account) > 254 {
					return errors.New("network")
				}
				if e = c.SaveToken(out.Token); e != nil {
					return errors.New("vault")
				}
				r.Completed = true
				r.Account = out.Account
				if e = c.save(r); e != nil {
					return errors.New("vault")
				}
				continue // acknowledge only after both vault writes succeeded
			} else {
				return errors.New("network")
			}
		default:
			return errors.New("network")
		}
		if !wait(ctx, interval) {
			if time.Now().After(r.Expires) {
				break
			}
			return ctx.Err()
		}
	}
	_ = c.Vault.DeleteConnection()
	c.report("expired", "")
	return nil
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
