package stats

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"wowsync/internal/schema"
	tl "wowsync/internal/testlua"
)

// pvNominal : lu le 27/09/2026 à 11:00 UTC, après les relevés de la fiche (10:44:07).
const pvNominal = "1790506800:3:1002"

const pvpNominal = `{"read_at":"2026-09-27T11:00:00Z","rank":3,"honorable_kills":1002}`

func fichePv(ta, pv any) tl.M {
	f := ficheTa(ta)
	if pv != nil {
		f["pv"] = pv
	}
	return f
}

func cles(t *testing.T, payload []byte) []string {
	t.Helper()
	ms, err := schema.Membres(payload)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, m.K)
	}
	return out
}

// Sans pv, la charge et l'empreinte restent celles de la 0.9.0-rc.1, octet pour
// octet, avec ou sans talents_v ; aucune ligne de journal pvp n'est ajoutée.
func TestPvpAbsentChargeInchangee(t *testing.T) {
	for _, db := range []tl.M{base(tl.M{"Player-4619-00000001": fichePv(nil, nil)}), baseTalents(tl.M{"Player-4619-00000001": fichePv(nil, nil)})} {
		f := decodeA(t, db)
		if len(f.Fiches) != 1 || string(f.Fiches[0].Payload) != chargeRC1 || f.Fiches[0].Empreinte != empreinteRC1 {
			t.Fatalf("charge : %s", f.Fiches[0].Payload)
		}
		if f.Fiches[0].Pvp != nil || f.AvecPvp != 0 || f.PvpIllisibles != 0 || f.PvpHorsBornes != 0 {
			t.Fatalf("pvp inattendu : %+v", f)
		}
		for _, l := range f.Infos {
			if strings.Contains(l, "JcJ") {
				t.Fatalf("ligne pvp sans pv : %q", l)
			}
		}
	}
}

// Avec pv seul : la clé pvp part en dernier, après readings ; le reste de la
// charge est celle de la 0.9.0-rc.1. Le pv est lu même sans talents_v.
func TestPvpNominalSansTalents(t *testing.T) {
	f := decodeA(t, base(tl.M{"Player-4619-00000001": fichePv(nil, pvNominal)}))
	if len(f.Fiches) != 1 || f.AvecPvp != 1 || f.PvpIllisibles != 0 || f.PvpHorsBornes != 0 {
		t.Fatalf("fiches %d, avec pvp %d, illisibles %d, hors bornes %d", len(f.Fiches), f.AvecPvp, f.PvpIllisibles, f.PvpHorsBornes)
	}
	fi := f.Fiches[0]
	if p := string(fi.Payload); p != chargeRC1[:len(chargeRC1)-1]+`,"pvp":`+pvpNominal+"}" {
		t.Fatalf("charge : %s", p)
	}
	if fi.Empreinte == empreinteRC1 {
		t.Fatal("empreinte inchangée malgré pvp")
	}
	if fi.Pvp == nil || fi.Pvp.T != 1790506800 || fi.Pvp.Rang != 3 || fi.Pvp.Victoires != 1002 {
		t.Fatalf("pvp : %+v", fi.Pvp)
	}
	vu := false
	for _, l := range f.Infos {
		vu = vu || l == "rang JcJ : 1 fiches avec rang, 0 illisibles et 0 hors bornes ignorés"
	}
	if !vu {
		t.Fatalf("infos : %v", f.Infos)
	}
}

// Avec ta et pv : ordre exact … readings, talents, pvp.
func TestPvpApresTalents(t *testing.T) {
	f := decodeA(t, baseTalents(tl.M{"Player-4619-00000001": fichePv(taNominal, pvNominal)}))
	fi := f.Fiches[0]
	if f.AvecTalents != 1 || f.AvecPvp != 1 {
		t.Fatalf("avec talents %d, avec pvp %d", f.AvecTalents, f.AvecPvp)
	}
	want := "id,context,guid,name,realm,class,race,faction,sex,level,seen_at,read_at,readings,talents,pvp"
	if got := strings.Join(cles(t, fi.Payload), ","); got != want {
		t.Fatalf("clés : %s", got)
	}
	if !strings.HasSuffix(string(fi.Payload), `},"pvp":`+pvpNominal+"}") || !strings.HasPrefix(string(fi.Payload), chargeRC1[:len(chargeRC1)-1]+`,"talents":{`) {
		t.Fatalf("charge : %s", fi.Payload)
	}
}

