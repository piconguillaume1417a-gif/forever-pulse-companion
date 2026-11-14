//go:build !windows

package i18n

// Le même texte sert à la fenêtre Windows et à la fenêtre Linux. Hors Windows,
// les rares messages qui nommaient Windows sont remplacés ici : le fichier
// i18n.go reste identique à celui du Compagnon Windows.
func init() {
	msgs["connect.vault"] = [2]string{
		"Impossible d’enregistrer l’autorisation dans le coffre du système%s",
		"Unable to save the credential in the system keyring%s",
	}
	msgs["win.autostart"] = [2]string{"Lancer à la connexion", "Start at login"}
	msgs["tip.autostart"] = [2]string{
		"À la connexion, le compagnon se lance discrètement.",
		"At login, the companion starts quietly.",
	}
	msgs["tip.minimize"] = [2]string{
		"Réduit la fenêtre (Échap) ; le compagnon continue en arrière-plan.",
		"Minimizes the window (Esc); the companion keeps running in the background.",
	}
	msgs["uninstall.confirm"] = [2]string{
		"Désinstaller Forever Pulse Companion ?\n\nCela retire le lancement à la connexion, le jeton rangé dans le coffre du système, toutes les données locales (%s) et le programme lui-même.\n\nLes lots pas encore envoyés seront perdus. Pensez aussi à révoquer le jeton sur la page des jetons du site.",
		"Uninstall Forever Pulse Companion?\n\nThis removes the start-at-login entry, the token stored in the system keyring, all local data (%s) and the program itself.\n\nBatches not sent yet will be lost. Remember to revoke the token on the website's token page too.",
	}
	msgs["uninstall.autostart"] = [2]string{"lancement à la connexion : %s", "start at login: %s"}
	msgs["uninstall.cred"] = [2]string{"jeton du coffre du système : %s", "token in the system keyring: %s"}
}
