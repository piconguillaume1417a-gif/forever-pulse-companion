# Réouverture de la connexion Companion — 7 novembre 2026

## Préparation de publication 0.10.0-rc.9

Le propriétaire a demandé la publication après vérifications, puis l'installation
sur son PC. Dernière release et branche publique relues avant préparation : rc.8,
`d99dcb2` ; branche main non protégée selon l'API GitHub. Aucun contrôle contourné.

Version applicative et ressource Windows portées à `0.10.0-rc.9`, numéro MSI
`0.10.9`, ProductCode `{4A3246B6-5008-EE2F-DF82-B6DA84C393A8}`.
Guides anglais et français actualisés. Relecture manuelle du diff effectuée.

Sur le candidat rc.9 : `go vet ./...`, `go test ./... -count=1`, compilation
Windows déterministe sans CGO, version CLI et ressource : PASS. Empreinte EXE
`e9e1bea5147b2a254e003d63c0628e6d50863edb0112628d0215245c3444bd2a`.
`Build.ps1` et `Test.ps1` : PASS, contrôles natifs MSI, extraction, intégrité,
mise à niveau, propriété du démarrage et 24 cas de disposition FR/EN.
Aucune transaction d'installation à ce stade. Les tests réels de cycle MSI
restent réservés au runner Windows jetable de la CI existante.

CI distante, publication et installation : à consigner après exécution.

## Résultat local

Candidat `fix/companion-connect-reopen`, basé sur la publication rc.8
`d99dcb2726785d5d7c689834b708d3409a61deac`, dans
`D:/world of warcraft/work/companion-connect-reopen`.
Aucune publication, CI distante, installation ou modification des données du
Companion installé. Le signalement de Captnrex n'a pas été reproduit sur son PC.

## Cause et correction

Au démarrage, `connecter(true)` appelle `Resume` sans ouvrir le navigateur et
garde `connectMu` pendant l'attente (jusqu'à quinze minutes). Les nouveaux clics
sortent immédiatement sur `TryLock`, sans action. Sans redémarrage, la fenêtre
garde également le bouton occupé jusqu'au retour de `connecter(false)`.

Le bouton de fenêtre lance maintenant l'attente en arrière-plan et redevient
disponible. Si une tentative existe, le bouton ou le menu transmet un signal
borné à cette tentative pour rouvrir sa page. L'adresse et le code doivent avoir
passé les validations existantes. Une demande terminée/expirée n'est pas rouverte.
Les clics ne remplacent ni la demande ni son secret, ne créent pas de boucle
concurrente, n'accélèrent pas les requêtes et ne repoussent pas le prochain poll.
Le redémarrage seul reste silencieux côté navigateur.

Les files d'envoi, les réglages et l'autorisation d'envoi existante ne changent
pas. Les deux rejets Census visibles dans la capture sont un sujet distinct.

## Vérifications exécutées

- Régression synthétique avec serveur HTTP local et coffre mémoire : le client
  rc.8, muni uniquement du champ de signal inerte nécessaire à la compilation du
  test, échoue à `TestReopenResumedRequest` (aucune ouverture après le clic).
- Candidat : même test PASS, réouverture avant et après arrêt/reprise, même URL,
  même code, même identifiant et secret persistés ; deux appels start correspondant
  au lancement et à la reprise, aucune requête supplémentaire pour les clics.
- Clic mis en attente avant la réponse start, refus d'adresse non conforme,
  demande expirée et ACK d'une connexion terminée : PASS.
- Clics répétés pendant l'attente sans repousser le poll : PASS.
- `go test ./... -count=1`, `go vet ./...`, `git diff --check` : PASS.
- Build Windows amd64 sans CGO : PASS ; exécution limitée à `--version`.
  Binaire de recette `.go-tmp/ForeverPulseCompanion-connect-reopen-local.exe`,
  SHA-256 `5d8a62836c5709577f30c2bf7ec0912d6b6c4a448f5e0f119264b9c972e40fb3`.
  Il conserve le numéro source rc.8 : ce n'est pas une nouvelle release.
- Go 1.24.13 utilisé depuis
  `%LOCALAPPDATA%/ForeverPulseTools/go1.24.13/go/bin` ; archive officielle vérifiée
  contre l'empreinte du catalogue Go. L'ancienne chaîne temporaire était incomplète.

Limites : pas de clic natif sur une fenêtre candidate ni de validation de compte
hébergée. L'ouverture navigateur est capturée par les tests, pas exécutée sur le
compte du propriétaire. Pas de recette MSI, de signature ou de publication pour
ce candidat. Une livraison devra attribuer une nouvelle version, qualifier le
paquet et le parcours réel, puis publier et installer selon le flux existant.

## Contournement pour la rc.8 installée

Dans le menu de l'icône près de l'horloge : **Annuler la connexion / Cancel
connection**, attendre le résultat, puis **Connecter à Forever Pulse / Connect
to Forever Pulse**. Cette action annule la demande temporaire, sans effacer la
file ni l'autorisation d'envoi existante. Elle nécessite une réponse du serveur.
Une demande temporaire expire aussi après quinze minutes. Ne pas utiliser
**Effacer les données** pour résoudre ce blocage.
