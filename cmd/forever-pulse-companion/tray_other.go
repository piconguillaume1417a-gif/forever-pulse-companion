//go:build !windows && !(linux && cgo)

package main

import (
	"context"
	"errors"
	"os"
	"os/signal"

	"wowsync/internal/app"
	"wowsync/internal/sender"
)

// Hors Windows : pas d'icône, la boucle tourne au premier plan (tests).
func runTray(dataDir string, _ bool, _ int) error {
	cfg, log, st, _, err := ouvre(dataDir)
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	engine(ctx, a, cfg, log, nil, nil, nil)
	return nil
}

func quitRunning(string) error { return errors.New("--quit : Windows seulement") }

func menuRunning(string) error { return errors.New("--menu : Windows seulement") }
