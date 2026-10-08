// Forever Pulse Companion — envoie au site les relevés de l'addon Forever Pulse.
//
//	ForeverPulseCompanion.exe                  fenêtre + icône près de l'horloge (usage normal)
//	ForeverPulseCompanion.exe --tray           icône seule (lancement au démarrage de Windows)
//	ForeverPulseCompanion.exe --quit           ferme le compagnon déjà lancé
//	ForeverPulseCompanion.exe --once <fichier> décode, valide, résume ; n'envoie rien, n'écrit rien
//	ForeverPulseCompanion.exe --dry-run [fichiers] [--out <dossier>]
//	                                           construit les requêtes sans les poster
//	ForeverPulseCompanion.exe --sync [fichiers] traite et envoie une fois, puis s'arrête
//	ForeverPulseCompanion.exe --dump <fichiers> JSON des lots décodés et du cumul (test différentiel)
//	ForeverPulseCompanion.exe --store-token    lit un jeton sur l'entrée standard et l'enregistre
//	ForeverPulseCompanion.exe --status         état de la file locale (comptes seulement), sans réseau
//	ForeverPulseCompanion.exe --version        affiche la version (utilisé par outils/publier)
//	ForeverPulseCompanion.exe --update-check   lit et vérifie le manifeste publié ; ne télécharge rien
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"time"

	"wowsync/internal/app"
	"wowsync/internal/config"
	"wowsync/internal/connect"
	"wowsync/internal/cumul"
	"wowsync/internal/logx"
	"wowsync/internal/schema"
	"wowsync/internal/secret"
	"wowsync/internal/sender"
	"wowsync/internal/stats"
	"wowsync/internal/store"
	"wowsync/internal/update"
	"wowsync/internal/watch"
)

func main() {
	once := flag.String("once", "", "décode, valide et résume un fichier, sans rien envoyer")
	dry := flag.Bool("dry-run", false, "construit les requêtes sans les poster")
	out := flag.String("out", "", "avec --dry-run : dossier où écrire les corps JSON")
	sync := flag.Bool("sync", false, "traite et envoie une fois, sans icône")
	dump := flag.Bool("dump", false, "JSON des lots décodés et du cumul")
	storeToken := flag.Bool("store-token", false, "enregistre le jeton lu sur l'entrée standard")
	dataDir := flag.String("data-dir", "", "dossier des données (défaut : %APPDATA%\\ForeverPulse\\Companion)")
	tray := flag.Bool("tray", false, "démarre icône seule, fenêtre cachée (lancement au démarrage de Windows)")
	quit := flag.Bool("quit", false, "ferme proprement le compagnon déjà lancé")
	menu := flag.Bool("menu", false, "ouvre le menu de l'icône du compagnon lancé (diagnostic)")
	status := flag.Bool("status", false, "affiche l'état de la file locale, sans réseau")
	connectFlag := flag.Bool("connect", false, "associe ce Companion dans le navigateur (autorisation dans le coffre Windows)")
	versionFlag := flag.Bool("version", false, "affiche la version")
	updateCheck := flag.Bool("update-check", false, "vérifie la dernière version publiée, sans télécharger")
	apresMaj := flag.Int("after-update", 0, "interne : attend la fin de l'ancienne instance (pid) après une mise à jour")
	flag.Parse()

	var err error
	switch {
	case *versionFlag:
		fmt.Println(app.Version)
	case *updateCheck:
		err = runUpdateCheck()
	case *connectFlag:
		err = runConnect(*dataDir)
	case *quit:
		err = quitRunning()
	case *menu:
		err = menuRunning()
	case *status:
		err = runStatus(*dataDir)
	case *once != "":
		err = runOnce(*once)
	case *dump:
		err = runDump(flag.Args())
	case *storeToken:
		err = runStoreToken(*dataDir, os.Stdin)
	case *dry:
		err = runDry(*dataDir, flag.Args(), *out)
	case *sync:
		err = runSync(*dataDir, flag.Args())
	default:
		err = runTray(*dataDir, *tray, *apresMaj)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Forever Pulse Companion :", err)
		os.Exit(1)
	}
}

