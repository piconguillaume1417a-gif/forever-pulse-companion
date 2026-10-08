// Package update : mise à jour automatique du compagnon (0.10.0, 08/10/2026).
//
// Une fois par jour, le compagnon lit le manifeste de la dernière version publiée
// sur GitHub Releases :
//
//	https://github.com/<Depot>/releases/latest/download/latest.json
//	https://github.com/<Depot>/releases/latest/download/latest.json.sig
//
// latest.json.sig est la signature Ed25519 (base64) des octets exacts de
// latest.json, faite avec la clé privée du propriétaire (outils/publier). La clé
// publique est intégrée au programme (cle.go) : l'hébergeur ne peut ni modifier
// le manifeste ni changer l'exécutable qu'il désigne, puisque le manifeste porte
// son SHA-256 et sa taille. Une version n'est retenue que si elle est strictement
// plus récente que la version en cours : un vieux manifeste rejoué ne fait jamais
// revenir en arrière.
//
// L'exécutable vérifié attend dans <données>\update\ ; il remplace le programme au
// prochain démarrage, ou tout de suite sur « Redémarrer pour mettre à jour »
// (voir install.go). Aucun jeton, aucune donnée de jeu ne part dans ces requêtes.
package update

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Depot : dépôt GitHub public qui porte les versions publiées.
const Depot = "piconguillaume1417a-gif/forever-pulse-companion"

// Produit : nom signé dans le manifeste, pour qu'une signature faite pour un autre
// produit avec la même clé ne soit jamais acceptée ici.
const Produit = "forever-pulse-companion"

// FormatManifeste : version du format de latest.json.
const FormatManifeste = 1

// Bornes : un manifeste tient en quelques centaines d'octets, l'exécutable en 12 Mo.
const (
	maxManifeste  = 64 << 10
	maxSignature  = 1 << 10
	MaxExecutable = 64 << 20
)

// Hôtes autorisés. GitHub répond au téléchargement d'une pièce jointe par une
// redirection vers son stockage ; aucune autre destination n'est suivie.
var hotesAutorises = map[string]bool{
	"github.com":                            true,
	"objects.githubusercontent.com":         true,
	"release-assets.githubusercontent.com": true,
}

// Manifeste : contenu signé de latest.json.
type Manifeste struct {
	Format     int    `json:"format"`
	Produit    string `json:"product"`
	Version    string `json:"version"`
	URL        string `json:"url"`
	SHA256     string `json:"sha256"`
	Taille     int64  `json:"size"`
	MinVersion string `json:"min_version,omitempty"` // en dessous : mise à jour imposée
	Publiee    string `json:"published"`
}

// Client : vérification et téléchargement.
type Client struct {
	Version   string            // version en cours (app.Version)
	Cle       ed25519.PublicKey // clé publique de signature
	Base      string            // origine des manifestes ; vide : URL GitHub de Depot
	HTTP      *http.Client    // transport seulement (tests)
	Autorises map[string]bool // vide : hotesAutorises
}

// Nouveau : client de production.
func Nouveau(version string) *Client {
	return &Client{Version: version, Cle: ClePublique()}
}

func (c *Client) base() string {
	if c.Base != "" {
		return strings.TrimRight(c.Base, "/")
	}
	return "https://github.com/" + Depot + "/releases/latest/download"
}

func (c *Client) hotes() map[string]bool {
	if len(c.Autorises) > 0 {
		return c.Autorises
	}
	return hotesAutorises
}

// verifieURL : https, hôte autorisé, pas d'identifiants dans l'adresse.
func (c *Client) verifieURL(u *url.URL) error {
	if u.Scheme != "https" || u.User != nil || !c.hotes()[strings.ToLower(u.Hostname())] {
		return fmt.Errorf("adresse de mise à jour refusée : %s", u.Redacted())
	}
	return nil
}

// http : chaque redirection est revérifiée (https, hôte autorisé), cinq au plus.
// c.HTTP ne fournit que le transport (tests).
func (c *Client) http(timeout time.Duration) *http.Client {
	var tr http.RoundTripper
	if c.HTTP != nil {
		tr = c.HTTP.Transport
	}
	return &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("trop de redirections")
		}
		return c.verifieURL(r.URL)
	}}
}

// lit : GET borné ; toute réponse autre que 200 est une erreur.
func (c *Client) lit(ctx context.Context, adresse string, max int64, timeout time.Duration) ([]byte, error) {
	u, err := url.Parse(adresse)
	if err != nil {
		return nil, err
	}
	if err := c.verifieURL(u); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "ForeverPulseCompanion/"+c.Version)
	resp, err := c.http(timeout).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s : HTTP %d", u.Path, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > max {
		return nil, fmt.Errorf("%s : réponse trop grande", u.Path)
	}
	return b, nil
}

// Verifie contrôle la signature puis le contenu d'un manifeste.
func (c *Client) Verifie(brut, sig []byte) (Manifeste, error) {
	var m Manifeste
	s, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig)))
	if err != nil || len(s) != ed25519.SignatureSize {
		return m, errors.New("signature du manifeste illisible")
	}
	if len(c.Cle) != ed25519.PublicKeySize || !ed25519.Verify(c.Cle, brut, s) {
		return m, errors.New("signature du manifeste invalide")
	}
	dec := json.NewDecoder(bytes.NewReader(brut))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("manifeste illisible : %v", err)
	}
	if m.Format != FormatManifeste || m.Produit != Produit {
		return m, fmt.Errorf("manifeste d'un autre format ou produit (%d, %q)", m.Format, m.Produit)
	}
	if _, err := Plus(m.Version, "0.0.0"); err != nil {
		return m, fmt.Errorf("version du manifeste : %v", err)
	}
	if m.MinVersion != "" {
		if _, err := Plus(m.MinVersion, "0.0.0"); err != nil {
			return m, fmt.Errorf("version minimale du manifeste : %v", err)
		}
	}
	if h, err := hex.DecodeString(m.SHA256); err != nil || len(h) != sha256.Size {
		return m, errors.New("empreinte du manifeste illisible")
	}
	if m.Taille <= 0 || m.Taille > MaxExecutable {
		return m, fmt.Errorf("taille annoncée hors bornes : %d", m.Taille)
	}
	u, err := url.Parse(m.URL)
	if err != nil {
		return m, err
	}
	if err := c.verifieURL(u); err != nil {
		return m, err
	}
	return m, nil
}

