package mesh

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
)

// Config controls the public cache mesh. It is deliberately disabled unless
// the user opts in; enabling it never unlocks or reuses Gandr state.
type Config struct {
	Enabled      bool
	ShareCache   bool
	IdentityPath string
	ObjectPath   string
	Listen       []string
	Peers        []string
	PeerKey      ed25519.PublicKey
}

func DefaultConfig() Config { return Config{} }

// Load reads the temporary opt-in switch used until the shared TOML config is
// introduced. Only the explicit value "1" enables public mesh traffic.
func Load() Config {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, _ := os.UserHomeDir()
		dataHome = filepath.Join(home, ".local", "share")
	}
	base := filepath.Join(dataHome, "backflash", "mesh")
	return Config{
		Enabled:      os.Getenv("BACKFLASH_MESH_ENABLED") == "1",
		ShareCache:   os.Getenv("BACKFLASH_MESH_SHARE_CACHE") == "1",
		IdentityPath: filepath.Join(base, "identity.key"),
		ObjectPath:   filepath.Join(base, "objects"),
		Listen:       splitEnv("BACKFLASH_MESH_LISTEN"),
		Peers:        splitEnv("BACKFLASH_MESH_PEERS"),
		PeerKey:      parseKey(os.Getenv("BACKFLASH_MESH_PEER_KEY")),
	}
}

func splitEnv(name string) []string {
	var out []string
	for _, value := range strings.Split(os.Getenv(name), ",") {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func parseKey(value string) ed25519.PublicKey {
	b, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil || len(b) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(b)
}
