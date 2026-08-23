package mesh

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ObjectStore persists public immutable cache objects separately from the
// BACKFLASH SQLite database and completely separately from any Gandr store.
type ObjectStore struct {
	root string

	// resourceIndex avoids rescanning the object directory for every cache
	// lookup. It is only an acceleration index; the object file remains the
	// source of truth and is still hash-validated by Get.
	mu            sync.RWMutex
	resourceIndex map[string]string
}

func OpenObjectStore(root string) (*ObjectStore, error) {
	if root == "" {
		return nil, errors.New("mesh-lagring saknar sökväg")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	store := &ObjectStore{root: root, resourceIndex: make(map[string]string)}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		hash := strings.TrimSuffix(entry.Name(), ".json")
		o, err := store.Get(hash)
		if err != nil {
			// A damaged object must not prevent the cache from starting. Get
			// will continue to reject it if it is requested later.
			continue
		}
		store.resourceIndex[resourceKey(o.Source, o.ResourceID, o.Type)] = hash
	}
	return store, nil
}

func resourceKey(source, resourceID string, typ ObjectType) string {
	return source + "\x00" + resourceID + "\x00" + string(typ)
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
	if err := os.Rename(tmpName, s.path(o.HashString())); err != nil {
		return err
	}
	s.mu.Lock()
	s.resourceIndex[resourceKey(o.Source, o.ResourceID, o.Type)] = o.HashString()
	s.mu.Unlock()
	return nil
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

func (s *ObjectStore) Count() (int, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			count++
		}
	}
	return count, nil
}

func (s *ObjectStore) Find(source, resourceID string, typ ObjectType) (CacheObject, error) {
	if typ != "" {
		s.mu.RLock()
		hash := s.resourceIndex[resourceKey(source, resourceID, typ)]
		s.mu.RUnlock()
		if hash == "" {
			return CacheObject{}, os.ErrNotExist
		}
		return s.Get(hash)
	}
	// The normal path supplies a type. Keep the wildcard fallback for callers
	// that need it, but use the small in-memory index rather than walking files.
	s.mu.RLock()
	var hash string
	for key, candidate := range s.resourceIndex {
		parts := strings.SplitN(key, "\x00", 3)
		if len(parts) == 3 && parts[0] == source && parts[1] == resourceID {
			hash = candidate
			break
		}
	}
	s.mu.RUnlock()
	if hash != "" {
		return s.Get(hash)
	}
	return CacheObject{}, os.ErrNotExist
}
