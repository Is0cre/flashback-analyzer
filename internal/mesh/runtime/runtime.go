// Package runtime owns the opt-in BACKFLASH public cache-mesh lifecycle.
// It has no Gandr dependency and never opens Gandr storage.
package runtime

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	"github.com/backflash-cli/backflash/internal/diagnostics"
	"github.com/backflash-cli/backflash/internal/mesh"
	"github.com/backflash-cli/backflash/internal/mesh/ygg"
)

type State string

const (
	Disabled   State = "DISABLED"
	Configured State = "CONFIGURED"
	Starting   State = "STARTING"
	Running    State = "RUNNING"
	Degraded   State = "DEGRADED"
	Stopping   State = "STOPPING"
	Error      State = "ERROR"
)

type Snapshot struct {
	State         State
	ShareCache    bool
	Identity      string
	Peers         int
	Objects       int
	StartedAt     time.Time
	LastError     string
	ObjectsServed uint64
	ObjectsRecv   uint64
	BytesSent     uint64
	BytesRecv     uint64
}

type Runtime struct {
	cfg       mesh.Config
	mu        sync.RWMutex
	state     State
	lastError error
	identity  ed25519.PrivateKey
	transport *ygg.Node
	store     *mesh.ObjectStore
	cache     *mesh.Node
	cancel    context.CancelFunc
	done      chan struct{}
	startedAt time.Time
}

func New(cfg mesh.Config) *Runtime {
	state := Configured
	if !cfg.Enabled {
		state = Disabled
	}
	return &Runtime{cfg: cfg, state: state}
}

func (r *Runtime) State() State {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.state
}

func (r *Runtime) setState(state State, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.state, r.lastError = state, err
}

func (r *Runtime) Start(parent context.Context) error {
	if r == nil {
		return errors.New("mesh-runtime saknas")
	}
	if !r.cfg.Enabled {
		r.setState(Disabled, nil)
		return nil
	}
	r.mu.RLock()
	active := r.cancel != nil
	r.mu.RUnlock()
	if active {
		return errors.New("mesh-runtime kör redan")
	}
	r.setState(Starting, nil)
	finish := diagnostics.Start("mesh_start")
	defer finish()
	identity, err := mesh.LoadOrCreateIdentity(r.cfg.IdentityPath)
	if err != nil {
		r.setState(Error, err)
		return err
	}
	objectStore, err := mesh.OpenObjectStore(r.cfg.ObjectPath)
	if err != nil {
		r.setState(Error, err)
		return err
	}
	transport, err := ygg.New(ygg.Config{PrivateKey: identity, Listen: r.cfg.Listen, Peers: r.cfg.Peers, PeerKey: r.cfg.PeerKey})
	if err != nil {
		r.setState(Error, err)
		return err
	}
	ctx, cancel := context.WithCancel(parent)
	r.mu.Lock()
	r.identity, r.transport, r.store = identity, transport, objectStore
	cache := &mesh.Node{Store: objectStore, Peer: transport}
	r.cache = cache
	r.cancel, r.done, r.startedAt = cancel, make(chan struct{}), time.Now()
	r.mu.Unlock()
	if r.cfg.ShareCache {
		transport.Prepare(cache)
	} else {
		transport.Prepare(nil)
	}
	go r.serve(ctx)
	go r.monitor(ctx)
	if transport.PeerCount() > 0 {
		r.setState(Running, nil)
	} else {
		r.setState(Degraded, nil)
	}
	return nil
}

func (r *Runtime) serve(ctx context.Context) {
	r.mu.RLock()
	transport, cache := r.transport, r.cache
	if !r.cfg.ShareCache {
		cache = nil
	}
	r.mu.RUnlock()
	err := transport.Serve(ctx, cache)
	if ctx.Err() == nil && err != nil {
		r.setState(Error, err)
	}
	close(r.done)
}

func (r *Runtime) monitor(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.mu.RLock()
			transport := r.transport
			state := r.state
			r.mu.RUnlock()
			if state == Error || transport == nil {
				continue
			}
			if transport.PeerCount() > 0 {
				r.setState(Running, nil)
			} else {
				r.setState(Degraded, nil)
			}
		}
	}
}

func (r *Runtime) Stop() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.cancel == nil {
		state := r.state
		r.mu.Unlock()
		if state == Configured {
			r.setState(Configured, nil)
		}
		return nil
	}
	r.state = Stopping
	cancel, transport, done := r.cancel, r.transport, r.done
	r.cancel = nil
	r.mu.Unlock()
	cancel()
	_ = transport.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		r.setState(Error, errors.New("mesh-runtime stängdes inte i tid"))
		return errors.New("mesh-runtime stängdes inte i tid")
	}
	r.setState(Disabled, nil)
	return nil
}

func (r *Runtime) Get(hash string) (mesh.CacheObject, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return r.GetContext(ctx, hash)
}

func (r *Runtime) GetContext(ctx context.Context, hash string) (mesh.CacheObject, error) {
	r.mu.RLock()
	cache, state := r.cache, r.state
	r.mu.RUnlock()
	if cache == nil {
		return mesh.CacheObject{}, errors.New("mesh-runtime saknar cachelager")
	}
	if state != Running && state != Degraded {
		return mesh.CacheObject{}, errors.New("mesh-runtime är inte aktiv: " + string(state))
	}
	finish := diagnostics.Start("mesh_lookup")
	defer finish()
	return cache.GetContext(ctx, hash)
}

func (r *Runtime) GetResource(ctx context.Context, source, resourceID string, typ mesh.ObjectType) (mesh.CacheObject, error) {
	r.mu.RLock()
	cache, state := r.cache, r.state
	r.mu.RUnlock()
	if cache == nil {
		return mesh.CacheObject{}, errors.New("mesh-runtime saknar cachelager")
	}
	if state != Running && state != Degraded {
		return mesh.CacheObject{}, errors.New("mesh-runtime är inte aktiv: " + string(state))
	}
	finish := diagnostics.Start("mesh_lookup")
	defer finish()
	return cache.GetResource(ctx, source, resourceID, typ)
}

func (r *Runtime) PutLocal(object mesh.CacheObject) error {
	r.mu.RLock()
	store := r.store
	r.mu.RUnlock()
	if store == nil {
		return errors.New("mesh-lagring är inte startad")
	}
	return store.Put(object)
}

func (r *Runtime) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := Snapshot{State: r.state, ShareCache: r.cfg.ShareCache, StartedAt: r.startedAt}
	if r.lastError != nil {
		out.LastError = r.lastError.Error()
	}
	if r.transport != nil {
		out.Peers = r.transport.PeerCount()
		out.Identity = shortFingerprint(r.transport.PublicKey())
		stats := r.transport.Stats()
		out.ObjectsServed = stats.ObjectsServed
		out.ObjectsRecv = stats.ObjectsReceived
		out.BytesSent = stats.BytesSent
		out.BytesRecv = stats.BytesReceived
	}
	if r.store != nil {
		out.Objects, _ = r.store.Count()
	}
	return out
}

func shortFingerprint(key ed25519.PublicKey) string {
	if len(key) == 0 {
		return "—"
	}
	encoded := hex.EncodeToString(key)
	if len(encoded) > 8 {
		encoded = encoded[:8]
	}
	return "~" + encoded
}

func (r *Runtime) PublicKey() ed25519.PublicKey {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.transport == nil {
		return nil
	}
	return r.transport.PublicKey()
}

func (r *Runtime) ListenAddrs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.transport == nil {
		return nil
	}
	return r.transport.ListenAddrs()
}
