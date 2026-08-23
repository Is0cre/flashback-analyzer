package mesh

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LoadOrCreateIdentity loads a Backflash-only Yggdrasil transport key. It is
// called only after explicit mesh activation; disabled mode never touches it.
//
// # PRIVACY INVARIANT — Backflash mesh identity
//
// This key belongs only to the public Backflash cache mesh. It is not derived
// from Gandr, Flashback sessions/cookies, or local OS identity. Changing this
// boundary creates cross-system correlation.
func LoadOrCreateIdentity(path string) (ed25519.PrivateKey, error) {
	if path == "" {
		return nil, errors.New("meshidentitet saknar sökväg")
	}
	if data, err := os.ReadFile(path); err == nil {
		if len(data) != ed25519.SeedSize {
			return nil, errors.New("meshidentitet har ogiltig storlek")
		}
		return ed25519.NewKeyFromSeed(data), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	seed := make([]byte, ed25519.SeedSize)
	if _, err := rand.Read(seed); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".identity-*")
	if err != nil {
		return nil, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if _, err := tmp.Write(seed); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return nil, fmt.Errorf("meshidentitet kunde inte sparas: %w", err)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}