// Schéma 1 retire talents mais garde pvp ; schéma 2 rend la charge telle quelle.
func TestPvpDansLesDeuxSchemas(t *testing.T) {
	f := decodeA(t, baseTalents(tl.M{"Player-4619-00000001": fichePv(taNominal, pvNominal)}))
	payload := f.Fiches[0].Payload
	v2, err := FicheSchema(payload, SchemaEnvoi)
	if err != nil || string(v2) != string(payload) {
		t.Fatalf("schéma 2 : %s %v", v2, err)
	}
	v1, err := FicheSchema(payload, SchemaEnvoiRepli)
	if err != nil || string(v1) != chargeRC1[:len(chargeRC1)-1]+`,"pvp":`+pvpNominal+"}" {
		t.Fatalf("schéma 1 : %s %v", v1, err)
	}
	// Fiche sans talents : schéma 1 identique au schéma 2.
	g := decodeA(t, base(tl.M{"Player-4619-00000001": fichePv(nil, pvNominal)}))
	if v1, err := FicheSchema(g.Fiches[0].Payload, SchemaEnvoiRepli); err != nil || string(v1) != string(g.Fiches[0].Payload) {
		t.Fatalf("schéma 1 sans talents : %s %v", v1, err)
	}
}

// Bornes : rang 0 et 14, victoires 0 et 2^31-1, date juste dans les 5 min.
func TestPvpBornes(t *testing.T) {
	limite := maintenant.Add(5 * time.Minute).Unix()
	cas := map[string]string{
		"1790506800:0:0":              `{"read_at":"2026-09-27T11:00:00Z","rank":0,"honorable_kills":0}`,
		"1790506800:14:2147483647":    `{"read_at":"2026-09-27T11:00:00Z","rank":14,"honorable_kills":2147483647}`,
		"1790506800:03:0001002":       pvpNominal,
		fmt.Sprintf("%d:1:5", limite): `{"read_at":"2026-10-06T12:05:00Z","rank":1,"honorable_kills":5}`,
		"1:2:3":                       `{"read_at":"1970-01-01T00:00:01Z","rank":2,"honorable_kills":3}`,
	}
	for pv, want := range cas {
		f := decodeA(t, base(tl.M{"Player-4619-00000001": fichePv(nil, pv)}))
		var c map[string]json.RawMessage
		if err := json.Unmarshal(f.Fiches[0].Payload, &c); err != nil {
			t.Fatal(err)
		}
		if string(c["pvp"]) != want || f.AvecPvp != 1 {
			t.Fatalf("%q : %s", pv, c["pvp"])
		}
	}
}

