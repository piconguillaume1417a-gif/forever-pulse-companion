# Validation locale — talents des joueurs croisés (06/10/2026)

Contrat : `D:\world of warcraft\Claude outputs\specialisations-20261006\CONTRAT.md`
v1, §2.2-2.3 (format addon), §3 (compagnon), §4.2 (bornes du site). Branche
`feat/talent-specs`, issue de `feat/browser-connect` `1053b7e` (0.9.0-rc.1).
`app.Version` inchangée (`0.9.0-rc.1`). Aucun addon ni site modifié, aucun
exécutable construit ni installé, aucun envoi réel : fixtures construites en code
(`testlua`) et faux site HTTP local seulement.

| Écrit | Testé localement | Déployé |
|---|---|---|
| Lecture de `ta` (regex du contrat, 200 paires, `@entrée`), bornes §4.2, illisible / hors bornes comptés, fiche gardée | `internal/stats/talents_test.go` | Non |
| Lecture de `arbres` (`v = 1`, ≤ 64 arbres, 1 à 500 nœuds, ≤ 8 entrées, nom ≤ 200, `hg`), arbre hors bornes écarté et compté | idem | Non |
| `talents` en dernière clé de la fiche ; fiche sans `ta` : charge et empreinte de la 0.9.0-rc.1 (relevées sur `1053b7e`) | idem, `TestTalentsEmpreinte` | Non |
| `meta` : `file.talents_version`, `talent_trees` ; forme schéma 1 identique octet pour octet à la 0.9.0-rc.1 | `TestCorpsSchema`, `TestStatsBodyMetaAnterieure` | Non |
| Transport `X-Schema: 2`, repli 409 `supported: [1]`, mode persisté 6 h, retour au 2 et renvoi unique | `internal/app/talents_test.go` (faux site de `stats_test.go` étendu) | Non |
| Colonne additive `stats_queue.sent_schema`, ancienne base acceptée | `internal/store/stats_test.go` | Non |

## Contrôles

Go **1.24.13** (`dossier local de Go 1.24.13`).

- Avant changement (`1053b7e`) : `go vet ./...` **PASS** ; `go test ./...` **PASS**.
- Après : `go vet ./...` **PASS** ; `go test ./... -count=1` **PASS** (15 paquets) ;
  `git diff --check` **PASS**.
- Contre-épreuve : la remise en file désactivée, `TestTalentsRepliSchema1PuisRetourSchema2`
  échoue (`schémas au retour : [2 1 1 1 2]`) ; rétablie, il passe.
