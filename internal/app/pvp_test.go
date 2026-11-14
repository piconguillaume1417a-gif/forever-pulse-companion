package app

import (
	"strings"
	"testing"

	"wowsync/internal/store"
)

// Le rang JcJ (characters[].pvp) part dans les deux schémas : le schéma 1 retire
// talents mais garde pvp, à sa place, octet pour octet.
func TestStatsBodyPvpDansLesDeuxSchemas(t *testing.T) {
	meta := `{"file":{"sha256":"` + strings.Repeat("a", 64) + `","origin":"current","addon_version":"5.2.0","stats_version":4,"locale":"enUS","talents_version":1},"contexts":{},"catalog":null,"talent_trees":[]}`
	pvp := `"pvp":{"read_at":"2026-11-14T13:00:00Z","rank":3,"honorable_kills":1002}`
	sheets := []store.StatSheet{
		{ID: "x/1", Payload: []byte(`{"id":"x/1","readings":[],` + pvp + `}`)},
		{ID: "x/2", Payload: []byte(`{"id":"x/2","readings":[],"talents":{"read_at":"2026-09-27T10:44:07Z"},` + pvp + `}`)},
		{ID: "x/3", Payload: []byte(`{"id":"x/3","readings":[]}`)},
	}
	comp := `"companion":{"name":"` + Nom + `","version":"` + Version + `"},`
	v1, err := StatsBody([]byte(meta), sheets, 1)
	want1 := `{"schema":1,` + comp + `"file":{"sha256":"` + strings.Repeat("a", 64) + `","origin":"current","addon_version":"5.2.0","stats_version":4,"locale":"enUS"},"contexts":{},"catalog":null,` +
		`"characters":[{"id":"x/1","readings":[],` + pvp + `},{"id":"x/2","readings":[],` + pvp + `},{"id":"x/3","readings":[]}]}`
	if err != nil || string(v1) != want1 {
		t.Fatalf("schéma 1 :\n%s\nattendu :\n%s", v1, want1)
	}
	v2, err := StatsBody([]byte(meta), sheets, 2)
	want2 := `{"schema":2,` + comp + meta[1:len(meta)-1] + `,"characters":[` + string(sheets[0].Payload) + `,` + string(sheets[1].Payload) + `,` + string(sheets[2].Payload) + `]}`
	if err != nil || string(v2) != want2 {
		t.Fatalf("schéma 2 :\n%s\nattendu :\n%s", v2, want2)
	}
}
