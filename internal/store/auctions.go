package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"
	"wowsync/internal/auctions"
)

const auctionsSQL = `
create table if not exists auctions_files(id text primary key, sha256 text not null, fresh integer not null, counts text not null);
create table if not exists auctions_queue(key text primary key, market text not null, kind text not null, fresh integer not null, source_id text not null,
 digest text not null, payload text not null, meta text not null, ack text, state text not null, reason text not null default '');
create index if not exists auctions_pending on auctions_queue(market,state);
create table if not exists auctions_markets(market text primary key, retry_at integer not null default 0, opened_at integer not null default 0, blocked text not null default '', failures integer not null default 0);
create table if not exists auctions_parts(id integer primary key, market text not null, body text not null, entries text not null);
`

type AuctionEntry struct {
	Key, Digest, Kind string
	Payload           json.RawMessage
}
type AuctionPart struct {
	ID      int64
	Market  string
	Body    []byte
	Entries []AuctionEntry
}

func (s *Store) AuctionFileKnown(id, sig string) bool {
	var old string
	return s.db.QueryRow(`select sha256 from auctions_files where id=?`, id).Scan(&old) == nil && old == sig
}

// AddAuctions stores candidates independently from immutable in-flight parts.
// File modification time is the only available file order; it is never sent as
// an observation time. An older file cannot overwrite a newer candidate.
func (s *Store) AddAuctions(ctx context.Context, id, sha string, fresh int64, result auctions.Result) (int, error) {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return 0, e
	}
	defer tx.Rollback()
	var priorFile int64
	e = tx.QueryRow(`select fresh from auctions_files where id=?`, id).Scan(&priorFile)
	if e == nil && fresh < priorFile {
		return 0, nil
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return 0, e
	}
	counts, _ := json.Marshal(result.Counts)
	if _, e = tx.Exec(`insert into auctions_files values(?,?,?,?) on conflict(id) do update set sha256=excluded.sha256,fresh=excluded.fresh,counts=excluded.counts where excluded.fresh>=auctions_files.fresh`, id, sha, fresh, string(counts)); e != nil {
		return 0, e
	}
	added := 0
	valid := map[string]bool{}
	put := func(key, market, kind string, payload, meta []byte) error {
		digest := auctions.Hash(payload)
		var old, ack string
		var prior int64
		var state string
		e := tx.QueryRow(`select payload,coalesce(ack,''),fresh,state from auctions_queue where key=?`, key).Scan(&old, &ack, &prior, &state)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if e == nil && fresh < prior {
			return nil
		}
		changed := old != string(payload)
		if kind == "row" && e == nil {
			var a, b auctions.Row
			_ = json.Unmarshal(payload, &a)
			_ = json.Unmarshal([]byte(old), &b)
			changed = auctions.Changed(a, b)
		}
		if e == nil && !changed && state != "withdrawn" {
			_, e = tx.Exec(`update auctions_queue set fresh=max(fresh,?) where key=?`, fresh, key)
			return e
		}
		next := "pending"
		if ack != "" {
			same := ack == string(payload)
			if kind == "row" {
				var a, b auctions.Row
				_ = json.Unmarshal(payload, &a)
				_ = json.Unmarshal([]byte(ack), &b)
				same = !auctions.Changed(a, b)
			}
			if same {
				next = "sent"
			}
		}
		_, e = tx.Exec(`insert into auctions_queue(key,market,kind,fresh,source_id,digest,payload,meta,state) values(?,?,?,?,?,?,?,?,?)
   on conflict(key) do update set fresh=excluded.fresh,source_id=excluded.source_id,digest=excluded.digest,payload=excluded.payload,meta=excluded.meta,state=excluded.state,reason=''`, key, market, kind, fresh, id, digest, string(payload), string(meta), next)
		if e == nil && next == "pending" {
			added++
		}
		return e
	}
	for _, b := range result.Bodies {
		_, e = tx.Exec(`insert into auctions_markets(market) values(?) on conflict do nothing`, b.Market.ID)
		if e != nil {
			return 0, e
		}
		rows, cat := b.Rows, b.Catalog
		b.Rows = []auctions.Row{}
		b.Catalog = nil
		meta, _ := json.Marshal(b)
		for _, r := range rows {
			valid[r.Key()] = true
			p, _ := json.Marshal(r)
			if e = put(r.Key(), b.Market.ID, "row", p, meta); e != nil {
				return 0, e
			}
		}
		if cat != nil {
			for _, item := range cat.Items {
				p, _ := json.Marshal(item)
				c := *cat
				c.Items = nil
				h := b
				h.Catalog = &c
				meta, _ := json.Marshal(h)
				locale := "unknown"
				if cat.Locale != nil {
					locale = *cat.Locale
				}
				if e = put(fmt.Sprintf("%s/catalog/%s/%d", b.Market.ID, locale, item.ID), b.Market.ID, "catalog", p, meta); e != nil {
					return 0, e
				}
			}
		}
	}
	// J4 is re-evaluated when the sibling note changes. A newly ambiguous day
	// withdraws unsent rows and their in-flight snapshot before another POST.
	var withdrawn []string
	q, e := tx.Query(`select key from auctions_queue where source_id=? and kind='row' and fresh<=? and state='pending'`, id, fresh)
	if e != nil {
		return 0, e
	}
	for q.Next() {
		var key string
		if e = q.Scan(&key); e != nil {
			q.Close()
			return 0, e
		}
		if !valid[key] {
			withdrawn = append(withdrawn, key)
		}
	}
	e = q.Err()
	q.Close()
	if e != nil {
		return 0, e
	}
	for _, key := range withdrawn {
		if _, e = tx.Exec(`delete from auctions_parts where market=(select market from auctions_queue where key=?)`, key); e != nil {
			return 0, e
		}
		if _, e = tx.Exec(`update auctions_queue set state='withdrawn',reason='jour_non_attribuable' where key=?`, key); e != nil {
			return 0, e
		}
	}
	return added, tx.Commit()
}
func (s *Store) AuctionMarkets() ([]string, error) {
	rows, e := s.db.Query(`select market from auctions_markets where exists(select 1 from auctions_queue q where q.market=auctions_markets.market and q.state='pending') or exists(select 1 from auctions_parts p where p.market=auctions_markets.market) order by market`)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var m string
		if e = rows.Scan(&m); e != nil {
			return nil, e
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) AuctionGate(m string, now time.Time) bool {
	var retry, opened int64
	var blocked string
	if s.db.QueryRow(`select retry_at,opened_at,blocked from auctions_markets where market=?`, m).Scan(&retry, &opened, &blocked) != nil {
		return false
	}
	if blocked != "" || now.Unix() < retry {
		return false
	}
	var parts int
	_ = s.db.QueryRow(`select count(*) from auctions_parts where market=?`, m).Scan(&parts)
	if opened == 0 || now.Unix() >= opened+1800 {
		return true
	}
	return parts > 0 && now.Unix() < opened+600
}

// AuctionNext freezes all remaining candidates for one file/market. Other files
// wait for this update to finish, including across process restarts (H9).
func (s *Store) AuctionNext(m string, now time.Time) (*AuctionPart, error) {
	var part AuctionPart
	var body, entries string
	e := s.db.QueryRow(`select id,body,entries from auctions_parts where market=? order by id limit 1`, m).Scan(&part.ID, &body, &entries)
	if e == nil {
		part.Market = m
		part.Body = []byte(body)
		e = json.Unmarshal([]byte(entries), &part.Entries)
		return &part, e
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	rows, e := tx.Query(`select key,digest,kind,payload,meta from auctions_queue where market=? and state='pending' order by fresh,key`, m)
	if e != nil {
		return nil, e
	}
	var all []AuctionEntry
	var header auctions.Body
	chosen := ""
	for rows.Next() {
		var x AuctionEntry
		var p, meta string
		if e = rows.Scan(&x.Key, &x.Digest, &x.Kind, &p, &meta); e != nil {
			rows.Close()
			return nil, e
		}
		var h auctions.Body
		if e = json.Unmarshal([]byte(meta), &h); e != nil {
			rows.Close()
			return nil, e
		}
		if chosen == "" {
			chosen = h.Update
			header = h
			header.Catalog = nil
		}
		if chosen != h.Update {
			continue
		}
		x.Payload = json.RawMessage(p)
		all = append(all, x)
		if x.Kind == "catalog" && header.Catalog == nil {
			header.Catalog = h.Catalog
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	if len(all) == 0 {
		return nil, tx.Commit()
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Kind < all[j].Kind })
	cat := header.Catalog
	reset := func() auctions.Body { b := header; b.Rows = []auctions.Row{}; b.Catalog = nil; return b }
	b := reset()
	var batch []AuctionEntry
	bytesN := 0
	save := func() error {
		if len(batch) == 0 {
			return nil
		}
		p, e := json.Marshal(b)
		if e != nil {
			return e
		}
		if len(p) > auctions.MaxBody {
			return errors.New("corps HdV trop grand")
		}
		es, _ := json.Marshal(batch)
		_, e = tx.Exec(`insert into auctions_parts(market,body,entries) values(?,?,?)`, m, string(p), string(es))
		return e
	}
	for _, x := range all {
		if len(b.Rows) >= 5000 || (b.Catalog != nil && len(b.Catalog.Items) >= 5000) || bytesN+len(x.Payload) > auctions.MaxBody-65536 {
			if e = save(); e != nil {
				return nil, e
			}
			b = reset()
			batch = nil
			bytesN = 0
		}
		if x.Kind == "row" {
			var r auctions.Row
			_ = json.Unmarshal(x.Payload, &r)
			b.Rows = append(b.Rows, r)
		} else {
			if b.Catalog == nil {
				c := *cat
				c.Items = []auctions.Item{}
				b.Catalog = &c
			}
			var it auctions.Item
			_ = json.Unmarshal(x.Payload, &it)
			b.Catalog.Items = append(b.Catalog.Items, it)
		}
		batch = append(batch, x)
		bytesN += len(x.Payload)
	}
	if e = save(); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return s.AuctionNext(m, now)
}
func (s *Store) AuctionStart(m string, now time.Time) error {
	_, e := s.db.Exec(`update auctions_markets set opened_at=? where market=? and (opened_at=0 or opened_at<=?)`, ceilUnix(now), m, now.Unix()-1800)
	return e
}
func (s *Store) AuctionDelay(m string, until time.Time, blocked string) error {
	_, e := s.db.Exec(`update auctions_markets set retry_at=max(retry_at,?),blocked=?,failures=failures+1 where market=?`, ceilUnix(until), blocked, m)
	return e
}
func (s *Store) AuctionFailures(m string) int {
	var n int
	_ = s.db.QueryRow(`select failures from auctions_markets where market=?`, m).Scan(&n)
	return n
}
func (s *Store) AuctionUnblock() error {
	_, e := s.db.Exec(`update auctions_markets set blocked='' where blocked<>'unsupported_schema'`)
	return e
}

// Unsupported schema can resume only with a new companion version (H8).
func (s *Store) AuctionVersion(version string) error {
	if old := s.Get("auctions_companion_version"); old != version {
		if _, e := s.db.Exec(`update auctions_markets set blocked='' where blocked='unsupported_schema'`); e != nil {
			return e
		}
		return s.Set("auctions_companion_version", version)
	}
	return nil
}
func (s *Store) SuspendAuctions(source string) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`delete from auctions_parts where market in(select market from auctions_queue where source_id=? and state='pending')`, source); e != nil {
		return e
	}
	if _, e = tx.Exec(`update auctions_queue set state='withdrawn',reason='lecture_incomplete' where source_id=? and state='pending'`, source); e != nil {
		return e
	}
	return tx.Commit()
}

// AuctionAck is atomic, and acknowledges snapshot digests only. Capacity refusals
// stay pending; invalid rows stay rejected until their content changes.
func (s *Store) AuctionAck(p *AuctionPart, rejected map[string]string, now time.Time) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	capacity := false
	for _, x := range p.Entries {
		state, reason := "sent", ""
		if x.Kind == "row" {
			var r auctions.Row
			_ = json.Unmarshal(x.Payload, &r)
			reason = rejected[r.ID]
			switch reason {
			case "invalid_row", "no_value":
				state = "rejected"
			case "retention_expired":
				state = "expired"
			case "capacity_reached":
				state = "pending"
				capacity = true
			}
		}
		if state == "sent" {
			ack := x.Payload
			if x.Kind == "row" {
				var row auctions.Row
				_ = json.Unmarshal(ack, &row)
				if row.Current == "" {
					var old string
					_ = tx.QueryRow(`select coalesce(ack,'') from auctions_queue where key=?`, x.Key).Scan(&old)
					var prior auctions.Row
					_ = json.Unmarshal([]byte(old), &prior)
					if prior.Current != "" {
						row.Current = prior.Current
						ack, _ = json.Marshal(row)
					}
				}
			}
			if _, e = tx.Exec(`update auctions_queue set ack=? where key=?`, string(ack), x.Key); e != nil {
				return e
			}
		}
		if _, e = tx.Exec(`update auctions_queue set state=?,reason=? where key=? and digest=?`, state, reason, x.Key, x.Digest); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(`delete from auctions_parts where id=?`, p.ID); e != nil {
		return e
	}
	if capacity {
		_, e = tx.Exec(`update auctions_markets set retry_at=max(retry_at,?),failures=0 where market=?`, ceilUnix(now.Add(6*time.Hour)), p.Market)
	} else {
		_, e = tx.Exec(`update auctions_markets set failures=0 where market=?`, p.Market)
	}
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) AuctionCounts() map[string]int {
	out := map[string]int{}
	rows, e := s.db.Query(`select state,count(*) from auctions_queue where kind='row' group by state`)
	if e != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var state string
		var n int
		_ = rows.Scan(&state, &n)
		out[state] = n
	}
	return out
}
func (s *Store) AuctionDayCounts() map[string]int {
	out := map[string]int{}
	rows, e := s.db.Query(`select counts from auctions_files`)
	if e != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var raw string
		_ = rows.Scan(&raw)
		var c map[string]int
		_ = json.Unmarshal([]byte(raw), &c)
		for k, v := range c {
			out[k] += v
		}
	}
	return out
}

