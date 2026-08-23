package mesh

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// Config controls the public cache mesh. It is deliberately disabled unless
// the user opts in; enabling it never unlocks or reuses Gandr state.
type Config struct {
	Enabled                 bool
	ShareCache              bool
	IdentityPath            string
	ObjectPath              string
	Listen                  []string
	Peers                   []string
	PeerKey                 ed25519.PublicKey
	OriginEnabled           bool
	OriginForums            []string
	OriginInterval          time.Duration
	OriginMaxPages          int
	OriginDiscoverSubforums bool
	OriginMaxForums         int
	OriginBatchSize         int
}

func DefaultConfig() Config { return Config{} }

type fileConfig struct {
	Mesh   fileMeshConfig   `toml:"mesh"`
	Origin fileOriginConfig `toml:"origin"`
}

type fileMeshConfig struct {
	Enabled    bool     `toml:"enabled"`
	ShareCache bool     `toml:"share_cache"`
	Listen     []string `toml:"listen"`
	Peers      []string `toml:"peers"`
	PeerKey    string   `toml:"peer_key"`
}

type fileOriginConfig struct {
	Enabled           bool     `toml:"enabled"`
	Forums            []string `toml:"forums"`
	Interval          string   `toml:"interval"`
	MaxPages          int      `toml:"max_pages"`
	DiscoverSubforums bool     `toml:"discover_subforums"`
	MaxForums         int      `toml:"max_forums"`
	BatchSize         int      `toml:"batch_size"`
}

// Load reads the local configuration, then applies explicitly supplied
// environment variables as development/test overrides. A missing or invalid
// optional config file falls back to safe defaults; the runtime remains
// disabled unless a valid explicit opt-in is present.
func Load() Config {
	path := os.Getenv("BACKFLASH_CONFIG")
	if path == "" {
		path = defaultConfigPath()
	}
	cfg, _ := LoadFrom(path)
	return applyEnvOverrides(cfg)
}

// LoadFrom loads one TOML file without applying environment overrides. It is
// kept separate so tests and callers with an explicit config path can inspect
// configuration deterministically.
func LoadFrom(path string) (Config, error) {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, _ := os.UserHomeDir()
		dataHome = filepath.Join(home, ".local", "share")
	}
	base := filepath.Join(dataHome, "backflash", "mesh")
	cfg := Config{
		IdentityPath:    filepath.Join(base, "identity.key"),
		ObjectPath:      filepath.Join(base, "objects"),
		OriginInterval:  30 * time.Minute,
		OriginMaxPages:  1,
		OriginMaxForums: 200,
		OriginBatchSize: 10,
	}
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, err
	}
	var file fileConfig
	if _, err := toml.Decode(string(data), &file); err != nil {
		return cfg, fmt.Errorf("läs mesh-konfiguration %s: %w", path, err)
	}
	cfg.Enabled = file.Mesh.Enabled
	cfg.ShareCache = file.Mesh.ShareCache
	cfg.Listen = cleanList(file.Mesh.Listen)
	cfg.Peers = cleanList(file.Mesh.Peers)
	cfg.PeerKey = parseKey(file.Mesh.PeerKey)
	cfg.OriginEnabled = file.Origin.Enabled
	cfg.OriginForums = cleanList(file.Origin.Forums)
	if file.Origin.Interval != "" {
		if interval, err := time.ParseDuration(file.Origin.Interval); err == nil && interval >= time.Minute {
			cfg.OriginInterval = interval
		}
	}
	if file.Origin.MaxPages > 0 && file.Origin.MaxPages <= 10 {
		cfg.OriginMaxPages = file.Origin.MaxPages
	}
	cfg.OriginDiscoverSubforums = file.Origin.DiscoverSubforums
	if file.Origin.MaxForums > 0 && file.Origin.MaxForums <= 5000 {
		cfg.OriginMaxForums = file.Origin.MaxForums
	}
	if file.Origin.BatchSize > 0 && file.Origin.BatchSize <= 100 {
		cfg.OriginBatchSize = file.Origin.BatchSize
	}
	return cfg, nil
}

func defaultConfigPath() string {
	configHome := os.Getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		home, _ := os.UserHomeDir()
		configHome = filepath.Join(home, ".config")
	}
	return filepath.Join(configHome, "backflash", "config.toml")
}

func applyEnvOverrides(cfg Config) Config {
	if value, ok := envBool("BACKFLASH_MESH_ENABLED"); ok {
		cfg.Enabled = value
	}
	if value, ok := envBool("BACKFLASH_MESH_SHARE_CACHE"); ok {
		cfg.ShareCache = value
	}
	if value, ok := os.LookupEnv("BACKFLASH_MESH_LISTEN"); ok {
		cfg.Listen = splitList(value)
	}
	if value, ok := os.LookupEnv("BACKFLASH_MESH_PEERS"); ok {
		cfg.Peers = splitList(value)
	}
	if value, ok := os.LookupEnv("BACKFLASH_MESH_PEER_KEY"); ok {
		cfg.PeerKey = parseKey(value)
	}
	return cfg
}

func envBool(name string) (bool, bool) {
	value, ok := os.LookupEnv(name)
	if !ok || strings.TrimSpace(value) == "" {
		return false, false
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err == nil {
		return parsed, true
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "on", "yes":
		return true, true
	case "0", "off", "no":
		return false, true
	default:
		return false, false
	}
}

func splitEnv(name string) []string {
	return splitList(os.Getenv(name))
}

func splitList(value string) []string {
	return cleanList(strings.Split(value, ","))
}

func cleanList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
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
