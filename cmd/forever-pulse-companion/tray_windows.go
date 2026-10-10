//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"wowsync/internal/app"
	"wowsync/internal/config"
	"wowsync/internal/connect"
	"wowsync/internal/i18n"
	"wowsync/internal/logx"
	"wowsync/internal/secret"
	"wowsync/internal/sender"
	"wowsync/internal/store"
	"wowsync/internal/update"
	"wowsync/internal/vue"
	"wowsync/internal/winui"
)

// runTray : fenêtre + icône près de l'horloge. hidden : lancé au démarrage de
// Windows, la fenêtre reste cachée (un clic sur l'icône l'ouvre). apresMaj : pid de
// l'instance qui vient d'installer cette version ; on attend sa sortie.
func runTray(dataDir string, hidden bool, apresMaj int) error {
	// An isolated candidate must never interact with the installed UI/startup.
	if dataDir != "" || os.Getenv("WOWSYNC_DATA_DIR") != "" {
		return fmt.Errorf("isolated data directory: use --connect or --sync")
	}
	if apresMaj > 0 {
		update.AttendFin(apresMaj, 30*time.Second)
	}
	if !winui.SingleInstance() {
		return nil // déjà lancé : sa fenêtre vient d'être ramenée au premier plan
	}
	cfg, log, st, dir, err := ouvre(dataDir)
	if err != nil {
		return err
	}
	// Application fenêtrée : sans console, un plantage serait muet. Le runtime Go
	// écrit alors sa trace dans crash.txt (dossier de données).
	if f, err := os.OpenFile(filepath.Join(dir, "crash.txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		_ = debug.SetCrashOutput(f, debug.CrashOptions{})
		f.Close() // le runtime garde sa propre copie du descripteur
	}
	log.Printf("démarrage, version %s%s", app.Version, map[bool]string{true: ", icône seule", false: ""}[hidden])
	i18n.Set(i18n.Choisie(cfg.Language))

	// 0.10.0 : version à l'essai, puis version téléchargée en attente d'installation.
	exe, _ := os.Executable()
	majDir := filepath.Join(dir, update.DossierMaj)
	maj := update.Nouveau(app.Version)
	update.Menage(exe)
	if e, retour := update.Demarre(majDir, app.Version); retour {
		log.Printf("mise à jour %s : %d démarrages sans confirmation, retour à %s", e.Vers, e.Tentatives-1, e.De)
		if err := update.Retablit(exe, majDir, e); err != nil {
			log.Printf("retour arrière impossible : %v", err)
		} else {
			st.Close()
			log.Close()
			return update.Relance(exe, os.Getpid())
		}
	}
	if m, pret, ok := maj.Prete(majDir); ok && cfg.AutoUpdate {
		if relance, err := installeMaj(exe, majDir, m, pret, log); relance {
			st.Close()
			log.Close()
			return err
		}
	}
	a, err := app.New(st, log, sender.New(cfg.SiteURL, app.Version))
	if err != nil {
		st.Close()
		log.Close()
		return err
	}
	vault := secret.ForContext(dir, cfg.SiteURL, false)
	a.Token, a.SaveToken = vault.Get, vault.Set
	// La configuration fait foi : coché par défaut au premier lancement ; la valeur
	// est réécrite à chaque démarrage pour suivre l'exécutable s'il a été déplacé.
	_ = winui.StartWithWindows(cfg.StartWithWindows)

	companionURL := config.PageCompte(cfg.SiteURL)
	ctx, cancel := context.WithCancel(context.Background())
	var fin sync.Once
	t := &winui.Tray{}
	w := &winui.Window{}
	w.SetEtat(vue.Modele{Version: app.Version}) // « Démarrage… » jusqu'au premier état

	// Réglages modifiés depuis plusieurs fils (case, langue) : un seul verrou.
	var cfgMu sync.Mutex
	sauve := func(maj func(*config.Config)) {
		cfgMu.Lock()
		defer cfgMu.Unlock()
		maj(&cfg)
		if err := config.Save(filepath.Join(dir, config.FichierConfig), cfg); err != nil {
			log.Printf("config.toml : %v", err)
		}
	}

	// Dernier état connu : le menu du clic droit le lit sans toucher à la base,
	// pour s'ouvrir tout de suite même pendant un envoi.
	var etatMu sync.Mutex
	etat := i18n.T("win.starting")
	var connectionMu sync.Mutex
	connectionState, connectionDetail := "", ""
	refresh := func() {
		e := a.Etat()
		connectionMu.Lock()
		state, detail := connectionState, connectionDetail
		connectionMu.Unlock()
		connectionText, connectionCode := vue.Connexion(e.Code, state, detail)
		etatMu.Lock()
		etat = e.Message
		etatMu.Unlock()
		t.Set(int(e.Couleur), e.Message)
		m := vue.Modele{Pret: true, Couleur: int(e.Couleur), Code: e.Code, Message: e.Message,
			Envoyes: e.Sent, Attente: e.Pending, Refuses: e.Rejected, DernierEnvoi: e.DernierEnvoi, Version: app.Version,
			Details: e.Details, Auctions: true, AuctionSent: e.AuctionSent, AuctionPending: e.AuctionPending, AuctionUnproven: e.AuctionUnproven}
		m.Connection, m.ConnectionCode = connectionText, connectionCode
		for _, p := range e.Portees {
			m.Portees = append(m.Portees, vue.Portee{Nom: p.ScopeID, Total: p.Total, Depuis: p.Depuis})
		}
		w.SetEtat(m)
	}
	a.OnChange = refresh

	// Version téléchargée et vérifiée, proposée dans le menu de l'icône.
	var majMu sync.Mutex
	majPrete := ""
	relancerMaj := false
	quitter := func() {
		fin.Do(func() {
			log.Printf("arrêt demandé")
			cancel()
			t.Quit()
		})
	}
	// « Envoyer maintenant » réveille aussi la surveillance : un ForeverPulse.lua
	// écrit depuis le dernier sondage (10 min) est lu sans attendre.
	reveil := make(chan struct{}, 1)
	var connectMu sync.Mutex
	var stopMu sync.Mutex
	var stopConnect context.CancelFunc
	connector := &connect.Client{Site: cfg.SiteURL, Vault: vault, SaveToken: a.SetToken, Open: winui.Open,
		Status: func(state, detail string) {
			connectionMu.Lock()
			previous, previousDetail := connectionState, connectionDetail
			connectionState, connectionDetail = state, detail
			connectionMu.Unlock()
			if state != previous {
				log.Printf("association : %s", state) // jamais le détail (adresse du compte)
			}
			refresh()
			if state == "pending" && detail != "" && (previous != "pending" || previousDetail != detail) {
				// Le code doit être visible même quand l'association part du menu de
				// l'icône, fenêtre fermée : on l'ouvre et on le notifie.
				w.PostShow()
				t.Notify(winui.AppName, i18n.T("connect.pending", detail), false)
			}
			if state == "connected" {
				select {
				case reveil <- struct{}{}:
				default:
				}
			}
		}}
	connecter := func(resume bool) {
		if !connectMu.TryLock() {
			return
		}
		defer connectMu.Unlock()
		cctx, stop := context.WithCancel(ctx)
		stopMu.Lock()
		stopConnect = stop
		stopMu.Unlock()
		defer stop()
		var err error
		if resume {
			err = connector.Resume(cctx)
		} else {
			err = connector.Start(cctx)
		}
		if err != nil && cctx.Err() == nil {
			connector.Status("unavailable", "")
		}
	}
	annulerConnexion := func() {
		stopMu.Lock()
		if stopConnect != nil {
			stopConnect()
		}
		stopMu.Unlock()
		connectMu.Lock()
		defer connectMu.Unlock()
		_ = connector.Cancel(ctx)
	}
	envoyer := func() {
		select {
		case reveil <- struct{}{}:
		default:
		}
		if err := a.Flush(ctx, true); err != nil {
			t.Notify(winui.AppName, err.Error(), true)
		} else {
			t.Notify(winui.AppName, i18n.T("notify.sent"), false)
		}
	}
	demarrage := func(on bool) {
		if err := winui.StartWithWindows(on); err != nil {
			w.SetChecked(!on)
			w.Info(i18n.T("autostart.fail", err))
			return
		}
		sauve(func(c *config.Config) { c.StartWithWindows = on })
		log.Printf("lancer au démarrage de Windows : %t", on)
	}
	langue := func(l i18n.Lang) {
		i18n.Set(l)
		sauve(func(c *config.Config) { c.Language = string(l) })
		log.Printf("langue : %s", l)
		w.Relabel()
		refresh()
	}
	effacer := func() {
		if !w.Confirm(i18n.T("wipe.confirm"), i18n.T("dlg.wipe")) {
			return
		}
		if err := a.EffacerDonnees(); err != nil {
			w.Info(i18n.T("wipe.fail", err))
			return
		}
		w.Info(i18n.T("wipe.ok"))
	}
	redemarrer := func() {
		majMu.Lock()
		relancerMaj = true
		majMu.Unlock()
		quitter()
	}
	annoncer := func(v string) {
		majMu.Lock()
		deja := majPrete == v
		majPrete = v
		majMu.Unlock()
		if !deja {
			t.Notify(winui.AppName, i18n.T("update.ready", v), false)
		}
	}
	// Vérification 3 minutes après le démarrage, puis une fois par jour (6 h après
	// un échec réseau). Une version imposée (min_version) s'installe aussitôt.
	verifier := func() {
		attente := 3 * time.Minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(attente):
			}
			attente = 24 * time.Hour
			o, err := maj.Cherche(ctx)
			if err != nil {
				log.Printf("mise à jour : %v", err)
				attente = 6 * time.Hour
				continue
			}
			v := o.Manifeste.Version
			if !o.Nouvelle || update.Refusee(majDir, v) {
				continue
			}
			if m, _, ok := maj.Prete(majDir); !ok || m.Version != v {
				if err := maj.Telecharge(ctx, o, majDir); err != nil {
					log.Printf("mise à jour %s : %v", v, err)
					attente = 6 * time.Hour
					continue
				}
				log.Printf("mise à jour %s téléchargée et vérifiée", v)
			}
			if o.Imposee {
				log.Printf("mise à jour %s imposée (version minimale %s)", v, o.Manifeste.MinVersion)
				redemarrer()
				return
			}
			annoncer(v)
		}
	}
	desinstaller := func() {
		if !w.Confirm(i18n.T("uninstall.confirm", dir), i18n.T("dlg.uninstall")) {
			return
		}
		cancel()
		connectMu.Lock()
		defer connectMu.Unlock()
		_ = vault.DeleteConnection()
		errs := desinstalle(dir, st, log)
		msg := i18n.T("uninstall.done")
		if len(errs) > 0 {
			msg += "\n\n" + i18n.T("uninstall.check") + "\n- " + strings.Join(errs, "\n- ")
		}
		w.Info(msg + "\n\n" + i18n.T("uninstall.revoke", companionURL))
		supprimeExe()
		fin.Do(func() { t.Quit() })
	}

	t.OnOpen = w.PostShow
	t.OnBalloonClick = w.PostShow
	t.Menu = func() []winui.Item {
		etatMu.Lock()
		msg := etat
		etatMu.Unlock()
		majMu.Lock()
		v := majPrete
		majMu.Unlock()
		items := []winui.Item{{Label: msg}}
		connectionMu.Lock()
		if connectionState == "pending" && connectionDetail != "" {
			items = append(items, winui.Item{Label: i18n.T("connect.pending", connectionDetail)})
		}
		connectionMu.Unlock()
		items = append(items, winui.Item{Separator: true})
		if v != "" {
			items = append(items, winui.Item{Label: i18n.T("menu.update", v), Action: redemarrer}, winui.Item{Separator: true})
		}
		return append(items, []winui.Item{
			{Label: i18n.T("menu.open"), Action: w.PostShow, Default: true},
			{Label: i18n.T("btn.send"), Action: envoyer},
			{Label: i18n.T("btn.connect"), Action: func() { connecter(false) }},
			{Label: i18n.T("connect.cancel"), Action: annulerConnexion},
			{Label: i18n.T("btn.installations"), Action: func() { winui.Open(companionURL) }},
			{Label: i18n.T("btn.log"), Action: func() { winui.Open(log.Path()) }},
			{Label: i18n.T("win.autostart"), Checked: winui.StartsWithWindows(), Action: func() {
				on := !winui.StartsWithWindows()
				demarrage(on)
				w.SetChecked(on)
			}},
			{Separator: true},
			{Label: i18n.T("btn.quit"), Action: quitter},
		}...)
	}
	w.OnToggle = demarrage
	w.OnLangue = langue
	w.OnCommand = func(id int) {
		switch id {
		case winui.CmdEnvoyer:
			envoyer()
		case winui.CmdInstallations:
			winui.Open(companionURL)
		case winui.CmdJournal:
			winui.Open(log.Path())
		case winui.CmdPage:
			connecter(false)
		case winui.CmdEffacer:
			effacer()
		case winui.CmdDesinstaller:
			desinstaller()
		case winui.CmdQuitter:
			quitter()
		}
	}

	err = t.Run(func(inst uintptr) {
		if err := w.Create(inst, winui.StartsWithWindows()); err != nil {
			log.Printf("fenêtre : %v", err)
			return
		}
		if !hidden {
			w.Show()
		}
	}, func() {
		refresh()
		go connecter(true)
		if _, err := a.Token(); err != nil {
			t.Notify(i18n.T("notify.notoken", winui.AppName), i18n.T("notify.howto"), false)
		}
		cfgMu.Lock()
		c := cfg
		cfgMu.Unlock()
		engine(ctx, a, c, log, reveil, func() {
			if e, ok := update.Confirme(exe, majDir, app.Version); ok {
				log.Printf("mise à jour %s → %s confirmée", e.De, e.Vers)
				t.Notify(winui.AppName, i18n.T("update.done", e.Vers), false)
			}
			if c.AutoUpdate {
				go verifier()
			}
		})
	})
	cancel()
	connectMu.Lock()
	defer connectMu.Unlock()
	majMu.Lock()
	relancer := relancerMaj
	majMu.Unlock()
	if relancer {
		if m, pret, ok := maj.Prete(majDir); ok {
			if relance, rerr := installeMaj(exe, majDir, m, pret, log); !relance {
				_ = update.Relance(exe, os.Getpid()) // version actuelle, déjà acceptée par Windows
			} else if rerr != nil {
				log.Printf("relance : %v", rerr)
			}
		}
	}
	st.Close()
	log.Close()
	return err
}

