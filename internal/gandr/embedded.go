package gandr

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/hkdf"

	gandrclientdb "github.com/gandr-net/gandr/pkg/clientdb"
	gandrdaemon "github.com/gandr-net/gandr/pkg/daemon"
	gandridentity "github.com/gandr-net/gandr/pkg/identity"
	"github.com/gandr-net/gandr/pkg/ipc"
	"github.com/gandr-net/gandr/pkg/network"
	"github.com/gandr-net/gandr/pkg/proto"
	"github.com/gandr-net/gandr/pkg/store"
)

// embeddedUserAgent identifies BACKFLASH's in-process daemon in federation
// handshakes, distinct from a standalone gandrd so peer lists make it
// obvious which is which.
const embeddedUserAgent = "backflash-e2e-chatt/0.1.0"

// EmbeddedOptions configures BACKFLASH's in-process Gandr daemon — no
// separate gandrd process, no socket, no systemd unit. This is what makes
// the chat feature usable without asking a normal user to install and run
// a second background service.
type EmbeddedOptions struct {
	// SeedYggdrasilKey is a hex-encoded Yggdrasil node key to federate
	// with continuously (retried forever in the background, same as
	// gandrd's own seed loop — see daemon.Options.Seeds). Empty disables
	// federation: local-only chat, nothing sent or received over the
	// network.
	SeedYggdrasilKey string
	// BootstrapPeers are Yggdrasil transport peer URIs used to reach the
	// overlay at all (see src/gandr/docs/SETUP.md's [network] peers).
	// Federation cannot happen without at least one real physical link,
	// regardless of SeedYggdrasilKey.
	BootstrapPeers []string
	// Listen optionally accepts inbound Yggdrasil links too. Ordinary
	// users leave this empty — outbound-only needs no port forwarding —
	// but it's a real capability for anyone who wants it, and lets tests
	// build a direct two-peer link without a third relay node.
	Listen []string
}

// ConnectEmbedded is Connect's network path for ordinary users: it starts
// a Daemon (pkg/daemon) directly in this process instead of dialing an
// external gandrd over a Unix socket. Everything downstream of Session —
// channels, sends, peers — works identically either way, since Session
// only ever talks to the daemonClient interface, never the concrete
// transport.
func (s *Subsystem) ConnectEmbedded(opts EmbeddedOptions) (*Session, error) {
	if s == nil {
		return nil, errors.New("E2E-CHATT-gränsen saknas")
	}
	s.mu.RLock()
	id := s.identity
	keyPath := s.path
	s.mu.RUnlock()
	if id == nil {
		return nil, errors.New("E2E-CHATT-valvet är låst")
	}
	dataDir := filepath.Dir(keyPath)
	db, err := gandrclientdb.Open(filepath.Join(dataDir, "client.db"), id.PrivateKey)
	if err != nil {
		return nil, err
	}
	client, err := newEmbeddedClient(id, dataDir, opts)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Session{db: db, client: client, id: id, groups: make(map[[32]byte][32]byte)}, nil
}

// embeddedTransportSeed derives a Yggdrasil transport keypair from the
// already-unlocked Gandr identity via HKDF, instead of persisting a
// separate keyfile: deterministic (a user's overlay address stays stable
// across restarts, so peers can keep reaching them), needs no additional
// passphrase, and nothing new touches disk beyond what BACKFLASH already
// protects.
//
// This is still a distinct key from the Gandr identity ("transport key ≠
// identity key", src/gandr/CLAUDE.md) — cryptographically independent
// even though both derive from the same root secret, via a domain
// separator unique to this derivation. Unlike a standalone gandrd shared
// by several human clients, an embedded daemon's node identity (used at
// the federation-handshake layer) and its one user's message-signing
// identity necessarily correlate 1:1 regardless of key separation, since
// no other user's traffic ever flows through it — so merging them here
// doesn't weaken anything gandrd's separate-key design was protecting
// against in the shared-daemon case.
func embeddedTransportSeed(gandrIdentity ed25519.PrivateKey) ed25519.PrivateKey {
	r := hkdf.New(sha256.New, gandrIdentity.Seed(), nil, []byte("backflash-embedded-ygg-transport-v1"))
	seed := make([]byte, ed25519.SeedSize)
	// A SHA-256 HKDF can produce up to 255*32 bytes; reading the first 32
	// never errors short of an io failure on an in-memory reader, which
	// hkdf.Reader cannot produce.
	_, _ = io.ReadFull(r, seed)
	return ed25519.NewKeyFromSeed(seed)
}

