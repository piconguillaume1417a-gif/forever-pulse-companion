package auctions

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"time"
	"wowsync/internal/lua"
)

const Variable = "AUCTIONATOR_PRICE_DATABASE"
const MaxBody = 3000000

type Row struct {
	ID       string `json:"id"`
	Item     int64  `json:"item_id"`
	Day      int64  `json:"source_day"`
	Basis    string `json:"day_basis"`
	Low      string `json:"low,omitempty"`
	High     string `json:"high,omitempty"`
	Quantity *int64 `json:"quantity,omitempty"`
	Current  string `json:"current_price,omitempty"`
}

func Digest(v any) string  { b, _ := json.Marshal(v); return Hash(b) }
func Hash(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }
func (r Row) Key() string  { return r.ID + "/" + r.Basis }

// Changed follows L7: losing the undated m alone does not trigger an upload.
func Changed(r, old Row) bool {
	return r.Low != old.Low || r.High != old.High || !equalQuantity(r.Quantity, old.Quantity) || (r.Current != "" && r.Current != old.Current)
}
func equalQuantity(a, b *int64) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }

type File struct {
	SHA          string  `json:"sha256"`
	DBVersion    int     `json:"auctionator_db_version"`
	RealmVersion int     `json:"realm_version"`
	AddonVersion *string `json:"addon_version"`
}
type Body struct {
	Schema    int `json:"schema"`
	Companion struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"companion"`
	Update  string   `json:"update_id"`
	File    File     `json:"file"`
	Market  Market   `json:"market"`
	Catalog *Catalog `json:"catalog"`
	Rows    []Row    `json:"rows"`
}

func NewBody(sha, addon, version string, m Market) Body {
	b := Body{Schema: 1, File: File{SHA: sha, DBVersion: 8, RealmVersion: 2}, Market: m, Rows: []Row{}}
	if text(addon, 40) {
		b.File.AddonVersion = &addon
	}
	b.Companion.Name = "forever-pulse-companion"
	b.Companion.Version = version
	b.Update = Hash([]byte(sha + "|" + m.ID))
	return b
}

type Result struct {
	Bodies        []Body
	Counts        map[string]int
	Objects       []int
	RealmVersions []int
}

var itemRE = regexp.MustCompile(`^[1-9][0-9]{0,8}$`)
var dayRE = regexp.MustCompile(`^[0-9]+$`)

// An empty Lua table can be represented as either an empty CBOR array or map.
// Nonempty arrays are never interpreted as day maps.
func history(v any) (Map, bool) {
	if m, ok := v.(Map); ok {
		return m, true
	}
	if a, ok := v.([]any); ok && len(a) == 0 {
		return nil, true
	}
	return nil, false
}

func money(v any) string {
	n, ok := v.(int64)
	if !ok || n < 1 || n > 999999999999999 {
		return ""
	}
	return strconv.FormatInt(n, 10)
}
func dayNumber(v any) (int64, bool) {
	if b, ok := v.([]byte); ok {
		v = string(b)
	}
	switch x := v.(type) {
	case int64:
		return x, x >= 0 && x <= 36500
	case string:
		if dayRE.MatchString(x) {
			return integer(x, 0, 36500)
		}
	}
	return 0, false
}
func Decode(data []byte, sha string, n Notes, catalog *Catalog, addon, version string, now time.Time) (Result, error) {
	r := Result{Counts: map[string]int{}}
	v, e := lua.Parse(data, map[string]bool{Variable: true})
	if e != nil {
		return r, errors.New("fichier Auctionator illisible")
	}
	db := lua.AsTable(v[Variable])
	if db == nil {
		return r, errors.New("base de prix Auctionator absente")
	}
	if ver, ok := lua.AsInt(db.Get("__dbversion")); !ok || ver != 8 {
		r.Counts["format_non_pris_en_charge"]++
		return r, errors.New("format Auctionator non pris en charge")
	}
	keys := make([]string, 0, len(db.Str))
	for k := range db.Str {
		if k != "__dbversion" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	bodies := map[string]*Body{}
	today := (now.UTC().Unix() - Epoch) / 86400
	for _, key := range keys {
		raw, ok := db.Str[key].(string)
		if !ok {
			r.Counts["format_non_pris_en_charge"]++
			continue
		}
		value, e := DecodeCBOR([]byte(raw))
		m, ok := value.(Map)
		if e != nil || !ok {
			r.Counts["format_non_pris_en_charge"]++
			continue
		}
		ver, ok := m.Get("version").(int64)
		r.RealmVersions = append(r.RealmVersions, int(ver))
		if !ok || ver != 2 {
			r.Counts["format_non_pris_en_charge"]++
			continue
		}
		items := map[int64]Map{}
		maxDay := int64(-1)
		uncertainLatest := false
		days := map[int64]bool{}
		for _, p := range m {
			key, ok := keyText(p.Key)
			if !ok || !itemRE.MatchString(key) {
				if key != "version" {
					r.Counts["cle_objet_non_prise_en_charge"]++
				}
				continue
			}
			id, _ := strconv.ParseInt(key, 10, 64)
			obj, ok := p.Value.(Map)
			if !ok {
				r.Counts["jour_illisible"]++
				continue
			}
			items[id] = obj
			for _, field := range []string{"h", "l", "a"} {
				if x := obj.Get(field); x != nil {
					hist, ok := history(x)
					if !ok {
						r.Counts["jour_illisible"]++
						continue
					}
					for _, p := range hist {
						d, ok := dayNumber(p.Key)
						if !ok {
							uncertainLatest = true
							r.Counts["jour_illisible"]++
							continue
						}
						if d > maxDay {
							maxDay = d
						}
						if d > today+2 {
							r.Counts["jour_illisible"]++
							continue
						}
						days[d] = true
						if d > maxDay {
							maxDay = d
						}
					}
				}
			}
		}
		r.Objects = append(r.Objects, len(items))
		if key == "PvP" {
			r.Counts["objets_pvp"] += len(items)
		}
		r.Counts["objets"] += len(items)
		joins := map[int64]struct {
			m Market
			b string
		}{}
		for d := range days {
			if d < today-31 {
				r.Counts["hors_retention"]++
				continue
			}
			market, basis, reason := n.Join(key, d)
			if reason != "" {
				r.Counts[reason]++
				r.Counts["jours_sans_faction_prouvee"]++
				continue
			}
			r.Counts["jours_attribues"]++
			joins[d] = struct {
				m Market
				b string
			}{market, basis}
		}
		ids := make([]int64, 0, len(items))
		for id := range items {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			obj := items[id]
			rows := map[int64]*Row{}
			for _, field := range []string{"h", "l", "a"} {
				hist, _ := history(obj.Get(field))
				seen := map[int64]bool{}
				for _, p := range hist {
					d, valid := dayNumber(p.Key)
					if !valid {
						continue
					}
					j, ok := joins[d]
					if !ok {
						continue
					}
					if seen[d] {
						return r, errors.New("jours Auctionator ambigus")
					}
					seen[d] = true
					row := rows[d]
					if row == nil {
						row = &Row{ID: fmt.Sprintf("%s/%d/%d", j.m.ID, id, d), Item: id, Day: d, Basis: j.b}
						rows[d] = row
					}
					if field == "a" {
						q, ok := p.Value.(int64)
						if ok && q >= 0 && q <= 2147483647 {
							row.Quantity = &q
						} else {
							r.Counts["jour_illisible"]++
						}
					} else {
						price := money(p.Value)
						if price == "" {
							r.Counts["jour_illisible"]++
						}
						if field == "h" {
							row.High = price
						} else {
							row.Low = price
						}
					}
				}
			}
			for d, row := range rows {
				if d == maxDay && !uncertainLatest {
					row.Current = money(obj.Get("m"))
				}
				if row.Low != "" && row.High != "" {
					lo, _ := strconv.ParseInt(row.Low, 10, 64)
					hi, _ := strconv.ParseInt(row.High, 10, 64)
					if lo > hi {
						r.Counts["jour_illisible"]++
						continue
					}
				}
				if row.Low == "" && row.High == "" && row.Quantity == nil && row.Current == "" {
					r.Counts["jour_illisible"]++
					continue
				}
				market := joins[d].m
				b := bodies[market.ID]
				if b == nil {
					x := NewBody(sha, addon, version, market)
					x.Catalog = catalog
					b = &x
					bodies[market.ID] = b
				}
				b.Rows = append(b.Rows, *row)
			}
		}
	}
	markets := make([]string, 0, len(bodies))
	for k := range bodies {
		markets = append(markets, k)
	}
	sort.Strings(markets)
	for _, k := range markets {
		b := bodies[k]
		sort.Slice(b.Rows, func(i, j int) bool { return b.Rows[i].ID < b.Rows[j].ID })
		r.Counts["lignes"] += len(b.Rows)
		r.Bodies = append(r.Bodies, *b)
	}
	return r, nil
}
