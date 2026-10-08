# Validation locale de Forever Pulse Companion 0.8.0 — 03/10/2026

Contrat autoritaire : `D:\world of warcraft\Claude outputs\hdv\CONTRAT-HDV-v1.md`, révision 2. Branche `feat/auctions`, issue de `master` au commit `2e642d4` (0.7.4). Un seul agent dans ce dépôt ; aucun addon ni site modifié. L1 du contrat exclut `Auctionator.lua.bak`, malgré la mention des sauvegardes dans le prompt.

| Écrit | Testé localement | Déployé | Vérifié |
|---|---|---|---|
| Lecture sélective Lua et décodeur CBOR indépendant, RFC 8949 | Exemples RFC, quatre séquences L5, versions refusées, données tronquées, limites et encodages refusés | Non | Tests verts ; données réelles en db8 / royaume 2 |
| Jointure hdv v2 et identité complète du marché | Les 23 cas J-a à J-w du contrat, retrait d'une file devenue ambiguë, absence de note | Non | 0 jour réel attribué ; aucune faction devinée |
| File persistante, empreintes, instant de reprise par marché, parties figées | Idempotence, ancien fichier, nouveau candidat pendant un envoi, redémarrage, capacité 6 h, limites 5 000 / 3 000 000 octets, catalogue | Non | Accusé exact obligatoire ; Retry-After persisté et jamais raccourci, même par un essai manuel |
| Route et recul propres aux prix | HTTP locaux : 200 non conforme, 403, 404, 409, 429, 500 ; panne réseau puis reprise ; recensement et statistiques continuent | Non | Serveurs HTTP fabriqués uniquement ; aucun POST réel |
| Fenêtre et état de ligne de commande | FR/EN, géométrie de la fenêtre, notification après accusé ; erreur de format conservée entre comptes | Non | Compteurs de lignes et de jours, motifs, dernière erreur et dernier accusé |
| Exécutable distinct et ressources 0.8.0 | Build Windows ; --dry-run et --once sur les fichiers réels sans réseau | Non installé | Propriétés FileVersion / ProductVersion : 0.8.0 ; exécutable en service conservé |
| Documentation FR/EN | LISEZMOI et README : 0.8.0, données envoyées, données jamais envoyées | Non | Contrat révision 2 et limites de cette vérification explicités |

## Contrôles

Exécutés avec Go **1.24.13** fourni par le propriétaire. `GOCACHE`, `GOTMPDIR`, `TEMP` et `TMP` sont dirigés vers les dossiers ignorés du dépôt pour cette session de commandes : aucune configuration globale modifiée.

- `go test ./...` : **PASS** sur l'arbre final. Windows a occasionnellement bloqué des exécutables de test ; les paquets concernés ont été relancés et la passe globale finale réussit.
- `go vet ./...` : **PASS**.
- `go mod verify` : **all modules verified**.
- `go run ./outils/mkres 0.8.0` : ressources régénérées.
- `go build -trimpath -ldflags "-H windowsgui -s -w" -o ForeverPulseCompanion-0.8.0.exe ./cmd/forever-pulse-companion` : **PASS**.
- `git diff --check` : **PASS**.
- Test de confidentialité : le vrai corps POST du serveur de test fait échouer la suite s'il contient un nom de variable personnelle d'Auctionator ou la sentinelle présente uniquement dans ces variables. `lua.Parse` ne garde que `AUCTIONATOR_PRICE_DATABASE`.

## Essai réel hors réseau

Exécutable candidat, `--dry-run --out <dossier temporaire>`, sur les deux `Auctionator.lua` courants du client bêta. Comptages seulement, aucun nom de compte ou de joueur affiché. Le jeton Windows n'a pas été lu.

| Mesure | Résultat |
|---|---:|
| Fichiers courants lus | 2 |
| Bases / royaumes | 8 / 2 |
| Clés de royaume décodées | 5 |
| Objets, toutes clés | 5 721 |
| Objets, clé PvP | 3 117 |
| Jours sans note | 7 |
| Jours attribués / illisibles | 0 / 0 |
| Lignes envoyables | 0 |
| Requêtes construites / postées | 0 / 0 |
| Code de sortie dry-run / once | 0 / 0 |

Le comptage PvP dépasse l'estimation antérieure d'environ 2 994 objets ; c'est le comptage du fichier réellement lu. L'absence de note `hdv` explique exactement l'absence de ligne envoyable. Aucun fichier réel copié ; les bases de dry-run et les dossiers de sortie sont supprimés après usage.

Exécutable livré : `D:\world of warcraft\wowsync\ForeverPulseCompanion-0.8.0.exe`, **11 912 192 octets**, SHA-256 `8b9977bdcde96008ce21ef850b050b89eca4d5d021f45adfcdd8ef9cfedddb2b`.

Exécutable en service `ForeverPulseCompanion.exe` : SHA-256 `6594610335c22dd34fbae2a31dfc8e7886b82850de392376d57d41e3398de8f5`, identique avant et après les compilations. Il n'a été ni remplacé ni arrêté. Aucune fusion dans `master`, aucune installation, aucune ingestion hébergée ni ouverture de politique de source vérifiée.
