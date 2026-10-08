package schema

// Portage des 57 cas de forever-pulse-roadmap/outils/test_validation.py :
// un contrôle qui ne rejette jamais rien ne contrôle rien. Chaque cas part d'un
// fichier sain, y introduit un défaut, et vérifie que le défaut est signalé —
// et que rien n'est signalé sur les cas sains.

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"wowsync/internal/lua"
	tl "wowsync/internal/testlua"
)

const (
	prefixe = "Player-4619-"
	scopeID = "4619-PVP-Alliance"
	baseID  = "11111111-1111-4111-8111-111111111111"
	deltaID = "22222222-2222-4222-8222-222222222222"
)

var lignesTest = []string{
	"00AA0101|Synthchat|Guildesynth|1|1|3|13",
	"00AA0102|Zy|Clansynth|2|1|2|8",
	"00AA0103|=한글 이름|한글|1|1|3|8",
}

func lotComplet() tl.M {
	return tl.M{"batch_id": baseID, "method": "channel_roster", "kind": "full", "chain_index": 0,
		"observed_at": 1789728022, "zone_name": "Dun Morogh", "ui_map_id": 27, "channel_name": "Général - Dun Morogh",
		"trigger": "zone", "zone_count": 3, "read": 3, "observer_faction": "Alliance", "scope_id": scopeID,
		"guid_prefix": prefixe, "fields": ChampsRoster, "classes": tl.L{"WARRIOR", "WARLOCK"}, "races": tl.L{"Gnome"},
		"rows": strings.Join(lignesTest, "\n"), "rows_count": 3}
}

func lotDelta() tl.M {
	return tl.M{"batch_id": deltaID, "method": "channel_roster", "kind": "delta", "base_batch_id": baseID,
		"chain_index": 1, "observed_at": 1789728622, "zone_name": "Dun Morogh", "ui_map_id": 27, "trigger": "refresh",
		"zone_count": 3, "read": 3, "observer_faction": "Alliance", "scope_id": scopeID, "guid_prefix": prefixe,
		"fields": ChampsRoster, "classes": tl.L{"WARRIOR"}, "races": tl.L{"Gnome"},
		"rows": "00FF0001|Arrivant|Nouveau|1|1|2|7\n00AA0101|Synthchat|Guildesynth|1|1|3|14", "rows_count": 2,
		"gone": "00AA0103", "gone_count": 1, "unchanged": 1}
}

func scope(realm int, faction string) tl.M {
	return tl.M{"game_version": "1.60.1", "build": 69913, "interface": 16001, "realm_id": realm,
		"realm_name": "Classic Beta PvP", "ruleset": "PVP", "ruleset_source": "nom du royaume",
		"region": "EU", "faction": faction, "deduit": tl.L{"region", "ruleset"}}
}

func fichierValide() tl.M {
	return tl.M{"schema": 4, "addon": tl.M{"name": "Recensement", "version": "2.2.0"},
		"observer": tl.M{"id": "8d4a1c2e-5b6f-4a7d-9e0f-123456789abc", "locale": "frFR"},
		"scopes":   tl.M{scopeID: scope(4619, "Alliance")},
		"batches":  tl.L{lotComplet(), lotDelta()}}
}

func lots(d tl.M) tl.L       { return d["batches"].(tl.L) }
func lot(d tl.M, i int) tl.M { return lots(d)[i].(tl.M) }

func lotCanal(sur tl.M) tl.M {
	l := tl.M{"batch_id": "44444444-4444-4444-8444-444444444444", "method": "channel_roster", "kind": "full",
		"chain_index": 0, "observed_at": 1789729800, "source_scope": "realm_channel", "channel_name": "world",
		"channel_category": "CHANNEL_CATEGORY_CUSTOM", "channel_opt_in": true, "observer_zone": "Dun Morogh",
		"trigger": "refresh", "read": 3, "observer_faction": "Alliance", "scope_id": scopeID, "guid_prefix": prefixe,
		"fields": ChampsRoster, "classes": tl.L{"WARRIOR", "WARLOCK"}, "races": tl.L{"Gnome"},
		"rows": strings.Join(lignesTest, "\n"), "rows_count": 3}
	for k, v := range sur {
		l[k] = v
	}
	return l
}

