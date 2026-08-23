// Package ygg adapts an embedded Yggdrasil core to BACKFLASH's public cache
// protocol. It is deliberately independent of GANDR's identity and network
// packages.
package ygg

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"sync"

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
	core      *core.Core
	listeners []*core.Listener
	peer      net.Addr
	mu        sync.Mutex
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
	n.mu.Lock()
	defer n.mu.Unlock()
	if _, err := n.core.WriteTo(b, n.peer); err != nil {
		return mesh.Message{}, err
	}
	buf := make([]byte, mesh.MaxObjectSize+1<<20)
	for {
		nread, from, err := n.core.ReadFrom(buf)
		if err != nil {
			return mesh.Message{}, err
		}
		if from.String() != n.peer.String() {
			continue
		}
		var response mesh.Message
		if err := json.Unmarshal(buf[:nread], &response); err != nil {
			return mesh.Message{}, err
		}
		return response, nil
	}
}

func (n *Node) Close() error {
	if n == nil || n.core == nil {
		return nil
	}
	n.core.Stop()
	return nil
}

// Serve reads public cache requests until the transport closes. It is meant
// for an opt-in cache node and performs no peer/user identity logging.
func (n *Node) Serve(ctx context.Context, cache *mesh.Node) error {
	if n == nil || n.core == nil || cache == nil {
		return errors.New("Yggdrasil-cache-node saknar state")
	}
	buf := make([]byte, mesh.MaxObjectSize+1<<20)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		nread, from, err := n.core.ReadFrom(buf)
		if err != nil {
			return err
		}
		var request mesh.Message
		if err := json.Unmarshal(buf[:nread], &request); err != nil {
			continue
		}
		response, err := cache.Serve(request)
		if err != nil {
			response = mesh.Message{Type: mesh.NotFound, Hash: request.Hash}
		}
		body, err := json.Marshal(response)
		if err != nil {
			continue
		}
		_, _ = n.core.WriteTo(body, from)
	}
}