// installeMaj remplace le programme par la version prête et lance la nouvelle
// instance. relance : vrai si le programme a été remplacé (l'appelant doit sortir).
func installeMaj(exe, majDir string, m update.Manifeste, pret string, log *logx.Logger) (relance bool, err error) {
	if err := update.Installe(exe, majDir, app.Version, m, pret); err != nil {
		log.Printf("mise à jour %s non installée : %v", m.Version, err)
		return false, nil
	}
	if err := update.Relance(exe, os.Getpid()); err != nil {
		// Lancement refusé (Smart App Control…) : l'ancienne version reprend sa place.
		log.Printf("mise à jour %s non lancée (%v) : retour à %s", m.Version, err, app.Version)
		if rerr := update.Retablit(exe, majDir, update.Essai{De: app.Version, Vers: m.Version}); rerr != nil {
			log.Printf("retour arrière impossible : %v", rerr)
			return true, err
		}
		return false, nil
	}
	log.Printf("mise à jour %s → %s installée, relance", app.Version, m.Version)
	return true, nil
}

func quitRunning() error {
	if !winui.QuitRunning() {
		return fmt.Errorf("le compagnon ne tourne pas")
	}
	return nil
}

func menuRunning() error {
	if !winui.MenuRunning() {
		return fmt.Errorf("le compagnon ne tourne pas")
	}
	return nil
}

