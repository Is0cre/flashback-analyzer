// Package ygg adapts an embedded Yggdrasil core to BACKFLASH's public cache
// protocol. It is deliberately independent of GANDR's identity and network
// packages.
package ygg

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	iwt "github.com/Arceliar/ironwood/types"
	"github.com/backflash-cli/backflash/internal/mesh"
	"github.com/yggdrasil-network/yggdrasil-go/src/config"
	"github.com/yggdrasil-network/yggdrasil-go/src/core"
)

type Config struct {
	PrivateKey ed25519.PrivateKey
	Listen     []string
	Peers      []string
	PeerKey    ed25519.PublicKey
}

// Node owns only the public cache transport key. It must never be populated
// with a Gandr identity key.
type Node struct {
	core       *core.Core
	listeners  []*core.Listener
	peer       net.Addr
	mu         sync.Mutex
	requestMu  sync.Mutex
	readerOnce sync.Once
	pending    map[string]chan mesh.Message
	readErr    chan error
	handler    *mesh.Node
	bytesSent  atomic.Uint64
	bytesRecv  atomic.Uint64
	served     atomic.Uint64
	received   atomic.Uint64
	lastOK     atomic.Int64
	lastFailed atomic.Int64
}

type Stats struct {
	BytesSent, BytesReceived       uint64
	ObjectsServed, ObjectsReceived uint64
	LastSuccess, LastFailure       time.Time
}

func New(cfg Config) (*Node, error) {
	if len(cfg.PrivateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("ogiltig Yggdrasil-transportnyckel: %d byte", len(cfg.PrivateKey))
	}
	ncfg := config.GenerateConfig()
	ncfg.PrivateKey = config.KeyBytes(cfg.PrivateKey)
	if err := ncfg.GenerateSelfSignedCertificate(); err != nil {
		return nil, err
	}
	ygg, err := core.New(ncfg.Certificate, nil)
	if err != nil {
		return nil, err
	}
	n := &Node{core: ygg}
	cleanup := func() { ygg.Stop() }
	for _, raw := range cfg.Listen {
		u, err := url.Parse(raw)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("ogiltig Yggdrasil-lyssnare %q: %w", raw, err)
		}
		listener, err := ygg.Listen(u, "")
		if err != nil {
			cleanup()
			return nil, err
		}
		n.listeners = append(n.listeners, listener)
	}
	for _, raw := range cfg.Peers {
		u, err := url.Parse(raw)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("ogiltig Yggdrasil-peer %q: %w", raw, err)
		}
		if err := ygg.AddPeer(u, ""); err != nil {
			cleanup()
			return nil, err
		}
	}
	if len(cfg.PeerKey) == ed25519.PublicKeySize {
		n.peer = iwt.Addr(cfg.PeerKey)
	}
	return n, nil
}

func (n *Node) PublicKey() ed25519.PublicKey {
	if n == nil || n.core == nil {
		return nil
	}
	return append(ed25519.PublicKey(nil), n.core.PublicKey()...)
}

func (n *Node) PublicKeyString() string { return hex.EncodeToString(n.PublicKey()) }

func (n *Node) ListenAddrs() []string {
	if n == nil {
		return nil
	}
	out := make([]string, 0, len(n.listeners))
	for _, listener := range n.listeners {
		out = append(out, listener.Addr().String())
	}
	return out
}

func (n *Node) PeerCount() int {
	if n == nil || n.core == nil {
		return 0
	}
	count := 0
	for _, peer := range n.core.GetPeers() {
		if peer.Up {
			count++
		}
	}
	return count
}

// Request implements mesh.Transport for one configured remote Yggdrasil key.
// A transport is serialized because the compact MVP protocol is request/
// response and has no multiplexing identifier yet.
func (n *Node) Request(request mesh.Message) (mesh.Message, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return n.RequestContext(ctx, request)
}

