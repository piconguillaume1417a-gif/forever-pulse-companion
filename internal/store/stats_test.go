package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
)

// Une base de la 0.9.0-rc.1 (stats_queue sans sent_schema), avec des fiches
// envoyées et en attente, s'ouvre sans perte ; la colonne est ajoutée une fois.
func TestAncienneBaseSansSentSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "companion.db")
	db, err := sql.Open("sqlite3", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	// Schéma de la 0.9.0-rc.1 : schemaSQL n'a pas changé, la colonne vient de la migration.
	if _, err := db.Exec(schemaSQL); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`insert into meta values('stats_blocked','')`,
		`insert into stats_files values('f1', 1, 2, 2, 0, '{"file":{}}')`,
		`insert into stats_queue(id,file_sha256,digest,fresh,payload,bytes,state,attempts,queued_at,sent_at) values('c/1','f1','d1',10,'{"id":"c/1"}',12,'sent',1,1,2)`,
		`insert into stats_queue(id,file_sha256,digest,fresh,payload,bytes,state,attempts,queued_at) values('c/2','f1','d2',10,'{"id":"c/2"}',12,'pending',0,1)`,
	} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	for i := 0; i < 2; i++ { // deux ouvertures : migration idempotente
		s, err := Open(path)
		if err != nil {
			t.Fatalf("ouverture %d : %v", i, err)
		}
		c := s.StatsCounts()
		if c["sent"] != 1 || c["pending"] != 1 {
			t.Fatalf("comptes : %v", c)
		}
		var n int
		if err := s.db.QueryRow(`select count(*) from pragma_table_info('stats_queue') where name='sent_schema'`).Scan(&n); err != nil || n != 1 {
			t.Fatalf("colonne sent_schema : %d %v", n, err)
		}
		s.Close()
	}

	s, _ := Open(path)
	defer s.Close()
	// Les fiches anciennes ont sent_schema nul : jamais remises en file par le retour du schéma 2.
	if n, err := s.StatsRequeueTalents(); err != nil || n != 0 {
		t.Fatalf("remise en file : %d %v", n, err)
	}
	meta, sheets, err := s.StatsPending(1 << 20)
	if err != nil || string(meta) != `{"file":{}}` || len(sheets) != 1 || sheets[0].ID != "c/2" {
		t.Fatalf("en attente : %s %+v %v", meta, sheets, err)
	}
	if err := s.MarkStatsSent(sheets, 1); err != nil {
		t.Fatal(err)
	}
	var sch sql.NullInt64
	_ = s.db.QueryRow(`select sent_schema from stats_queue where id='c/2'`).Scan(&sch)
	if !sch.Valid || sch.Int64 != 1 {
		t.Fatalf("sent_schema : %+v", sch)
	}
}

// Retour du schéma 2 : seules les fiches confirmées en schéma 1 dont la charge
// porte des talents repassent en attente, une fois, sans changer d'empreinte ; une
// fiche remplacée ensuite repart en attente avec sent_schema effacé.
func TestStatsRequeueTalents(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "companion.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	sheets := []StatSheet{
		{ID: "c/1", Digest: "d1", Fresh: 10, Payload: []byte(`{"id":"c/1","readings":[],"talents":{"tree_id":1}}`)},
		{ID: "c/2", Digest: "d2", Fresh: 10, Payload: []byte(`{"id":"c/2","name":"a,\"talents\":{","readings":[]}`)},
		{ID: "c/3", Digest: "d3", Fresh: 10, Payload: []byte(`{"id":"c/3","readings":[],"talents":{"tree_id":1}}`)},
	}
	if _, err := s.AddStatsFile(ctx, "f1", []byte(`{"file":{}}`), sheets, 3, 0); err != nil {
		t.Fatal(err)
	}
	_ = s.MarkStatsSent(sheets[:2], 1)
	_ = s.MarkStatsSent(sheets[2:], 2)
	if n, _ := s.StatsRequeueTalents(); n != 1 {
		t.Fatalf("remises en file : %d", n)
	}
	_, p, _ := s.StatsPending(1 << 20)
	if len(p) != 1 || p[0].ID != "c/1" || p[0].Digest != "d1" {
		t.Fatalf("en attente : %+v", p)
	}
	if n, _ := s.StatsRequeueTalents(); n != 0 {
		t.Fatalf("seconde remise en file : %d", n)
	}
	_ = s.MarkStatsSent(p, 2)
	if n, _ := s.StatsRequeueTalents(); n != 0 || s.StatsCounts()["sent"] != 3 {
		t.Fatalf("après renvoi : %d, %v", n, s.StatsCounts())
	}
	// Une nouvelle version d'une fiche efface sent_schema.
	sheets[2].Digest, sheets[2].Fresh = "d3b", 11
	if _, err := s.AddStatsFile(ctx, "f2", []byte(`{"file":{}}`), sheets[2:], 1, 0); err != nil {
		t.Fatal(err)
	}
	var sch sql.NullInt64
	_ = s.db.QueryRow(`select sent_schema from stats_queue where id='c/3'`).Scan(&sch)
	if sch.Valid {
		t.Fatalf("sent_schema non effacé : %+v", sch)
	}
}
