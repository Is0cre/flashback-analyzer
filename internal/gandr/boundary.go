// Package gandr is BACKFLASH's narrow, lazy boundary to the separate GANDR
// private subsystem. It never shares identity, storage or protocol state with
// Flashback or the public cache mesh.
package gandr

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"sync"

	gandridentity "github.com/gandr-net/gandr/pkg/identity"
)

type State string

const (
	Locked    State = "LÅST"
	Unlocked  State = "UPPLÅST"
	UnlockErr State = "FEL"
	Missing   State = "VALV SAKNAS"
)

type Summary struct {
	State       State
	Fingerprint string
	Error       string
}

// Subsystem owns only the in-memory result of a deliberate GANDR vault
// unlock. The GANDR client database and network are still separate follow-up
// lifecycle steps.
type Subsystem struct {
	mu        sync.RWMutex
	path      string
	identity  *gandridentity.Identity
	state     State
	lastError error
}

// New returns a locked boundary and does not read the GANDR keyfile.
//
// PRIVACY INVARIANT: BACKFLASH startup must not open GANDR's encrypted client
// database or identity key. This protects private state from accidental
// exposure and keeps BACKFLASH cookies, Flashback usernames, reader state and
// cache-peer identity outside the GANDR security domain.
func New() *Subsystem {
	return NewAt(defaultIdentityPath())
}

func NewAt(path string) *Subsystem {
	return &Subsystem{path: path, state: Locked}
}

func defaultIdentityPath() string {
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		home, _ := os.UserHomeDir()
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "gandr", "identity.key")
}

// Unlock verifies and decrypts GANDR's existing keyfile using GANDR's own
// Argon2id/XChaCha20 implementation. It never creates a new identity and
// never sends the passphrase or identity to BACKFLASH mesh/Flashback.
func (s *Subsystem) Unlock(passphrase string) error {
	if s == nil {
		return errors.New("GANDR-gränsen saknas")
	}
	id, err := gandridentity.Load(s.path, []byte(passphrase))
	s.mu.Lock()
	defer s.mu.Unlock()
	if err != nil {
		s.identity = nil
		if errors.Is(err, gandridentity.ErrNoKeyfile) {
			s.state = Missing
			s.lastError = nil
			return err
		}
		s.state = UnlockErr
		s.lastError = err
		return err
	}
	s.identity = id
	s.state = Unlocked
	s.lastError = nil
	return nil
}

// HasVault checks only whether a keyfile exists. It does not read or decrypt
// the file and therefore does not unlock GANDR state.
func (s *Subsystem) HasVault() bool {
	if s == nil {
		return false
	}
	_, err := os.Stat(s.path)
	return err == nil
}

// Create explicitly creates a new GANDR identity. Existing keyfiles are never
// overwritten; destroying an old identity is a separate, deliberate action.
func (s *Subsystem) Create(passphrase string) error {
	if s == nil {
		return errors.New("GANDR-gränsen saknas")
	}
	if passphrase == "" {
		return errors.New("GANDR-lösenordet får inte vara tomt")
	}
	if s.HasVault() {
		return errors.New("GANDR-valvet finns redan")
	}
	id, err := gandridentity.Generate("")
	if err != nil {
		return err
	}
	if err := id.Save(s.path, []byte(passphrase)); err != nil {
		return err
	}
	s.mu.Lock()
	s.identity = id
	s.state = Unlocked
	s.lastError = nil
	s.mu.Unlock()
	return nil
}

// Lock clears the active decrypted identity from the integration boundary.
func (s *Subsystem) Lock() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.identity = nil
	s.state = Locked
	s.lastError = nil
}

// DeleteVault permanently removes this GANDR identity and its private client
// database. The caller must perform an explicit confirmation first.
// PRIVACY INVARIANT: this never touches BACKFLASH storage, mesh identity,
// Flashback cookies, or public cache objects.
func (s *Subsystem) DeleteVault() error {
	if s == nil {
		return errors.New("GANDR-gränsen saknas")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.identity != nil {
		return errors.New("lås GANDR-valvet innan radering")
	}
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	clientDB := filepath.Join(filepath.Dir(s.path), "client.db")
	if err := os.Remove(clientDB); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.state = Missing
	s.lastError = nil
	return nil
}

func (s *Subsystem) Summary() Summary {
	if s == nil {
		return Summary{State: Locked}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := Summary{State: s.state}
	if s.identity != nil {
		out.Fingerprint = hex.EncodeToString(s.identity.PublicKey)[:8]
	}
	if s.lastError != nil {
		out.Error = s.lastError.Error()
	}
	return out
}
