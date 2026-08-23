package config

import (
	"os"
	"path/filepath"
)

type Paths struct{ Database, HTTPCache, State string }

func DefaultPaths() Paths {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home(), ".local", "share")
	}
	cacheHome := os.Getenv("XDG_CACHE_HOME")
	if cacheHome == "" {
		cacheHome = filepath.Join(home(), ".cache")
	}
	stateHome := os.Getenv("XDG_STATE_HOME")
	if stateHome == "" {
		stateHome = filepath.Join(home(), ".local", "state")
	}
	return Paths{Database: filepath.Join(dataHome, "backflash", "backflash.db"), HTTPCache: filepath.Join(cacheHome, "backflash", "http"), State: filepath.Join(stateHome, "backflash", "app.log")}
}

func home() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}
