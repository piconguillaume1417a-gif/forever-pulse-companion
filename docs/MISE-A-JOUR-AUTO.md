# Mise à jour automatique du Compagnon (0.10.0, candidate locale)

Branche `feat/auto-update` (worktree `wowsync-autoupdate`), depuis 0.9.0-rc.2.
Décisions du propriétaire (8 octobre 2026) : versions publiées sur un **dépôt
GitHub public**, mise à jour **silencieuse avec notification** (imposée seulement
sous `min_version`).

## Fonctionnement

- 3 minutes après le démarrage puis toutes les 24 h (6 h après un échec), lecture de
  `https://github.com/piconguillaume1417a-gif/forever-pulse-companion/releases/latest/download/latest.json`
  et de `latest.json.sig`. Aucun jeton, aucune donnée de jeu n'accompagne ces requêtes.
- Signature **Ed25519** des octets exacts de `latest.json`, vérifiée avec la clé publique
  intégrée (`internal/update/cle.go`). Le manifeste porte produit, version, URL,
  SHA-256 et taille de l'exécutable. Seuls `github.com`, `objects.githubusercontent.com`
  et `release-assets.githubusercontent.com` en https sont suivis, redirections comprises.
- Seule une version **strictement plus récente** est retenue (`0.9.0-rc.2 < 0.9.0 < 0.10.0-rc.1`).
- L'exécutable vérifié attend dans `%APPDATA%\ForeverPulse\Companion\update\`. Il s'installe
  au prochain démarrage ou par « Redémarrer pour mettre à jour (X) » dans le menu de l'icône.
- Installation : `exe → exe.old`, copie vérifiée `→ exe`, relance `--tray --after-update <pid>`
  (la nouvelle instance attend la sortie de l'ancienne, qui détient le verrou d'instance).
- Essai : `update\essai.json` compte les démarrages. Confirmation après la première passe
  de surveillance (`exe.old` retiré). Au-delà de 3 démarrages sans confirmation : retour à
  `exe.old`, version notée dans `update\refusees.txt`, jamais reproposée.
- `companion.db` n'est jamais copiée ni restaurée. `auto_update = false` dans `config.toml`
  coupe toute vérification. `--update-check` lit et vérifie le manifeste sans télécharger.

## Clé de signature

- **Clé privée** : `%USERPROFILE%\.forever-pulse\companion-update-ed25519.key`
  (créée le 8 octobre 2026 par `outils/publier cle`). Jamais dans un dépôt, un MD ou un chat.
- **Sauvegarde** : une copie dans le **Google Drive du propriétaire**, déposée par le
  propriétaire lui-même (l'agent ne transmet pas de clé secrète). Garder ce fichier privé,
  jamais partagé. Clé publique correspondante : `Uvf0WHYYkaAZIvBoUM5VtWKNq5/RUeGtmKwngcA4fiU=`.
- **Perte** : plus aucune mise à jour automatique possible ; une nouvelle clé exige
  une installation manuelle d'une version qui l'intègre.
- **Fuite** (ou compte Google compromis) : n'importe qui peut signer une version acceptée
  par tous les Compagnons. Retirer aussitôt les releases, publier manuellement une version
  avec une nouvelle clé et prévenir les utilisateurs.

## Publier une version

1. Monter `Version` dans `internal/app/app.go`, puis `go run ./outils/mkres <version>` (ressource de
   version : numéro Windows croissant `update.NumeroWindows`, icône de l'installateur). Commit, tag `v<version>`,
   push sur `main` du dépôt public.
2. La CI (`.github/workflows/build.yml`) construit `ForeverPulseCompanion.exe`, `ForeverPulseCompanion.msi` et
   `ForeverPulseCompanion-Setup.exe`, teste l'installateur sans installer et publie l'artefact.
3. Télécharger l'artefact (`gh run download`) ; reconstruire l'exe en local
   (`go build -trimpath -buildvcs=false -ldflags "-H windowsgui -s -w"`) et vérifier la **même empreinte**.
4. `go run ./outils/publier signer -cle <clé privée> -exe <exe de la CI> -out <dossier> [-min <version>]`.
5. Release GitHub `v<version>` **publiée comme « latest »** (une pré-version est ignorée par
   `/releases/latest`) avec les cinq fichiers : exe, MSI, Setup, `latest.json`, `latest.json.sig`.
   Lien stable de l'installateur : `https://github.com/piconguillaume1417a-gif/forever-pulse-companion/releases/latest/download/ForeverPulseCompanion-Setup.exe`.

## Limites

- Exécutable non signé Authenticode : SmartScreen / Smart App Control peuvent le bloquer.
- Le MSI 0.8.0 continue d'afficher sa version dans « Applications installées ».
- Les Compagnons ≤ 0.9.0-rc.2 n'ont pas ce mécanisme : la première version qui l'intègre
  s'installe à la main.
