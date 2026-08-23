package mesh

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMeshIsDisabledByDefault(t *testing.T) {
	t.Setenv("BACKFLASH_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("BACKFLASH_MESH_ENABLED", "")
	if DefaultConfig().Enabled || Load().Enabled {
		t.Fatal("mesh ska vara avstängt som standard")
	}
}

func TestMeshRequiresExplicitOptIn(t *testing.T) {
	t.Setenv("BACKFLASH_CONFIG", filepath.Join(t.TempDir(), "missing.toml"))
	t.Setenv("BACKFLASH_MESH_ENABLED", "1")
	if !Load().Enabled {
		t.Fatal("explicit mesh-opt-in ignorerades")
	}
}

func TestLoadFromTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	contents := []byte(`[mesh]
enabled = true
share_cache = true
listen = ["tcp://127.0.0.1:4242"]
peers = ["tcp://127.0.0.1:4243"]
[origin]
enabled = true
forums = ["https://www.flashback.org/f123"]
interval = "45m"
max_pages = 1
discover_subforums = true
max_forums = 42
batch_size = 7
`)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || !cfg.ShareCache {
		t.Fatalf("TOML mesh-inställningar lästes inte: %#v", cfg)
	}
	if len(cfg.Listen) != 1 || len(cfg.Peers) != 1 {
		t.Fatalf("TOML endpoints lästes inte: %#v", cfg)
	}
	if !cfg.OriginEnabled || len(cfg.OriginForums) != 1 || cfg.OriginInterval.String() != "45m0s" || cfg.OriginMaxPages != 1 || !cfg.OriginDiscoverSubforums || cfg.OriginMaxForums != 42 || cfg.OriginBatchSize != 7 {
		t.Fatalf("TOML origin-inställningar lästes inte: %#v", cfg)
	}
}

func TestEnvironmentOverridesTOML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte("[mesh]\nenabled = true\nshare_cache = true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("BACKFLASH_CONFIG", path)
	t.Setenv("BACKFLASH_MESH_ENABLED", "0")
	t.Setenv("BACKFLASH_MESH_SHARE_CACHE", "false")
	cfg := Load()
	if cfg.Enabled || cfg.ShareCache {
		t.Fatalf("miljövariabler ersatte inte TOML: %#v", cfg)
	}
}
