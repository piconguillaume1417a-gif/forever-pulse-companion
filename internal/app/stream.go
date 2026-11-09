package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"wowsync/internal/cumul"
	"wowsync/internal/guilds"
	"wowsync/internal/lua"
	"wowsync/internal/schema"
	"wowsync/internal/stats"
	"wowsync/internal/store"
	"wowsync/internal/watch"
)

const censusVar = "ForeverPulseCensusDB"

// Limits are on individual values that must be decoded together. Skipped
// values, the number of records and the file itself have no byte limit.
const maxBatchValue = 16 << 20
const maxSheetValue = 2 << 20
const maxHeaderValue = 64 << 20
const maxDeltaDepth = 64

func stringTable() *lua.Table           { return &lua.Table{Str: map[string]any{}} }
func put(t *lua.Table, k string, v any) { t.Order = append(t.Order, k); t.Str[k] = v }

type item struct {
	span     lua.Span
	explicit bool
}
type saveIndex struct {
	dictFields map[string]lua.Span
	nameSpans  map[int64]item
	headers    map[string]map[string]lua.Span
	batches    map[int64]item
	sheets     map[string]lua.Span
	legacy     lua.Span
	roots      map[string]bool
}

func (i *saveIndex) visit(path []lua.Key, span lua.Span) error {
	root := path[0].Name
	if len(path) == 1 {
		if i.roots[root] {
			return errors.New("duplicate SavedVariables assignment")
		}
		i.roots[root] = true
		if root == guilds.Variable {
			i.legacy = span
		}
		return nil
	}
	if root == guilds.Variable {
		return nil
	}
	k := path[1].Name
	if root == censusVar && k != "batches" && strings.Contains(path[len(path)-1].Name, "occurred_at") {
		return errors.New("forbidden occurred_at in Census header")
	}
	if len(path) == 2 && (k != "batches" || root != censusVar) && (k != "personnages" || root != stats.Variable) {
		if i.headers[root] == nil {
			i.headers[root] = map[string]lua.Span{}
		}
		i.headers[root][k] = span
	}
	if root == censusVar && k == "dicts" {
		if len(path) == 3 && path[2].Name != "names" {
			i.dictFields[path[2].Name] = span
		}
		if len(path) == 4 && path[2].Name == "names" && path[3].Index > 0 {
			n := path[3].Index
			if old, ok := i.nameSpans[n]; !ok || !old.explicit || path[3].Explicit {
				i.nameSpans[n] = item{span, path[3].Explicit}
			}
		}
	}
	if len(path) != 3 {
		return nil
	}
	if root == censusVar && k == "batches" && path[2].Index > 0 {
		n := path[2].Index
		if old, ok := i.batches[n]; !ok || !old.explicit || path[2].Explicit {
			i.batches[n] = item{span, path[2].Explicit}
		}
	}
	if root == stats.Variable && k == "personnages" && path[2].Name != "" {
		i.sheets[path[2].Name] = span
	}
	return nil
}
func (i *saveIndex) header(src io.ReaderAt, root string) (*lua.Table, error) {
	if !i.roots[root] {
		return nil, nil
	}
	t := stringTable()
	for k, span := range i.headers[root] {
		// Only format metadata used by the readers. Other fields were
		// syntax-checked already, including the unused scan/index state.
		if root == censusVar && k != "schema" && k != "scopes" && k != "dicts" && k != "observer" && k != "addon" {
			continue
		}
		if root == stats.Variable && k != "schema" && k != "version" && k != "locale" && k != "ctx" && k != "contextes" && k != "talents_v" && k != "cat" && k != "arbres" {
			continue
		}
		if root == censusVar && k == "dicts" {
			d := stringTable()
			for field, entry := range i.dictFields {
				v, e := entry.Value(src, maxHeaderValue)
				if e != nil {
					return nil, e
				}
				put(d, field, v)
			}
			put(t, k, d)
			continue
		}
		v, e := span.Value(src, maxHeaderValue)
		if e != nil {
			return nil, e
		}
		put(t, k, v)
	}
	return t, nil
}

type batchRef struct {
	id, base, scope string
	at              int64
	span            lua.Span
}

