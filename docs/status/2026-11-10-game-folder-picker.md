# Choix du dossier du jeu — 0.10.0-rc.11 (10 novembre 2026)

## Changement

- Carte « Dossier du jeu » dans la fenêtre : chemin surveillé, état « addon trouvé »,
  « addon jamais lancé » ou « jeu introuvable », bouton « Changer… » (en couleur si
  le jeu est introuvable). Entrée « Choisir le dossier du jeu… » dans le menu de l'icône.
- Sélecteur de dossiers de l'Explorateur (IFileOpenDialog, `internal/winui/gamedir_windows.go`).
  Choisir `World of Warcraft`, un client (`_forever_`, `_retail_`…) ou un sous-dossier
  (`WTF`…) donne la racine (`watch.Racine`). Confirmation si aucun ForeverPulse.lua.
- Le choix est écrit dans `wow_dir` et appliqué sans redémarrer : le moteur relit le
  dossier à chaque passage.
- Au démarrage, un `wow_dir` sans ForeverPulse.lua est remplacé par le premier dossier
  qui en contient un (ou, à défaut, un client de jeu) parmi les chemins Battle.net du
  registre (`Blizzard Entertainment\World of Warcraft\*\InstallPath`) puis les
  emplacements usuels de chaque lecteur.

## Indépendance

Construit sur rc.10 (`57dda4d`). N'inclut pas la branche Census des guildes
(forever-pulse#672), qui portait provisoirement le numéro rc.11 et devra passer à rc.12.
Aucune dépendance au site.

## Vérifications locales

- Go 1.24.13 : `go vet ./...` et `go test ./... -count=1` verts (tests de racine,
  détection, mise en page de la carte en FR/EN, thèmes clair et sombre).
- Sur le PC du propriétaire : registre lu, détection correcte ; sélecteur réel validé
  (`_forever_\WTF` → racine `World of Warcraft`) et annulé ; build de révision installé,
  carte affichée « addon found », bouton « Change… » ouvre le sélecteur sur le dossier configuré.
