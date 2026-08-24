package mesh

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// NetworkInvite contains only public connection information. It is safe to
// paste into another BACKFLASH setup; it never contains identity.key.
type NetworkInvite struct {
	Version   int       `json:"version"`
	Network   string    `json:"network"`
	PeerKey   string    `json:"peer_key"`
	Endpoints []string  `json:"endpoints"`
	CreatedAt time.Time `json:"created_at"`
}

func EncodeInvite(publicKey ed25519.PublicKey, endpoints []string) (string, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return "", errors.New("publik mesh-nyckel saknas")
	}
	clean := make([]string, 0, len(endpoints))
	for _, endpoint := range endpoints {
		if strings.TrimSpace(endpoint) != "" {
			clean = append(clean, strings.TrimSpace(endpoint))
		}
	}
	if len(clean) == 0 {
		return "", errors.New("ingen annonserad peer-adress finns")
	}
	payload, err := json.Marshal(NetworkInvite{Version: 1, Network: "backflash-public-cache", PeerKey: hexKey(publicKey), Endpoints: clean, CreatedAt: time.Now().UTC()})
	if err != nil {
		return "", err
	}
	return "BFM1." + base64.RawURLEncoding.EncodeToString(payload), nil
}

func hexKey(key ed25519.PublicKey) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(key)*2)
	for i, value := range key {
		out[i*2], out[i*2+1] = hex[value>>4], hex[value&15]
	}
	return string(out)
}
