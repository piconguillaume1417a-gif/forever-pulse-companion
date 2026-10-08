package auctions

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
	"wowsync/internal/lua"
)

const Epoch int64 = 1577836800

type Market struct {
	ID          string `json:"id"`
	Project     int64  `json:"project_id"`
	Environment string `json:"environment"`
	Region      int64  `json:"region"`
	Realm       int64  `json:"realm_id"`
	Ruleset     string `json:"ruleset"`
	Faction     string `json:"faction"`
	Name        string `json:"realm_name"`
}
type note struct {
	ts, dc, tl      int64
	clocks          bool
	market          Market
	av              string
	complete, hotel bool
}
type Notes struct {
	Reason string
	Since  int64
	realms map[string][]note
}

var avRE = regexp.MustCompile(`^[0-9A-Za-z._-]{1,20}$`)

func text(s string, max int) bool {
	return len(s) > 0 && len(s) <= max && utf8.ValidString(s) && !strings.ContainsAny(s, "|\x7f") && strings.IndexFunc(s, func(r rune) bool { return r < 32 }) < 0
}
func integer(s string, lo, hi int64) (int64, bool) {
	n, e := strconv.ParseInt(s, 10, 64)
	return n, e == nil && n >= lo && n <= hi
}
func ReadNotes(hdv *lua.Table, folder string) Notes {
	n := Notes{realms: map[string][]note{}}
	if hdv == nil {
		n.Reason = "note_absente"
		return n
	}
	if v, ok := lua.AsInt(hdv.Get("v")); !ok || v != 2 {
		n.Reason = "note_version_inconnue"
		return n
	}
	n.Since, _ = lua.AsInt(hdv.Get("depuis"))
	if n.Since < 1 {
		n.Since = 0
	}
	raw, ok := hdv.Get("notes").(string)
	if !ok || len(raw) > 650000 {
		n.Reason = "note_illisible"
		return n
	}
	lines := strings.Split(raw, "\n")
	if len(lines) > 61*65+1 {
		n.Reason = "note_illisible"
		return n
	}
	for _, line := range lines {
		if line == "" {
			continue
		}
		p := strings.Split(line, "|")
		if len(p) != 13 || len(line) > 160 || !text(p[8], 64) {
			n.Reason = "note_illisible"
			return n
		}
		ts, ok := integer(p[0], 1, 253402300799)
		if !ok {
			n.Reason = "note_illisible"
			return n
		}
		dc, dcOK := integer(p[1], -253402300799, 253402300799)
		tl, tlOK := integer(p[3], 1, 253402300799)
		x := note{ts: ts, dc: dc, tl: tl, clocks: dcOK && tlOK}
		m := Market{Name: p[8]}
		m.Project, _ = integer(p[4], 1, 9999)
		m.Region, _ = integer(p[6], 1, 9999)
		m.Realm, _ = integer(p[9], 1, 2147483647)
		switch p[7] {
		case "NORMAL", "PVP", "RP", "HARDCORE":
			m.Ruleset = p[7]
		}
		f := strings.ToLower(folder)
		beta, ptr := strings.Contains(f, "beta"), strings.Contains(f, "ptr")
		switch {
		case p[5] == "0" && !beta && !ptr && strings.HasPrefix(f, "_") && strings.HasSuffix(f, "_"):
			m.Environment = "live"
		case p[5] == "1" && beta && !ptr:
			m.Environment = "beta"
		case p[5] == "1" && ptr && !beta:
			m.Environment = "ptr"
		}
		if avRE.MatchString(p[12]) {
			x.av = p[12]
		}
		mapID, _ := integer(p[11], 1, 2147483647)
		switch mapID {
		case 1446, 1434, 1452:
			m.Faction = "Neutral"
		case 1453, 1455, 1457:
			if p[10] == "Alliance" {
				m.Faction = "Alliance"
			}
		case 1454, 1456, 1458:
			if p[10] == "Horde" {
				m.Faction = "Horde"
			}
		}
		_, tzOK := integer(p[2], -720, 840)
		x.complete = m.Project > 0 && m.Region > 0 && m.Realm > 0 && m.Ruleset != "" && m.Environment != "" && x.av != "" && x.clocks && tzOK
		x.hotel = m.Faction != "" && (p[10] == "Alliance" || p[10] == "Horde")
		m.ID = fmt.Sprintf("%d-%s-%d-%d-%s-%s", m.Project, m.Environment, m.Region, m.Realm, m.Ruleset, m.Faction)
		x.market = m
		n.realms[p[8]] = append(n.realms[p[8]], x)
	}
	return n
}

// Join applies J1-J4 in contract order; days are counted once per realm, not per item.
func (n Notes) Join(key string, day int64) (Market, string, string) {
	if n.Reason != "" {
		return Market{}, "", n.Reason
	}
	var names []string
	for r := range n.realms {
		if key == r || key == strings.ReplaceAll(r, " ", "") {
			names = append(names, r)
		}
	}
	if len(names) == 0 {
		return Market{}, "", "royaume_sans_note"
	}
	if len(names) > 1 {
		return Market{}, "", "royaume_ambigu"
	}
	start, end := Epoch+(day-1)*86400, Epoch+(day+2)*86400
	if n.Since == 0 || n.Since > start {
		return Market{}, "", "avant_note"
	}
	var selected []note
	for _, x := range n.realms[names[0]] {
		// Unknown dc/tl cannot establish that a note lies outside the window.
		if !x.clocks || (x.ts+x.dc >= start && x.ts+x.dc < end) || (x.tl >= start && x.tl < end) {
			selected = append(selected, x)
		}
	}
	if len(selected) == 0 {
		return Market{}, "", "jour_sans_note"
	}
	for _, x := range selected {
		if !x.complete {
			return Market{}, "", "jour_identite_incomplete"
		}
	}
	for _, x := range selected {
		if !x.hotel {
			return Market{}, "", "jour_hdv_non_reconnu"
		}
	}
	for _, x := range selected {
		if x.market.ID != selected[0].market.ID {
			return Market{}, "", "jour_multi_marches"
		}
	}
	for _, x := range selected {
		if x.av != selected[0].av {
			return Market{}, "", "version_auctionator_multiple"
		}
	}
	return selected[0].market, "auctionator/" + selected[0].av + "/db8/unverified", ""
}