// Un pv illisible ou hors bornes est ignoré et compté ; la fiche part sans lui,
// avec exactement la charge de la 0.9.0-rc.1.
func TestPvpIllisiblesEtHorsBornes(t *testing.T) {
	futur := maintenant.Add(5*time.Minute + time.Second).Unix()
	illisibles := []any{
		"", "1790506800:3", "1790506800:3:1002:7", "1790506800|3|1002", "1790506800:-1:5",
		"1790506800:3:-5", "1790506800:3:1e3", "1790506800:3:1.5", " 1790506800:3:1002",
		"1790506800:3:1002\n", "a:b:c", "1790506800:100:5", "1790506800:3:12345678901",
		"1234567890123:3:5", "1790506800::5", ":3:5", 1790506800, true, tl.M{"t": 1},
	}
	horsBornes := []string{
		"0:3:5", "000:3:5", fmt.Sprintf("%d:3:5", futur), "999999999999:3:5",
		"1790506800:15:5", "1790506800:99:5", "1790506800:3:2147483648", "1790506800:3:9999999999",
	}
	persos := tl.M{}
	n := 0
	ajoute := func(pv any) {
		n++
		persos[fmt.Sprintf("Player-4619-%08X", n)] = fichePv(nil, pv)
	}
	for _, pv := range illisibles {
		ajoute(pv)
	}
	for _, pv := range horsBornes {
		ajoute(pv)
	}
	ajoute(pvNominal)
	f := decodeA(t, base(persos))
	if len(f.Fiches) != n || f.PvpIllisibles != len(illisibles) || f.PvpHorsBornes != len(horsBornes) || f.AvecPvp != 1 {
		t.Fatalf("fiches %d/%d, illisibles %d/%d, hors bornes %d/%d, avec pvp %d", len(f.Fiches), n,
			f.PvpIllisibles, len(illisibles), f.PvpHorsBornes, len(horsBornes), f.AvecPvp)
	}
	for _, fi := range f.Fiches[:n-1] {
		if fi.Pvp != nil || strings.Contains(string(fi.Payload), `"pvp"`) || fi.Illisibles != 0 {
			t.Fatalf("%s : %s", fi.ID, fi.Payload)
		}
		// Même charge que la 0.9.0-rc.1, au GUID près.
		if want := strings.ReplaceAll(chargeRC1, "Player-4619-00000001", fi.GUID); string(fi.Payload) != want {
			t.Fatalf("%s : %s", fi.ID, fi.Payload)
		}
	}
	want := fmt.Sprintf("rang JcJ : 1 fiches avec rang, %d illisibles et %d hors bornes ignorés", len(illisibles), len(horsBornes))
	vu := false
	for _, l := range f.Infos {
		vu = vu || l == want
	}
	if !vu {
		t.Fatalf("infos : %v", f.Infos)
	}
}

// Fraîcheur : la date du pv compte ; un rang lu plus tard fait repartir la fiche.
func TestPvpFraicheur(t *testing.T) {
	sans := decodeA(t, base(tl.M{"Player-4619-00000001": fichePv(nil, nil)}))
	if sans.Fiches[0].Fraicheur() != 1790505847 {
		t.Fatalf("fraîcheur sans pv %d", sans.Fiches[0].Fraicheur())
	}
	avec := decodeA(t, base(tl.M{"Player-4619-00000001": fichePv(nil, pvNominal)}))
	if avec.Fiches[0].Fraicheur() != 1790506800 {
		t.Fatalf("fraîcheur avec pv %d", avec.Fiches[0].Fraicheur())
	}
	// Un pv plus ancien que les relevés ne change pas la fraîcheur.
	ancien := decodeA(t, base(tl.M{"Player-4619-00000001": fichePv(nil, "1790000000:3:1002")}))
	if ancien.Fiches[0].Fraicheur() != 1790505847 {
		t.Fatalf("fraîcheur pv ancien %d", ancien.Fiches[0].Fraicheur())
	}
	// Un rang plus récent : fraîcheur et empreinte changent.
	neuf := decodeA(t, base(tl.M{"Player-4619-00000001": fichePv(nil, "1790508000:4:1010")}))
	if neuf.Fiches[0].Fraicheur() != 1790508000 || neuf.Fiches[0].Empreinte == avec.Fiches[0].Empreinte {
		t.Fatalf("fraîcheur %d", neuf.Fiches[0].Fraicheur())
	}
	// Talents et pv : la plus récente des deux dates l'emporte.
	tp := decodeA(t, baseTalents(tl.M{"Player-4619-00000001": fichePv(taNominal, "1790508000:4:1010")}))
	if tp.Fiches[0].Fraicheur() != 1790508000 {
		t.Fatalf("fraîcheur talents + pv %d", tp.Fiches[0].Fraicheur())
	}
}
