// Package i18n : textes de l'interface en français et en anglais.
//
// Le journal technique et les sorties de la ligne de commande restent en français ;
// tout ce que la fenêtre, le menu et les notifications affichent passe par T.
package i18n

import (
	"fmt"
	"sync/atomic"
)

type Lang string

const (
	FR Lang = "fr"
	EN Lang = "en"
)

var cur atomic.Value

func init() { cur.Store(FR) }

// Parse : « fr » ou « en » ; toute autre valeur est refusée.
func Parse(s string) (Lang, bool) {
	switch Lang(s) {
	case FR, EN:
		return Lang(s), true
	}
	return "", false
}

// Set choisit la langue ; une valeur inconnue est ignorée.
func Set(l Lang) {
	if _, ok := Parse(string(l)); ok {
		cur.Store(l)
	}
}

func Get() Lang { return cur.Load().(Lang) }

// Choisie : la langue du réglage si elle est valide, sinon l'anglais (0.6.0 : la
// fenêtre, le menu et les notifications démarrent en anglais tant qu'aucune langue
// n'a été choisie ; le journal et la ligne de commande restent en français).
func Choisie(reglage string) Lang {
	if l, ok := Parse(reglage); ok {
		return l
	}
	return EN
}

// T renvoie le texte de la clé dans la langue choisie, formaté avec args.
func T(key string, args ...any) string {
	m, ok := msgs[key]
	if !ok {
		return key
	}
	s := m[0]
	if Get() == EN {
		s = m[1]
	}
	if len(args) == 0 {
		return s
	}
	return fmt.Sprintf(s, args...)
}

// Nom : le nom d'une langue dans cette langue (liste de choix).
func Nom(l Lang) string {
	if l == EN {
		return "English"
	}
	return "Français"
}

// Toutes : dans l'ordre de la liste de choix.
var Toutes = []Lang{FR, EN}