// desinstalle retire tout ce que le compagnon a créé sur ce PC, sauf l'exécutable
// (supprimé juste après la sortie, voir supprimeExe). Ne touche jamais au dossier du jeu.
func desinstalle(dir string, st *store.Store, log *logx.Logger) []string {
	var errs []string
	if err := winui.StartWithWindows(false); err != nil {
		errs = append(errs, i18n.T("uninstall.autostart", err))
	}
	if err := secret.Delete(); err != nil {
		errs = append(errs, i18n.T("uninstall.cred", err))
	}
	log.Printf("désinstallation demandée")
	st.Close()
	log.Close()
	_ = debug.SetCrashOutput(nil, debug.CrashOptions{}) // libère crash.txt avant l'effacement
	if err := os.RemoveAll(dir); err != nil {
		errs = append(errs, i18n.T("uninstall.dir", dir, err))
	}
	if base, err := config.BaseDir(); err == nil {
		_ = os.RemoveAll(filepath.Join(base, "wowsync")) // ancienne version 0.1
		_ = os.Remove(base)                              // seulement s'il est vide
	}
	return errs
}

// supprimeExe efface l'exécutable deux secondes après la sortie du programme
// (Windows interdit d'effacer un programme en cours d'exécution).
func supprimeExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command("cmd.exe")
	// Ligne de commande brute : cmd.exe ne comprend pas l'échappement \" de Go.
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000, /* CREATE_NO_WINDOW */
		CmdLine: `cmd.exe /d /c ping -n 3 127.0.0.1 >nul & del /f /q "` + exe + `"`}
	_ = cmd.Start()
}
