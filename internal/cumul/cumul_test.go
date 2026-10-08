package cumul_test

// Portage des 14 contrôles de forever-pulse-roadmap/outils/test_cumul.py, mêmes
// attentes. Chaque fichier passe par le vrai chemin : texte Lua → lecteur →
// reconstitution des deltas → cumul.

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"wowsync/internal/app"
	"wowsync/internal/cumul"
	"wowsync/internal/lua"
	"wowsync/internal/store"
	tl "wowsync/internal/testlua"
)

var n int

func bid() string { n++; return fmt.Sprintf("00000000-0000-4000-8000-%012d", n) }

func zone(t int, lignes []string, sur tl.M) tl.M {
	l := tl.M{"batch_id": bid(), "method": "channel_roster", "kind": "full", "chain_index": 0, "observed_at": t,
		"scope_id": "4619-PVP-Alliance", "zone_name": "Stormwind City", "zone_count": len(lignes), "read": len(lignes),
		"guid_prefix": "Player-4619-", "fields": "id|surname|name|class|race|sex|level",
		"classes": tl.L{"MAGE", "PRIEST"}, "races": tl.L{"Human"}, "rows": strings.Join(lignes, "\n"), "rows_count": len(lignes)}
	for k, v := range sur {
		if v == nil {
			delete(l, k)
			continue
		}
		l[k] = v
	}
	return l
}

func who(t int, lignes []string) tl.M {
	return tl.M{"batch_id": bid(), "method": "who_manual", "kind": "full", "observed_at": t, "scope_id": "4619-PVP-Alliance",
		"fields": "name|class_loc|race_loc|level|guild|zone", "rows": strings.Join(lignes, "\n"),
		"rows_count": len(lignes), "read": len(lignes)}
}

