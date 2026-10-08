package update

import (
	"crypto/ed25519"
	"encoding/base64"
)

// clePublique : clé Ed25519 du propriétaire pour les manifestes de mise à jour.
// La clé privée correspondante est conservée hors du dépôt (outils/publier), créée
// le 08/10/2026. La perdre impose une réinstallation manuelle pour changer de clé.
const clePublique = "Uvf0WHYYkaAZIvBoUM5VtWKNq5/RUeGtmKwngcA4fiU="

// ClePublique : nil si la constante est illisible (toute signature est alors refusée).
func ClePublique() ed25519.PublicKey {
	b, err := base64.StdEncoding.DecodeString(clePublique)
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(b)
}