// ouvre prépare dossier, configuration, journal et base.
func ouvre(dataDir string) (config.Config, *logx.Logger, *store.Store, string, error) {
	if dataDir == "" {
		d, err := config.DataDir()
		if err != nil {
			return config.Config{}, nil, nil, "", err
		}
		dataDir = d
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return config.Config{}, nil, nil, "", err
	}
	cfg, err := config.Load(filepath.Join(dataDir, config.FichierConfig))
	if err != nil {
		return cfg, nil, nil, "", err
	}
	log, err := logx.Open(filepath.Join(dataDir, config.FichierJournal))
	if err != nil {
		return cfg, nil, nil, "", err
	}
	st, err := store.Open(filepath.Join(dataDir, config.FichierBase))
	if err != nil {
		log.Close()
		return cfg, nil, nil, "", err
	}
	return cfg, log, st, dataDir, nil
}

func runOnce(path string) error {
	if app.AuctionPath(path) {
		_, err := auctionSummary(path)
		fmt.Println("rien envoyé")
		return err
	}
	data, sha, err := watch.Read(path)
	if err != nil {
		return err
	}
	start := time.Now()
	f, db, sf, err := app.DecodeTout(data, time.Now())
	if err != nil {
		return err
	}
	fmt.Printf("%s\n  %d octets, SHA-256 %s, décodé en %s\n", path, len(data), sha, time.Since(start).Round(time.Millisecond))
	fmt.Printf("  schéma %d, addon %s, session d'observation %s\n", f.Schema, f.AddonVersion, f.ObserverID)
	for _, l := range f.Infos {
		fmt.Println("  " + l)
	}
	for _, a := range f.Avertis {
		fmt.Println("  ⚠  " + a)
	}
	for _, e := range f.Fatales {
		fmt.Println("  ✗  " + e)
	}
	ok, chars, zc := 0, 0, 0
	for _, l := range f.Lots {
		etat := "ok"
		if !l.Valide() {
			etat = "ÉCARTÉ"
		} else {
			ok++
			chars += l.Personnages
			if l.Payload.Get("zone_count") != nil {
				zc++
			}
		}
		src := l.SourceScope
		if src == "" && l.Method == "channel_roster" {
			src = "zone"
		}
		fmt.Printf("  lot %2d %-14s %-13s %-5s %5d personnages  %s\n", l.Index, l.Method, src, l.Kind, l.Personnages, etat)
		for _, a := range l.Avertis {
			fmt.Println("         ⚠  " + a)
		}
		for _, e := range l.Erreurs {
			fmt.Println("         ✗  " + e)
		}
	}
	fmt.Printf("  %d lots, %d envoyables (%d avec zone_count), %d lignes de personnages\n", len(f.Lots), ok, zc, chars)
	c := cumul.Nouveau()
	c.Integrer(app.CumulLots(db))
	res := c.Resume(time.Now().Unix())
	ids := make([]string, 0, len(res))
	for k := range res {
		ids = append(ids, k)
	}
	sort.Strings(ids)
	for _, sid := range ids {
		r := res[sid]
		fmt.Printf("  [%s] personnages distincts observés dans ce fichier : %d (avec GUID %d, vus seulement par /who %d)\n",
			sid, r.Total, r.AvecGUID, r.SeulementWho)
	}
	if sf != nil && sf.Present {
		for _, l := range sf.Infos {
			fmt.Println("  " + l)
		}
		for _, l := range sf.Ecartees {
			fmt.Println("  ⚠  statistiques : " + l)
		}
		for _, l := range sf.Fatales {
			fmt.Println("  ✗  statistiques : " + l)
		}
	} else {
		fmt.Println("  statistiques : variable ForeverPulseStatsDB absente du fichier")
	}
	fmt.Println("  rien n'a été envoyé")
	if len(f.Fatales) > 0 {
		return fmt.Errorf("fichier refusé")
	}
	return nil
}

