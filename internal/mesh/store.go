package mesh

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ObjectStore persists public immutable cache objects separately from the
// BACKFLASH SQLite database and completely separately from any Gandr store.
type ObjectStore struct{ root string }

func OpenObjectStore(root string) (*ObjectStore, error) {
	if root == "" {
		return nil, errors.New("mesh-lagring saknar sökväg")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &ObjectStore{root: root}, nil
}

func (s *ObjectStore) path(hash string) string { return filepath.Join(s.root, hash+".json") }

func validHash(hash string) bool {
	if len(hash) != 64 {
		return false
	}
	_, err := hex.DecodeString(hash)
	return err == nil
}

func (s *ObjectStore) Put(o CacheObject) error {
	if err := o.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(o)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.root, ".object-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, s.path(o.HashString()))
}

func (s *ObjectStore) Get(hash string) (CacheObject, error) {
	if !validHash(hash) {
		return CacheObject{}, errors.New("ogiltig meshadress")
	}
	b, err := os.ReadFile(s.path(hash))
	if err != nil {
		return CacheObject{}, err
	}
	var o CacheObject
	if err := json.Unmarshal(b, &o); err != nil {
		return CacheObject{}, fmt.Errorf("meshobjekt är ogiltigt: %w", err)
	}
	if o.HashString() != hash {
		return CacheObject{}, errors.New("meshobjektets adress stämmer inte")
	}
	if err := o.Validate(); err != nil {
		return CacheObject{}, err
	}
	return o, nil
}
