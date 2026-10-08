package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"wowsync/internal/i18n"
	"wowsync/internal/schema"
	"wowsync/internal/store"
	tl "wowsync/internal/testlua"
)

// Talents (addon 4.2.0) : schéma HTTP 2 des statistiques et repli sur le schéma 1
// (contrat « spécialisations » v1, §3.4), contre le faux site de stats_test.go.

const (
	vuTalents  = 1790505847
	taTalents  = "1790505847|30|n|1114|21,0,0|1|0|104001:5,104002:3,104007:1@123456"
	jsonTalent = `{"read_at":"2026-09-27T10:44:07Z","level":30,"source":"nameplate","tree_id":1114,"points":[[104001,5,null],[104002,3,null],[104007,1,123456]],"branch_points":[21,0,0],"dominant":1,"off_grid":0}`
	jsonArbres = `[{"tree_id":1114,"class":"MAGE","build":70170,"locale":"enUS","exported_at":"2026-10-02T11:20:00Z","off_grid":[104982],"nodes":[` +
		`{"id":104001,"x":1020,"y":2130,"max_ranks":5,"branch":1,"spell_id":14522,"name":"Arcane Focus","entries":null},` +
		`{"id":104002,"x":2820,"y":2730,"max_ranks":3,"branch":1,"spell_id":14523,"name":"Arcane Mind","entries":null},` +
		`{"id":104007,"x":5020,"y":2130,"max_ranks":1,"branch":2,"spell_id":15237,"name":"Blast Wave","entries":[[123456,15237],[123457,null]]},` +
		`{"id":104982,"x":102800,"y":900,"max_ranks":1,"branch":3,"spell_id":31661,"name":"Dragon's Breath","entries":null}]}]`
	ficheTalents = "beta-4619-PVP/Player-4619-00000001"
	ficheSans    = "beta-4619-PVP/Player-4619-00000002"
)

func arbresTest() tl.I {
	return tl.I{1114: tl.M{"v": 1, "t": 1790940000, "build": 70170, "locale": "enUS", "c": "MAGE", "hg": "104982",
		"n": "104001|1020|2130|5|1|14522|Arcane Focus\n104002|2820|2730|3|1|14523|Arcane Mind\n" +
			"104007|5020|2130|1|2|15237|Blast Wave|123456:15237;123457:\n104982|102800|900|1|3|31661|Dragon's Breath\n"}}
}

// fichierAvecTalents : la fiche 1 porte un relevé de talents, la fiche 2 aucun.
func fichierAvecTalents(seed, vu int, valeur string) []byte {
	return fichierTalents(seed, vu, map[string][]string{
		"00000001": {"1790505847|20|n|60:1"},
		"00000002": {"1790505847|20|n|60:" + valeur},
	}, map[string]string{"00000001": taTalents}, arbresTest())
}

func cles(t *testing.T, obj []byte) ([]string, map[string]string) {
	t.Helper()
	ms, err := schema.Membres(obj)
	if err != nil {
		t.Fatalf("%v : %s", err, obj)
	}
	var k []string
	v := map[string]string{}
	for _, m := range ms {
		k = append(k, m.K)
		v[m.K] = string(m.V)
	}
	return k, v
}

