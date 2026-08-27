// Package daemon is Gandr's node daemon logic — transport, federation,
// storage, and message handling — decoupled from any specific way of
// exposing it to clients. cmd/gandrd wraps a Daemon with pkg/ipc's Unix
// socket server for the standalone daemon binary. Anything else that can
// hold a network.Transport, an identity, and a store.Store in the same
// process (see BACKFLASH's internal/gandr embedded mode) can construct one
// directly and skip IPC entirely.
package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/yggdrasil-network/yggdrasil-go/src/address"

	"github.com/gandr-net/gandr/pkg/federation"
	"github.com/gandr-net/gandr/pkg/identity"
	"github.com/gandr-net/gandr/pkg/ipc"
	"github.com/gandr-net/gandr/pkg/network"
	"github.com/gandr-net/gandr/pkg/proto"
	"github.com/gandr-net/gandr/pkg/store"
)

// Options configures a Daemon, independent of how a caller sources these
// values (cmd/gandrd's TOML config, or plain defaults for an embedded
// client).
type Options struct {
	// UserAgent is announced in federation handshakes.
	UserAgent string
	// Capabilities is the announced capability bitmask (proto.Cap*).
	Capabilities uint32
	// Relay gates flood-relaying envelopes to other peers. Kept separate
	// from Capabilities (rather than derived from the CapRelay bit) to
	// match the daemon's own historical behavior of gating on the
	// operator's explicit relay setting.
	Relay bool
	// MaxPeers bounds the federation table.
	MaxPeers int
	// DefaultTrust is the trust level assigned to newly federated peers
	// (proto.Trust*).
	DefaultTrust uint8
	// MaxMessageAge is the announced policy and prune threshold, in
	// seconds.
	MaxMessageAge uint32
	// MaxPayloadSize bounds accepted envelope payloads.
	MaxPayloadSize uint32
	// RateLimitRPM bounds messages accepted per peer per minute; <= 0
	// disables the limit.
	RateLimitRPM int
	// Seeds are hex-decoded Yggdrasil node keys of Gandr nodes to
	// federate with at startup and keep retrying indefinitely.
	Seeds [][]byte
}

// EventSink receives events a Daemon can't route anywhere on its own: new
// or delivered envelopes, and peer table changes. *ipc.Server satisfies
// this already (see cmd/gandrd); an in-process caller can implement it
// directly with plain channels instead of a socket.
type EventSink interface {
	Push(env *proto.Envelope)
	Delivered(env *proto.Envelope)
	PushPeerUpdate(peers []ipc.PeerInfo)
}

// Daemon wires transport, federation, and storage together. It implements
// ipc.Handler structurally, so cmd/gandrd can hand a *Daemon straight to
// ipc.Listen without this package importing ipc.Handler by name.
type Daemon struct {
	opts      Options
	id        *identity.Identity
	transport network.Transport
	table     *federation.Table
	objects   *store.Store
	fedCfg    federation.Config

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu       sync.Mutex
	sink     EventSink
	profiles map[[32]byte][32]byte // pubkey -> latest profile content hash
	rates    map[[32]byte]*rateWindow
}

// New assembles a Daemon from loaded components. The caller provides the
// identity (already decrypted) and a started transport. The daemon has no
// EventSink until SetEventSink is called — safe to leave unset if the
// caller has no use for push events (RunLoops works either way; pushes
// are simply no-ops until a sink is attached).
func New(opts Options, id *identity.Identity, transport network.Transport, objects *store.Store) (*Daemon, error) {
	table, err := federation.NewTable(opts.MaxPeers, opts.DefaultTrust)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	d := &Daemon{
		opts:      opts,
		id:        id,
		transport: transport,
		table:     table,
		objects:   objects,
		profiles:  make(map[[32]byte][32]byte),
		rates:     make(map[[32]byte]*rateWindow),
		ctx:       ctx,
		cancel:    cancel,
		fedCfg: federation.Config{
			Identity:     id.PrivateKey,
			Capabilities: opts.Capabilities,
			UserAgent:    opts.UserAgent,
			Policy: proto.PeerPolicyPayload{
				MaxMessageAge:  opts.MaxMessageAge,
				MaxPayloadSize: opts.MaxPayloadSize,
				RateLimitRPM:   uint16(opts.RateLimitRPM),
				TrustLevel:     opts.DefaultTrust,
			},
		},
	}
	return d, nil
}

