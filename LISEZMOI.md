# Forever Pulse Companion

## Base locale allégée (0.10.0)

Le contenu d'un lot de recensement **envoyé depuis plus de 3 jours** est effacé de `companion.db`, deux minutes
après le démarrage puis une fois par jour, par petites tranches. L'identifiant du lot (un lot déjà envoyé n'est jamais
renvoyé) et ses compteurs (personnages, octets, dates) sont gardés ; les lots en attente ou refusés, les fiches de
statistiques et le cumul des personnages ne sont pas touchés. La première purge compacte la base une fois (elle peut
prendre quelques dizaines de secondes sur une grosse base) ; ensuite seule la place libérée est rendue au disque.

## Connexion par navigateur — candidat 0.9.0-rc.1

Cliquez sur **Connecter à Forever Pulse**, connectez-vous ou créez votre compte
dans le navigateur, comparez le code puis confirmez cette installation. Le
Companion conserve automatiquement son autorisation limitée aux envois dans le
Gestionnaire d’identifiants Windows et reprend les envois. Aucun jeton à copier.
**Gérer les installations** permet de voir la dernière utilisation et d’en
révoquer une seule. Se déconnecter du navigateur ne révoque pas le PC.

Ce candidat est préparé, sans installation ni activation publique. L’ouverture
des comptes et la recette SMTP/site restent à qualifier. Les anciens jetons,
réglages, files, langue et intervalle de surveillance sont conservés. Les sections
datées 0.8.0 ci-dessous décrivent la livraison historique, pas l’état installé
actuel. Voir [validation, mise à jour et retour arrière](docs/COMPANION-CONNECT-0.9.0.md).

## Version 0.9.0-rc.2 (06/10/2026) — connexion par navigateur + talents

