package app

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	tl "wowsync/internal/testlua"
)

func TestStreamingPreservesSharedDictionaryAndDeltaAcrossRegions(t *testing.T) {
	b := nouveauBancStats(t)
	const scope = "4619-PVP-Alliance"
	makeSave := func(region string, seed int) []byte {
		base := fmt.Sprintf("%08d-0000-4000-8000-000000000001", seed)
		delta := fmt.Sprintf("%08d-0000-4000-8000-000000000002", seed)
		common := func(id string, at int, rows string) tl.M {
			return tl.M{"batch_id": id, "scope_id": scope, "method": "channel_roster", "observed_at": at,
				"zone_name": "Synthetic", "ui_map_id": 1, "zone_count": 2, "read": 2, "rows": rows}
		}
		first := common(base, 1790180000, "00000001|1|2|1|1|2|20\n00000002|3|4|1|1|2|20")
		second := common(delta, 1790180010, "00000002|3|4|1|1|2|25")
		second["kind"], second["chain_index"], second["base_batch_id"], second["unchanged"] = "delta", 1, base, 1
		return tl.Fichier(tl.M{"schema": 6, "addon": tl.M{"version": "5.0.1-test.3"},
			"observer": tl.M{"id": "ae536a98-ca7c-4d1f-9f95-6ab41e6b0001"},
			"scopes":   tl.M{scope: tl.M{"realm_id": 4619, "ruleset": "PVP", "faction": "Alliance", "region": region}},
			"dicts":    tl.M{"classes": tl.L{"MAGE"}, "races": tl.L{"Human"}, "names": tl.L{"First\nPerson", "Second\nSynthetic"}},
			"batches":  tl.L{second, first}})
	}
	for i, r := range []string{"90", "3"} {
		path := b.fichier(fmt.Sprintf("save-%d.lua", i), makeSave(r, i+1))
		if n, e := b.a.ProcessFile(context.Background(), path); e != nil || n != 2 {
			t.Fatalf("region %s: added=%d, err=%v", r, n, e)
		}
		if i == 0 {
			_, pending, e := b.st.Pending(1 << 20)
			if e != nil || len(pending) != 2 {
				t.Fatalf("pending: %v %v", pending, e)
			}
			var p struct {
				Reconstructed bool `json:"reconstructed_from_delta"`
				Characters    []struct {
					Name  string
					Level int
				}
			}
			if e = json.Unmarshal(pending[1].Payload, &p); e != nil || !p.Reconstructed || len(p.Characters) != 2 || p.Characters[1].Level != 25 {
				t.Fatalf("delta: %+v, %v", p, e)
			}
		}
	}
	c, e := b.st.ResumeCumul()
	if e != nil || len(c) != 2 || c[0].Total != 2 || c[1].Total != 2 {
		t.Fatalf("regional identity collision: %v %v", c, e)
	}
}