- Windows a une fois bloqué un exécutable de test (« stratégie de contrôle
  d'application ») ; la commande relancée a réussi.

## Scénarios couverts

- `ta` nominal, `@entrée`, paires vides, niveau vide, sources `n/t/f/m`, paires
  dans le désordre (triées) ; illisibles (9 formes) et hors bornes (13 formes :
  source `g`/vide/`s`, niveau 0/101, arbre 0, date 0 ou > 5 min dans le futur,
  `hg` 201, points 0, nœud en double ou 0, 201 paires).
- `talents_v` absent ou inconnu : ni `ta` ni `arbres` lus, empreintes inchangées.
- Arbres : nominal avec nom contenant « | », entrées `e:s` et `e:` (null), nœud
  hors grille x = 102800 gardé (branche 3, `off_grid`) ; version inconnue, sans
  version/date, classe, langue, branche 4, x hors bornes, rangs 0, ligne courte,
  nœud en double, nom > 200, 9 entrées, aucun nœud, `hg` illisible, 501 nœuds
  (500 acceptés), 65 arbres.
- Corps schéma 2 exact : clés `schema, companion, file, contexts, catalog,
  talent_trees, characters`, `file.talents_version = 1`, `talent_trees` attendu,
  `talents` reçus par le site ; idempotence ; nouveau `ta` → seule cette fiche repart.
- Site schéma 1 seul : `2` puis `1`, lot accepté sans talents, mode relu après
  redémarrage, schéma 1 encore à 5 h même si le site connaît le 2 ; après 6 h,
  schéma 2 accepté et la fiche à talents envoyée en schéma 1 repart une fois
  (empreinte inchangée), plus jamais ensuite (envoi auto, redémarrage, manuel).
- 409 `supported: [3]` : blocage `schema` comme avant, aucun repli.
- Fichier 4.1.0 : schéma 2 avec `talents_version: null`, `talent_trees: []`, fiches
  identiques ; en repli, corps identique à la 0.9.0-rc.1.

## Correctifs de la revue adversariale (contrat §7.2)

- **I2** : une ligne d'arbre illisible, hors bornes ou en double n'écarte plus
  l'arbre : le nœud est écarté et compté (`NoeudsIgnores`), l'arbre et ses
  autres nœuds partent. Un nœud écrit deux fois est ambigu : toutes ses lignes
  sont écartées. Un identifiant `hg` illisible est écarté seul. Classe, build ou
  langue hors bornes deviennent `null`. L'arbre n'est écarté que sans version
  connue, sans date d'export, au-delà de 500 lignes ou sans aucun nœud valable.
- **I4** : bornes alignées sur le site : nœud 1..2 147 483 647 (catalogue,
  `off_grid`, points), x/y ±10 000 000, `max_ranks` 1..99, `spell_id` et sort
  d'entrée 0..2 147 483 647 ou `null`, `build` 0..2 147 483 647 ou `null`,
  entrée ≥ 1 (`@0` dans `ta` : hors bornes ; `e:` : sort `null`). Défense
  d'unicité des `tree_id` (clé texte en double écartée, comptée).
- Tests : `TestArbresNoeudEcarteArbreGarde` (20 cas de nœud, bornes atteintes,
  champs facultatifs à `null`), `TestArbresEcartes` (6 cas d'arbre, 500 nœuds,
  `tree_id` en double, 65 arbres), `@0` ajouté aux relevés hors bornes.

## Limites

Pas de fichier réel de l'addon 4.2.0 (non disponible) ni de site schéma 2 réel :
la recette avec l'addon et le site reste à faire. Les arbres partent avec chaque
requête de statistiques (plafond local 1 Mo de catalogue).

## Bout en bout SYNTHÉTIQUE avec l'addon et le site (06/10/2026)

`internal/app/talents_site_test.go` (`TestTalentsActualLocalSite`, opt-in : sauté sans
`PULSE_COMPANION_TEST_SITE` et `PULSE_TALENTS_E2E=1`) est lancé par le harnais du site
`scripts/supabase/talent-specs-e2e.mjs` (branche `feat/talent-specs` de
`forever-pulse-talents`) : vraies routes du site, PostgreSQL 17 éphémère, coffre en mémoire.
Entrée : `testdata/talents-e2e/ForeverPulse-{1,2}.lua`, écrits par le **vrai code de l'addon
4.2.0** (`.toc` complet) dans un client simulé (`addon-4.2.0/outils/fixture_talents_e2e.lua`),
époques décalées d'un même écart au lancement ; `attendu.json` = relevés et
`ns.Talents.Comptes` de l'addon. Étapes contrôlées par le harnais : repli 409
`supported:[1]` → schéma 1 accepté sans talents ; site au schéma 2, horloge +6 h → session 2
envoyée, puis les 5 fiches à talents envoyées en schéma 1 repartent **une fois** ; renvoi
identique (autre base locale) → doublons ; fichier plus ancien : ignoré par le compagnon qui
connaît le récent, doublons et rien de remplacé côté site. Résultat local : **PASS** (voir
`docs/STATUS.md` du site). Données SYNTHÉTIQUES ; aucun client réel, aucun site hébergé.

Constat (antérieur aux talents, `internal/schema/validate.go:205` et `:244`, refus dans
`app.ProcessFile`) : un fichier sans périmètre ou sans **aucun lot** de recensement est refusé
en entier, statistiques et talents compris. Un joueur qui aurait coupé le roster et le /who ne
transmettrait donc jamais ses statistiques ; la fixture contient pour cela un vrai lot
`channel_roster`.