// Offre : ce que la vérification a trouvé.
type Offre struct {
	Manifeste Manifeste
	Brut, Sig []byte // pour la revérification au moment d'installer
	Nouvelle  bool   // strictement plus récente que la version en cours
	Imposee   bool   // la version en cours est sous min_version
}

// Cherche lit et vérifie le manifeste publié. Rien n'est téléchargé.
func (c *Client) Cherche(ctx context.Context) (Offre, error) {
	var o Offre
	brut, err := c.lit(ctx, c.base()+"/latest.json", maxManifeste, 30*time.Second)
	if err != nil {
		return o, err
	}
	sig, err := c.lit(ctx, c.base()+"/latest.json.sig", maxSignature, 30*time.Second)
	if err != nil {
		return o, err
	}
	m, err := c.Verifie(brut, sig)
	if err != nil {
		return o, err
	}
	o.Manifeste, o.Brut, o.Sig = m, brut, sig
	if o.Nouvelle, err = Plus(m.Version, c.Version); err != nil {
		return o, fmt.Errorf("version en cours : %v", err)
	}
	if m.MinVersion != "" {
		sous, _ := Plus(m.MinVersion, c.Version)
		o.Imposee = sous && o.Nouvelle
	}
	return o, nil
}

// Noms dans <données>\update\.
const (
	DossierMaj      = "update"
	fichierExe      = "ForeverPulseCompanion.exe"
	fichierManif    = "latest.json"
	fichierSig      = "latest.json.sig"
	fichierEssai    = "essai.json"
	fichierRefusees = "refusees.txt"
)

// Telecharge récupère l'exécutable annoncé, vérifie taille et SHA-256, puis le
// range avec le manifeste signé dans dir. Un fichier qui ne correspond pas exactement
// n'est jamais gardé.
func (c *Client) Telecharge(ctx context.Context, o Offre, dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := c.lit(ctx, o.Manifeste.URL, o.Manifeste.Taille, 10*time.Minute)
	if err != nil {
		return err
	}
	if err := Conforme(b, o.Manifeste); err != nil {
		return err
	}
	tmp := filepath.Join(dir, fichierExe+".tmp")
	if err := ecrit(tmp, b); err != nil {
		return err
	}
	// Ordre : l'exécutable d'abord, le manifeste qui le désigne ensuite. Un arrêt
	// entre les deux laisse un manifeste ancien ou absent : Prete le refuse.
	_ = os.Remove(filepath.Join(dir, fichierManif))
	if err := os.Rename(tmp, filepath.Join(dir, fichierExe)); err != nil {
		return err
	}
	if err := ecrit(filepath.Join(dir, fichierSig), o.Sig); err != nil {
		return err
	}
	return ecrit(filepath.Join(dir, fichierManif), o.Brut)
}

// Conforme : taille et SHA-256 exacts.
func Conforme(b []byte, m Manifeste) error {
	if int64(len(b)) != m.Taille {
		return fmt.Errorf("exécutable de %d octets, %d annoncés", len(b), m.Taille)
	}
	h := sha256.Sum256(b)
	if !strings.EqualFold(hex.EncodeToString(h[:]), m.SHA256) {
		return errors.New("empreinte de l'exécutable différente du manifeste")
	}
	return nil
}

// Prete : la version téléchargée dans dir, revérifiée (signature, taille, empreinte),
// si elle est plus récente que la version en cours et n'a pas été refusée.
func (c *Client) Prete(dir string) (Manifeste, string, bool) {
	brut, err1 := os.ReadFile(filepath.Join(dir, fichierManif))
	sig, err2 := os.ReadFile(filepath.Join(dir, fichierSig))
	if err1 != nil || err2 != nil {
		return Manifeste{}, "", false
	}
	m, err := c.Verifie(brut, sig)
	if err != nil {
		return m, "", false
	}
	if nouv, err := Plus(m.Version, c.Version); err != nil || !nouv || Refusee(dir, m.Version) {
		return m, "", false
	}
	exe := filepath.Join(dir, fichierExe)
	b, err := os.ReadFile(exe)
	if err != nil || Conforme(b, m) != nil {
		return m, "", false
	}
	return m, exe, true
}

// Nettoie retire la version téléchargée (installée, refusée ou périmée).
func Nettoie(dir string) {
	for _, n := range []string{fichierManif, fichierSig, fichierExe, fichierExe + ".tmp"} {
		_ = os.Remove(filepath.Join(dir, n))
	}
}

// Refusee : version retirée après un échec au démarrage (voir install.go).
func Refusee(dir, version string) bool {
	b, err := os.ReadFile(filepath.Join(dir, fichierRefusees))
	if err != nil {
		return false
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == version {
			return true
		}
	}
	return false
}

func refuse(dir, version string) error {
	if Refusee(dir, version) {
		return nil
	}
	f, err := os.OpenFile(filepath.Join(dir, fichierRefusees), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, version)
	return err
}

// ecrit : écriture atomique (fichier temporaire puis renommage).
func ecrit(path string, b []byte) error {
	tmp := path + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