// runStatus : état de la file locale, en comptes seulement, sans réseau.
func runStatus(dataDir string) error {
	cfg, log, st, dir, err := ouvre(dataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	defer log.Close()
	a, err := app.New(st, log, nil)
	if err != nil {
		return err
	}
	vault := secret.ForContext(dir, cfg.SiteURL, dataDir != "" || os.Getenv("WOWSYNC_DATA_DIR") != "")
	a.Token, a.SaveToken = vault.Get, vault.Set
	e := a.Etat()
	fmt.Printf("état : %s\n", e.Message)
	fmt.Printf("lots : %d envoyés, %d en attente, %d refusés\n", e.Sent, e.Pending, e.Rejected)
	fmt.Printf("statistiques : %d fiches envoyées, %d en attente, %d refusées\n", e.StatsSent, e.StatsPending, e.StatsRejected)
	for _, l := range e.Details {
		fmt.Println(l)
	}
	for _, l := range e.Cumul {
		fmt.Println(l)
	}
	return nil
}

func runDump(paths []string) error {
	c := cumul.Nouveau()
	var fichiers []any
	for _, p := range paths {
		data, sha, err := watch.Read(p)
		if err != nil {
			return err
		}
		f, db, err := app.Decode(data, time.Now())
		if err != nil {
			return err
		}
		var lots []any
		for _, l := range f.Lots {
			lots = append(lots, map[string]any{"batch_id": l.BatchID, "valide": l.Valide(), "erreurs": l.Erreurs, "payload": l.Payload})
		}
		st := c.Integrer(app.CumulLots(db))
		fichiers = append(fichiers, map[string]any{"path": p, "sha256": sha, "fatales": f.Fatales, "lots": lots, "stats": st})
	}
	b, err := schema.Marshal(map[string]any{"fichiers": fichiers, "cumul": c.Dump()})
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(append(b, '\n'))
	return err
}

func runStoreToken(dataDir string, r io.Reader) error {
	line, _ := bufio.NewReader(r).ReadString('\n')
	t, ok := secret.Normalise(line)
	if !ok {
		return fmt.Errorf("jeton au mauvais format (attendu fpc_ suivi de 43 caractères)")
	}
	// Le compagnon lancé relit ce blocage à chaque tour : il reprend seul.
	cfg, log, st, dir, err := ouvre(dataDir)
	if err != nil {
		return err
	}
	defer log.Close()
	defer st.Close()
	vault := secret.ForContext(dir, cfg.SiteURL, dataDir != "" || os.Getenv("WOWSYNC_DATA_DIR") != "")
	if err := vault.Set(t); err != nil {
		return err
	}
	_ = vault.DeleteConnection()
	if app.LeverBlocageJeton(st) {
		log.Printf("nouveau jeton enregistré (--store-token) : blocage « jeton » levé")
		fmt.Println("blocage « jeton » levé : l'envoi automatique reprend")
	}
	fmt.Println("jeton enregistré dans le Gestionnaire d'identifiants Windows")
	return nil
}

func fichiers(cfg config.Config, args []string) []string {
	if len(args) > 0 {
		return args
	}
	return watch.DiscoverAll(cfg.WowDir)
}

// runDry : base temporaire, rien n'est posté ; les corps sont construits comme pour un envoi.
func runDry(dataDir string, args []string, out string) error {
	tmp, err := os.MkdirTemp("", "companion-dry-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	cfg := config.Defaults()
	if dataDir != "" {
		if c, err := config.Load(filepath.Join(dataDir, "config.toml")); err == nil {
			cfg = c
		}
	}
	// Même calcul que l'envoi réel : site_url + /api/ingest/census, jamais
	// Supabase, jamais un chemin en double.
	cible, err := sender.Cible(cfg.SiteURL)
	if err != nil {
		return err
	}
	fmt.Printf("destination : POST %s (aucune requête ne sera postée)\n", cible)
	st, err := store.Open(filepath.Join(tmp, "dry.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	log := &logx.Logger{Echo: true}
	a, err := app.New(st, log, nil)
	if err != nil {
		return err
	}
	for _, p := range fichiers(cfg, args) {
		if app.AuctionPath(p) {
			r, err := auctionSummary(p)
			if err != nil {
				return err
			}
			if r.Counts["format_non_pris_en_charge"] > 0 {
				return fmt.Errorf("format Auctionator non pris en charge")
			}
		}
		if _, err := a.ProcessFile(context.Background(), p); err != nil {
			fmt.Fprintf(os.Stderr, "%s : %v\n", filepath.Base(p), err)
		}
	}
	n := 0
	for {
		f, batches, err := st.Pending(sender.MaxCensusBody - 64<<10)
		if err != nil || f == nil {
			break
		}
		n++
		body, err := app.Body(f, batches)
		if err != nil {
			return err
		}
		ids := make([]string, len(batches))
		for i, b := range batches {
			ids[i] = b.BatchID
		}
		fmt.Printf("requête %d : POST %s — %d lots, %d octets de JSON (fichier %s)\n",
			n, cible, len(batches), len(body), f.SHA256[:12])
		if out != "" {
			_ = os.MkdirAll(out, 0o700)
			if err := os.WriteFile(filepath.Join(out, fmt.Sprintf("requete-%02d.json", n)), body, 0o600); err != nil {
				return err
			}
		}
		_ = st.MarkSent(ids) // base temporaire : seulement pour passer au paquet suivant
	}
	fmt.Printf("%d requêtes construites, aucune postée\n", n)
	cibleStats, err := sender.CibleRoute(cfg.SiteURL, sender.RouteStats)
	if err != nil {
		return err
	}
	ns := 0
	for {
		meta, sheets, err := st.StatsPending(sender.MaxBody - 64<<10)
		if err != nil || len(sheets) == 0 {
			break
		}
		ns++
		body, err := app.StatsBody(meta, sheets, stats.SchemaEnvoi)
		if err != nil {
			return err
		}
		fmt.Printf("requête de statistiques %d : POST %s (schéma %d) — %d fiches, %d octets de JSON\n", ns, cibleStats, stats.SchemaEnvoi, len(sheets), len(body))
		if out != "" {
			_ = os.MkdirAll(out, 0o700)
			if err := os.WriteFile(filepath.Join(out, fmt.Sprintf("statistiques-%02d.json", ns)), body, 0o600); err != nil {
				return err
			}
		}
		_ = st.MarkStats(sheets, "sent", "") // base temporaire
	}
	fmt.Printf("%d requêtes de statistiques construites, aucune postée\n", ns)
	if err := dryAuctions(st, out); err != nil {
		return err
	}
	return nil
}

func runSync(dataDir string, args []string) error {
	cfg, log, st, dir, err := ouvre(dataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	defer log.Close()
	log.Echo = true
	a, err := app.New(st, log, sender.New(cfg.SiteURL, app.Version))
	if err != nil {
		return err
	}
	ctx := context.Background()
	vault := secret.ForContext(dir, cfg.SiteURL, dataDir != "" || os.Getenv("WOWSYNC_DATA_DIR") != "")
	a.Token, a.SaveToken = vault.Get, vault.Set
	for _, p := range fichiers(cfg, args) {
		if _, err := a.ProcessFile(ctx, p); err != nil {
			log.Printf("%s : %v", filepath.Base(p), err)
		}
	}
	ferr := a.Flush(ctx, true)
	e := a.Etat()
	fmt.Printf("état : %s — %d en attente, %d envoyés, %d refusés\n", e.Message, e.Pending, e.Sent, e.Rejected)
	fmt.Printf("statistiques : %d fiches en attente, %d envoyées, %d refusées\n", e.StatsPending, e.StatsSent, e.StatsRejected)
	if e.DernierEnvoi != "" {
		fmt.Println(e.DernierEnvoi)
	}
	for _, l := range e.Cumul {
		fmt.Println(l)
	}
	if ferr != nil {
		return ferr
	}
	return nil
}

func runUpdateCheck() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	o, err := update.Nouveau(app.Version).Cherche(ctx)
	if err != nil {
		return err
	}
	m := o.Manifeste
	fmt.Printf("version en cours : %s\nversion publiée : %s (%s, %d octets, SHA-256 %s)\n", app.Version, m.Version, m.Publiee, m.Taille, m.SHA256)
	fmt.Printf("signature valide ; plus récente : %t ; imposée : %t ; rien n'a été téléchargé\n", o.Nouvelle, o.Imposee)
	return nil
}

func runConnect(dataDir string) error {
	cfg, log, st, dir, err := ouvre(dataDir)
	if err != nil {
		return err
	}
	defer st.Close()
	defer log.Close()
	vault := secret.ForContext(dir, cfg.SiteURL, dataDir != "" || os.Getenv("WOWSYNC_DATA_DIR") != "")
	c := connect.Client{Site: cfg.SiteURL, Vault: vault, SaveToken: func(t string) error {
		if err := vault.Set(t); err != nil {
			return err
		}
		app.LeverBlocageJeton(st)
		return nil
	},
		Open: func(uri string) { fmt.Println(uri) }, Status: func(state, detail string) { fmt.Println(state, detail) }}
	return c.Start(context.Background())
}

// engine : la boucle de fond commune (icône ou non).
//
// 0.6.0 : elle se réveille toutes les cfg.PollSeconds secondes (10 minutes par
// défaut) ; un fichier qui vient de changer est revérifié 3 s plus tard, jusqu'à ce
// qu'il soit stable, puis traité. reveil (« Envoyer maintenant ») la réveille tout
// de suite pour ne pas attendre le sondage suivant.
//
// 0.10.0 : pret (facultatif) est appelé une fois la première passe terminée ; il
// confirme une mise à jour à l'essai.
func engine(ctx context.Context, a *app.App, cfg config.Config, log *logx.Logger, reveil <-chan struct{}, pret func()) {
	w := watch.New()
	// Au démarrage : les deux fichiers, .lua et .bak (la session précédente peut
	// n'exister plus que dans le .bak).
	// 0.7.2 : l'état est noté AVANT la lecture. Noté après, un fichier réécrit par le
	// jeu pendant la lecture de démarrage passait pour déjà lu jusqu'à l'écriture
	// suivante ; noté avant, il est relu (une empreinte déjà lue est ignorée).
	initiaux := watch.Discover(cfg.WowDir)
	w.Mark(initiaux)
	for _, p := range initiaux {
		if _, err := a.ProcessFile(ctx, p); err != nil {
			log.Printf("%s : %v", filepath.Base(p), err)
			w.Retry(p) // illisible au démarrage : nouvel essai au sondage suivant
		}
	}
	_ = a.Flush(ctx, false)
	if pret != nil && ctx.Err() == nil {
		pret()
	}
	// 0.6.0 : la lecture du cumul et des fichiers au démarrage laisse derrière elle
	// des dizaines de Mo inutilisés ; ils sont rendus à Windows tout de suite.
	debug.FreeOSMemory()
	sondage := time.Duration(cfg.PollSeconds) * time.Second
	w.Ready(watch.DiscoverAuctions(cfg.WowDir), time.Now())
	premierDelai := sondage
	if w.EnAttente() {
		premierDelai = w.Stable
	}
	minuterie := time.NewTimer(premierDelai)
	defer minuterie.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-reveil:
			if !minuterie.Stop() {
				select {
				case <-minuterie.C:
				default:
				}
			}
		case <-minuterie.C:
		}
		for _, p := range w.Ready(watch.DiscoverAll(cfg.WowDir), time.Now()) {
			if app.AuctionPath(p) && !w.IsStable(filepath.Join(filepath.Dir(p), "ForeverPulse.lua"), time.Now()) {
				w.Retry(p)
				continue
			}
			if _, err := a.ProcessFile(ctx, p); err != nil {
				log.Printf("%s : %v", filepath.Base(p), err)
				w.Retry(p) // nouvel essai au sondage suivant
			}
		}
		for _, p := range watch.DiscoverAuctions(cfg.WowDir) {
			if w.IsStable(p, time.Now()) && w.IsStable(filepath.Join(filepath.Dir(p), "ForeverPulse.lua"), time.Now()) {
				if _, err := a.ProcessAuctions(ctx, p); err != nil {
					w.Retry(p)
				}
			}
		}
		if err := a.Flush(ctx, false); err != nil {
			log.Printf("envoi : %v", err)
		}
		if w.EnAttente() {
			minuterie.Reset(w.Stable) // confirmer la stabilité du fichier
		} else {
			minuterie.Reset(sondage)
		}
	}
}