func integre(t *testing.T, c *cumul.Cumul, lots ...tl.M) cumul.Stats {
	t.Helper()
	l := make(tl.L, len(lots))
	for i, x := range lots {
		l[i] = x
	}
	v, err := lua.Parse(tl.Fichier(tl.M{"schema": 4, "batches": l}), map[string]bool{"ForeverPulseCensusDB": true})
	if err != nil {
		t.Fatal(err)
	}
	return c.Integrer(app.CumulLots(lua.AsTable(v["ForeverPulseCensusDB"])))
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func TestCumul14Controles(t *testing.T) {
	controles, echecs := 0, 0
	ok := func(cond bool, msg string) {
		controles++
		if !cond {
			echecs++
			t.Error("ECHEC : " + msg)
		}
	}
	c := cumul.Nouveau()

	// 1. /who d'abord, roster ensuite : une seule entrée, qui garde la guilde
	integre(t, c, who(100, []string{"John Pvp|Mage|Human|10|Les Trois Fromages|Elwynn"}))
	p := c.Scopes["4619-PVP-Alliance"]
	_, parNom := p.Persos["nom:john pvp"]
	ok(len(p.Persos) == 1 && parNom, "1 : /who seul -> entrée par nom")
	integre(t, c, zone(200, []string{"000A|Pvp|John|1|1|2|11"}, nil))
	ok(len(p.Persos) == 1, "1 : roster ensuite -> fusion, pas de doublon")
	e := p.Persos["Player-4619-000A"]
	ok(e != nil && e.Vals["guild"] == "Les Trois Fromages" && e.Vals["level"] == 11 &&
		reflect.DeepEqual(e.Sources, []string{"who", "zone"}), "1 : entrée GUID avec guilde, niveau récent, deux sources")

	// 2. Roster d'abord, /who ensuite : rattaché au GUID
	integre(t, c, zone(300, []string{"000B|Doe|Jane|2|1|3|20"}, nil), who(310, []string{"Jane Doe|Prêtre|Humain|20||Stormwind"}))
	ok(len(p.Persos) == 2 && p.Persos["Player-4619-000B"].Vals["guild"] == "", "2 : /who rattaché, « sans guilde » gardé")

	// 3. Idempotence : même fichier relu
	f3 := zone(400, []string{"000C|X|Bob|1|1|2|5"}, nil)
	integre(t, c, f3)
	nb := len(p.Persos)
	s := integre(t, c, f3)
	ok(len(p.Persos) == nb && s.LotsDeja == 1 && s.Lots == 0, "3 : fichier relu -> rien ne change")

	// 4. Pas de régression
	integre(t, c, zone(150, []string{"000A|Pvp|John|1|1|2|3"}, nil))
	ok(p.Persos["Player-4619-000A"].Vals["level"] == 11, "4 : niveau récent conservé")
	ok(*p.Persos["Player-4619-000A"].Premier == 100, "4 : première vue = la plus ancienne")

	// 5. Renommage
	integre(t, c, zone(500, []string{"000C|Y|Robert|1|1|2|6"}, nil))
	ok(len(p.Persos) == nb && str(p.Persos["Player-4619-000C"].DisplayName) == "Robert Y", "5 : renommage")
	_, bob := p.Noms["bob x"]
	ok(!bob && p.Noms["robert y"] == "Player-4619-000C", "5 : l'ancien nom ne pointe plus")

	// 6. Deux périmètres ne se mélangent jamais
	integre(t, c, zone(600, []string{"000A|Pvp|John|1|1|2|11"}, tl.M{"scope_id": "4619-NORMAL-Alliance"}))
	ok(len(c.Scopes) == 2 && len(c.Scopes["4619-NORMAL-Alliance"].Persos) == 1, "6 : périmètres séparés")

	// 7. Canal rejoint d'office et delta : sources comptées, pas de doublon
	base := zone(700, []string{"000D|A|Ann|1|1|3|30", "000E|B|Ben|2|1|2|31"}, nil)
	delta := zone(710, []string{"000F|C|Cid|1|1|2|32"}, tl.M{"kind": "delta", "base_batch_id": base["batch_id"],
		"chain_index": 1, "gone": "000E", "gone_count": 1, "unchanged": 1, "read": 2, "zone_count": 2})
	monde := zone(720, []string{"000D|A|Ann|1|1|3|30"}, tl.M{"source_scope": "world_channel", "channel_name": "Trade - City",
		"channel_category": "CHANNEL_CATEGORY_WORLD", "channel_opt_in": false, "zone_name": nil, "zone_count": nil})
	avant := len(p.Persos)
	integre(t, c, base, delta, monde)
	ok(len(p.Persos) == avant+3, "7 : base + delta + canal -> 3 nouveaux, pas 4")
	ok(reflect.DeepEqual(p.Persos["Player-4619-000D"].Sources, []string{"monde", "zone"}), "7 : sources zone + monde")
	ok(*p.Persos["Player-4619-000D"].Dernier == 720, "7 : dernière vue mise à jour par le canal")

	r := c.Resume(1000)
	ok(r["4619-PVP-Alliance"].Total == len(p.Persos), "8 : résumé cohérent")

	if controles != 14 {
		t.Fatalf("%d contrôles, 14 attendus", controles)
	}
	t.Logf("%d contrôles, %d échec(s)", controles, echecs)

	// Persistance SQLite : l'état relu est identique à l'état en mémoire.
	st, err := store.Open(filepath.Join(t.TempDir(), "wowsync.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if _, err := st.AddFile(context.Background(), store.File{SHA256: "x", Path: "x", Origin: "manual",
		Scopes: json.RawMessage("{}")}, nil, c); err != nil {
		t.Fatal(err)
	}
	relu, err := st.LoadCumul()
	if err != nil {
		t.Fatal(err)
	}
	a, _ := json.Marshal(c.Dump())
	b, _ := json.Marshal(relu.Dump())
	if string(a) != string(b) {
		t.Fatalf("cumul relu différent :\n%s\n%s", a, b)
	}
}
