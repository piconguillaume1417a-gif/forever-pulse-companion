// Package guilds decodes one complete guild snapshot, never a census sighting.
package guilds

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
	"wowsync/internal/lua"
)

const Variable = "ForeverPulseGuildDB"
const MaxMembers = 10000

type Member struct {
	GUID   string `json:"guid"`
	Name   string `json:"name"`
	Level  int    `json:"level"`
	Class  string `json:"class"`
	Online *bool  `json:"online"`
}
type Scope struct {
	Realm   int64  `json:"realm_id"`
	Ruleset string `json:"ruleset"`
	Faction string `json:"faction"`
	Region  string `json:"region"`
}
type Snapshot struct {
	ID         string   `json:"id"`
	ScopeID    string   `json:"scope_id"`
	Scope      Scope    `json:"scope"`
	Guild      string   `json:"guild"`
	ObservedAt string   `json:"observed_at"`
	Total      int      `json:"total"`
	Online     int      `json:"online"`
	Members    []Member `json:"members"`
}
type Body struct {
	Schema    int        `json:"schema"`
	Companion string     `json:"companion"`
	Snapshots []Snapshot `json:"snapshots"`
}

var guid = regexp.MustCompile(`^Player-[0-9]+-[0-9A-Fa-f]+$`)
var scopeID = regexp.MustCompile(`^[1-9][0-9]*-(PVP|PVE|NORMAL|RP|RP_PVP|RPPVP|HARDCORE)-(Alliance|Horde|Neutral)$`)
var class = regexp.MustCompile(`^[A-Z_]{1,40}$`)

func Text(s string, max int) bool {
	return len(s) > 0 && utf8.ValidString(s) && utf8.RuneCountInString(s) <= max && !strings.ContainsAny(s, "|\r\n\x00") && strings.IndexFunc(s, func(r rune) bool { return r < 32 || r == 127 }) < 0
}

// Decode rejects an incomplete snapshot as a whole: missing members are not departures.
func Decode(t *lua.Table, scopes *lua.Table, now time.Time) (*Snapshot, error) {
	if t == nil {
		return nil, nil
	}
	bad := errors.New("guild snapshot invalid or incomplete")
	v, _ := lua.AsInt(t.Get("v"))
	at, ok := lua.AsInt(t.Get("at"))
	if v != 1 || !ok || at < 1 || at > now.Add(5*time.Minute).Unix() || lua.Str(t.Get("fields")) != "guid|name|level|class|online" {
		return nil, bad
	}
	s := Snapshot{ScopeID: lua.Str(t.Get("scope")), Guild: lua.Str(t.Get("guild")), ObservedAt: time.Unix(at, 0).UTC().Format(time.RFC3339), Members: []Member{}}
	if !scopeID.MatchString(s.ScopeID) || !Text(s.Guild, 120) {
		return nil, bad
	}
	context := lua.AsTable(t.Get("context"))
	if context == nil {
		context = lua.AsTable(scopes.Get(s.ScopeID))
	}
	if context == nil {
		return nil, bad
	}
	s.Scope.Realm, ok = lua.AsInt(context.Get("realm_id"))
	if !ok || s.Scope.Realm < 1 || s.Scope.Realm > 2147483647 {
		return nil, bad
	}
	s.Scope.Ruleset = lua.Str(context.Get("ruleset"))
	s.Scope.Faction = lua.Str(context.Get("faction"))
	s.Scope.Region = lua.Str(context.Get("region"))
	if s.ScopeID != strconv.FormatInt(s.Scope.Realm, 10)+"-"+s.Scope.Ruleset+"-"+s.Scope.Faction || !Text(s.Scope.Region, 40) {
		return nil, bad
	}
	total, tok := lua.AsInt(t.Get("total"))
	online, ook := lua.AsInt(t.Get("online"))
	if !tok || !ook || total < 1 || total > MaxMembers || online < 0 || online > total {
		return nil, bad
	}
	s.Total = int(total)
	s.Online = int(online)
	blocks := lua.AsTable(t.Get("blocs"))
	if blocks == nil || len(blocks.Int) > 313 || len(blocks.Str) != 0 || len(blocks.Other) != 0 {
		return nil, bad
	}
	seen := map[string]bool{}
	connected, unknown := 0, 0
	for _, b := range blocks.List() {
		str, ok := b.(string)
		if !ok || len(str) > 32768 {
			return nil, bad
		}
		for _, row := range strings.Split(str, "\n") {
			c := strings.Split(row, "|")
			if len(c) != 5 {
				return nil, bad
			}
			lv, e := strconv.Atoi(c[2])
			if e != nil || lv < 1 || lv > 100 || !guid.MatchString(c[0]) || len(c[0]) > 80 || seen[c[0]] || !Text(c[1], 120) || !class.MatchString(c[3]) {
				return nil, bad
			}
			seen[c[0]] = true
			m := Member{GUID: c[0], Name: c[1], Level: lv, Class: c[3]}
			switch c[4] {
			case "1":
				v := true
				m.Online = &v
				connected++
			case "0":
				v := false
				m.Online = &v
			case "?":
				unknown++
			default:
				return nil, bad
			}
			s.Members = append(s.Members, m)
			if len(s.Members) > MaxMembers {
				return nil, bad
			}
		}
	}
	read, rok := lua.AsInt(t.Get("onlineRead"))
	unk, uok := lua.AsInt(t.Get("unknown"))
	if len(blocks.List()) != len(blocks.Int) || len(s.Members) != s.Total || !rok || !uok || int(read) != connected || int(unk) != unknown || connected > s.Online || connected+unknown < s.Online {
		return nil, bad
	}
	// Content id includes date and scope but excludes the file hash and row order.
	// The server uses scope + guild + date for chronology; repeated file reads keep this id.
	sort.Slice(s.Members, func(i, j int) bool { return s.Members[i].GUID < s.Members[j].GUID })
	p, _ := json.Marshal(s)
	h := sha256.Sum256(p)
	s.ID = hex.EncodeToString(h[:])
	return &s, nil
}
func (s Snapshot) Key() string {
	h := sha256.Sum256([]byte(s.Scope.Region + "\n" + s.ScopeID + "\n" + s.Guild))
	return hex.EncodeToString(h[:])
}