// SetEventSink attaches (or replaces) the sink pushes are sent to. Safe
// to call before or after RunLoops; nil is a valid "no sink" value.
func (d *Daemon) SetEventSink(sink EventSink) {
	d.mu.Lock()
	d.sink = sink
	d.mu.Unlock()
}

// RunLoops starts all daemon loops and blocks until the daemon is
// stopped. The caller is responsible for exposing the daemon to clients
// (e.g. cmd/gandrd wraps it with ipc.Listen before calling this).
func (d *Daemon) RunLoops() {
	d.wg.Add(3)
	go d.acceptLoop()
	go d.pruneLoop()
	go d.seedLoop()
	<-d.ctx.Done()
}

// Stop shuts the daemon down. It does not close any EventSink — sinks
// that own external resources (like an *ipc.Server's socket) are the
// caller's to close.
func (d *Daemon) Stop() {
	d.cancel()
	d.transport.Close()
	d.wg.Wait()
}

// acceptLoop responds to inbound federation attempts.
func (d *Daemon) acceptLoop() {
	defer d.wg.Done()
	for {
		conn, err := d.transport.Accept(d.ctx)
		if err != nil {
			return
		}
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			sess, err := federation.Respond(d.ctx, conn, d.fedCfg)
			if err != nil {
				// silence on invalid handshakes: close, say nothing
				conn.Close()
				return
			}
			d.adoptSession(sess)
		}()
	}
}

// seedRetryInterval is how often the seed loop re-checks that every
// configured seed has a live session.
const seedRetryInterval = 15 * time.Second