func lotMonde(sur tl.M) tl.M {
	l := lotCanal(tl.M{"source_scope": "world_channel", "channel_name": "Trade - City",
		"channel_category": "CHANNEL_CATEGORY_WORLD", "channel_opt_in": false,
		"batch_id": "55555555-5555-4555-8555-555555555555"})
	for k, v := range sur {
		l[k] = v
	}
	return l
}

func ajoute(d tl.M, l tl.M) { d["batches"] = append(lots(d), l) }

// 3.7.0 : le canal relu dix minutes plus tard, en delta sur le lot de canal complet.
func canalDeltaValide(d tl.M) {
	ajoute(d, lotCanal(nil))
	ajoute(d, lotCanal(tl.M{"batch_id": "66666666-6666-4666-8666-666666666666", "kind": "delta",
		"base_batch_id": "44444444-4444-4444-8444-444444444444", "chain_index": 1, "observed_at": 1789730400,
		"read": 3, "unchanged": 2, "rows": "00FF0002|Nouveau|Venu|1|1|2|9", "rows_count": 1,
		"gone": "00AA0103", "gone_count": 1}))
}

// Schéma 5 (3.7.0) : dictionnaires partagés, en-têtes par défaut, /who codé, champs
// redondants absents. Le delta de la fixture a son propre dictionnaire {WARRIOR} : mêmes
// indices ici.
func schema5(d tl.M) {
	d["schema"] = 5
	d["dicts"] = tl.M{"classes": tl.L{"WARRIOR", "WARLOCK"}, "races": tl.L{"Gnome"},
		"class_loc": tl.L{"Guerrier"}, "race_loc": tl.L{"Gnome"}, "guilds": tl.L{"Les Trois Fromages"},
		"zones": tl.L{"Dun Morogh", "Kharanos"}}
	for _, v := range lots(d) {
		l := v.(tl.M)
		for _, k := range []string{"fields", "classes", "races", "guid_prefix", "rows_count", "gone_count", "observer_faction"} {
			delete(l, k)
		}
		if l["kind"] == "full" {
			delete(l, "kind")
		}
	}
	ajoute(d, tl.M{"batch_id": "77777777-7777-4777-8777-777777777777", "scope_id": scopeID,
		"method": "who_manual", "observed_at": 1789729900, "trigger": "who", "fields": ChampsWhoCodes,
		"rows": "Guildesynth|1|1|13|1|2\nSolo|1|1|9||1", "read": 2, "queries_sent": 1, "truncated": false})
}

// Schéma 6 (3.7.1) : table des noms partagée (dicts.names : éléments d'un ou plusieurs noms
// séparés par « \n »), colonnes surname#, name#.
func schema6(d tl.M) {
	schema5(d)
	d["schema"] = 6
	var noms []string
	idx := func(v string) string {
		if v == "" {
			return ""
		}
		for i, n := range noms {
			if n == v {
				return strconv.Itoa(i + 1)
			}
		}
		noms = append(noms, v)
		return strconv.Itoa(len(noms))
	}
	for _, v := range lots(d) {
		l := v.(tl.M)
		rows, _ := l["rows"].(string)
		var sortie []string
		switch {
		case l["method"] == "channel_roster":
			for _, ligne := range strings.Split(rows, "\n") {
				p := strings.Split(ligne, "|")
				p[1], p[2] = idx(p[1]), idx(p[2])
				sortie = append(sortie, strings.Join(p, "|"))
			}
		case l["fields"] == ChampsWhoCodes:
			for _, ligne := range strings.Split(rows, "\n") {
				p := strings.Split(ligne, "|")
				p[0] = idx(p[0])
				sortie = append(sortie, strings.Join(p, "|"))
			}
			l["fields"] = "name#|class_loc#|race_loc#|level|guild#|zone#"
		default:
			continue
		}
		l["rows"] = strings.Join(sortie, "\n")
	}
	el := tl.L{strings.Join(noms[:3], "\n")}
	for _, n := range noms[3:] {
		el = append(el, n)
	}
	d["dicts"].(tl.M)["names"] = el
}

