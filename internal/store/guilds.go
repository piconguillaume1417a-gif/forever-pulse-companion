package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
	"wowsync/internal/guilds"
)

const guildSQL = `
create table if not exists guild_files(sha text primary key, processed_at integer not null);
create table if not exists guild_queue(key text primary key,id text not null, fresh integer not null,payload text not null,state text not null check(state in ('pending','sent','rejected')),reason text not null default '');
create index if not exists guild_pending on guild_queue(state,fresh);
`

func (s *Store) GuildFileKnown(sha string) bool {
	var n int
	return s.db.QueryRow(`select 1 from guild_files where sha=?`, sha).Scan(&n) == nil
}
func (s *Store) AddGuild(ctx context.Context, sha string, g *guilds.Snapshot) error {
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if g != nil {
		p, e := json.Marshal(g)
		if e != nil {
			return e
		}
		t, e := time.Parse(time.RFC3339, g.ObservedAt)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, `insert into guild_queue(key,id,fresh,payload,state) values(?,?,?,?,'pending') on conflict(key) do update set id=excluded.id,fresh=excluded.fresh,payload=excluded.payload,state='pending',reason='' where excluded.fresh>guild_queue.fresh`, g.Key(), g.ID, t.Unix(), string(p))
		if e != nil {
			return e
		}
	}
	_, e = tx.ExecContext(ctx, `insert or ignore into guild_files values(?,?)`, sha, time.Now().Unix())
	if e != nil {
		return e
	}
	// Latest state only, bounded by the same 30-day observation retention.
	if _, e = tx.ExecContext(ctx, `delete from guild_queue where fresh<?`, time.Now().Add(-30*24*time.Hour).Unix()); e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, `delete from guild_files where processed_at<?`, time.Now().Add(-30*24*time.Hour).Unix()); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) GuildPending() (*guilds.Snapshot, error) {
	var p string
	e := s.db.QueryRow(`select payload from guild_queue where state='pending' order by fresh limit 1`).Scan(&p)
	if e == sql.ErrNoRows {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	var g guilds.Snapshot
	e = json.Unmarshal([]byte(p), &g)
	return &g, e
}
func (s *Store) MarkGuild(g *guilds.Snapshot, state, reason string) error {
	_, e := s.db.Exec(`update guild_queue set state=?,reason=? where key=? and id=?`, state, reason, g.Key(), g.ID)
	return e
}
func (s *Store) GuildCounts() map[string]int {
	m := map[string]int{}
	r, e := s.db.Query(`select state,count(*) from guild_queue group by state`)
	if e != nil {
		return m
	}
	defer r.Close()
	for r.Next() {
		var k string
		var n int
		if r.Scan(&k, &n) == nil {
			m[k] = n
		}
	}
	return m
}