// seedLoop keeps federation with the configured seed nodes alive: at
// startup the overlay may not be routable yet, and a seed can reboot at
// any time, so a single dial attempt is never enough. Each pass dials
// only seeds without a live session; failures stay silent per protocol
// and are simply retried next tick.
func (d *Daemon) seedLoop() {
	defer d.wg.Done()
	if len(d.opts.Seeds) == 0 {
		return
	}
	ticker := time.NewTicker(seedRetryInterval)
	defer ticker.Stop()
	for {
		for _, key := range d.opts.Seeds {
			if d.seedConnected(key) {
				continue
			}
			d.dialSeed(key)
		}
		select {
		case <-d.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// seedConnected reports whether a live peer session runs over the given
// yggdrasil node key.
func (d *Daemon) seedConnected(key []byte) bool {
	for _, p := range d.table.List() {
		if bytes.Equal(p.Addr.YggKey, key) {
			return true
		}
	}
	return false
}

// dialSeed makes one bounded federation attempt to a seed node.
func (d *Daemon) dialSeed(key []byte) {
	ctx, cancel := context.WithTimeout(d.ctx, seedRetryInterval)
	defer cancel()
	conn, err := d.transport.Dial(ctx, network.PeerAddr{YggKey: ed25519.PublicKey(key)})
	if err != nil {
		return
	}
	sess, err := federation.Initiate(ctx, conn, d.fedCfg)
	if err != nil {
		conn.Close()
		return
	}
	d.adoptSession(sess)
}

// adoptSession registers an established session and pumps its messages.
func (d *Daemon) adoptSession(sess *federation.Session) {
	if _, err := d.table.Add(sess); err != nil {
		sess.Close()
		return
	}
	d.notifyPeerUpdate()
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		defer func() {
			// remove only if this session still owns the entry — a
			// reconnect may have replaced it under the same identity
			d.table.RemoveSession(sess.PeerIdentity, sess)
			d.notifyPeerUpdate()
		}()
		for {
			env, err := sess.Recv(d.ctx)
			if err != nil {
				return
			}
			d.handleEnvelope(env, sess)
		}
	}()
}

// handleEnvelope processes one validated envelope from a peer. All
// rejections are silent.
func (d *Daemon) handleEnvelope(env *proto.Envelope, from *federation.Session) {
	if err := proto.ValidateTimestamp(env.Timestamp, time.Now()); err != nil {
		return
	}
	if len(env.Payload) > int(d.opts.MaxPayloadSize) {
		return
	}
	if !d.allowRate(from.PeerIdentity) {
		return
	}
	d.table.Touch(from.PeerIdentity)

	switch env.Type {
	case proto.MsgChat, proto.MsgPost, proto.MsgThread, proto.MsgReply,
		proto.MsgProfile, proto.MsgGuestbook, proto.MsgStatus, proto.MsgSealed:
		if !d.validatePayload(env) {
			return
		}
		_, existed, err := d.objects.Put(env)
		if err != nil || existed {
			return // duplicates end the flood here
		}
		if env.Type == proto.MsgProfile {
			d.recordProfile(env)
		}
		d.relay(env, &from.PeerIdentity)
		d.push(env)
	case proto.MsgAck, proto.MsgSealedAck:
		if !d.validatePayload(env) {
			return
		}
		d.relay(env, &from.PeerIdentity)
		d.delivered(env)
	case proto.MsgDelete:
		if !d.handleDelete(env) {
			return
		}
		d.relay(env, &from.PeerIdentity)
		d.push(env)
	case proto.MsgPeerIntro:
		// introductions are accepted only from vouched peers
		p, ok := d.table.Get(from.PeerIdentity)
		if !ok || !p.Vouched() {
			return
		}
		// v1 records nothing and does not auto-dial; organic discovery
		// arrives in a later milestone
	default:
		// handshake types outside a handshake, unknown types: drop
	}
}

// validatePayload decodes the payload against its schema; failures mean
// the envelope is structurally invalid and silently dropped.
func (d *Daemon) validatePayload(env *proto.Envelope) bool {
	p, err := proto.NewPayload(env.Type)
	if err != nil {
		return false
	}
	return proto.DecodePayload(env.Payload, p) == nil
}

// handleDelete enforces the only deletion rule: you delete your own
// content and nothing else.
func (d *Daemon) handleDelete(env *proto.Envelope) bool {
	del := &proto.DeletePayload{}
	if err := proto.DecodePayload(env.Payload, del); err != nil {
		return false
	}
	hash, err := store.ParseHash(del.TargetHash)
	if err != nil {
		return false
	}
	target, err := d.objects.Get(hash)
	if err != nil {
		return false
	}
	owner := target.Sender == env.Sender
	// guestbook entries may also be deleted by the profile owner
	if !owner && target.Type == proto.MsgGuestbook {
		gb := &proto.GuestbookPayload{}
		if err := proto.DecodePayload(target.Payload, gb); err == nil {
			owner = gb.TargetPubkey == env.Sender
		}
	}
	if !owner {
		return false
	}
	if err := d.objects.Delete(hash); err != nil {
		return false
	}
	return true
}

// recordProfile tracks the newest profile per pubkey.
func (d *Daemon) recordProfile(env *proto.Envelope) {
	hash := env.ContentID()
	d.mu.Lock()
	defer d.mu.Unlock()
	if existing, ok := d.profiles[env.Sender]; ok {
		// keep only the newest by timestamp
		if cur, err := d.objects.Get(existing); err == nil && cur.Timestamp >= env.Timestamp {
			return
		}
	}
	d.profiles[env.Sender] = hash
}

// relay floods an envelope to all relaying peers except its origin.
func (d *Daemon) relay(env *proto.Envelope, except *[32]byte) {
	if !d.opts.Relay {
		return
	}
	for _, p := range d.table.List() {
		if p.Session == nil || p.Trust < proto.TrustNeutral {
			continue
		}
		if except != nil && p.Identity == *except {
			continue
		}
		sess := p.Session
		d.wg.Add(1)
		go func() {
			defer d.wg.Done()
			ctx, cancel := context.WithTimeout(d.ctx, 30*time.Second)
			defer cancel()
			sess.Send(ctx, env)
		}()
	}
}

// rateWindow is a fixed one-minute message counter.
type rateWindow struct {
	start time.Time
	count int
}

// allowRate enforces the per-peer rate limit.
func (d *Daemon) allowRate(peer [32]byte) bool {
	limit := d.opts.RateLimitRPM
	if limit <= 0 {
		return true
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	w, ok := d.rates[peer]
	if !ok || now.Sub(w.start) > time.Minute {
		d.rates[peer] = &rateWindow{start: now, count: 1}
		return true
	}
	w.count++
	return w.count <= limit
}

func (d *Daemon) push(env *proto.Envelope) {
	d.mu.Lock()
	sink := d.sink
	d.mu.Unlock()
	if sink != nil {
		sink.Push(env)
	}
}

func (d *Daemon) delivered(env *proto.Envelope) {
	d.mu.Lock()
	sink := d.sink
	d.mu.Unlock()
	if sink != nil {
		sink.Delivered(env)
	}
}

// notifyPeerUpdate pushes the current peer set to the attached sink.
func (d *Daemon) notifyPeerUpdate() {
	d.mu.Lock()
	sink := d.sink
	d.mu.Unlock()
	if sink != nil {
		sink.PushPeerUpdate(d.peerInfos())
	}
}

func (d *Daemon) peerInfos() []ipc.PeerInfo {
	peers := d.table.List()
	out := make([]ipc.PeerInfo, 0, len(peers))
	for _, p := range peers {
		info := ipc.PeerInfo{
			Identity:     p.Identity,
			Trust:        p.Trust,
			Capabilities: p.Capabilities,
			UserAgent:    p.UserAgent,
			ConnectedAt:  p.ConnectedAt.Unix(),
			LastSeen:     p.LastSeen.Unix(),
		}
		if len(p.Addr.YggKey) == ed25519.PublicKeySize {
			if a := address.AddrForKey(p.Addr.YggKey); a != nil {
				info.Addr = net.IP(a[:]).String()
			}
		}
		out = append(out, info)
	}
	return out
}

// pruneLoop runs the nightly object prune.
func (d *Daemon) pruneLoop() {
	defer d.wg.Done()
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			maxAge := time.Duration(d.opts.MaxMessageAge) * time.Second
			d.objects.Prune(maxAge, time.Now())
		case <-d.ctx.Done():
			return
		}
	}
}

// --- ipc.Handler (structural — cmd/gandrd hands a *Daemon to ipc.Listen) ---

// HandleSend routes a signed envelope submitted by the local client.
func (d *Daemon) HandleSend(env *proto.Envelope) error {
	if err := proto.ValidateTimestamp(env.Timestamp, time.Now()); err != nil {
		return errors.New("stale timestamp")
	}
	if !d.validatePayload(env) {
		return errors.New("invalid payload")
	}
	switch env.Type {
	case proto.MsgChat, proto.MsgPost, proto.MsgThread, proto.MsgReply,
		proto.MsgProfile, proto.MsgGuestbook, proto.MsgStatus, proto.MsgSealed:
		if _, _, err := d.objects.Put(env); err != nil {
			return errors.New("storage failure")
		}
		if env.Type == proto.MsgProfile {
			d.recordProfile(env)
		}
		d.relay(env, nil)
		return nil
	case proto.MsgAck, proto.MsgSealedAck:
		d.relay(env, nil)
		return nil
	case proto.MsgDelete:
		if !d.handleDelete(env) {
			return errors.New("delete rejected")
		}
		d.relay(env, nil)
		return nil
	default:
		return errors.New("unroutable message type")
	}
}

// HandleFetch serves an object by hash.
func (d *Daemon) HandleFetch(hash [32]byte) (*proto.Envelope, error) {
	return d.objects.Get(hash)
}

// HandlePeerList reports current peers.
func (d *Daemon) HandlePeerList() []ipc.PeerInfo {
	return d.peerInfos()
}

// HandleTrust sets the local trust level for a peer.
func (d *Daemon) HandleTrust(identity [32]byte, level uint8) error {
	if err := d.table.SetTrust(identity, level); err != nil {
		return err
	}
	d.notifyPeerUpdate()
	return nil
}

// HandleConnect queues a federation attempt with a yggdrasil node key.
func (d *Daemon) HandleConnect(yggKey [32]byte) error {
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ctx, cancel := context.WithTimeout(d.ctx, 60*time.Second)
		defer cancel()
		conn, err := d.transport.Dial(ctx, network.PeerAddr{YggKey: ed25519.PublicKey(yggKey[:])})
		if err != nil {
			return
		}
		sess, err := federation.Initiate(ctx, conn, d.fedCfg)
		if err != nil {
			conn.Close()
			return
		}
		d.adoptSession(sess)
	}()
	return nil
}

// HandleProfile returns the newest known profile for a pubkey.
func (d *Daemon) HandleProfile(pubkey [32]byte) (*proto.Envelope, error) {
	d.mu.Lock()
	hash, ok := d.profiles[pubkey]
	d.mu.Unlock()
	if !ok {
		return nil, store.ErrNotFound
	}
	return d.objects.Get(hash)
}