type cas struct {
	nom     string
	transfo func(tl.M)
	attendu string // "" : aucune erreur
}

var tousLesCas = []cas{
	{"fichier sain (complet + delta)", nil, ""},
	{"lot de canal personnalisé valide", func(d tl.M) { ajoute(d, lotCanal(nil)) }, ""},
	{"lot de canal avec zone_count", func(d tl.M) { ajoute(d, lotCanal(tl.M{"zone_count": 252})) }, "ne peut pas porter zone_count"},
	{"lot de canal avec zone_name", func(d tl.M) { ajoute(d, lotCanal(tl.M{"zone_name": "Dun Morogh"})) }, "ne peut pas porter zone_name"},
	{"lot de canal sans channel_opt_in", func(d tl.M) { l := lotCanal(nil); delete(l, "channel_opt_in"); ajoute(d, l) }, "channel_opt_in"},
	{"lot de canal sans channel_name", func(d tl.M) { l := lotCanal(nil); delete(l, "channel_name"); ajoute(d, l) }, "sans channel_name"},
	// 3.7.0 : un delta de canal est permis, mais un delta sans `unchanged` reste une erreur.
	{"lot de canal en delta sans unchanged", func(d tl.M) {
		ajoute(d, lotCanal(tl.M{"kind": "delta", "base_batch_id": baseID, "chain_index": 1}))
	}, "sans unchanged"},
	{"lot de canal en delta valide (3.7.0)", canalDeltaValide, ""},
	{"source_scope inconnu", func(d tl.M) { ajoute(d, lotCanal(tl.M{"source_scope": "royaume_entier"})) }, "source_scope inconnu"},
	{"canal rejoint d'office valide", func(d tl.M) { ajoute(d, lotMonde(nil)) }, ""},
	{"canal rejoint d'office avec opt_in = true", func(d tl.M) { ajoute(d, lotMonde(tl.M{"channel_opt_in": true})) }, "channel_opt_in = false"},
	{"canal rejoint d'office sans opt_in", func(d tl.M) { l := lotMonde(nil); delete(l, "channel_opt_in"); ajoute(d, l) }, "channel_opt_in = false"},
	{"canal rejoint d'office avec zone_count", func(d tl.M) { ajoute(d, lotMonde(tl.M{"zone_count": 2455})) }, "ne peut pas porter zone_count"},
	{"canal rejoint d'office de catégorie perso", func(d tl.M) {
		ajoute(d, lotMonde(tl.M{"channel_category": "CHANNEL_CATEGORY_CUSTOM"}))
	}, "CHANNEL_CATEGORY_CUSTOM"},
	{"canal rejoint d'office en delta sans unchanged", func(d tl.M) {
		ajoute(d, lotMonde(tl.M{"kind": "delta", "base_batch_id": baseID, "chain_index": 1}))
	}, "sans unchanged"},
	{"schéma 5 : lots compacts valides", schema5, ""},
	{"schéma 5 : delta de canal compact", func(d tl.M) { canalDeltaValide(d); schema5(d) }, ""},
	{"schéma 5 : indice /who hors dictionnaire", func(d tl.M) {
		schema5(d)
		l := lots(d)
		l[len(l)-1].(tl.M)["rows"] = "Guildesynth|1|1|13|9|2"
	}, "hors dictionnaire"},
	// Sans guid_prefix, le préfixe vient du realm_id du périmètre : un realm_id absent rend
	// le lot indécodable, jamais attribué à un autre royaume.
	{"schéma 5 : préfixe sans realm_id du périmètre", func(d tl.M) {
		schema5(d)
		delete(d["scopes"].(tl.M)[scopeID].(tl.M), "realm_id")
	}, "guid_prefix mal formé"},
	{"schéma 6 : table des noms partagée valide", schema6, ""},
	{"schéma 6 : delta de canal avec noms en indices", func(d tl.M) { canalDeltaValide(d); schema6(d) }, ""},
	{"schéma 6 : indice de nom hors table", func(d tl.M) {
		schema6(d)
		l := lot(d, 0)
		lignes := strings.Split(l["rows"].(string), "\n")
		p := strings.Split(lignes[0], "|")
		p[2] = "999"
		lignes[0] = strings.Join(p, "|")
		l["rows"] = strings.Join(lignes, "\n")
	}, "hors dictionnaire"},
	{"champ en trop dans une ligne", func(d tl.M) { lot(d, 0)["rows"] = lot(d, 0)["rows"].(string) + "|surplus" }, "champs au lieu de 7"},
	{"séparateur non échappé dans un nom", func(d tl.M) {
		lot(d, 0)["rows"] = "00AA0101|Synth|chat|Guildesynth|1|1|3|13\n" + strings.Join(lignesTest[1:], "\n")
	}, "champs au lieu de 7"},
	{"rows_count ≠ lignes", func(d tl.M) { lot(d, 0)["rows_count"] = 2 }, "rows_count"},
	{"read ≠ lignes d'un lot complet", func(d tl.M) { lot(d, 0)["read"] = 2; lot(d, 0)["zone_count"] = 2 }, "lignes pour read"},
	{"read ≠ zone_count sans partial", func(d tl.M) { lot(d, 0)["zone_count"] = 5 }, "sans partial"},
	{"lot partiel correctement déclaré", func(d tl.M) { lot(d, 0)["zone_count"] = 5; lot(d, 0)["partial"] = true }, ""},
	{"index de classe hors dictionnaire", func(d tl.M) {
		lot(d, 0)["rows"] = "00AA0101|Synthchat|Guildesynth|9|1|3|13\n" + strings.Join(lignesTest[1:], "\n")
	}, "hors dictionnaire"},
	{"niveau non entier", func(d tl.M) {
		lot(d, 0)["rows"] = "00AA0101|Synthchat|Guildesynth|1|1|3|treize\n" + strings.Join(lignesTest[1:], "\n")
	}, "niveau non entier"},
	{"niveau absent (toléré)", func(d tl.M) {
		lot(d, 0)["rows"] = "00AA0101|Synthchat|Guildesynth|1|1|3|\n" + strings.Join(lignesTest[1:], "\n")
	}, ""},
	{"guid_prefix mal formé", func(d tl.M) { lot(d, 0)["guid_prefix"] = "4619" }, "guid_prefix mal formé"},
	{"delta sans sa base", func(d tl.M) { d["batches"] = tl.L{lot(d, 1)} }, "base"},
	{"chain_index qui ne suit pas", func(d tl.M) { lot(d, 1)["chain_index"] = 4 }, "chain_index"},
	{"delta sans unchanged", func(d tl.M) { delete(lot(d, 1), "unchanged") }, "sans unchanged"},
	{"unchanged incohérent", func(d tl.M) { lot(d, 1)["unchanged"] = 0 }, "≠ read"},
	{"identifiant à la fois parti et émis", func(d tl.M) { lot(d, 1)["gone"] = "00AA0101" }, "partis et émis"},
	{"gone_count faux", func(d tl.M) { lot(d, 1)["gone_count"] = 7 }, "gone_count"},
	{"effectif reconstitué ≠ read", func(d tl.M) {
		lot(d, 1)["read"] = 9
		lot(d, 1)["zone_count"] = 9
		lot(d, 1)["unchanged"] = 7
	}, "reconstitué"},
	{"occurred_at interdit", func(d tl.M) { lot(d, 0)["occurred_at"] = 1789728022 }, "occurred_at"},
	{"batch_id en double", func(d tl.M) { lot(d, 1)["batch_id"] = baseID }, "en double"},
	{"observer.id non UUID", func(d tl.M) { d["observer"].(tl.M)["id"] = "observateur-1" }, "UUID v4"},
	{"schéma inconnu", func(d tl.M) { d["schema"] = 2 }, "attendu 4, 5 ou 6"},
	{"ui_map_id absent non déclaré", func(d tl.M) { delete(lot(d, 0), "ui_map_id") }, "ui_map_id_absent"},
	{"ui_map_id absent mais déclaré", func(d tl.M) { delete(lot(d, 0), "ui_map_id"); lot(d, 0)["ui_map_id_absent"] = true }, ""},
	{"lot /who avec un zone_count", func(d tl.M) {
		ajoute(d, tl.M{"batch_id": "33333333-3333-4333-8333-333333333333", "method": "who_manual", "kind": "full",
			"observed_at": 1789729000, "fields": ChampsWho, "rows": "Guildesynth|Guerrier|Gnome|13||Dun Morogh",
			"rows_count": 1, "read": 1, "zone_count": 1})
	}, "zone_count"},
	{"scope_id inconnu", func(d tl.M) { lot(d, 0)["scope_id"] = "9999-NORMAL-Horde" }, "absent de la table scopes"},
	{"table scopes absente", func(d tl.M) { delete(d, "scopes") }, "aucun périmètre"},
	{"mode de jeu inconnu (toléré)", func(d tl.M) { delete(d["scopes"].(tl.M)[scopeID].(tl.M), "ruleset") }, ""},
	{"deux périmètres dans un fichier", func(d tl.M) {
		autre := "5001-NORMAL-Horde"
		sc := scope(5001, "Horde")
		sc["realm_name"], sc["ruleset"] = "Normal", "NORMAL"
		d["scopes"].(tl.M)[autre] = sc
		l := lotComplet()
		l["batch_id"], l["scope_id"], l["guid_prefix"] = "44444444-4444-4444-8444-444444444444", autre, "Player-5001-"
		l["observed_at"], l["zone_name"] = 1789729500, "Durotar"
		ajoute(d, l)
	}, ""},
	{"realm ID différent du périmètre (toléré)", func(d tl.M) { lot(d, 0)["guid_prefix"] = "Player-5001-" }, ""},
	{"identifiant en double non déclaré", func(d tl.M) {
		b := lot(d, 0)
		b["rows"] = b["rows"].(string) + "\n" + lignesTest[0]
		b["rows_count"], b["read"], b["zone_count"] = 4, 4, 4
	}, "identifiants en double"},
	{"doublon déclaré mais ligne émise deux fois", func(d tl.M) {
		b := lot(d, 0)
		b["rows"] = b["rows"].(string) + "\n" + lignesTest[0]
		b["rows_count"], b["read"], b["zone_count"], b["duplicates"] = 4, 4, 4, 1
	}, "identifiants en double"},
	{"duplicates déclaré sans écart avec read", func(d tl.M) { lot(d, 0)["duplicates"] = 1 }, "lignes pour read"},
	{"duplicates négatif", func(d tl.M) { lot(d, 0)["duplicates"] = -1 }, "duplicates"},
	{"doublon correctement déclaré", func(d tl.M) {
		b := lot(d, 0)
		b["read"], b["zone_count"], b["duplicates"] = 4, 4, 1
	}, ""},
	{"doublon déclaré dans un delta", func(d tl.M) {
		b := lot(d, 1)
		b["read"], b["zone_count"], b["duplicates"] = 4, 4, 1
	}, ""},
}

