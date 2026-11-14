//go:build linux && cgo

package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
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
	"wowsync/internal/vue"
	"wowsync/internal/watch"
	"wowsync/internal/webui"
)

// runTray : fenêtre WebKitGTK. hidden : démarrage sans afficher la fenêtre
// (lancement automatique) ; un second lancement la ramène au premier plan.
// apresMaj est ignoré : la mise à jour automatique reste désactivée sur Linux.
func runTray(dataDir string, hidden bool, apresMaj int) error {
	dir := dataDir
	if dir == "" {
		d, err := config.DataDir()
		if err != nil {
			return err
		}
		dir = d
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	unique, arreter, err := verrouInstance(dir)
	if err != nil {
		return err
	}
	defer arreter()
	if !unique {
		return nil // déjà lancé : sa fenêtre vient d'être ramenée au premier plan
	}

	cfg, log, st, dir, err := ouvre(dir)
	if err != nil {
		return err
	}
	defer st.Close()
	defer log.Close()
	if f, err := os.OpenFile(filepath.Join(dir, "crash.txt"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
		_ = debug.SetCrashOutput(f, debug.CrashOptions{})
		f.Close()
	}
	log.Printf("démarrage, version %s%s", app.Version, map[bool]string{true: ", sans fenêtre", false: ""}[hidden])
	i18n.Set(i18n.Choisie(cfg.Language))

	a, err := app.New(st, log, sender.New(cfg.SiteURL, app.Version))
	if err != nil {
		return err
	}
	vault := secret.ForContext(dir, cfg.SiteURL, false)
	a.Token, a.SaveToken = vault.Get, vault.Set
	_ = webui.StartWithWindows(cfg.StartWithWindows)

	if actuel := watch.Examine(cfg.WowDir); actuel != watch.DossierPret {
		candidats := webui.DossiersBattleNet()
		if d := watch.Detecte(candidats); d != "" && !strings.EqualFold(d, cfg.WowDir) &&
			(watch.Examine(d) == watch.DossierPret || actuel == watch.DossierAbsent) {
			log.Printf("dossier du jeu détecté : %s (au lieu de %s)", d, cfg.WowDir)
			cfg.WowDir = d
			if err := config.Save(filepath.Join(dir, config.FichierConfig), cfg); err != nil {
				log.Printf("config.toml : %v", err)
			}
		}
	}

	companionURL := config.PageCompte(cfg.SiteURL)
	ctx, cancel := context.WithCancel(context.Background())
	var fin sync.Once

	w := webui.NewWindow()
	w.SetEtat(vue.Modele{Version: app.Version})
	w.SetChecked(webui.StartsWithWindows())
	t := webui.NewTray()
	var etatMu sync.Mutex
	etat := i18n.T("win.starting")

	var cfgMu sync.Mutex
	sauve := func(maj func(*config.Config)) {
		cfgMu.Lock()
		defer cfgMu.Unlock()
		maj(&cfg)
		if err := config.Save(filepath.Join(dir, config.FichierConfig), cfg); err != nil {
			log.Printf("config.toml : %v", err)
		}
	}
	dossier := func() string {
		cfgMu.Lock()
		defer cfgMu.Unlock()
		return cfg.WowDir
	}

	var connectionMu sync.Mutex
	connectionState, connectionDetail := "", ""
	refresh := func() {
		e := a.Etat()
		etatMu.Lock()
		etat = e.Message
		etatMu.Unlock()
		t.Set(int(e.Couleur), e.Message)
		connectionMu.Lock()
		state, detail := connectionState, connectionDetail
		connectionMu.Unlock()
		connectionText, connectionCode := vue.Connexion(e.Code, state, detail)
		m := vue.Modele{Pret: true, Couleur: int(e.Couleur), Code: e.Code, Message: e.Message,
			Envoyes: e.Sent, Attente: e.Pending, Refuses: e.Rejected, DernierEnvoi: e.DernierEnvoi, Version: app.Version,
			Details: e.Details, Auctions: true, AuctionSent: e.AuctionSent, AuctionPending: e.AuctionPending, AuctionUnproven: e.AuctionUnproven}
		m.Connection, m.ConnectionCode = connectionText, connectionCode
		m.Dossier = dossier()
		m.DossierEtat = int(watch.Examine(m.Dossier))
		for _, p := range e.Portees {
			m.Portees = append(m.Portees, vue.Portee{Nom: p.ScopeID, Total: p.Total, Depuis: p.Depuis})
		}
		w.SetEtat(m)
	}
	a.OnChange = refresh

	quitter := func() {
		fin.Do(func() {
			log.Printf("arrêt demandé")
			cancel()
			t.Quit()
			w.Quit()
		})
	}

	reveil := make(chan struct{}, 1)
	var connectMu sync.Mutex
	openConnection := make(chan struct{}, 1)
	var stopMu sync.Mutex
	var stopConnect context.CancelFunc
	connector := &connect.Client{Site: cfg.SiteURL, Vault: vault, SaveToken: a.SetToken, Open: webui.Open, OpenRequests: openConnection,
		Status: func(state, detail string) {
			connectionMu.Lock()
			previous, previousDetail := connectionState, connectionDetail
			connectionState, connectionDetail = state, detail
			connectionMu.Unlock()
			if state != previous {
				log.Printf("association : %s", state)
			}
			refresh()
			if state == "pending" && detail != "" && (previous != "pending" || previousDetail != detail) {
				w.PostShow()
				t.Notify(webui.AppName, i18n.T("connect.pending", detail), false)
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
			if !resume {
				select {
				case openConnection <- struct{}{}:
				default:
				}
			}
			return
		}
		defer connectMu.Unlock()
		defer func() {
			select {
			case <-openConnection:
			default:
			}
		}()
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
			log.Printf("association : échec (%s)", err)
			if err.Error() == "vault" {
				connector.Status("vault", "")
			} else {
				connector.Status("unavailable", "")
			}
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
			t.Notify(webui.AppName, err.Error(), true)
		} else {
			t.Notify(webui.AppName, i18n.T("notify.sent"), false)
		}
	}
	choisirDossier := func() {
		choisi, ok := w.ChoisirDossier(i18n.T("dir.pick"), dossier())
		if !ok {
			return
		}
		d := watch.Racine(choisi)
		if watch.Examine(d) != watch.DossierPret &&
			!w.Confirm(i18n.T("dir.confirm", d, i18n.T("dir.confirmwhy")), i18n.T("dir.keep")) {
			return
		}
		if strings.EqualFold(d, dossier()) {
			refresh()
			return
		}
		sauve(func(c *config.Config) { c.WowDir = d })
		log.Printf("dossier du jeu choisi : %s", d)
		t.Notify(webui.AppName, i18n.T("notify.dir", d), false)
		refresh()
		select {
		case reveil <- struct{}{}:
		default:
		}
	}
	demarrage := func(on bool) {
		if err := webui.StartWithWindows(on); err != nil {
			w.SetChecked(!on)
			w.Info(i18n.T("autostart.fail", err))
			return
		}
		sauve(func(c *config.Config) { c.StartWithWindows = on })
		log.Printf("lancer au démarrage : %t", on)
	}
	langue := func(l i18n.Lang) {
		i18n.Set(l)
		sauve(func(c *config.Config) { c.Language = string(l) })
		log.Printf("langue : %s", l)
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
	purger := func() {
		attente := 2 * time.Minute
		for {
			select {
			case <-ctx.Done():
				return
			case <-time.After(attente):
			}
			attente = 24 * time.Hour
			if _, err := a.PurgeLots(ctx); err != nil && ctx.Err() == nil {
				log.Printf("purge : %v", err)
			}
		}
	}
	desinstaller := func() {
		if !w.Confirm(i18n.T("uninstall.confirm", dir), i18n.T("dlg.uninstall")) {
			return
		}
		cancel()
		connectMu.Lock()
		_ = vault.DeleteConnection()
		errs := desinstalle(dir, st, log)
		connectMu.Unlock()
		msg := i18n.T("uninstall.done")
		if len(errs) > 0 {
			msg += "\n\n" + i18n.T("uninstall.check") + "\n- " + strings.Join(errs, "\n- ")
		}
		w.Info(msg + "\n\n" + i18n.T("uninstall.revoke", companionURL))
		supprimeExe()
		quitter()
	}

	t.OnOpen = w.PostShow
	t.OnBalloonClick = w.PostShow
	t.Menu = func() []webui.Item {
		etatMu.Lock()
		msg := etat
		etatMu.Unlock()
		items := []webui.Item{{Key: "status", Label: msg}}
		connectionMu.Lock()
		if connectionState == "pending" && connectionDetail != "" {
			items = append(items, webui.Item{Key: "connection", Label: i18n.T("connect.pending", connectionDetail)})
		}
		connectionMu.Unlock()
		items = append(items, webui.Item{Key: "sep-status", Separator: true},
			webui.Item{Key: "open", Label: i18n.T("menu.open"), Action: w.PostShow, Default: true},
			webui.Item{Key: "send", Label: i18n.T("btn.send"), Action: envoyer},
			webui.Item{Key: "connect", Label: i18n.T("btn.connect"), Action: func() { connecter(false) }},
			webui.Item{Key: "cancel", Label: i18n.T("connect.cancel"), Action: annulerConnexion},
			webui.Item{Key: "installations", Label: i18n.T("btn.installations"), Action: func() { webui.Open(companionURL) }},
			webui.Item{Key: "log", Label: i18n.T("btn.log"), Action: func() { webui.Open(log.Path()) }},
			webui.Item{Key: "dir", Label: i18n.T("menu.dir"), Action: func() { w.PostShow(); choisirDossier() }},
			webui.Item{Key: "autostart", Label: i18n.T("win.autostart"), Toggle: true, Checked: webui.StartsWithWindows(), Action: func() {
				on := !webui.StartsWithWindows()
				demarrage(on)
				w.SetChecked(on)
			}},
			webui.Item{Key: "sep-actions", Separator: true},
			webui.Item{Key: "quit", Label: i18n.T("btn.quit"), Action: quitter})
		return items
	}

	w.OnToggle = demarrage
	w.OnLangue = langue
	w.OnClose = func() {
		if t.Active() {
			w.Hide()
			return
		}
		quitter()
	}
	w.OnCommand = func(id int) {
		switch id {
		case vue.Envoyer:
			envoyer()
		case vue.Installations:
			webui.Open(companionURL)
		case vue.Journal:
			webui.Open(log.Path())
		case vue.Page:
			go connecter(false)
		case vue.Dossier:
			choisirDossier()
		case vue.Effacer:
			effacer()
		case vue.Desinstaller:
			desinstaller()
		case vue.Quitter:
			quitter()
		}
	}

	onShow, onQuit := w.PostShow, quitter
	remplacerInstance(dir, onShow, onQuit)

	if err := w.Create(); err != nil {
		return err
	}
	if err := t.Start(); err != nil {
		log.Printf("icône près de la barre : %v", err)
	}
	defer t.Quit()
	err = w.Run(hidden, func() {
		refresh()
		go connecter(true)
		go purger()
		if _, err := a.Token(); err != nil {
			t.Notify(webui.AppName, i18n.T("notify.notoken", webui.AppName), false)
		}
		cfgMu.Lock()
		c := cfg
		cfgMu.Unlock()
		engine(ctx, a, c, log, reveil, nil, dossier)
	})
	cancel()
	return err
}

// verrouInstance garde un seul compagnon par dossier de données. Le second
// lancement envoie « show » au premier et sort.
func verrouInstance(dir string) (unique bool, arreter func(), err error) {
	lock, err := os.OpenFile(filepath.Join(dir, "companion.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, func() {}, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			for i := 0; i < 20; i++ {
				if envoyerCommande(dir, "show") == nil {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			return false, func() {}, nil
		}
		return false, func() {}, err
	}

	sock := filepath.Join(dir, "companion.sock")
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		lock.Close()
		return false, func() {}, err
	}
	_ = os.Chmod(sock, 0o600)

	var mu sync.Mutex
	var surShow, surQuit func()
	prets := make(chan struct{})
	var once sync.Once
	go func() {
		<-prets
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				ligne, err := bufio.NewReader(c).ReadString('\n')
				if err != nil {
					return
				}
				mu.Lock()
				show, quit := surShow, surQuit
				mu.Unlock()
				switch strings.TrimSpace(ligne) {
				case "quit":
					if quit != nil {
						quit()
					}
				default:
					if show != nil {
						show()
					}
				}
				_, _ = c.Write([]byte("ok\n"))
			}(c)
		}
	}()

	remplacer := func(show, quit func()) {
		mu.Lock()
		surShow, surQuit = show, quit
		mu.Unlock()
		once.Do(func() { close(prets) })
	}
	instanceMu.Lock()
	instanceDir = dir
	instanceSet = remplacer
	instanceMu.Unlock()

	return true, func() {
		ln.Close()
		_ = os.Remove(sock)
		lock.Close()
	}, nil
}

var (
	instanceMu  sync.Mutex
	instanceDir string
	instanceSet func(show, quit func())
)

func remplacerInstance(dir string, show, quit func()) {
	instanceMu.Lock()
	defer instanceMu.Unlock()
	if instanceDir == dir && instanceSet != nil {
		instanceSet(show, quit)
	}
}

func envoyerCommande(dir, commande string) error {
	c, err := net.DialTimeout("unix", filepath.Join(dir, "companion.sock"), 2*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := fmt.Fprintln(c, commande); err != nil {
		return err
	}
	reponse, err := bufio.NewReader(c).ReadString('\n')
	if err != nil {
		return err
	}
	if strings.TrimSpace(reponse) != "ok" {
		return fmt.Errorf("réponse inattendue : %s", strings.TrimSpace(reponse))
	}
	return nil
}

func quitRunning(dataDir string) error {
	dir, err := dirDonnees(dataDir)
	if err != nil {
		return err
	}
	if err := envoyerCommande(dir, "quit"); err != nil {
		return fmt.Errorf("le compagnon ne tourne pas")
	}
	return nil
}

func menuRunning(dataDir string) error {
	dir, err := dirDonnees(dataDir)
	if err != nil {
		return err
	}
	if err := envoyerCommande(dir, "show"); err != nil {
		return fmt.Errorf("le compagnon ne tourne pas")
	}
	return nil
}

func dirDonnees(dataDir string) (string, error) {
	if dataDir != "" {
		return filepath.Abs(dataDir)
	}
	return config.DataDir()
}

// desinstalle retire les réglages et les données, jamais le dossier du jeu.
func desinstalle(dir string, st *store.Store, log *logx.Logger) []string {
	var errs []string
	if err := webui.StartWithWindows(false); err != nil {
		errs = append(errs, i18n.T("uninstall.autostart", err))
	}
	if err := secret.Delete(); err != nil {
		errs = append(errs, i18n.T("uninstall.cred", err))
	}
	log.Printf("désinstallation demandée")
	st.Close()
	log.Close()
	_ = debug.SetCrashOutput(nil, debug.CrashOptions{})
	if err := os.RemoveAll(dir); err != nil {
		errs = append(errs, i18n.T("uninstall.dir", dir, err))
	}
	if base, err := config.BaseDir(); err == nil {
		_ = os.RemoveAll(filepath.Join(base, "wowsync"))
		_ = os.Remove(base)
	}
	return errs
}

func supprimeExe() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	_ = os.Remove(exe) // Linux permet d'effacer un programme en cours d'exécution
}
