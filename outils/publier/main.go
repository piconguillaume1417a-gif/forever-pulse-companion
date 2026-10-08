// publier : clé de signature et manifeste des mises à jour du compagnon.
//
//	go run ./outils/publier cle -cle <fichier>
//	    crée la paire Ed25519 (refuse d'écraser) ; affiche la clé publique à
//	    recopier dans internal/update/cle.go. La clé privée ne va JAMAIS dans le dépôt.
//
//	go run ./outils/publier signer -cle <fichier> -exe <ForeverPulseCompanion.exe> -out <dossier> [-min <version>]
//	    lit la version du programme (--version), écrit <dossier>/latest.json et
//	    latest.json.sig, puis les revérifie avec la clé publique intégrée.
//
// Les trois fichiers (exécutable renommé ForeverPulseCompanion.exe, latest.json,
// latest.json.sig) sont joints à la release GitHub v<version> de update.Depot,
// publiée comme « latest » (pas en pré-version : /releases/latest l'ignorerait).
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"wowsync/internal/update"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage : publier cle|signer …")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "cle":
		err = cle(os.Args[2:])
	case "signer":
		err = signer(os.Args[2:])
	default:
		err = fmt.Errorf("commande inconnue : %s", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "publier :", err)
		os.Exit(1)
	}
}

func cle(args []string) error {
	fs := flag.NewFlagSet("cle", flag.ExitOnError)
	chemin := fs.String("cle", "", "fichier de la clé privée (hors du dépôt)")
	_ = fs.Parse(args)
	if *chemin == "" {
		return errors.New("-cle obligatoire")
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*chemin), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(*chemin, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("clé non créée (existe déjà ?) : %v", err)
	}
	defer f.Close()
	if _, err := fmt.Fprintln(f, base64.StdEncoding.EncodeToString(priv.Seed())); err != nil {
		return err
	}
	fmt.Println("clé privée écrite :", *chemin)
	fmt.Println("clé publique (internal/update/cle.go) :", base64.StdEncoding.EncodeToString(pub))
	return nil
}

func lireCle(chemin string) (ed25519.PrivateKey, error) {
	b, err := os.ReadFile(chemin)
	if err != nil {
		return nil, err
	}
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("clé privée illisible")
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func signer(args []string) error {
	fs := flag.NewFlagSet("signer", flag.ExitOnError)
	chemin := fs.String("cle", "", "fichier de la clé privée")
	exe := fs.String("exe", "", "exécutable construit")
	out := fs.String("out", "", "dossier de sortie")
	min := fs.String("min", "", "version minimale imposée (facultatif)")
	_ = fs.Parse(args)
	if *chemin == "" || *exe == "" || *out == "" {
		return errors.New("-cle, -exe et -out obligatoires")
	}
	priv, err := lireCle(*chemin)
	if err != nil {
		return err
	}
	if !priv.Public().(ed25519.PublicKey).Equal(update.ClePublique()) {
		return errors.New("cette clé privée ne correspond pas à la clé publique intégrée (cle.go)")
	}
	sortie, err := exec.Command(*exe, "--version").Output()
	if err != nil {
		return fmt.Errorf("%s --version : %v", *exe, err)
	}
	version := strings.TrimSpace(string(sortie))
	b, err := os.ReadFile(*exe)
	if err != nil {
		return err
	}
	h := sha256.Sum256(b)
	m := update.Manifeste{
		Format: update.FormatManifeste, Produit: update.Produit, Version: version,
		URL:    fmt.Sprintf("https://github.com/%s/releases/download/v%s/ForeverPulseCompanion.exe", update.Depot, version),
		SHA256: hex.EncodeToString(h[:]), Taille: int64(len(b)), MinVersion: *min,
		Publiee: time.Now().UTC().Format(time.RFC3339),
	}
	brut, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	brut = append(brut, '\n')
	sig := []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(priv, brut)) + "\n")
	c := update.Nouveau("0.0.0")
	if _, err := c.Verifie(brut, sig); err != nil {
		return fmt.Errorf("revérification : %v", err)
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "latest.json"), brut, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "latest.json.sig"), sig, 0o644); err != nil {
		return err
	}
	fmt.Printf("version %s, %d octets, SHA-256 %s\n", version, len(b), m.SHA256)
	fmt.Printf("à joindre à la release v%s : ForeverPulseCompanion.exe, latest.json, latest.json.sig\n", version)
	return nil
}
