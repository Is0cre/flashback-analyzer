package daemon

import (
	"context"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/gandr-net/gandr/pkg/crypto"
	"github.com/gandr-net/gandr/pkg/identity"
	"github.com/gandr-net/gandr/pkg/ipc"
	"github.com/gandr-net/gandr/pkg/network"
	"github.com/gandr-net/gandr/pkg/proto"
	"github.com/gandr-net/gandr/pkg/store"
)

// testDefaultOptions mirrors cmd/gandrd's defaultConfig(), translated to
// Options, so these tests exercise realistic settings rather than zero
// values.
func testDefaultOptions() Options {
	return Options{
		UserAgent:      "gandrd-test/0.1.0",
		Capabilities:   proto.CapChat | proto.CapFeed | proto.CapForum | proto.CapRelay,
		Relay:          true,
		MaxPeers:       200,
		DefaultTrust:   proto.TrustNeutral,
		MaxMessageAge:  604800,
		MaxPayloadSize: 65535,
		RateLimitRPM:   600,
	}
}

// testNode is one running daemon plus its plumbing, exposed over a real
// IPC socket exactly like cmd/gandrd wires it — these tests exercise the
// daemon the same way a standalone gandrd process would be used, not just
// the in-process API.
type testNode struct {
	daemon    *Daemon
	server    *ipc.Server
	transport *network.EmbeddedTransport
	socket    string
}