// A successful read of another account must not hide this account's format error.
func (s *Store) AuctionReadErrors() []string {
	rows, e := s.db.Query(`select distinct value from meta where key like 'auction_read_error/%' and value<>'' order by value`)
	if e != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var msg string
		if rows.Scan(&msg) == nil {
			out = append(out, msg)
		}
	}
	return out
}

// Split a 413 without discarding the immutable update or changing update_id.
func (s *Store) AuctionSplit(p *AuctionPart) error {
	if len(p.Entries) < 2 {
		return errors.New("ligne HdV trop grande")
	}
	var base auctions.Body
	if e := json.Unmarshal(p.Body, &base); e != nil {
		return e
	}
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	for _, entries := range [][]AuctionEntry{p.Entries[:len(p.Entries)/2], p.Entries[len(p.Entries)/2:]} {
		b := base
		b.Rows = []auctions.Row{}
		b.Catalog = nil
		for _, x := range entries {
			if x.Kind == "row" {
				var r auctions.Row
				_ = json.Unmarshal(x.Payload, &r)
				b.Rows = append(b.Rows, r)
			} else {
				if b.Catalog == nil {
					c := *base.Catalog
					c.Items = []auctions.Item{}
					b.Catalog = &c
				}
				var it auctions.Item
				_ = json.Unmarshal(x.Payload, &it)
				b.Catalog.Items = append(b.Catalog.Items, it)
			}
		}
		raw, _ := json.Marshal(b)
		es, _ := json.Marshal(entries)
		if _, e = tx.Exec(`insert into auctions_parts(market,body,entries) values(?,?,?)`, p.Market, string(raw), string(es)); e != nil {
			return e
		}
	}
	if _, e = tx.Exec(`delete from auctions_parts where id=?`, p.ID); e != nil {
		return e
	}
	return tx.Commit()
}

func ceilUnix(t time.Time) int64 {
	n := t.Unix()
	if t.Nanosecond() > 0 {
		n++
	}
	return n
}