var msgs = map[string][2]string{
	"btn.connect":         {"Connecter à Forever Pulse", "Connect to Forever Pulse"},
	"tip.connect":         {"Ouvre le navigateur pour associer ce PC à votre compte. Aucun jeton à copier.", "Open your browser to connect this PC to your account. No token to copy."},
	"btn.installations":   {"Gérer les installations", "Manage installations"},
	"connect.pending":     {"En attente de confirmation — code %s", "Waiting for confirmation — code %s"},
	"connect.compare":     {"En attente de confirmation sur le site. Vérifiez que la page affiche ce code :", "Waiting for confirmation on the website. Check that the page shows this code:"},
	"connect.connected":   {"Connecté : %s", "Connected: %s"},
	"connect.cancelled":   {"Connexion annulée%s", "Connection cancelled%s"},
	"connect.expired":     {"Demande expirée : reconnectez-vous%s", "Request expired: connect again%s"},
	"connect.network":     {"Réseau indisponible : nouvel essai automatique%s", "Network unavailable: retrying automatically%s"},
	"connect.unavailable": {"Connexion indisponible. Réessayez%s", "Connection unavailable. Try again%s"},
	"connect.vault":       {"Impossible d’enregistrer l’autorisation Windows%s", "Unable to save Windows credential%s"},
	"connect.revoked":     {"Accès refusé ou révoqué : reconnectez le Companion%s", "Access refused or revoked: reconnect your Companion%s"},
	"connect.cancel":      {"Annuler la connexion", "Cancel connection"},
	// Fenêtre
	"win.subtitle":  {"Envoie les relevés de l'addon à forever-pulse.com.", "Sends the addon readings to forever-pulse.com."},
	"win.starting":  {"Démarrage…", "Starting…"},
	"win.autostart": {"Lancer au démarrage de Windows", "Start with Windows"},
	"win.language":  {"Langue :", "Language:"},
	"btn.send":      {"Envoyer maintenant", "Send now"},
	"btn.paste":     {"Coller le jeton", "Paste token"},
	"btn.log":       {"Ouvrir le journal", "Open log"},
	"btn.tokens":    {"Page des jetons (site)", "Token page (website)"},
	"btn.wipe":      {"Effacer les données locales…", "Clear local data…"},
	"btn.uninstall": {"Désinstaller…", "Uninstall…"},
	"btn.minimize":  {"Réduire", "Minimize"},
	"btn.quit":      {"Quitter", "Quit"},

	// Fenêtre 0.6.0
	"btn.tokenpage": {"Page des jetons", "Token page"},
	"btn.sending":   {"Envoi…", "Sending…"},
	"btn.busy":      {"Un instant…", "One moment…"},
	"tile.sent":     {"lots envoyés", "batches sent"},
	"tile.pending":  {"lots en attente", "batches pending"},
	"tile.rejected": {"lots refusés", "batches rejected"},
	"lnk.wipe":      {"Effacer les données…", "Clear data…"},
	"lnk.uninstall": {"Désinstaller…", "Uninstall…"},
	"win.cumul":     {"Personnages distincts observés", "Distinct characters observed"},
	"win.since":     {"depuis le %s", "since %s"},
	"win.more":      {"+ %d autres périmètres", "+ %d more scopes"},
	"hint.starting": {"Lecture de la base locale et des fichiers de l'addon…", "Reading the local database and the addon files…"},
	"hint.allsent":  {"Le compagnon surveille ForeverPulse.lua et envoie chaque nouveau relevé tout seul.", "The companion watches ForeverPulse.lua and uploads every new reading on its own."},
	"hint.pending":  {"L'envoi se fait tout seul ; « Envoyer maintenant » le lance sans attendre.", "Uploads happen on their own; “Send now” starts one right away."},
	"hint.notoken":  {"Cliquez sur « Connecter à Forever Pulse », puis confirmez cette installation dans votre navigateur.", "Click “Connect to Forever Pulse”, then approve this installation in your browser."},
	"hint.token":    {"L’accès a été refusé ou révoqué. Cliquez sur « Connecter à Forever Pulse » pour vous reconnecter.", "Access was refused or revoked. Click “Connect to Forever Pulse” to reconnect."},
	"hint.schema":   {"Les envois reprendront avec une version à jour du compagnon.", "Uploads will resume with an up-to-date companion."},
	"hint.source":   {"Le site n'accepte pas ces relevés pour l'instant. « Envoyer maintenant » réessaie.", "The website does not accept these readings for now. “Send now” tries again."},
	"hint.body":     {"Le journal donne le détail des lots refusés. « Envoyer maintenant » reprend les autres.", "The log lists the refused batches. “Send now” resumes the others."},
	"tip.send":      {"Envoie tout de suite les lots en attente, même après une erreur.", "Uploads pending batches right away, even after an error."},
	"tip.paste":     {"Enregistre le jeton fpc_… copié depuis le site, puis vide le presse-papiers. Raccourci : Ctrl+V.", "Saves the fpc_… token copied from the website, then clears the clipboard. Shortcut: Ctrl+V."},
	"tip.tokens":    {"Ouvre la page du site (forever-pulse.com/account/companion) où créer ou révoquer un jeton.", "Opens the website page (forever-pulse.com/account/companion) where tokens are created or revoked."},
	"tip.log":       {"Ouvre le journal : fichiers lus, envois, erreurs.", "Opens the log: files read, uploads, errors."},
	"tip.autostart": {"Au démarrage de Windows, le compagnon se lance discrètement, icône seule près de l'horloge.", "When Windows starts, the companion starts quietly, as an icon next to the clock."},
	"tip.lang":      {"Langue de la fenêtre, du menu et des notifications.", "Language of the window, the menu and the notifications."},
	"tip.wipe":      {"Vide la file d'envoi, le cumul et la liste des fichiers lus sur ce PC.", "Empties the upload queue, the tally and the list of files read on this PC."},
	"tip.uninstall": {"Retire le compagnon de ce PC : démarrage automatique, jeton, données et programme.", "Removes the companion from this PC: autostart, token, data and program."},
	"tip.minimize":  {"Cache la fenêtre (Échap) ; le compagnon continue près de l'horloge.", "Hides the window (Esc); the companion keeps running next to the clock."},
	"tip.quit":      {"Arrête le compagnon : plus de surveillance ni d'envoi jusqu'au prochain lancement.", "Stops the companion: no more watching or uploads until it is started again."},
	"dlg.wipe":      {"Effacer", "Clear"},
	"dlg.uninstall": {"Désinstaller", "Uninstall"},
	"dlg.cancel":    {"Annuler", "Cancel"},

	// Menu de l'icône
	"menu.open": {"Ouvrir Forever Pulse Companion", "Open Forever Pulse Companion"},

	// État
	"state.counts":    {"%s\n%d envoyés, %d en attente, %d refusés", "%s\n%d sent, %d pending, %d rejected"},
	"state.nocumul":   {"Aucun personnage observé pour l'instant.", "No characters observed yet."},
	"state.nosend":    {"Aucun envoi pour l'instant.", "Nothing sent yet."},
	"st.schema":       {"Version du site incompatible : envois arrêtés", "Incompatible website version: uploads stopped"},
	"st.token":        {"Accès refusé : reconnectez le Companion", "Access refused: reconnect your Companion"},
	"st.source":       {"Source désactivée par le site", "Source disabled by the website"},
	"st.body":         {"Lots refusés par le site : voir le journal", "Batches refused by the website: see the log"},
	"st.notoken":      {"Companion déconnecté", "Companion disconnected"},
	"st.pending":      {"%d lots en attente d'envoi", "%d batches waiting to be sent"},
	"st.retry":        {", nouvel essai à %s", ", next attempt at %s"},
	"st.statspending": {"%d fiches de statistiques en attente d'envoi", "%d statistics sheets waiting to be sent"},
	"st.allsent":      {"Tout est envoyé", "Everything has been sent"},
	"st.lastsend":     {"Dernier envoi : %s, %s lots, %s personnages", "Last upload: %s, %s batches, %s characters"},
	"st.cumul":        {"%s : %d personnages distincts observés depuis le %s", "%s: %d distinct characters observed since %s"},
	"fmt.date":        {"02/01/2006", "Jan 2, 2006"},
	"fmt.datetime":    {"02/01 15:04", "Jan 2, 15:04"},
	"fmt.time":        {"15:04", "15:04"},
	"notify.sent":     {"Envoi terminé", "Upload complete"},
	"notify.notoken":  {"%s : connexion nécessaire", "%s: connection needed"},
	"notify.howto":    {"Cliquez sur « Connecter à Forever Pulse », puis confirmez dans votre navigateur.", "Click “Connect to Forever Pulse”, then confirm in your browser."},
	"update.ready":    {"Version %s prête : elle s'installera au prochain démarrage, ou tout de suite par le menu de l'icône.", "Version %s is ready: it will be installed on next start, or right away from the icon menu."},
	"update.done":     {"Mis à jour en version %s", "Updated to version %s"},
	"update.rollback": {"La version %s n'a pas démarré correctement : retour à la version précédente", "Version %s did not start correctly: back to the previous version"},
	"menu.update":     {"Redémarrer pour mettre à jour (%s)", "Restart to update (%s)"},

	// Jeton
	"paste.none": {"Le presse-papiers ne contient pas de jeton fpc_…\n\nCréez un jeton sur la page des jetons du site, copiez-le, puis recommencez.",
		"The clipboard does not contain an fpc_… token.\n\nCreate a token on the website's token page, copy it, then try again."},
	"paste.fail": {"Jeton non enregistré : %s", "Token not saved: %s"},
	"paste.ok":   {"Jeton enregistré, presse-papiers vidé", "Token saved, clipboard cleared"},

	// Réglages
	"autostart.fail": {"Réglage impossible : %s", "Could not change the setting: %s"},

	// Effacer
	"wipe.confirm": {"Effacer les données locales ?\n\nLa file d'envoi (y compris les lots pas encore envoyés), le cumul des personnages observés et la liste des fichiers déjà lus seront supprimés de ce PC.\n\nLe jeton, les réglages et les données déjà reçues par le site sont conservés.",
		"Clear local data?\n\nThe upload queue (including batches not sent yet), the tally of observed characters and the list of files already read will be deleted from this PC.\n\nThe token, the settings and the data the website already received are kept."},
	"wipe.fail": {"Effacement impossible : %s", "Could not clear the data: %s"},
	"wipe.ok":   {"Données locales effacées.", "Local data cleared."},

	// Désinstaller
	"uninstall.confirm": {"Désinstaller Forever Pulse Companion ?\n\nCela retire le lancement au démarrage de Windows, le jeton rangé dans le Gestionnaire d'identifiants, toutes les données locales (%s) et le programme lui-même.\n\nLes lots pas encore envoyés seront perdus. Pensez aussi à révoquer le jeton sur la page des jetons du site.",
		"Uninstall Forever Pulse Companion?\n\nThis removes the start with Windows entry, the token stored in Credential Manager, all local data (%s) and the program itself.\n\nBatches not sent yet will be lost. Remember to revoke the token on the website's token page too."},
	"uninstall.done":      {"Forever Pulse Companion est désinstallé.", "Forever Pulse Companion has been uninstalled."},
	"uninstall.check":     {"À vérifier :", "Please check:"},
	"uninstall.revoke":    {"Révoquez le jeton sur le site : %s", "Revoke the token on the website: %s"},
	"uninstall.autostart": {"lancement au démarrage : %s", "start with Windows: %s"},
	"uninstall.cred":      {"jeton du Gestionnaire d'identifiants : %s", "token in Credential Manager: %s"},
	"uninstall.dir":       {"dossier %s : %s", "folder %s: %s"},

	// Erreurs d'envoi
	"err.notoken":       {"aucun jeton enregistré", "no token saved"},
	"err.400":           {"envoi refusé par le site (400)", "upload refused by the website (400)"},
	"err.413":           {"envoi trop gros (413)", "upload too large (413)"},
	"err.401":           {"jeton refusé (401)", "token refused (401)"},
	"err.403":           {"source désactivée par le site (403)", "source disabled by the website (403)"},
	"err.409":           {"version du site incompatible (409)", "incompatible website version (409)"},
	"err.429":           {"quota atteint (429), nouvel essai dans %s", "rate limited (429), next attempt in %s"},
	"err.retry":         {"%s, nouvel essai dans %s", "%s, next attempt in %s"},
	"err.network":       {"réseau : %s", "network: %s"},
	"err.notsite":       {"réponse inattendue (pas la route d'envoi du site)", "unexpected response (not the site's upload route)"},
	"err.redirect":      {"redirection refusée (pas la route d'envoi du site)", "redirect refused (not the site's upload route)"},
	"err.request":       {"requête impossible", "could not build the request"},
	"err.stats403":      {"statistiques : source désactivée par le site (403)", "statistics: source disabled by the website (403)"},
	"err.stats409":      {"statistiques : version du site incompatible (409)", "statistics: incompatible website version (409)"},
	"err.statsretry":    {"statistiques : %s, nouvel essai dans %s", "statistics: %s, next attempt in %s"},
	"win.auctions":      {"HdV : %d lignes envoyées · %d en attente", "Auctions: %d rows sent · %d pending"},
	"win.auctiondays":   {"%d jours sans faction prouvée", "%d days without proven faction"},
	"st.auctionpending": {"%d lignes de prix HdV en attente", "%d auction price rows waiting"},
	"det.auctions":      {"Prix HdV : %d lignes envoyées, %d en attente, %d jours sans faction prouvée", "Auction prices: %d rows sent, %d pending, %d days without proven faction"},
	"det.auctiondays":   {"Prix HdV, comptages : %s", "Auction prices, counts: %s"},
	"det.auctionerror":  {"Prix HdV, dernière erreur : %s", "Auction prices, last error: %s"},
	"det.auctionsent":   {"Prix HdV, dernier accusé : %s", "Auction prices, last acknowledgement: %s"},
	"det.file":          {"Fichier suivi : %s", "Tracked file: %s"},
	"det.nofile":        {"Fichier suivi : aucun fichier lu pour l'instant", "Tracked file: no file read yet"},
	"det.read":          {"Dernière lecture : %s (empreinte %s)", "Last read: %s (fingerprint %s)"},
	"det.sent":          {"Dernier envoi de lots confirmé : %s (%s lots)", "Last confirmed batch upload: %s (%s batches)"},
	"det.nosent":        {"Dernier envoi de lots confirmé : aucun", "Last confirmed batch upload: none"},
	"det.statssent":     {"Dernier envoi de statistiques confirmé : %s (%s fiches)", "Last confirmed statistics upload: %s (%s sheets)"},
	"det.nostatssent":   {"Dernier envoi de statistiques confirmé : aucun", "Last confirmed statistics upload: none"},
	"det.pending":       {"En attente : %d lots, %d fiches de statistiques", "Waiting: %d batches, %d statistics sheets"},
	"det.stats":         {"Statistiques : %d fiches envoyées, %d refusées", "Statistics: %d sheets sent, %d refused"},
	"det.statsblocked":  {"Statistiques arrêtées : %s", "Statistics stopped: %s"},
	"det.statsschema1":  {"Statistiques envoyées en schéma 1, sans talents (site ancien) ; schéma 2 retenté après %s", "Statistics sent as schema 1, without talents (older website); schema 2 retried after %s"},
	"det.error":         {"Dernière erreur : %s, %s", "Last error: %s, %s"},
	"det.noerror":       {"Dernière erreur : aucune", "Last error: none"},
	"tip.details":       {"Détail de l'état", "Status details"},
}