func TestTalentsSchema2CorpsExactEtIdempotence(t *testing.T) {
	b := nouveauBancStats(t)
	ctx := context.Background()
	data := fichierAvecTalents(1, vuTalents, "2")
	p := b.fichier("ForeverPulse.lua", data)
	if _, err := b.a.ProcessFile(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	b.etat(0, 2, 0)
	if h := b.srv.entetes; h.Get("X-Schema") != "2" || h.Get("X-Source") != "forever-pulse-stats" {
		t.Fatalf("en-têtes : %v", h)
	}
	f, _, _, _ := DecodeTout(data, b.now)
	k, v := cles(t, b.srv.corpsStats[0])
	if strings.Join(k, ",") != "schema,companion,file,contexts,catalog,talent_trees,characters" {
		t.Fatalf("clés du corps : %v", k)
	}
	if v["schema"] != "2" || v["catalog"] != "null" || v["talent_trees"] != jsonArbres {
		t.Fatalf("corps : %s", b.srv.corpsStats[0])
	}
	wantFile := `{"sha256":"` + sha256Hex(data) + `","origin":"current","addon_version":"` + f.AddonVersion +
		`","stats_version":4,"locale":"enUS","talents_version":1}`
	if v["file"] != wantFile {
		t.Fatalf("file :\n%s\nattendu :\n%s", v["file"], wantFile)
	}
	var fiches []json.RawMessage
	_ = json.Unmarshal([]byte(v["characters"]), &fiches)
	if len(fiches) != 2 {
		t.Fatalf("fiches : %d", len(fiches))
	}
	k1, v1 := cles(t, fiches[0])
	k2, _ := cles(t, fiches[1])
	if strings.Join(k1, ",") != "id,context,guid,name,realm,class,race,faction,sex,level,seen_at,read_at,readings,talents" ||
		v1["talents"] != jsonTalent || strings.Join(k2, ",") != "id,context,guid,name,realm,class,race,faction,sex,level,seen_at,read_at,readings" {
		t.Fatalf("fiches : %s / %s", fiches[0], fiches[1])
	}
	if b.srv.talents[ficheTalents] != jsonTalent || b.srv.talents[ficheSans] != "" || string(b.srv.arbres) != jsonArbres {
		t.Fatalf("reçu par le site : %v", b.srv.talents)
	}

	// Idempotence : même fichier, même contenu sous une autre empreinte, nouvel envoi automatique.
	if n, _ := b.a.ProcessFile(ctx, p); n != 0 {
		t.Fatal("fichier relu remis en file")
	}
	b.a.ProcessFile(ctx, b.fichier("autre.lua", append(data, '\n')))
	b.etat(0, 2, 0)
	b.now = b.now.Add(time.Hour)
	_ = b.a.Flush(ctx, false)
	if b.srv.appelsStats != 1 || b.srv.recusTalents[ficheTalents] != 1 {
		t.Fatalf("appels %d, talents reçus %d", b.srv.appelsStats, b.srv.recusTalents[ficheTalents])
	}

	// Un nouveau relevé de talents fait repartir la fiche, elle seule.
	autre := fichierTalents(2, vuTalents, map[string][]string{
		"00000001": {"1790505847|20|n|60:1"}, "00000002": {"1790505847|20|n|60:2"},
	}, map[string]string{"00000001": strings.Replace(taTalents, "104002:3", "104002:2", 1)}, arbresTest())
	if n, err := b.a.ProcessFile(ctx, b.fichier("b.lua", autre)); err != nil || n != 1 {
		t.Fatalf("mise en file : %d, %v", n, err)
	}
	b.etat(1, 1, 0)
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if b.srv.recusTalents[ficheTalents] != 2 || !strings.Contains(b.srv.talents[ficheTalents], "[104002,2,null]") {
		t.Fatalf("nouveau relevé non reçu : %s", b.srv.talents[ficheTalents])
	}
}

// Site PROD actuel (schéma 1 seul) : repli, lot accepté sans talents, mode
// mémorisé après redémarrage ; puis site au schéma 2 : nouvel essai après 6 h
// seulement, et les fiches à talents envoyées en schéma 1 repartent une fois.
func TestTalentsRepliSchema1PuisRetourSchema2(t *testing.T) {
	b := nouveauBancStats(t)
	ctx := context.Background()
	b.srv.schemas = []int{1}
	debut := b.now
	b.a.ProcessFile(ctx, b.fichier("a.lua", fichierAvecTalents(1, vuTalents, "2")))
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if strings.Join(b.srv.schemasVus, ",") != "2,1" {
		t.Fatalf("schémas essayés : %v", b.srv.schemasVus)
	}
	b.etat(0, 2, 0)
	corps := string(b.srv.corpsStats[1])
	if !strings.HasPrefix(corps, `{"schema":1,`) || strings.Contains(corps, "talent") {
		t.Fatalf("corps de repli : %s", corps)
	}
	if strings.Contains(b.srv.fiches[ficheTalents], "talents") || len(b.srv.talents) != 0 {
		t.Fatal("talents transmis en schéma 1")
	}
	e := b.a.Etat()
	ligne := i18n.T("det.statsschema1", debut.Add(ReessaiSchema2).Local().Format(i18n.T("fmt.datetime")))
	if e.Couleur != Vert || !strings.Contains(strings.Join(e.Details, "\n"), ligne) {
		t.Fatalf("état : %+v", e)
	}

	// Redémarrage : le mode schéma 1 est relu de la base, sans nouvel essai du 2.
	b.st.Close()
	b.ouvre()
	b.a.ProcessFile(ctx, b.fichier("b.lua", fichierAvecTalents(2, vuTalents+100, "3")))
	b.etat(2, 0, 0)
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if strings.Join(b.srv.schemasVus, ",") != "2,1,1" {
		t.Fatalf("schémas après redémarrage : %v", b.srv.schemasVus)
	}
	b.etat(0, 2, 0)

	// Le site passe au schéma 2, mais 5 h seulement se sont écoulées : toujours le 1.
	b.srv.schemas = nil
	b.now = debut.Add(5 * time.Hour)
	b.a.ProcessFile(ctx, b.fichier("c.lua", fichierAvecTalents(3, vuTalents+200, "4")))
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if strings.Join(b.srv.schemasVus, ",") != "2,1,1,1" || len(b.srv.talents) != 0 {
		t.Fatalf("schéma 2 retenté avant 6 h : %v", b.srv.schemasVus)
	}

	// Après 6 h : seule la fiche sans talents a changé. Le schéma 2 est accepté, la
	// fiche à talents envoyée en schéma 1 repart une fois, sans changer d'empreinte.
	b.now = debut.Add(ReessaiSchema2 + time.Minute)
	b.a.ProcessFile(ctx, b.fichier("d.lua", fichierTalents(4, vuTalents+300, map[string][]string{
		"00000002": {"1790505847|20|n|60:5"}}, nil, arbresTest())))
	b.etat(1, 1, 0)
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	if strings.Join(b.srv.schemasVus, ",") != "2,1,1,1,2,2" {
		t.Fatalf("schémas au retour : %v", b.srv.schemasVus)
	}
	if b.srv.talents[ficheTalents] != jsonTalent || b.srv.recusTalents[ficheTalents] != 1 || b.srv.talents[ficheSans] != "" {
		t.Fatalf("talents reçus au retour : %v", b.srv.talents)
	}
	if !strings.Contains(string(b.srv.corpsStats[5]), `"talent_trees":`+jsonArbres) {
		t.Fatalf("catalogue des arbres absent du renvoi : %s", b.srv.corpsStats[5])
	}
	b.etat(0, 2, 0)
	if e := b.a.Etat(); strings.Contains(strings.Join(e.Details, "\n"), ligne) {
		t.Fatal("mode schéma 1 encore affiché")
	}

	// Une fois seulement : ni un nouvel envoi automatique ni un redémarrage ne les renvoient.
	appels := b.srv.appelsStats
	b.now = b.now.Add(7 * time.Hour)
	_ = b.a.Flush(ctx, false)
	b.st.Close()
	b.ouvre()
	_ = b.a.Flush(ctx, true)
	if b.srv.appelsStats != appels || b.srv.recusTalents[ficheTalents] != 1 {
		t.Fatalf("renvoi en trop : %d appels, talents reçus %d fois", b.srv.appelsStats-appels, b.srv.recusTalents[ficheTalents])
	}
}

// Un 409 qui n'annonce pas le schéma 1 garde le comportement de la 0.9.0-rc.1.
func TestTalents409SansSchema1(t *testing.T) {
	b := nouveauBancStats(t)
	ctx := context.Background()
	b.srv.schemas = []int{3}
	b.a.ProcessFile(ctx, b.fichier("a.lua", fichierAvecTalents(1, vuTalents, "2")))
	if err := b.a.Flush(ctx, false); err == nil {
		t.Fatal("409 sans erreur")
	}
	if strings.Join(b.srv.schemasVus, ",") != "2" || b.st.Get("stats_blocked") != BlocSchema {
		t.Fatalf("schémas %v, blocage %q", b.srv.schemasVus, b.st.Get("stats_blocked"))
	}
	if e := b.a.Etat(); e.Sent != 1 || e.StatsPending != 2 || e.Couleur == Rouge {
		t.Fatalf("état : %+v", e)
	}
	b.now = b.now.Add(7 * time.Hour)
	_ = b.a.Flush(ctx, false)
	if b.srv.appelsStats != 1 {
		t.Fatal("409 : nouvel essai automatique")
	}
}

// Fichier de l'addon 4.1.0 (ni ta, ni arbres, ni talents_v) : mêmes fiches, mêmes
// empreintes ; schéma 2 avec talents_version null et talent_trees vide ; en repli,
// corps identique à celui de la 0.9.0-rc.1.
func TestTalentsFichierAddon410(t *testing.T) {
	b := nouveauBancStats(t)
	ctx := context.Background()
	data := fichierStats(1, vuTalents, map[string][]string{"00000001": {"1790505847|20|n|60:1"}, "00000002": {"1790505847|20|n|60:2"}})
	_, _, sf, err := DecodeTout(data, b.now)
	if err != nil || sf.TalentsVersion != nil || len(sf.Arbres) != 0 || sf.AvecTalents != 0 {
		t.Fatalf("décodage : %+v %v", sf, err)
	}
	b.a.ProcessFile(ctx, b.fichier("a.lua", data))
	if err := b.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	_, v := cles(t, b.srv.corpsStats[0])
	if v["talent_trees"] != "[]" || !strings.HasSuffix(v["file"], `"locale":"enUS","talents_version":null}`) ||
		strings.Contains(v["characters"], "talents") {
		t.Fatalf("corps : %s", b.srv.corpsStats[0])
	}
	if v["characters"] != "["+string(sf.Fiches[0].Payload)+","+string(sf.Fiches[1].Payload)+"]" {
		t.Fatal("fiches modifiées")
	}

	// Repli : le corps du schéma 1 est celui qu'aurait construit la 0.9.0-rc.1.
	b2 := nouveauBancStats(t)
	b2.srv.schemas = []int{1}
	b2.a.ProcessFile(ctx, b2.fichier("a.lua", data))
	if err := b2.a.Flush(ctx, false); err != nil {
		t.Fatal(err)
	}
	k, v1 := cles(t, b2.srv.corpsStats[1])
	if strings.Join(k, ",") != "schema,companion,file,contexts,catalog,characters" || v1["schema"] != "1" ||
		strings.Contains(string(b2.srv.corpsStats[1]), "talent") || v1["characters"] != v["characters"] {
		t.Fatalf("corps de repli : %s", b2.srv.corpsStats[1])
	}
}

// Une meta enregistrée par la 0.9.0-rc.1 (base existante, fiches en attente) part
// au schéma 2 avec les valeurs par défaut, et au schéma 1 exactement comme avant.
func TestStatsBodyMetaAnterieure(t *testing.T) {
	meta := `{"file":{"sha256":"` + strings.Repeat("a", 64) + `","origin":"current","addon_version":"4.1.0","stats_version":4,"locale":"enUS"},"contexts":{},"catalog":null}`
	sheets := []store.StatSheet{{ID: "x/1", Payload: []byte(`{"id":"x/1"}`)}, {ID: "x/2", Payload: []byte(`{"id":"x/2","talents":{"read_at":"2026-09-27T10:44:07Z"}}`)}}
	comp := `"companion":{"name":"` + Nom + `","version":"` + Version + `"},`
	v1, err := StatsBody([]byte(meta), sheets, 1)
	if err != nil || string(v1) != `{"schema":1,`+comp+meta[1:len(meta)-1]+`,"characters":[{"id":"x/1"},{"id":"x/2"}]}` {
		t.Fatalf("schéma 1 : %s %v", v1, err)
	}
	v2, err := StatsBody([]byte(meta), sheets, 2)
	want := `{"schema":2,` + comp + `"file":{"sha256":"` + strings.Repeat("a", 64) + `","origin":"current","addon_version":"4.1.0","stats_version":4,"locale":"enUS","talents_version":null},"contexts":{},"catalog":null,"talent_trees":[],"characters":[{"id":"x/1"},{"id":"x/2","talents":{"read_at":"2026-09-27T10:44:07Z"}}]}`
	if err != nil || string(v2) != want {
		t.Fatalf("schéma 2 :\n%s\nattendu :\n%s", v2, want)
	}
	if _, err := StatsBody([]byte(meta), sheets, 3); err == nil {
		t.Fatal("schéma 3 accepté")
	}
}