La 0.9.0-rc.2 réunit la connexion par navigateur de la 0.9.0-rc.1 et l'envoi des
talents ci-dessous. Le site les accepte depuis la migration 53 (appliquée en PROD le
06/10 à 16:44 UTC) et la PR #119. Installation : l'exécutable remplace
`%LOCALAPPDATA%\Programs\ForeverPulseCompanion\ForeverPulseCompanion.exe` (copie de
l'ancien gardée à côté) ; l'installateur MSI reste celui de la 0.8.0, une version MSI
devant être numérique.

## Spécialisations des joueurs croisés — branche `feat/talent-specs` (06/10/2026)

Avec l'addon **4.2.0**, chaque fiche de statistiques peut porter le dernier relevé
de talents du personnage (`ta`) et le fichier un catalogue des arbres (`arbres`,
`talents_v = 1`). Le compagnon les lit, valide localement les bornes du site et
envoie les statistiques au **schéma HTTP 2** (`X-Schema: 2`, `"schema": 2`) :
schéma 1 + `talent_trees` + `file.talents_version` + `characters[].talents`.
Un relevé illisible ou hors bornes est écarté et compté, la fiche part sans lui ;
une fiche sans talents garde exactement la charge et l'empreinte de la 0.9.0-rc.1.

**Repli :** tant que le site ne connaît que le schéma 1 (409 dont `supported`
contient 1 mais pas 2), le même paquet repart aussitôt en schéma 1, sans talents
ni arbres ; le schéma 2 est retenté au plus tôt 6 h plus tard (mode gardé dans
`companion.db`, il survit à un redémarrage, et affiché dans le détail de l'état).
Quand le schéma 2 est de nouveau accepté, les fiches envoyées en schéma 1 avec
talents repartent une fois. Un 409 qui n'annonce pas le schéma 1 arrête les
statistiques comme avant. Voir [validation des talents](docs/VALIDATION-TALENTS.md).

## Installateur Windows 11 (0.8.0)

Ouvrez **ForeverPulseCompanion-0.8.0-Setup.exe**. L'assistant en français installe pour votre compte Windows 11 **x64**, sans téléchargement, dans `%LOCALAPPDATA%\Programs\ForeverPulseCompanion`. Il ajoute le compagnon et ses guides, un raccourci au menu Démarrer et, au choix, sur le Bureau. Aucune demande de droits administrateur n'est prévue. L'installateur est **non signé** ; une politique Windows peut refuser son exécution.

Avant de cliquer sur **Installer**, choisissez **Quitter** dans le menu de l'icône du compagnon près de l'horloge. L'assistant demande de fermer une instance encore ouverte ; il ne termine aucun processus. Après installation, **Ouvrir** lance la 0.8.0 ; **Fermer** vous laisse la lancer plus tard depuis le menu Démarrer. Le lancement peut reprendre les envois avec votre jeton existant. Le démarrage avec Windows reste réglé dans le compagnon : au premier lancement de la nouvelle copie, celle-ci met à jour l'entrée de démarrage selon votre configuration existante. La création et la vérification du paquet ne remplacent pas l'exécutable 0.7.4 actuellement en service dans le dépôt.

La configuration, la base SQLite avec ses files d'attente, les journaux et le jeton Windows existants sont conservés. L'installateur n'accède pas aux SavedVariables, ne lit pas le jeton et ne lance pas automatiquement le compagnon.

Pour enlever le programme **en conservant vos données**, quittez le compagnon puis utilisez **Paramètres Windows → Applications → Applications installées → Forever Pulse Companion → Désinstaller**. Windows retire uniquement les fichiers et raccourcis installés et l'entrée de démarrage si elle pointe exactement vers cette installation. Le bouton **Désinstaller…** à l'intérieur du compagnon conserve son comportement historique de suppression des données et du jeton : ce n'est pas la désinstallation Windows décrite ici.

La construction et les contrôles du paquet sont documentés dans [outils/installateur/README.md](outils/installateur/README.md). Le fichier `.msi` contient le même programme ; l'assistant `.exe` est le point d'entrée conseillé.

## Version 0.8.0 — prix de l'hôtel des ventes (03/10/2026)

La candidate **ForeverPulseCompanion-0.8.0.exe** lit les données de prix d'Auctionator, selon `CONTRAT-HDV-v1.md`, révision 2 du 03/10. Elle n'est **pas installée** : `ForeverPulseCompanion.exe` reste l'exécutable en service 0.7.4, et `master` reste au commit `2e642d4`. Le site et l'addon ne sont pas modifiés par cette livraison.

`Auctionator.lua` est découvert dans les mêmes dossiers SavedVariables que Forever Pulse. Le sondage utilise le même `poll_seconds` (300 par défaut), la stabilité de 3 secondes et la lecture partagée sans verrou. **Le contrat autoritaire L1 exclut `Auctionator.lua.bak`**, malgré la mention des sauvegardes dans le prompt initial. Seule `AUCTIONATOR_PRICE_DATABASE` est construite en mémoire ; le Lua est analysé comme des données, jamais exécuté. Le décodeur CBOR est écrit à partir de la [RFC 8949](https://www.rfc-editor.org/rfc/rfc8949.html), sans consultation du code d'Auctionator : profondeur 8, 200 000 éléments, chaîne de 8 Mio ; étiquettes, flottants, longueurs indéfinies, doublons de clés et octets restants refusés. Seuls `__dbversion = 8` et royaume `version = 2` sont acceptés.

L'identité du marché vient exclusivement de `ForeverPulseCensusDB.hdv` v2, dans le **même** dossier SavedVariables. La fenêtre prudente couvre la veille, le jour et le lendemain du jour source, via les instants bruts de la note. Sans note, activation trop tardive, identité incomplète, hôtel inconnu, plusieurs marchés ou plusieurs versions d'Auctionator : le jour est écarté et compté. Le jour reste une clé brute, sous `auctionator/<version>/db8/unverified` : il n'est pas présenté comme une date UTC d'observation. Sans `l`, le prix bas reste inconnu ; `m` n'accompagne que les objets présents au plus grand jour de toute la clé de royaume.

La file SQLite utilise `auctions_files`, `auctions_queue`, `auctions_markets` et `auctions_parts`. Chaque ligne possède une empreinte et une dernière version accusée ; les versions anciennes ne remplacent pas les nouvelles, les lignes identiques ne repartent pas, et la disparition seule de `current_price` ne déclenche rien. Les parties d'une mise à jour sont conservées pendant une panne ou un redémarrage. `update_id` est SHA-256 de `file.sha256|market.id`. Une nouvelle note ambiguë ou une lecture tronquée retire les prix non confirmés concernés avant un autre envoi. Le cumul du recensement reste chargé seulement pendant le traitement d'un fichier (comportement 0.7.4).

Un corps porte un seul marché, au plus 5 000 prix et 5 000 objets de catalogue, et au plus 3 000 000 octets décompressés. Seul l'accusé JSON exact (`accepted + duplicates + rejected.length = rows.length`, statut 202 si `accepted > 0`, 200 sinon) confirme les lignes. `invalid_row` / `no_value` restent refusées jusqu'à modification ; `retention_expired` sort de la file ; `capacity_reached` reste en attente au moins 6 heures. Une route absente 404 garde la file. Un refus 403, 409 ou une panne de ce flux ne bloque pas le recensement ni les statistiques. Le catalogue d'objets v1 ne renvoie que les objets nouveaux ou modifiés après un accusé exact.

Chaque marché possède son propre recul. `429/Retry-After` est persisté, arrondi vers le haut, jamais raccourci et **jamais contourné par Envoyer maintenant**. Une nouvelle mise à jour attend au moins 30 minutes ; les parties d'une même mise à jour peuvent se suivre pendant 10 minutes. Le serveur reste seul juge. `market_source_conflict` suspend les essais automatiques ; `unsupported_schema` attend une nouvelle version du compagnon.

La fenêtre affiche les lignes de prix envoyées, en attente et les jours sans faction prouvée ; leur survol détaille les motifs, la dernière erreur et le dernier accusé. `--status` contient les mêmes comptages. `--once Auctionator.lua` et `--dry-run --out <dossier> [fichiers]` comptent les prix sans envoyer ni lire le jeton Windows ; le second utilise une base temporaire supprimée à sa fermeture. Les options doivent précéder les chemins. Les JSON de `--out` sont destinés à un diagnostic local et doivent être supprimés après usage.

Essai réel sans envoi du 03/10 : deux fichiers courants lus, bases version 8, cinq clés de royaume décodées en version 2, **5 721 objets au total, dont 3 117 pour PvP** (l'estimation antérieure était environ 2 994), **7 jours sans note**, 0 jour attribué, 0 ligne envoyable, **0 requête construite et 0 requête postée**, 0 jour illisible. Aucune donnée réelle copiée ou conservée ; dossier de sortie supprimé. Cet essai ne prouve ni l'ingestion HTTP hébergée ni l'ouverture de la source sur le site.

**Version 0.7.4 (03/10/2026), dix fois moins de mémoire au repos :** le cumul des personnages distincts observés (environ 200 000 personnages, jusqu’à 220 Mo en mémoire) n’est plus gardé entre deux `/reload`. Il est relu de `companion.db` le temps d’intégrer un nouveau fichier, puis libéré et rendu à Windows. Les lignes « N personnages distincts observés depuis le … » de la fenêtre et de `--status` sont comptées directement dans la base (mêmes totaux et mêmes dates, vérifiés sur la vraie base). MESURÉ le 03/10 : 358 Mo au repos en 0.7.3, **39 Mo** en 0.7.4 ; pic au démarrage 727 Mo puis 253 Mo ; `--status` 2,9 s puis 1,7 s. Le traitement d’un nouveau fichier demande toujours quelques centaines de Mo pendant quelques secondes, rendus aussitôt.

**Version 0.7.3 (03/10/2026), surveillance toutes les 5 minutes :** à la demande du propriétaire, `poll_seconds` vaut 300 par défaut ; les anciens défauts 600 (0.6.0 à 0.7.1) et 30 (0.7.2) passent à 300 une fois, une autre valeur choisie à la main est gardée. Un relevé part donc au plus 5 minutes et quelques secondes après le `/reload` ou la déconnexion ; **Envoyer maintenant** reste immédiat.

**Version 0.7.2 (03/10/2026), envoi automatique plus rapide :** la surveillance passe de 10 minutes à 30 secondes (`poll_seconds = 30` ; l’ancien défaut 600 est remplacé une fois, une autre valeur choisie à la main est gardée). Un tour de surveillance ne fait que regarder la taille et la date des fichiers, sans en ouvrir aucun : la lecture complète (5 à 11 Mo) n’a lieu qu’après une vraie écriture du jeu. Un relevé part donc environ 35 secondes après le `/reload` ou la déconnexion. `--store-token` lève désormais le blocage « jeton » comme le bouton **Coller le jeton** : l’icône ne reste plus sur « Jeton absent ou refusé » et l’envoi reprend seul. Au démarrage, un fichier réécrit par le jeu pendant la première lecture est relu au tour suivant au lieu d’attendre l’écriture d’après.

**Version 0.7.1 (03/10/2026), envois plus petits :** premier envoi réel vers la production le 03/10 (1 758 lots et 4 415 fiches acceptés, aucun refusé). Les plus gros envois de recensement (≈ 19 000 personnages, 2,7 Mo) ont pris jusqu’à 27 s côté site pour une limite de 30 s ; chaque envoi de lots est désormais limité à 1 Mo (≈ 7 000 personnages). Les statistiques gardent le plafond de 3 Mo (4 s au plus mesurées).

**Version 0.7.0 (02/10/2026), statistiques :** le compagnon envoie désormais aussi les statistiques de personnages relevées par l'addon (`ForeverPulseStatsDB`), vers `<site_url>/api/ingest/stats`, avec le même jeton que le recensement. Chaque fiche (un personnage dans un contexte de jeu) passe par une file persistante dans `companion.db` : elle n'est marquée envoyée que sur l'accusé JSON exact de la route (`accepted + duplicates + rejected` égal au nombre de fiches), elle survit à un redémarrage, et une fiche plus ancienne ou identique à la dernière envoyée n'est pas remise en file. Une fiche refusée seule ne bloque pas les autres ; un refus de la source statistiques (`403`, `409`) ne bloque pas le recensement. Un zéro reste un zéro, une statistique absente n'est jamais envoyée comme zéro, une valeur illisible est écartée et comptée. Les nombres partent en texte, caractère pour caractère. La fenêtre et `--status` indiquent pour chaque flux le fichier suivi, la dernière lecture, le dernier envoi confirmé, l'attente et la dernière erreur.

Le 02/10, la 0.7.0 candidate a décodé hors réseau (`--once`) un vrai fichier de l'addon 3.9.5 (24,6 Mo) : 1 193 lots envoyables, 710 359 lignes de personnages, 4 089 fiches de statistiques envoyables (208 295 valeurs), 7 fiches sans valeur, 0 valeur illisible. `--dry-run` a construit 35 requêtes de recensement et 2 de statistiques, sans rien poster.

**Version 0.6.3 (28/09/2026) :** l'addon 3.7.9 conserve `observer.id` dans son fichier local. Le compagnon place désormais un UUID v4 aléatoire neuf dans le champ HTTP historique `file.observer_session_id` à chaque requête. Le site remplace aussi cet identifiant avant stockage. Le jeton compagnon est l'identifiant durable côté site. Après la fermeture normale du processus 0.6.2, l'exécutable installé a été remplacé par 0.6.3 ; une copie de l'ancien est conservée sous `ForeverPulseCompanion-0.6.2-backup.exe`.

Le 28/09, l'exécutable candidat 0.6.3 distinct a démarré et décodé hors réseau, avec `--once`, les deux gros fichiers SavedVariables de la bêta (schéma 6). L'exécutable installé 0.6.3 a la même empreinte SHA-256 ; son propre `--once` sur le plus gros fichier bêta s'est terminé sans erreur. Ce contrôle local ne prouve pas un envoi HTTP réel.

**Version 0.6.4 (29/09/2026), envoi fiable :** le POST vise toujours `<site_url>/api/ingest/census` ; en production, `site_url = 'https://forever-pulse.com'` et la requête part vers `https://forever-pulse.com/api/ingest/census`. Ne mettez jamais `/api/ingest/census` dans `site_url` : le compagnon l'ajoute. Une adresse avec un chemin, une requête, des identifiants, `http://` hors de la machine locale ou un hôte Supabase est refusée avant toute connexion, et `--dry-run` affiche la destination exacte. Aucune redirection n'est suivie : le corps et le jeton ne partent jamais vers un autre hôte. Un lot n'est marqué « envoyé » que sur l'accusé JSON exact de la route : `202` si au moins un lot est nouveau, `200` si tous sont des doublons, et `accepted + duplicates + rejected` égal au nombre de lots envoyés. Une page de protection Vercel, une page HTML, une redirection ou un simple `200` laissent les lots en attente ; les refus qui bloquent (`400`, `401`, `403`, `409`, `413`) ne sont retenus que sur l'erreur JSON de la route. Le 29/09, la 0.6.4 a remplacé `ForeverPulseCompanion.exe`, compagnon fermé (il n'a pas été lancé) ; une copie de la 0.6.3 est gardée sous `ForeverPulseCompanion-0.6.3-backup.exe`.

`ForeverPulseCompanion.exe` envoie au site Forever Pulse, **https://forever-pulse.com**, les relevés écrits par l'addon **Forever Pulse** dans WoW: Forever. Il a une petite fenêtre et tourne en fond avec une icône près de l'horloge. Il ne touche jamais au jeu.

**Depuis la 0.6.2 (28/09/2026), le site est `https://forever-pulse.com`** (avec un tiret), l'adresse par défaut. Au premier lancement de la 0.6.2, un `config.toml` qui portait l'ancien défaut (`http://localhost:3000`) passe à cette adresse ; l'ancien nom `foreverpulse.com` (sans tiret) est toujours remplacé ; une autre adresse choisie à la main est gardée. Par prudence, un lot n'est marqué « envoyé » que si la réponse vient bien de la route d'envoi du site (accusé JSON avec `accepted`) : une page d'accueil ou un hébergeur de domaine qui répondrait 200 ne fait rien perdre, le lot est réessayé plus tard.

Jusqu'à la version 0.1, il s'appelait `wowsync.exe`. Au premier lancement de la version 0.2, il reprend automatiquement les réglages, la base et le jeton de l'ancienne version. La version 0.1 n'avait pas de case « Lancer au démarrage » : sa valeur `false` n'était qu'un défaut, elle est donc cochée une fois à la reprise, puis votre choix est respecté.

## Installer

1. Copiez `ForeverPulseCompanion.exe` où vous voulez, par exemple dans `Documents\Forever Pulse\`. Il n'y a rien à installer et aucun droit administrateur n'est demandé.
2. Double-cliquez dessus. La fenêtre s'ouvre et l'icône apparaît près de l'horloge : le symbole Forever Pulse (infini traversé d'un pouls) sur une tuile bleu nuit, avec une pastille de couleur. Elle se trouve parfois derrière la flèche « Afficher les icônes cachées ».
3. **Lancer au démarrage de Windows** est coché d'office. Décochez la case si vous préférez le lancer vous-même.
4. Cliquez sur **Connecter à Forever Pulse** puis confirmez le compte et le code dans votre navigateur.

Les données du compagnon vivent dans `%APPDATA%\ForeverPulse\Companion\` :

| Fichier | Rôle |
|---|---|
| `config.toml` | adresse du site (`site_url`, par défaut `https://forever-pulse.com`), dossier de WoW, lancement au démarrage, langue |
| `companion.db` | base locale : fichiers déjà lus, file d'envoi, cumul des personnages observés |
| `companion.log` | journal (5 Mo × 3 fichiers au plus), sans jeton ni nom de personnage |

Si WoW n'est pas installé dans `C:\Program Files (x86)\World of Warcraft`, corrigez `wow_dir` dans `config.toml`, puis relancez le compagnon. Le sous-dossier du client (`_classic_beta_` aujourd'hui) est trouvé tout seul.

## La fenêtre

Depuis la 0.6.0, la fenêtre suit le thème clair ou sombre de Windows (et change avec lui). Elle s'ouvre près de l'horloge, là où se trouve l'icône, puis là où vous l'avez laissée. De haut en bas :

- **État** : une pastille verte, orange ou rouge, la phrase d'état et, dessous, ce qu'il y a à faire (par exemple « Connecter à Forever Pulse » quand il manque un jeton).
- **Trois compteurs** : lots envoyés, en attente, refusés. En attente s'affiche en orange et refusés en rouge dès qu'ils ne sont pas nuls.
- **Dernier envoi** : date, nombre de lots et de personnages.
- **Personnages distincts observés** : une ligne par périmètre, « depuis le … » et le nombre. Ce nombre n'est jamais additionné entre périmètres. Au-delà de quatre périmètres, la dernière ligne indique combien d'autres existent ; le survol de cette ligne les liste.
- **Boutons** : **Envoyer maintenant** (en couleur), **Connecter à Forever Pulse**, **Gérer les installations** (votre compte sur le site), **Ouvrir le journal**. Sans autorisation, ou si le site l'a refusée, **Connecter à Forever Pulse** passe en premier et en couleur. Pendant un envoi, le bouton affiche « Envoi… » et ne se clique pas deux fois.
- **Lancer au démarrage de Windows** : un interrupteur, allumé par défaut. À l'ouverture de session, le compagnon démarre discrètement, avec l'icône seule.
- **FR / EN** : la langue. Tant qu'aucune langue n'a été choisie, le compagnon est en anglais ; un clic sur FR le passe en français. Le choix s'applique tout de suite à la fenêtre, au menu de l'icône et aux notifications, et il est gardé dans `config.toml` (`language = 'fr'` ou `'en'`). Le journal reste en français.
- **En bas à gauche**, les actions rares : **Effacer les données…** (après confirmation : vide la file d'envoi, le cumul et la liste des fichiers déjà lus ; les lots pas encore envoyés sont perdus ; le jeton et les réglages restent) et **Désinstaller…** (voir plus bas). Le lien passe en rouge au survol.
- **En bas à droite** : **Réduire** et **Quitter**. Réduire, le bouton « — » de la barre de titre, la croix ou la touche Échap ferment la fenêtre ; le compagnon continue près de l'horloge, sans notification ni bouton dans la barre des tâches. Quitter arrête le compagnon.

Chaque bouton a une infobulle qui dit ce qu'il fait. Les confirmations nomment l'action (« Effacer » ou « Désinstaller », face à « Annuler », choisi par défaut).

Au clavier : **Tab** et **Maj+Tab** (ou les flèches) passent d'un bouton à l'autre, **Entrée** ou **Espace** le déclenche, **Échap** ferme la fenêtre.

### Léger en fond

La fenêtre n'existe que lorsqu'elle est ouverte : la fermer libère tout ce qu'elle occupe. Lancé avec Windows, le compagnon ne la crée même pas tant qu'on ne clique pas sur l'icône. La surveillance se réveille toutes les 5 minutes (10 minutes jusqu’à la 0.7.1, 30 secondes en 0.7.2) : elle regarde seulement la taille et la date de `ForeverPulse.lua`, sans l’ouvrir, et vérifie s'il reste des lots à envoyer. Un fichier qui vient de changer est revérifié 3 secondes plus tard, puis traité. Un nouveau relevé part donc au plus 5 minutes et quelques secondes après la déconnexion ou le `/reload` ; **Envoyer maintenant** réveille la surveillance tout de suite. L'intervalle se règle dans `config.toml` (`poll_seconds`, de 1 à 3600 secondes). La mémoire demandée pour décoder un gros fichier et relire le cumul est rendue à Windows dès la fin du traitement : au repos, le compagnon occupe une quarantaine de Mo (0.7.4).

## Près de l'horloge

- **Clic gauche** sur l'icône : ouvre la fenêtre.
- **Clic droit** : un menu affiche l'état et propose :
  - **Ouvrir Forever Pulse Companion** (en gras : c'est aussi le clic gauche) ;
  - **Envoyer maintenant** ;
  - **Connecter à Forever Pulse**, **Annuler la connexion**, **Gérer les installations** ;
  - **Ouvrir le journal** ;
  - **Lancer au démarrage de Windows** ;
  - **Quitter**.

Si vous relancez l'exécutable alors qu'il tourne déjà, sa fenêtre revient simplement au premier plan.

| Pastille | Sens |
|---|---|
| verte | tout est envoyé |
| orange | des lots attendent (coupure réseau, site indisponible, quota) : nouvel essai automatique, de 30 s à 30 min |
| rouge | une action de votre part est nécessaire : jeton absent ou refusé, source désactivée par le site, version du site incompatible, lots refusés |

## Relier le compagnon à votre compte

Cliquez sur **Connecter à Forever Pulse**. Le compagnon affiche un code de 8 caractères (en grand dans la fenêtre,
dans une notification et dans le menu de l'icône) et ouvre la page du site. Vérifiez que la page montre **le même
code**, cochez la confirmation puis **Connecter cette installation**. L'autorisation est rangée dans le
**Gestionnaire d'identifiants Windows** (« ForeverPulse/Companion ») ; aucun jeton n'est à copier. Depuis la 0.10.0,
le bouton **Coller le jeton** n'existe plus : le site ne délivre plus de jeton manuel.

## Au quotidien

Le jeu écrit `ForeverPulse.lua` à la déconnexion et au `/reload`. Le compagnon le repère à son sondage suivant (toutes les 5 minutes, ou tout de suite avec **Envoyer maintenant**), vérifie que le fichier ne bouge plus pendant 3 secondes, puis :

1. il le lit **en partage**, sans le verrouiller ;
2. il le décode et reconstitue les deltas ;
3. il met à jour le cumul ;
4. il envoie.

Au lancement suivant, le jeu renomme le fichier en `.bak`. Le compagnon lit aussi le `.bak` à son démarrage, au cas où une session n'aurait pas été envoyée. Un fichier déjà lu (même empreinte SHA-256) ou un lot déjà envoyé n'est jamais renvoyé.

## Démarrer avec Windows

La case écrit la valeur `ForeverPulseCompanion` dans `HKEY_CURRENT_USER\Software\Microsoft\Windows\CurrentVersion\Run`. Cela ne concerne que votre session et ne demande pas de droits administrateur. L'option `--tray` fait démarrer le compagnon avec l'icône seule. Décocher la case retire la valeur. Si vous déplacez l'exécutable, relancez-le une fois pour mettre le chemin à jour.

## Ce qui est envoyé

Le recensement et les statistiques de `ForeverPulse.lua` sont envoyés après décodage, selon `docs/data-contract.md` du site. Depuis 0.8.0, les prix autorisés de `Auctionator.lua` et le catalogue d'objets de Forever Pulse suivent aussi `CONTRAT-HDV-v1.md` révision 2.

### Recensement : `ForeverPulseCensusDB` → `/api/ingest/census` (schéma 4)

- par lot :
  - identifiant, périmètre (région × mode × faction) et méthode (roster de canal ou `/who`) ;
  - horodatages ;
  - zone et effectif annoncé par le serveur pour cette zone, nom du canal ;
  - version de l'addon, identifiant aléatoire d'envoi, empreinte du fichier ;
- par personnage :
  - roster : GUID, nom, nom affiché, classe, race, sexe, niveau ;
  - `/who` : nom, classe et race telles qu'affichées, niveau, guilde, zone.

Le site garde ces observations 30 jours au plus et en publie des agrégats (population observée, classes, races, niveaux, guildes) par périmètre.

### Statistiques : `ForeverPulseStatsDB` → `/api/ingest/stats` (schéma 1 depuis la 0.7.0 ; schéma 2 avec repli, branche `feat/talent-specs`)

- par fichier : empreinte, origine (`current` ou `bak`), version de l'addon et du format des statistiques, langue du client ;
- par contexte de jeu : environnement, royaume, mode, région, version et build du jeu ;
- par personnage : GUID, nom, royaume, classe, race, faction, sexe, niveau, date de la dernière observation ;
- par relevé : date, niveau, source (cible, survol, groupe…) et valeurs des statistiques, rangées par unité (`count`, `copper`, `percent`) ;
- le catalogue des noms de statistiques quand l'addon l'a exporté (addon 3.9.8 et suivants) ;
- au schéma 2 (addon 4.2.0, branche `feat/talent-specs`) : par personnage, le dernier relevé de talents (date, niveau, source parmi plaque, cible, focus, survol, arbre, points par nœud avec l'entrée choisie, points par branche, branche dominante, nœuds hors grille) ; par fichier, la version du format des talents et le catalogue des arbres (nœuds, position, rangs, branche, sort, nom). Le nom de configuration de talents de l'inspecté n'est jamais lu ni envoyé. En repli sur le schéma 1, rien de cela ne part.

Le site garde la dernière valeur connue de chaque statistique par personnage, 30 jours au plus, et en publie des agrégats (moyenne, médiane, minimum, maximum, nombre de personnages qui ont réellement la mesure).

### Prix : `AUCTIONATOR_PRICE_DATABASE` → `/api/ingest/auctions` (schéma 1, depuis 0.8.0)

- Empreinte du fichier, versions de stockage 8/2, version de Forever Pulse et `update_id`.
- Marché unique : projet, environnement du jeu, région, royaume, règles et faction de l'hôtel (`Alliance`, `Horde`, `Neutral`), issus de la note de l'addon.
- Numéro d'objet, jour source brut, `day_basis`, prix bas et haut en texte décimal quand connus, quantité quand connue (zéro conservé), prix courant si son rattachement au dernier jour de la clé est possible.
- Objets nouveaux ou modifiés du catalogue `ForeverPulseStatsDB.objets` v1 : nom, langue, build, date d'export, qualité, classe, sous-classe, niveaux, prix marchand ; chaque information absente reste inconnue.

Les jours nominaux antérieurs à aujourd'hui UTC moins 31 jours ou postérieurs à aujourd'hui plus 2 jours ne partent pas. Le corps ne donne aucune heure d'observation des prix. Le jeton reste uniquement dans l'en-tête d'authentification.

Depuis la 0.4.0, le compagnon lit aussi le format compact de l'addon 3.7.0 (schéma 5 : dictionnaires partagés, lots `/who` codés, deltas de canaux). Il le développe avant tout contrôle : ce qui part vers le site est exactement ce qu'aurait donné le même relevé au format précédent (schéma 4).

Depuis la 0.5.0, il lit aussi le schéma 6 de l'addon 3.7.1 : les noms sont rangés une seule fois dans une table partagée du fichier et les lignes n'en portent que le numéro. Même principe : tout est développé au schéma 4 avant l'envoi.

## Ce qui n'est jamais envoyé

- `ForeverPulseScanDB`, l'état interne de l'addon : il n'est même pas lu.
- Aucun jeton, aucun identifiant de compte, aucun chemin de fichier ni aucun réglage du compagnon dans les données de jeu.
- Aucun contenu de chat, aucun identifiant Battle.net ni aucun chemin de fichier. Seuls les relevés Forever Pulse et les prix autorisés d'Auctionator sont utilisés.
- Aucune variable personnelle d'Auctionator : `AUCTIONATOR_POSTING_HISTORY`, `AUCTIONATOR_SHOPPING_LISTS`, `AUCTIONATOR_RECENT_SEARCHES`, `AUCTIONATOR_SELLING_GROUPS`, `AUCTIONATOR_CONFIG`, `AUCTIONATOR_SAVEDVARS`, `AUCTIONATOR_CHARACTER_CONFIG`, `AUCTIONATOR_VENDOR_PRICE_CACHE`, ni leurs valeurs. Elles sont ignorées pendant l'analyse, sans construction en mémoire.
- Aucun instant brut de la note `hdv` (`ts`, `dc`, `tz`, `tl`, `depuis`), aucun `uiMapID`, aucune faction de personnage dans le flux des prix. Seule l'identité prouvée du marché et la provenance du jour source partent.
- Aucun jour de prix sans marché unique prouvé, aucun prix bas fabriqué à partir du haut, aucune donnée d'`Auctionator.lua.bak`.
- Aucun `occurred_at` : un relevé prouve une présence à un instant, jamais la date d'un événement.

Le compagnon lit `ForeverPulse.lua`, `ForeverPulse.lua.bak` et, depuis 0.8.0, `Auctionator.lua`. Il n'écrit, ne renomme ni ne supprime rien dans le dossier du jeu. Il ne lit pas la mémoire du jeu et n'automatise aucune touche.

## Désinstaller

Dans la fenêtre, cliquez sur **Désinstaller…** (en bas à gauche), puis sur **Désinstaller**. Le compagnon retire :

1. le lancement au démarrage de Windows ;
2. le jeton rangé dans le Gestionnaire d'identifiants ;
3. le dossier `%APPDATA%\ForeverPulse\Companion\` (réglages, base, journal) ;
4. l'exécutable lui-même, deux secondes après s'être fermé.

Les lots pas encore envoyés sont perdus. Il reste à **révoquer cette installation** depuis votre compte sur le site (`https://forever-pulse.com/account`, panneau Companion) : le compagnon ne peut pas le faire à votre place. Rien n'est jamais touché dans le dossier du jeu.

## Ligne de commande (tests)

```
ForeverPulseCompanion.exe --once <fichier>            décode, valide et résume, sans rien envoyer ni écrire
ForeverPulseCompanion.exe --dry-run --out <dossier> [fichiers]   construit les requêtes sans les poster (options avant les fichiers)
ForeverPulseCompanion.exe --status                    état de la file locale (recensement, statistiques et prix), sans réseau
ForeverPulseCompanion.exe --sync [fichiers]           traite et envoie une fois, sans fenêtre
ForeverPulseCompanion.exe --store-token < jeton.txt   range un jeton lu sur l'entrée standard
ForeverPulseCompanion.exe --dump <fichiers>           JSON des lots et du cumul (test différentiel)
ForeverPulseCompanion.exe --tray                      démarre icône seule
ForeverPulseCompanion.exe --quit                      ferme proprement le compagnon déjà lancé
ForeverPulseCompanion.exe --menu                      ouvre le menu de l'icône du compagnon lancé (diagnostic)
```

État de la file locale, en comptes seulement (aucun nom, GUID, identifiant de lot ou d'observateur, empreinte ni jeton), sans réseau : `python outils\etat-file-locale.py`. Le script refuse de travailler si le compagnon tourne, copie ensemble `companion.db`, `companion.db-wal` et `companion.db-shm` dans un dossier temporaire, lit la copie puis l'efface. La base d'origine n'est jamais ouverte : même en lecture seule, SQLite crée les fichiers `-wal` et `-shm` à côté d'elle.

L'exécutable est compilé en application fenêtrée : lancé depuis PowerShell, il n'affiche rien à l'écran. Pour voir la sortie, utilisez `ForeverPulseCompanion.exe --once $f | Out-String`, ou redirigez-la vers un fichier.

## Construire

Il faut Go 1.24 ou plus récent, sans CGO :

```
go test ./...
set GOOS=windows& set GOARCH=amd64& set CGO_ENABLED=0
go build -trimpath -ldflags "-H windowsgui -s -w" -o ForeverPulseCompanion-0.8.0.exe ./cmd/forever-pulse-companion
```

Le fichier `cmd/forever-pulse-companion/rsrc_windows_amd64.syso` contient l'icône, le nom « Forever Pulse Companion » affiché dans les propriétés du fichier et le manifeste. Pour le régénérer : `go run ./outils/mkres 0.8.0`. Depuis la 0.6.1, l'icône (exécutable, barre des tâches, zone de notification et notifications) est le symbole officiel Forever Pulse, `internal/icone/forever.png`, posé sur une tuile bleu nuit → prune ; `go run ./outils/apercu-icone planche.png` en produit une planche de 16 à 256 px sur fonds clair et sombre. Depuis la 0.6.0, le manifeste déclare les contrôles communs v6 (boîtes de dialogue à boutons nommés, infobulles). L'outil utilise une copie réduite de `tc-hib/winres`, sous licence 0BSD, qui n'est pas liée dans l'exécutable.

Dépendances :
- `github.com/ncruces/go-sqlite3` : SQLite en pur Go, via WebAssembly ;
- `golang.org/x/sys` : appels Windows.

La fenêtre est décrite dans `internal/vue` (mise en page, sans Windows) et dessinée par `internal/winui` avec GDI+ (formes lissées) et GDI (textes ClearType, police Segoe UI, pictogrammes Segoe MDL2 Assets). Pour relire la mise en page hors Windows : `go run ./outils/apercu logo.png > apercu.json`, puis `python outils/apercu/rendu.py apercu.json apercus logo.png` (images PNG approchées).

La fenêtre, l'icône, le Gestionnaire d'identifiants et la surveillance du fichier sont écrits directement sur l'API Windows, sans `fyne.io/systray`, `wincred` ni `fsnotify`. Les registres de modules étaient inaccessibles au moment de l'écriture, et ces besoins tiennent en quelques centaines de lignes sur `golang.org/x/sys`, déjà requis par SQLite. La surveillance se fait par sondage toutes les 5 minutes (2 s jusqu'à la 0.5.0, 10 minutes de la 0.6.0 à la 0.7.1, 30 secondes en 0.7.2), car le jeu n'écrit le fichier qu'à la déconnexion et au `/reload`. Un sondage ne garde aucun fichier ni dossier ouvert entre deux tours ; une notification du système de fichiers tiendrait en permanence un descripteur sur le dossier `SavedVariables` pour un gain de quelques minutes sur des données qui ne s'écrivent qu'au `/reload` ou à la déconnexion.

Test différentiel avec la référence Python :

```
python outils\compare_go_python.py ForeverPulseCompanion.exe "..\forever-pulse-roadmap\outils" testdata\reel\ForeverPulse.lua testdata\reel\ForeverPulse.lua.bak
```

Les fichiers réels de `testdata\reel\` contiennent des noms de joueurs : ce dossier est ignoré par git et ne doit jamais être commité.