func analyse(t *testing.T, d tl.M) *Fichier {
	t.Helper()
	v, err := lua.Parse(tl.Fichier(d), map[string]bool{"ForeverPulseCensusDB": true})
	if err != nil {
		t.Fatalf("fixture illisible : %v", err)
	}
	return Analyse(lua.AsTable(v["ForeverPulseCensusDB"]), time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
}

func erreurs(f *Fichier) []string {
	out := append([]string(nil), f.Fatales...)
	for _, l := range f.Lots {
		out = append(out, l.Erreurs...)
	}
	return out
}

func TestValidation57Cas(t *testing.T) {
	if len(tousLesCas) != 57 {
		t.Fatalf("%d cas, 57 attendus", len(tousLesCas))
	}
	for _, c := range tousLesCas {
		t.Run(c.nom, func(t *testing.T) {
			d := fichierValide()
			if c.transfo != nil {
				c.transfo(d)
			}
			errs := erreurs(analyse(t, d))
			if c.attendu == "" {
				if len(errs) > 0 {
					t.Fatalf("aucune erreur attendue : %v", errs)
				}
				return
			}
			for _, e := range errs {
				if strings.Contains(e, c.attendu) {
					return
				}
			}
			t.Fatalf("« %s » attendu parmi %v", c.attendu, errs)
		})
	}
}

// Le delta reconstitué part comme lot complet, avec son propre batch_id.
func TestDeltaReconstitue(t *testing.T) {
	f := analyse(t, fichierValide())
	d := f.Lots[1]
	if !d.Valide() {
		t.Fatal(d.Erreurs)
	}
	p := d.Payload
	if p.Get("kind") != "full" || p.Get("reconstructed_from_delta") != true || p.Get("base_batch_id") != baseID ||
		p.Get("batch_id") != deltaID || p.Get("chain_index") != int64(1) {
		t.Fatalf("en-tête du delta reconstitué : %v", p)
	}
	chars := p.Get("characters").([]any)
	var guids []string
	for _, c := range chars {
		guids = append(guids, c.(Obj).Get("guid").(string))
	}
	// base (3) − gone (00AA0103) + rows (00FF0001 nouveau, 00AA0101 modifié)
	want := "Player-4619-00AA0101 Player-4619-00AA0102 Player-4619-00FF0001"
	if strings.Join(guids, " ") != want {
		t.Fatalf("effectif reconstitué : %v", guids)
	}
	if lv := chars[0].(Obj).Get("level"); lv != 14 {
		t.Fatalf("niveau mis à jour par le delta : %v", lv)
	}
	if dn := f.Lots[0].Payload.Get("characters").([]any)[2].(Obj).Get("display_name"); dn != "한글 이름" {
		t.Fatalf("nom affiché littéral « = » : %v", dn)
	}
}

// Un lot invalide écarte la chaîne entière qui s'appuie dessus.
func TestChaineEcartee(t *testing.T) {
	d := fichierValide()
	lot(d, 0)["zone_count"] = 5 // base invalide (sans partial)
	f := analyse(t, d)
	if f.Lots[0].Valide() || f.Lots[1].Valide() {
		t.Fatal("la base est invalide : son delta doit être écarté aussi")
	}
}

func TestCanalSansZone(t *testing.T) {
	d := fichierValide()
	ajoute(d, lotMonde(nil))
	f := analyse(t, d)
	p := f.Lots[2].Payload
	for _, k := range []string{"zone_name", "zone_count", "ui_map_id", "partial"} {
		for _, kv := range p {
			if kv.K == k {
				t.Fatalf("un lot de canal ne porte pas %s", k)
			}
		}
	}
	if p.Get("source_scope") != "world_channel" || p.Get("channel_opt_in") != false {
		t.Fatal(p)
	}
}

func TestFuturRefuse(t *testing.T) {
	d := fichierValide()
	lot(d, 0)["observed_at"] = 1790000000 + 86400*30
	f := analyse(t, d)
	if f.Lots[0].Valide() {
		t.Fatal("observed_at dans le futur accepté")
	}
}