func newEmbeddedClient(id *gandridentity.Identity, dataDir string, opts EmbeddedOptions) (*embeddedClient, error) {
	transport, err := network.NewEmbedded(network.EmbeddedConfig{
		PrivateKey: embeddedTransportSeed(id.PrivateKey),
		Listen:     opts.Listen,
		Peers:      opts.BootstrapPeers,
	})
	if err != nil {
		return nil, err
	}
	objects, err := store.Open(filepath.Join(dataDir, "objects"))
	if err != nil {
		_ = transport.Close()
		return nil, err
	}
	var seeds [][]byte
	if opts.SeedYggdrasilKey != "" {
		if key, err := hex.DecodeString(opts.SeedYggdrasilKey); err == nil && len(key) == ed25519.PublicKeySize {
			seeds = append(seeds, key)
		}
	}
	d, err := gandrdaemon.New(gandrdaemon.Options{
		UserAgent:    embeddedUserAgent,
		Capabilities: proto.CapChat,
		// Relay isn't "forward strangers' traffic as a courtesy" here —
		// it's the only mechanism that pushes any envelope (including
		// this client's own outgoing messages) to federated peers at
		// all; HandleSend routes through the same relay() call as
		// flood-forwarding. Without it, sends would silently store
		// locally and go nowhere. In practice this still doesn't turn a
		// personal device into relay infrastructure for others: an
		// embedded client federates with exactly one peer (the seed) by
		// default and never accepts inbound links, so relay() never has
		// a second peer to forward anything to.
		Relay:          true,
		MaxPeers:       32,
		DefaultTrust:   proto.TrustNeutral,
		MaxMessageAge:  7 * 24 * 3600,
		MaxPayloadSize: 65535,
		RateLimitRPM:   600,
		Seeds:          seeds,
	}, id, transport, objects)
	if err != nil {
		_ = transport.Close()
		return nil, err
	}
	client := &embeddedClient{
		daemon:   d,
		subs:     make(map[[32]byte]struct{}),
		incoming: make(chan *proto.Envelope, 32),
		done:     make(chan struct{}),
	}
	d.SetEventSink(client)
	go d.RunLoops()
	return client, nil
}

// embeddedClient runs a Daemon in-process and satisfies the same
// daemonClient interface an *ipc.Client does, so Session's methods never
// need to know which transport they're talking to.
type embeddedClient struct {
	daemon *gandrdaemon.Daemon

	mu       sync.Mutex
	subs     map[[32]byte]struct{}
	incoming chan *proto.Envelope
	done     chan struct{}
	closed   bool
}

func (e *embeddedClient) Close() error {
	e.daemon.Stop()
	e.mu.Lock()
	if !e.closed {
		e.closed = true
		close(e.done)
	}
	e.mu.Unlock()
	return nil
}

func (e *embeddedClient) Incoming() <-chan *proto.Envelope { return e.incoming }
func (e *embeddedClient) Done() <-chan struct{}            { return e.done }

func (e *embeddedClient) Subscribe(ctx context.Context, channel [32]byte) error {
	e.mu.Lock()
	e.subs[channel] = struct{}{}
	e.mu.Unlock()
	return nil
}

func (e *embeddedClient) Unsubscribe(ctx context.Context, channel [32]byte) error {
	e.mu.Lock()
	delete(e.subs, channel)
	e.mu.Unlock()
	return nil
}

func (e *embeddedClient) Send(ctx context.Context, env *proto.Envelope) error {
	return e.daemon.HandleSend(env)
}

func (e *embeddedClient) Connect(ctx context.Context, yggKey [32]byte) error {
	return e.daemon.HandleConnect(yggKey)
}

func (e *embeddedClient) PeerList(ctx context.Context) ([]ipc.PeerInfo, error) {
	return e.daemon.HandlePeerList(), nil
}

// wants mirrors serverConn.wants in pkg/ipc/server.go exactly: direct
// messages always pass, channel broadcasts only if subscribed. There's
// only ever one "connection" here, so this is that same filter applied
// to a single subscriber instead of fanning out to several.
func (e *embeddedClient) wants(env *proto.Envelope) bool {
	if env.Type != proto.MsgChat {
		return true
	}
	chat := &proto.ChatPayload{}
	if err := proto.DecodePayload(env.Payload, chat); err != nil {
		return false
	}
	if chat.ChannelID == ([32]byte{}) {
		return true // DM
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.subs[chat.ChannelID]
	return ok
}

// --- gandrdaemon.EventSink ---

func (e *embeddedClient) Push(env *proto.Envelope) {
	if !e.wants(env) {
		return
	}
	select {
	case e.incoming <- env:
	default:
		// Mirrors ipc's own semantics: a slow or absent reader must never
		// block the daemon's message-processing loop. The TUI drains
		// Incoming() promptly via waitGandrIncoming; dropping a push here
		// only loses the notification, not the message itself, which is
		// already durably stored in the object cache.
	}
}

func (e *embeddedClient) Delivered(env *proto.Envelope)       {}
func (e *embeddedClient) PushPeerUpdate(peers []ipc.PeerInfo) {}
