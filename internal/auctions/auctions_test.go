package auctions_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
	"wowsync/internal/auctions"
	f "wowsync/internal/auctionstest"
	"wowsync/internal/lua"
)

func notes(t *testing.T, s string) auctions.Notes {
	t.Helper()
	v, e := lua.Parse(f.Forever(s), nil)
	if e != nil {
		t.Fatal(e)
	}
	return auctions.ReadNotes(lua.AsTable(lua.AsTable(v["ForeverPulseCensusDB"]).Get("hdv")), "_classic_beta_")
}
func TestRFC8949AndL3Bounds(t *testing.T) {
	for _, h := range []string{"00", "17", "1818", "190100", "1a00010000", "1b0000000100000000", "20", "390100", "40", "4401020304", "60", "6161", "83010203", "a1616101", "f4", "f5", "f6"} {
		b, _ := hex.DecodeString(h)
		if _, e := auctions.DecodeCBOR(b); e != nil {
			t.Errorf("RFC A example %s: %v", h, e)
		}
	}
	for _, h := range []string{"", "18", "5a00800001", "7a00800001", "9a00030d40", "ba00030d40", "c001", "f90000", "fa00000000", "fb0000000000000000", "9f01ff", "bf616101ff", "1c", "00ff", "61ff", "a2616101616102", "1bffffffffffffffff", "a2416101616102"} {
		b, _ := hex.DecodeString(h)
		if _, e := auctions.DecodeCBOR(b); e == nil {
			t.Errorf("unexpected CBOR accepted: %s", h)
		}
	}
	tooDeep := append(bytes.Repeat([]byte{0x81}, 9), 0x00)
	if _, e := auctions.DecodeCBOR(tooDeep); e == nil {
		t.Fatal("L3 depth")
	}
	if _, e := auctions.DecodeCBOR(append(bytes.Repeat([]byte{0x81}, 8), 0x00)); e != nil {
		t.Fatal("L3 allowed depth", e)
	}
}
func TestContract23JoinCases(t *testing.T) {
	raw, e := os.ReadFile("testdata/jointure.json")
	if e != nil {
		t.Fatal(e)
	}
	var fixture struct {
		Cases []struct {
			ID     string `json:"id"`
			Key    string `json:"cle"`
			Day    int64  `json:"jour"`
			Folder string `json:"dossier"`
			HDV    *struct {
				V     int64  `json:"v"`
				Since int64  `json:"depuis"`
				Notes string `json:"notes"`
			} `json:"hdv"`
			Expected struct {
				Market string `json:"marche"`
				Basis  string `json:"day_basis"`
				Reason string `json:"refus"`
			} `json:"attendu"`
		} `json:"cas"`
	}
	if e = json.Unmarshal(raw, &fixture); e != nil {
		t.Fatal(e)
	}
	if len(fixture.Cases) != 23 {
		t.Fatal("contract fixture incomplete")
	}
	for _, c := range fixture.Cases {
		t.Run(c.ID, func(t *testing.T) {
			var h *lua.Table
			if c.HDV != nil {
				h = &lua.Table{Str: map[string]any{"v": lua.Number{I: c.HDV.V, IsInt: true}, "depuis": lua.Number{I: c.HDV.Since, IsInt: true}, "notes": c.HDV.Notes}}
			}
			m, b, r := auctions.ReadNotes(h, c.Folder).Join(c.Key, c.Day)
			if m.ID != c.Expected.Market || b != c.Expected.Basis || r != c.Expected.Reason {
				t.Fatalf("J1-J4: got %s %s %s expected %+v", m.ID, b, r, c.Expected)
			}
		})
	}
}
func TestL5LowSequencesVersionsAndPrivacy(t *testing.T) {
	now := time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC)
	for _, seq := range [][3]int64{{110, 0, 105}, {110, 0, 105}, {120, 100, 115}, {100, 95, 97}} {
		data := f.File(f.Prices(seq[0], seq[1], seq[2]), 8)
		private := []string{"AUCTIONATOR_POSTING_HISTORY", "AUCTIONATOR_SHOPPING_LISTS", "AUCTIONATOR_RECENT_SEARCHES", "AUCTIONATOR_SELLING_GROUPS", "AUCTIONATOR_CONFIG", "AUCTIONATOR_SAVEDVARS", "AUCTIONATOR_CHARACTER_CONFIG", "AUCTIONATOR_VENDOR_PRICE_CACHE"}
		for _, name := range private {
			data = append(data, []byte(name+" = { private = \"PersonalSentinelOnlyInPrivateVariable\" }\n")...)
		}
		kept, e := lua.Parse(data, map[string]bool{auctions.Variable: true})
		if e != nil || len(kept) != 1 {
			t.Fatal("L2 personal variables constructed")
		}
		r, e := auctions.Decode(data, auctions.Hash(data), notes(t, f.Note), nil, "3.11.0", "0.8.0", now)
		if e != nil || len(r.Bodies) != 1 || len(r.Bodies[0].Rows) != 1 {
			t.Fatalf("L3-L6: %+v %v", r, e)
		}
		row := r.Bodies[0].Rows[0]
		if seq[1] == 0 && row.Low != "" || seq[1] > 0 && row.Low == "" || row.Quantity == nil || *row.Quantity != 0 {
			t.Fatalf("L5 absent or zero changed: %+v", row)
		}
		b := r.Bodies[0]
		if b.Update != auctions.Hash([]byte(b.File.SHA+"|"+b.Market.ID)) {
			t.Fatal("H3 update_id")
		}
		raw, _ := json.Marshal(b)
		for _, x := range append(private, "PersonalSentinelOnlyInPrivateVariable", "occurred_at", "low_source") {
			if strings.Contains(string(raw), x) {
				t.Fatalf("L2 forbidden personal data in body: %s", x)
			}
		}
	}
	for _, v := range []int{0, 7, 9} {
		if _, e := auctions.Decode(f.File(f.Prices(110, 0, 105), v), strings.Repeat("a", 64), notes(t, f.Note), nil, "", "0.8.0", now); e == nil {
			t.Fatal("L3 database version accepted")
		}
	}
	m := f.Prices(110, 0, 105)
	m[0].Value = int64(3)
	r, e := auctions.Decode(f.File(m, 8), strings.Repeat("a", 64), notes(t, f.Note), nil, "", "0.8.0", now)
	if e != nil || len(r.Bodies) != 0 || r.Counts["format_non_pris_en_charge"] != 1 {
		t.Fatal("L3 realm version")
	}
	data := f.File(f.Prices(110, 0, 105), 8)
	if _, e = auctions.Decode(data[:len(data)-3], strings.Repeat("a", 64), notes(t, f.Note), nil, "", "0.8.0", now); e == nil {
		t.Fatal("truncated Lua accepted")
	}
}
func TestL6GlobalLatestDayAndRetention(t *testing.T) {
	m := f.Prices(110, 0, 105)
	m = append(m, auctions.Pair{Key: "2592", Value: auctions.Map{{Key: "h", Value: auctions.Map{{Key: int64(2468), Value: int64(120)}}}, {Key: "m", Value: int64(115)}}})
	r, e := auctions.Decode(f.File(m, 8), strings.Repeat("a", 64), notes(t, f.Note), nil, "", "0.8.0", time.Date(2026, 10, 3, 20, 0, 0, 0, time.UTC))
	if e != nil {
		t.Fatal(e)
	}
	for _, b := range r.Bodies {
		for _, row := range b.Rows {
			if row.Item == 2589 && row.Current != "" {
				t.Fatal("L6 m incorrectly attached to item's own latest day")
			}
		}
	}
}
