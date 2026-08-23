package mesh

import "os"

// Config controls the public cache mesh. It is deliberately disabled unless
// the user opts in; enabling it never unlocks or reuses Gandr state.
type Config struct {
	Enabled bool
}

func DefaultConfig() Config { return Config{} }

// Load reads the temporary opt-in switch used until the shared TOML config is
// introduced. Only the explicit value "1" enables public mesh traffic.
func Load() Config {
	return Config{Enabled: os.Getenv("BACKFLASH_MESH_ENABLED") == "1"}
}
