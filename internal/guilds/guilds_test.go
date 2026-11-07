package guilds

import (
	"strings"
	"testing"
	"time"
	"wowsync/internal/lua"
	tl "wowsync/internal/testlua"
)

func fixture() tl.M {
	return tl.M{"v": 1, "at": time.Now().Add(-time.Hour).Unix(), "scope": "9998-PVP-Alliance", "guild": "Synthetic Guild", "total": 3, "online": 1, "onlineRead": 1, "unknown": 1, "fields": "guid|name|level|class|online", "blocs": tl.L{"Player-9998-0001|Synthetic One|10|MAGE|1\nPlayer-9998-0002|Synthetic Two|11|DRUID|0\nPlayer-9998-0003|Synthetic Three|12|ROGUE|?"}}
}
func parse(t *testing.T, m tl.M) (*Snapshot, error) {
	t.Helper()
	d, e := lua.Parse([]byte("G="+tl.Serialise(m)+"\nS="+tl.Serialise(tl.M{"9998-PVP-Alliance": tl.M{"realm_id": 9998, "ruleset": "PVP", "faction": "Alliance", "region": "3"}})), nil)
	if e != nil {
		t.Fatal(e)
	}
	return Decode(lua.AsTable(d["G"]), lua.AsTable(d["S"]), time.Now())
}
func TestSnapshot(t *testing.T) {
	m := fixture()
	s, e := parse(t, m)
	if e != nil {
		t.Fatal(e)
	}
	if len(s.Members) != 3 || *s.Members[1].Online || s.Members[2].Online != nil {
		t.Fatal("presence lost")
	}
	rows := strings.Split(m["blocs"].(tl.L)[0].(string), "\n")
	m["blocs"] = tl.L{rows[2] + "\n" + rows[0] + "\n" + rows[1]}
	r, e := parse(t, m)
	if e != nil || r.ID != s.ID {
		t.Fatal("order must not change id", e)
	}
}
func TestRejectIncomplete(t *testing.T) {
	for _, edit := range []func(tl.M){func(m tl.M) { m["total"] = 4 }, func(m tl.M) { m["online"] = 0 }, func(m tl.M) { m["unknown"] = 0 }, func(m tl.M) { m["at"] = time.Now().Add(time.Hour).Unix() }, func(m tl.M) { m["blocs"] = tl.L{"Player-9998-0001|Name|10|MAGE|1\nPlayer-9998-0001|Other|10|MAGE|0"} }, func(m tl.M) { m["blocs"] = tl.I{1: m["blocs"].(tl.L)[0], 3: "extra"} }, func(m tl.M) { m["fields"] = "invalid" }} {
		m := fixture()
		edit(m)
		if _, e := parse(t, m); e == nil {
			t.Fatal("accepted incomplete snapshot")
		}
	}
}