func startNode(t *testing.T, listen, peers []string, seeds []string) *testNode {
	t.Helper()
	dir := t.TempDir()
	opts := testDefaultOptions()

	var seedKeys [][]byte
	for _, s := range seeds {
		k, err := hex.DecodeString(s)
		if err != nil {
			t.Fatal(err)
		}
		seedKeys = append(seedKeys, k)
	}
	opts.Seeds = seedKeys

	id, err := identity.Generate("node")
	if err != nil {
		t.Fatal(err)
	}
	_, yggPriv, err := crypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	transport, err := network.NewEmbedded(network.EmbeddedConfig{
		PrivateKey: yggPriv,
		Listen:     listen,
		Peers:      peers,
	})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := store.Open(filepath.Join(dir, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(opts, id, transport, objects)
	if err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(dir, "gandr.sock")
	srv, err := ipc.Listen(socket, d)
	if err != nil {
		t.Fatal(err)
	}
	d.SetEventSink(srv)
	go d.RunLoops()
	t.Cleanup(func() {
		d.Stop()
		srv.Close()
	})
	return &testNode{daemon: d, server: srv, transport: transport, socket: socket}
}

// TestTwoDaemonsEndToEnd runs the full stack: two daemons federate over
// embedded Yggdrasil; a client posts a chat message through one daemon's
// Unix socket and another client receives it from the other daemon's
// socket.
func TestTwoDaemonsEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full-stack test in -short mode")
	}

	a := startNode(t, []string{"tcp://127.0.0.1:0"}, nil, nil)

	// B peers with A at the yggdrasil link layer and federates with A's
	// transport key as its seed.
	seed := hex.EncodeToString(a.transport.LocalAddr().YggKey)
	b := startNode(t, nil,
		[]string{fmt.Sprintf("tcp://%s", a.transport.ListenAddrs()[0])},
		[]string{seed},
	)

	// wait for federation to establish on both sides
	deadline := time.Now().Add(30 * time.Second)
	for a.daemon.table.Count() == 0 || b.daemon.table.Count() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("daemons did not federate within 30s")
		}
		time.Sleep(100 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// user clients, one per node, with their own identities
	userA, err := identity.Generate("aira")
	if err != nil {
		t.Fatal(err)
	}
	cliA, err := ipc.Dial(a.socket)
	if err != nil {
		t.Fatal(err)
	}
	defer cliA.Close()
	cliB, err := ipc.Dial(b.socket)
	if err != nil {
		t.Fatal(err)
	}
	defer cliB.Close()

	channel := [32]byte{0x6E}
	if err := cliB.Subscribe(ctx, channel); err != nil {
		t.Fatal(err)
	}

	// client A sends a chat to the channel through daemon A
	payload, err := proto.EncodePayload(&proto.ChatPayload{ChannelID: channel, Content: "across two daemons"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := proto.NewEnvelope(userA.PrivateKey, proto.MsgChat, channel, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := cliA.Send(ctx, env); err != nil {
		t.Fatalf("client send: %v", err)
	}

	// client B receives it from daemon B
	select {
	case got := <-cliB.Incoming():
		if got.ContentID() != env.ContentID() {
			t.Fatal("received wrong envelope")
		}
		chat := &proto.ChatPayload{}
		if err := proto.DecodePayload(got.Payload, chat); err != nil {
			t.Fatal(err)
		}
		if chat.Content != "across two daemons" {
			t.Fatal("content mismatch")
		}
	case <-ctx.Done():
		t.Fatal("message never crossed the federation")
	}

	// the object is now stored on both nodes and fetchable from B
	fetched, err := cliB.Fetch(ctx, env.ContentID())
	if err != nil {
		t.Fatalf("fetch from B: %v", err)
	}
	if fetched.ContentID() != env.ContentID() {
		t.Fatal("fetched envelope differs")
	}

	// peer lists are visible over IPC
	peers, err := cliA.PeerList(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 1 {
		t.Fatalf("peer list length = %d", len(peers))
	}

	// profiles propagate and are queryable from the remote node
	profPayload, err := proto.EncodePayload(&proto.ProfilePayload{DisplayName: "aira", UpdatedAt: time.Now().Unix()})
	if err != nil {
		t.Fatal(err)
	}
	profEnv, err := proto.NewEnvelope(userA.PrivateKey, proto.MsgProfile, proto.Broadcast, profPayload)
	if err != nil {
		t.Fatal(err)
	}
	if err := cliA.Send(ctx, profEnv); err != nil {
		t.Fatal(err)
	}
	profDeadline := time.Now().Add(15 * time.Second)
	for {
		if prof, err := cliB.Profile(ctx, userA.Pubkey()); err == nil {
			p := &proto.ProfilePayload{}
			if err := proto.DecodePayload(prof.Payload, p); err != nil || p.DisplayName != "aira" {
				t.Fatal("profile content mismatch")
			}
			break
		}
		if time.Now().After(profDeadline) {
			t.Fatal("profile never propagated")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// deletion: author deletes their chat message; both stores drop it
	delPayload, err := proto.EncodePayload(&proto.DeletePayload{TargetHash: hex.EncodeToString(func() []byte { h := env.ContentID(); return h[:] }())})
	if err != nil {
		t.Fatal(err)
	}
	delEnv, err := proto.NewEnvelope(userA.PrivateKey, proto.MsgDelete, proto.Broadcast, delPayload)
	if err != nil {
		t.Fatal(err)
	}
	if err := cliA.Send(ctx, delEnv); err != nil {
		t.Fatalf("delete send: %v", err)
	}
	delDeadline := time.Now().Add(15 * time.Second)
	for b.daemon.objects.Has(env.ContentID()) {
		if time.Now().After(delDeadline) {
			t.Fatal("delete never propagated to B")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if a.daemon.objects.Has(env.ContentID()) {
		t.Fatal("delete did not remove object on A")
	}
}

// TestDeleteRejectsForeignContent verifies nobody can delete content they
// did not author.
func TestDeleteRejectsForeignContent(t *testing.T) {
	a := startNode(t, []string{"tcp://127.0.0.1:0"}, nil, nil)

	author, err := identity.Generate("author")
	if err != nil {
		t.Fatal(err)
	}
	attacker, err := identity.Generate("attacker")
	if err != nil {
		t.Fatal(err)
	}

	payload, err := proto.EncodePayload(&proto.ChatPayload{Content: "mine"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := proto.NewEnvelope(author.PrivateKey, proto.MsgChat, proto.Broadcast, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.daemon.HandleSend(env); err != nil {
		t.Fatal(err)
	}

	hash := env.ContentID()
	delPayload, err := proto.EncodePayload(&proto.DeletePayload{TargetHash: hex.EncodeToString(hash[:])})
	if err != nil {
		t.Fatal(err)
	}
	delEnv, err := proto.NewEnvelope(attacker.PrivateKey, proto.MsgDelete, proto.Broadcast, delPayload)
	if err != nil {
		t.Fatal(err)
	}
	if err := a.daemon.HandleSend(delEnv); err == nil {
		t.Fatal("foreign delete accepted")
	}
	if !a.daemon.objects.Has(hash) {
		t.Fatal("object deleted by non-author")
	}
}

// TestRateLimit verifies the per-peer rate limiter.
func TestRateLimit(t *testing.T) {
	dir := t.TempDir()
	opts := testDefaultOptions()
	opts.RateLimitRPM = 5

	id, err := identity.Generate("node")
	if err != nil {
		t.Fatal(err)
	}
	_, yggPriv, err := crypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	transport, err := network.NewEmbedded(network.EmbeddedConfig{PrivateKey: yggPriv, Listen: []string{"tcp://127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := store.Open(filepath.Join(dir, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(opts, id, transport, objects)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)

	var peer [32]byte
	peer[0] = 1
	for i := 0; i < 5; i++ {
		if !d.allowRate(peer) {
			t.Fatalf("message %d blocked under the limit", i)
		}
	}
	if d.allowRate(peer) {
		t.Fatal("sixth message in a minute allowed at limit 5")
	}
	var other [32]byte
	other[0] = 2
	if !d.allowRate(other) {
		t.Fatal("rate limit leaked across peers")
	}
}

// TestPruneTrackedPeerStateEvictsStaleEntries pins a real unbounded-growth
// bug: d.profiles is keyed by a relayed message's signed sender (not a
// directly connected peer at all), so anyone able to relay flood traffic
// through the daemon can plant one entry per freshly generated identity
// forever. d.rates is keyed by directly-connected peer identity, which is
// bounded by table size at any instant but still accumulates one
// abandoned entry per identity that ever connected and moved on. Both
// need to be reclaimed once inactive, without touching entries that are
// still within their TTL.
func TestPruneTrackedPeerStateEvictsStaleEntries(t *testing.T) {
	dir := t.TempDir()
	opts := testDefaultOptions()
	id, err := identity.Generate("node")
	if err != nil {
		t.Fatal(err)
	}
	_, yggPriv, err := crypto.GenerateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	transport, err := network.NewEmbedded(network.EmbeddedConfig{PrivateKey: yggPriv, Listen: []string{"tcp://127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := store.Open(filepath.Join(dir, "objects"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := New(opts, id, transport, objects)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)

	var stalePeer, freshPeer, staleRatePeer, freshRatePeer [32]byte
	stalePeer[0], freshPeer[0], staleRatePeer[0], freshRatePeer[0] = 1, 2, 3, 4

	d.mu.Lock()
	d.profiles[stalePeer] = profileEntry{lastSeen: time.Now().Add(-profileTTL - time.Hour)}
	d.profiles[freshPeer] = profileEntry{lastSeen: time.Now()}
	d.rates[staleRatePeer] = &rateWindow{start: time.Now().Add(-rateWindowTTL - time.Minute)}
	d.rates[freshRatePeer] = &rateWindow{start: time.Now()}
	d.mu.Unlock()

	d.pruneTrackedPeerState()

	d.mu.Lock()
	defer d.mu.Unlock()
	if _, ok := d.profiles[stalePeer]; ok {
		t.Fatal("stale profile entry was not pruned")
	}
	if _, ok := d.profiles[freshPeer]; !ok {
		t.Fatal("fresh profile entry was incorrectly pruned")
	}
	if _, ok := d.rates[staleRatePeer]; ok {
		t.Fatal("stale rate window was not pruned")
	}
	if _, ok := d.rates[freshRatePeer]; !ok {
		t.Fatal("fresh rate window was incorrectly pruned")
	}
}
