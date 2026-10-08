# Companion 0.9.0-rc.1 — association au compte

4 octobre 2026. Branche isolée `feat/browser-connect`, base `d0f83f5`
(0.8.0). Aucun changement dans le checkout installé, ses fichiers, son coffre,
son processus, ses SavedVariables ou son démarrage Windows.

## Parcours livré dans le candidat

Bouton **Connecter à Forever Pulse**, navigateur du système, connexion/inscription
email du site, comparaison d’un code et approbation explicite. La demande dure
15 minutes. Le Companion interroge le serveur entre 5 et 60 secondes, avec reprise
après coupure/redémarrage grâce au secret temporaire dans le coffre Windows.
Il sauvegarde le jeton `fpc_` avant d’accuser réception, puis oublie le secret
temporaire. Il affiche le compte, reprend les envois et détecte le refus/la
révocation au prochain envoi. Annuler une demande est proposé au menu de l’icône.

Le compte du site liste les installations, dernière utilisation et révocation
individuelle. Le logout web conserve les autorisations persistantes. Le jeton
manuel reste un secours avancé. EN par défaut, FR mémorisé. Les textes d’état
« copier un jeton » ont été remplacés par la connexion navigateur.

Le protocole n’introduit aucun secret privilégié dans le binaire. Le site conserve
les empreintes ; aucun jeton dans l’URL de navigateur ou le fichier de configuration.
Voir le contrat site `docs/security/companion-connect.md` dans la branche
`forever-pulse-companion-connect`.

## Isolation et compatibilité

Le contexte par défaut conserve le credential existant `ForeverPulse/Companion`,
avec la migration historique 0.1 inchangée. Un `--data-dir`, `WOWSYNC_DATA_DIR`
explicite ou une autre origine crée une cible de coffre distincte : ces tests
ne peuvent pas retomber sur le jeton installé. Dans ce contexte isolé, la fenêtre
est refusée pour ne pas réveiller la singleton installée ni changer le démarrage ;
les recettes utilisent `--connect` / `--sync` ou le code Go réel avec coffre mémoire.
La commande `--connect` est un outil de recette, pas une étape du parcours normal.

Aucun changement de schéma SQLite : les files census/stats/HDV, leurs ACK exacts,
la déduplication, la lecture seule des Lua, les cinq minutes et réglages personnels
restent en place. Une nouvelle autorisation ne contourne jamais une source fermée.
Une demande annulée, expirée ou inaccessible ne supprime pas l’ancienne autorisation.

## Contrôles réellement exécutés

- `go test ./...` : PASS après adaptation de l’assertion de texte de reconnexion.
- `go vet ./...` : PASS.
- Coffre Windows réel : écriture/lecture de valeurs fictives dans une cible
  unique de test, suppression vérifiée, aucune lecture des cibles installées.
- Go → routes HTTP réelles site → PostgreSQL jetable : PASS association,
  deux ACK census/stats exacts, déduplication, révocation et file conservée.
  Identité synthétique injectée au niveau SQL, coffre mémoire : aucune preuve SMTP.
- Tests du client : écriture avant réseau/ACK, redémarrage, ACK perdu, refus des
  redirections, erreurs HTTP non divulguées, coffre indisponible, expiration/annulation.
  Dernière correction : une expiration pendant une coupure réseau affiche bien
  expiré et efface le secret temporaire ; un arrêt conserve la demande pour reprise.
  Test de non-régression, `go test ./...` et `go vet ./...` repassés après correction.
- Géométrie FR/EN et états connexion ; aperçus synthétiques rendus avec Segoe UI,
  revus visuellement. Ce rendu n’est pas un test interactif de la fenêtre Windows.
- Build Windows amd64, `-trimpath -ldflags "-H windowsgui -s -w"`, CGO désactivé.
  Version applicative 0.9.0-rc.1, ressources FileVersion/ProductVersion 0.9.0.

Exécutable : `Claude outputs/companion-connect-0.9.0-rc.1/ForeverPulseCompanion-0.9.0-rc.1.exe`
à la racine commune du workspace. SHA-256 :

`eb0e70a5c9b12448dc9a763457bb18c09f3b6f5edeed0710fc7bdbd7126707ea`

La recette Auth Supabase + email + navigateur reste bloquée localement par
l’absence de Docker ; le site prévoit sa CI manuelle. Aucune recette hébergée,
installation ou vérification de production n’est déclarée réussie ici.

## Mise à jour après qualification et accord

1. Vérifier la version, le chemin et l’empreinte du programme réellement installé.
   Quitter normalement le Companion ; ne pas lancer le candidat en parallèle.
2. Sauvegarder l’exécutable courant et, application fermée, le dossier de données
   `%APPDATA%\ForeverPulse\Companion` avec configuration, base et éventuels sidecars.
   Garder cette sauvegarde privée ; ne pas exporter le jeton Windows.
3. Remplacer uniquement l’exécutable au chemin prévu. Ne pas désinstaller,
   effacer les données ou changer le dossier par défaut. Garder le coffre existant.
4. Relancer après accord : le programme réutilise les réglages et reprend les
   envois ordinaires. Vérifier identité/état, langue, intervalle et ACK sans
   divulguer de jeton. L’entrée de démarrage suit le chemin choisi comme avant.

Retour arrière : quitter, remettre l’exécutable sauvegardé 0.8.0 et relancer.
Conserver la configuration, la file la plus récente et le coffre. Ne pas restaurer
une vieille base au-dessus de nouveaux ACK. Côté site, désactiver uniquement le
parcours d’association/les comptes et revenir au SHA qualifié ; conserver le
pepper et les jetons existants. Aucune révocation globale ni migration inverse
destructive n’est nécessaire.