func indexBatches(src io.ReaderAt, idx *saveIndex) ([]batchRef, map[string]batchRef, error) {
	out := make([]batchRef, 0, len(idx.batches))
	byID := map[string]batchRef{}
	for n := int64(1); ; n++ {
		it, ok := idx.batches[n]
		if !ok {
			break
		}
		v, err := it.span.Value(src, maxBatchValue)
		if err != nil {
			return nil, nil, err
		}
		t := lua.AsTable(v)
		at, _ := lua.AsInt(t.Get("observed_at"))
		b := batchRef{id: lua.Str(t.Get("batch_id")), scope: lua.Str(t.Get("scope_id")), at: at, span: it.span}
		if lua.Str(t.Get("kind")) == "delta" {
			b.base = lua.Str(t.Get("base_batch_id"))
		}
		if _, exists := byID[b.id]; !exists {
			byID[b.id] = b
		} else {
			b.id = ""
		}
		out = append(out, b)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].at < out[j].at })
	return out, byID, nil
}
func readChain(src io.ReaderAt, b batchRef, byID map[string]batchRef, dev *schema.Developer) ([]*lua.Table, error) {
	var chain []*lua.Table
	for {
		if len(chain) >= maxDeltaDepth {
			return nil, errors.New("delta chain exceeds supported depth")
		}
		v, err := b.span.Value(src, maxBatchValue)
		if err != nil {
			return nil, err
		}
		t := lua.AsTable(v)
		if t == nil {
			return nil, errors.New("invalid batch")
		}
		d, e := dev.Lot(t)
		if e != nil {
			return nil, e
		}
		chain = append(chain, d)
		if lua.Str(d.Get("kind")) != "delta" {
			break
		}
		parent, ok := byID[b.base]
		if !ok || parent.scope != b.scope || parent.span == b.span {
			return nil, errors.New("missing or incompatible delta base")
		}
		b = parent
	}
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

func stageCensus(w *store.FileWriter, r *schema.LotResultat, region string) error {
	p, err := schema.Marshal(r.Payload)
	if err != nil {
		return err
	}
	localScope, err := w.CumulScope(r.ScopeID, region)
	if err != nil {
		return err
	}
	l := cumul.Lot{BatchID: r.BatchID, ScopeID: localScope, ObservedAt: r.ObservedAt, Source: cumul.Source(r.Method, r.SourceScope)}
	for _, row := range r.Who {
		l.Who = append(l.Who, cumul.WhoPerso{Name: row.Name, ClassLoc: row.ClassLoc, RaceLoc: row.RaceLoc, Level: row.Level, Guild: row.Guild, Zone: row.Zone})
	}
	for _, row := range r.Roster {
		l.Roster = append(l.Roster, cumul.RosterPerso{GUID: row.GUID, DisplayName: row.DisplayName, Class: row.Class, Race: row.Race, Level: row.Level, Zone: r.ZoneName})
	}
	return w.Batch(store.QueuedBatch{BatchID: r.BatchID, ScopeID: r.ScopeID, ObservedAt: r.ObservedAt, Characters: r.Personnages, Payload: p}, l)
}

func stageGuild(w *store.FileWriter, g *guilds.Snapshot) error {
	at, e := time.Parse(time.RFC3339, g.ObservedAt)
	if e != nil {
		return e
	}
	localScope, e := w.CumulScope(g.ScopeID, g.Scope.Region)
	if e != nil {
		return e
	}
	for offset := 0; offset < len(g.Members); offset += guilds.Chunk {
		group := g.Members[offset:min(offset+guilds.Chunk, len(g.Members))]
		part := offset / guilds.Chunk
		id := guilds.BatchID(g.ID, part)
		p, e := schema.Marshal(schema.Obj{
			{K: "batch_id", V: id}, {K: "scope_id", V: g.ScopeID}, {K: "method", V: guilds.Method},
			{K: "kind", V: "membership"}, {K: "observed_at", V: g.ObservedAt}, {K: "read", V: len(group)},
			{K: "guild", V: g.Guild}, {K: "snapshot_id", V: g.ID}, {K: "part", V: part}, {K: "characters", V: group},
		})
		if e != nil {
			return e
		}
		lot := cumul.Lot{BatchID: id, ScopeID: localScope, ObservedAt: at.Unix(), Source: cumul.SrcGuild}
		for _, m := range group {
			level := m.Level
			lot.Roster = append(lot.Roster, cumul.RosterPerso{GUID: m.GUID, DisplayName: m.Name, Class: m.Class, Level: &level, Guild: &g.Guild, MembershipOnly: m.Online == nil || !*m.Online})
		}
		if e = w.Batch(store.QueuedBatch{BatchID: id, ScopeID: g.ScopeID, ObservedAt: at.Unix(), Characters: len(group), Payload: p}, lot); e != nil {
			return e
		}
	}
	return nil
}

// ProcessFile hashes and validates the input incrementally before queuing
// anything. Only the requested headers and one observation/delta chain or
// statistics chunk are decoded at a time. The queue is committed after the
// final file-identity check, or rolled back together with cumulative changes.
func (a *App) ProcessFile(ctx context.Context, path string) (int, error) {
	if AuctionPath(path) {
		return a.ProcessAuctions(ctx, path)
	}
	src, stable, err := watch.OpenSnapshot(path)
	if err != nil {
		return 0, err
	}
	defer src.Close()
	h := sha256.New()
	idx := &saveIndex{dictFields: map[string]lua.Span{}, nameSpans: map[int64]item{}, headers: map[string]map[string]lua.Span{}, batches: map[int64]item{}, sheets: map[string]lua.Span{}, roots: map[string]bool{}}
	if err = lua.Index(ctx, io.TeeReader(src, h), map[string]bool{censusVar: true, stats.Variable: true, guilds.Variable: true}, idx.visit); err != nil {
		return 0, err
	}
	sha := hex.EncodeToString(h.Sum(nil))
	if err = stable(); err != nil {
		return 0, err
	}
	a.mu.Lock()
	locked := true
	defer func() {
		if locked {
			a.mu.Unlock()
		}
	}()
	censusNew, statsNew := !a.Store.FileKnown(sha), !a.Store.StatsFileKnown(sha)
	if !censusNew && !statsNew {
		return 0, nil
	}
	db, err := idx.header(src, censusVar)
	if err != nil {
		return 0, err
	}
	now := a.Now()
	head := schema.AnalysePrepared(db, now, []*lua.Table{})
	if len(head.Fatales) > 0 {
		return 0, fmt.Errorf("census header refused (%d defects)", len(head.Fatales))
	}
	var refs []batchRef
	var ids map[string]batchRef
	var legacy *guilds.Snapshot
	scopes := lua.AsTable(db.Get("scopes"))
	if censusNew {
		refs, ids, err = indexBatches(src, idx)
		if err != nil {
			return 0, err
		}
		if idx.legacy.End > idx.legacy.Start {
			v, e := idx.legacy.Value(src, maxBatchValue)
			if e != nil {
				return 0, e
			}
			legacy, e = guilds.Decode(lua.AsTable(v), scopes, now)
			if e != nil {
				a.Log.Printf("legacy guild snapshot refused")
				legacy = nil
			}
			if legacy != nil {
				if known := scopes.Get(legacy.ScopeID); known == nil {
					head.ScopeIDs = append(head.ScopeIDs, legacy.ScopeID)
					head.Scopes[legacy.ScopeID] = legacy.Scope
				} else if lua.Str(lua.AsTable(known).Get("region")) != legacy.Scope.Region {
					return 0, errors.New("conflicting regional guild scope in legacy save")
				}
			}
		}
	}
	var statsDB *lua.Table
	meta := []byte("{}")
	var statsHead *stats.Fichier
	if statsNew {
		statsDB, err = idx.header(src, stats.Variable)
		if err != nil {
			return 0, err
		}
		if statsDB != nil {
			put(statsDB, "personnages", stringTable())
		}
		statsHead = stats.DecodeHeader(statsDB, now)
		if statsHead.Present && len(statsHead.Fatales) == 0 {
			meta, err = statsHead.Meta(sha, Origin(path), head.AddonVersion)
			if err != nil {
				return 0, err
			}
		}
	}
	scopeBytes, err := schema.Marshal(scopeObj(head))
	if err != nil {
		return 0, err
	}
	f := store.File{SHA256: sha, Path: path, Origin: Origin(path), AddonVersion: head.AddonVersion, ObserverSessionID: head.ObserverID, Scopes: scopeBytes}
	added, err := a.Store.WriteStream(ctx, f, censusNew, statsNew, meta, func(w *store.FileWriter) error {
		valid, total := 0, len(refs)
		dev := schema.Developer{DB: db}
		var names *nameIndex
		if censusNew {
			var e error
			names, e = openNames(src, idx.nameSpans)
			if e != nil {
				return e
			}
			dev.LookupName = names.lookup
		}
		if legacy != nil {
			total++
			if err := stageGuild(w, legacy); err != nil {
				return err
			}
			valid++
		}
		for _, b := range refs {
			if err := ctx.Err(); err != nil {
				return err
			}
			if b.id == "" {
				continue
			}
			v, e := b.span.Value(src, maxBatchValue)
			if e != nil {
				return e
			}
			raw := lua.AsTable(v)
			if lua.Str(raw.Get("method")) == guilds.Method {
				g, err := guilds.Decode(lua.AsTable(raw.Get("snapshot")), scopes, now)
				if err != nil || g == nil || g.ScopeID != b.scope || g.ObservedAt != schema.RFC3339(b.at) ||
					g.Scope.Region != lua.Str(lua.AsTable(scopes.Get(b.scope)).Get("region")) {
					a.Log.Printf("guild membership batch refused")
					continue
				}
				if err = stageGuild(w, g); err != nil {
					return err
				}
				valid++
				continue
			}
			chain, e := readChain(src, b, ids, &dev)
			if names != nil && names.failure != nil {
				return names.failure
			}
			if e != nil {
				a.Log.Printf("census lot discarded: invalid delta or fields")
				continue
			}
			r := schema.AnalysePrepared(db, now, chain).Lots[len(chain)-1]
			if !r.Valide() {
				a.Log.Printf("census lot discarded: failed content validation")
				continue
			}
			if e = stageCensus(w, r, lua.Str(lua.AsTable(scopes.Get(r.ScopeID)).Get("region"))); e != nil {
				return e
			}
			valid++
		}
		if censusNew {
			if e := w.Counts(total, valid); e != nil {
				return e
			}
		}
		if statsNew && statsDB != nil {
			if len(statsHead.Fatales) > 0 {
				if e := w.Stats(nil, len(idx.sheets)); e != nil {
					return e
				}
			} else {
				names := make([]string, 0, len(idx.sheets))
				for name := range idx.sheets {
					names = append(names, name)
				}
				sort.Strings(names)
				for start := 0; start < len(names); {
					if err := ctx.Err(); err != nil {
						return err
					}
					chunk := stringTable()
					end, used := start, int64(0)
					for end < min(start+64, len(names)) {
						span := idx.sheets[names[end]]
						if end > start && used+span.End-span.Start > 4<<20 {
							break
						}
						used += span.End - span.Start
						end++
					}
					for _, n := range names[start:end] {
						v, e := idx.sheets[n].Value(src, maxSheetValue)
						if e != nil {
							return e
						}
						put(chunk, n, v)
					}
					start = end
					statsDB.Str["personnages"] = chunk
					s := stats.DecodeAt(statsDB, now)
					if len(s.Fatales) > 0 {
						return errors.New("could not serialize statistics chunk")
					}
					sheets := make([]store.StatSheet, 0, len(s.Fiches))
					for _, fi := range s.Fiches {
						sheets = append(sheets, store.StatSheet{ID: fi.ID, Digest: fi.Empreinte, Fresh: fi.Fraicheur(), Payload: fi.Payload})
					}
					if e := w.Stats(sheets, s.Total); e != nil {
						return e
					}
				}
			}
		}
		return stable()
	})
	if err != nil {
		return 0, err
	}
	a.resumeValid = false
	_ = a.Store.Set("last_read", fmt.Sprintf("%s|%s|%s", now.Format(time.RFC3339), sha[:12], path))
	a.Log.Printf("fichier %s (%s, %s, addon %s) : %d lots en file", filepath.Base(path), sha[:12], Origin(path), head.AddonVersion, added)
	locked = false
	a.mu.Unlock()
	a.changed()
	return added, nil
}