func (n *Node) RequestContext(ctx context.Context, request mesh.Message) (mesh.Message, error) {
	if n == nil || n.core == nil || n.peer == nil {
		return mesh.Message{}, errors.New("Yggdrasil-peer saknas")
	}
	b, err := json.Marshal(request)
	if err != nil {
		return mesh.Message{}, err
	}
	if len(b) > mesh.MaxObjectSize+1<<20 {
		return mesh.Message{}, errors.New("meshförfrågan är för stor")
	}
	if request.ID == "" {
		request.ID = requestID()
		b, err = json.Marshal(request)
		if err != nil {
			return mesh.Message{}, err
		}
	}
	n.requestMu.Lock()
	defer n.requestMu.Unlock()
	n.startLoop(nil)
	responseCh := make(chan mesh.Message, 1)
	n.pending[request.ID] = responseCh
	defer delete(n.pending, request.ID)
	for {
		if _, err := n.core.WriteTo(b, n.peer); err != nil {
			n.lastFailed.Store(time.Now().UnixNano())
			return mesh.Message{}, err
		}
		n.bytesSent.Add(uint64(len(b)))
		select {
		case <-ctx.Done():
			return mesh.Message{}, ctx.Err()
		case err := <-n.readErr:
			return mesh.Message{}, err
		case response := <-responseCh:
			if response.Type == mesh.Object {
				n.received.Add(1)
			}
			n.lastOK.Store(time.Now().UnixNano())
			return response, nil
		case <-time.After(500 * time.Millisecond):
			// A link can be up before the overlay route is ready. Retry the
			// datagram without blocking the caller indefinitely.
			continue
		}
	}
}

func requestID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func (n *Node) startLoop(handler *mesh.Node) {
	n.readerOnce.Do(func() {
		n.pending = make(map[string]chan mesh.Message)
		n.readErr = make(chan error, 1)
		n.handler = handler
		go func() {
			buf := make([]byte, mesh.MaxObjectSize+1<<20)
			for {
				nread, from, err := n.core.ReadFrom(buf)
				if err != nil {
					n.lastFailed.Store(time.Now().UnixNano())
					n.readErr <- err
					return
				}
				n.bytesRecv.Add(uint64(nread))
				var request mesh.Message
				if err := json.Unmarshal(buf[:nread], &request); err != nil {
					continue
				}
				if request.Type == mesh.Get && n.handler != nil {
					response, err := n.handler.Serve(request)
					response.ID = request.ID
					if err != nil {
						response = mesh.Message{Type: mesh.NotFound, Hash: request.Hash, ID: request.ID}
					}
					body, err := json.Marshal(response)
					if err == nil {
						if _, writeErr := n.core.WriteTo(body, from); writeErr == nil {
							n.bytesSent.Add(uint64(len(body)))
							if response.Type == mesh.Object {
								n.served.Add(1)
							}
							n.lastOK.Store(time.Now().UnixNano())
						}
					}
					continue
				}
				if request.ID == "" {
					continue
				}
				n.mu.Lock()
				responseCh := n.pending[request.ID]
				n.mu.Unlock()
				if responseCh != nil {
					select {
					case responseCh <- request:
					default:
					}
				}
			}
		}()
	})
}

func (n *Node) Stats() Stats {
	stats := Stats{BytesSent: n.bytesSent.Load(), BytesReceived: n.bytesRecv.Load(), ObjectsServed: n.served.Load(), ObjectsReceived: n.received.Load()}
	if value := n.lastOK.Load(); value != 0 {
		stats.LastSuccess = time.Unix(0, value)
	}
	if value := n.lastFailed.Load(); value != 0 {
		stats.LastFailure = time.Unix(0, value)
	}
	return stats
}

func (n *Node) Close() error {
	if n == nil || n.core == nil {
		return nil
	}
	n.core.Stop()
	return nil
}

// Prepare installs the public cache handler before the runtime becomes
// visible to callers. This avoids a startup race between the first GET and
// the serving loop; it does not start any network activity by itself.
func (n *Node) Prepare(cache *mesh.Node) {
	if n != nil && n.core != nil {
		n.startLoop(cache)
	}
}

// Serve reads public cache requests until the transport closes. It is meant
// for an opt-in cache node and performs no peer/user identity logging.
func (n *Node) Serve(ctx context.Context, cache *mesh.Node) error {
	if n == nil || n.core == nil || cache == nil {
		return errors.New("Yggdrasil-cache-node saknar state")
	}
	n.startLoop(cache)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-n.readErr:
		return err
	}
}
