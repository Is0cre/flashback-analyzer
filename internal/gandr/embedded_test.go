package gandr

import (
	"context"
	"encoding/hex"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gandrdaemon "github.com/gandr-net/gandr/pkg/daemon"
	gandridentity "github.com/gandr-net/gandr/pkg/identity"
	"github.com/gandr-net/gandr/pkg/ipc"
	"github.com/gandr-net/gandr/pkg/network"
	"github.com/gandr-net/gandr/pkg/proto"
	"github.com/gandr-net/gandr/pkg/store"
)

func TestEmbeddedTransportSeedIsDeterministicAndDistinctFromIdentity(t *testing.T) {
	id, err := gandridentity.Generate("test")
	if err != nil {
		t.Fatal(err)
	}
	a := embeddedTransportSeed(id.PrivateKey)
	b := embeddedTransportSeed(id.PrivateKey)
	if a.Equal(b) == false {
		t.Fatal("derived transport key changed between calls with the same identity — overlay address would not be stable across restarts")
	}
	if a.Equal(id.PrivateKey) {
		t.Fatal("derived transport key equals the Gandr identity key — transport key must be distinct from identity key")
	}
	other, err := gandridentity.Generate("other")
	if err != nil {
		t.Fatal(err)
	}
	if a.Equal(embeddedTransportSeed(other.PrivateKey)) {
		t.Fatal("two different identities derived the same transport key")
	}
}

func TestEmbeddedClientWantsFiltersByChannelSubscription(t *testing.T) {
	e := &embeddedClient{subs: make(map[[32]byte]struct{})}

	dm, err := proto.EncodePayload(&proto.ChatPayload{Content: "direct"})
	if err != nil {
		t.Fatal(err)
	}
	if !e.wants(&proto.Envelope{Type: proto.MsgChat, Payload: dm}) {
		t.Fatal("a direct message (no channel id) must always pass")
	}

	var channel [32]byte
	channel[0] = 0xAB
	channelPayload, err := proto.EncodePayload(&proto.ChatPayload{ChannelID: channel, Content: "in a channel"})
	if err != nil {
		t.Fatal(err)
	}
	env := &proto.Envelope{Type: proto.MsgChat, Payload: channelPayload}
	if e.wants(env) {
		t.Fatal("an unsubscribed channel message should not pass")
	}
	e.subs[channel] = struct{}{}
	if !e.wants(env) {
		t.Fatal("a subscribed channel message should pass")
	}
}

// rawDaemonPeer is a hand-built federation peer standing in for another
// BACKFLASH user (or the seed), used to prove ConnectEmbedded's daemon
// actually federates and exchanges messages over the wire — not just that
// it constructs without error. It deliberately does not go through
// ConnectEmbedded itself, so the test exercises that code path from only
// one side without needing it on both.
type rawDaemonPeer struct {
	daemon    *gandrdaemon.Daemon
	transport *network.EmbeddedTransport
	sink      *capturingSink
}

type capturingSink struct {
	push chan *proto.Envelope
}

func (c *capturingSink) Push(env *proto.Envelope)            { c.push <- env }
func (c *capturingSink) Delivered(env *proto.Envelope)       {}
func (c *capturingSink) PushPeerUpdate(peers []ipc.PeerInfo) {}

func startRawDaemonPeer(t *testing.T, listen []string) *rawDaemonPeer {
	t.Helper()
	id, err := gandridentity.Generate("peer")
	if err != nil {
		t.Fatal(err)
	}
	transport, err := network.NewEmbedded(network.EmbeddedConfig{PrivateKey: id.PrivateKey, Listen: listen})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := store.Open(filepath.Join(t.TempDir(), "objects"))
	if err != nil {
		t.Fatal(err)
	}
	d, err := gandrdaemon.New(gandrdaemon.Options{
		UserAgent:      "raw-test-peer/0.1.0",
		Capabilities:   proto.CapChat | proto.CapRelay,
		Relay:          true,
		MaxPeers:       8,
		DefaultTrust:   proto.TrustNeutral,
		MaxMessageAge:  86400,
		MaxPayloadSize: 65535,
		RateLimitRPM:   600,
	}, id, transport, objects)
	if err != nil {
		t.Fatal(err)
	}
	sink := &capturingSink{push: make(chan *proto.Envelope, 8)}
	d.SetEventSink(sink)
	go d.RunLoops()
	t.Cleanup(d.Stop)
	return &rawDaemonPeer{daemon: d, transport: transport, sink: sink}
}

// TestConnectEmbeddedFederatesAndExchangesAChatMessage is the actual
// proof this works: a real Subsystem/Session built the same way
// internal/tui/app.go's connectGandr builds one, federating with another
// live peer and exchanging a signed chat message — not a mock.
func TestConnectEmbeddedFederatesAndExchangesAChatMessage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping embedded federation test in -short mode")
	}

	peer := startRawDaemonPeer(t, []string{"tcp://127.0.0.1:0"})
	seedKey := hex.EncodeToString(peer.transport.LocalAddr().YggKey)

	subsystem := NewAt(filepath.Join(t.TempDir(), "identity.key"))
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}

	session, err := subsystem.ConnectEmbedded(EmbeddedOptions{
		SeedYggdrasilKey: seedKey,
		BootstrapPeers:   []string{"tcp://" + peer.transport.ListenAddrs()[0]},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if !session.Online() {
		t.Fatal("an embedded session should always report Online (it has a real client, unlike the pure-offline fallback)")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	channels, err := session.Join(ctx, "general")
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) == 0 {
		t.Fatal("joining a channel returned none")
	}
	channelID := channels[0].ID

	// wait for the embedded daemon's seed loop to federate
	deadline := time.Now().Add(20 * time.Second)
	for {
		peers, err := session.Peers(ctx)
		if err == nil && len(peers) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("embedded daemon never federated with the seed")
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := session.SendChannel(ctx, channelID, "hello from an embedded client"); err != nil {
		t.Fatalf("SendChannel: %v", err)
	}

	select {
	case got := <-peer.sink.push:
		chat := &proto.ChatPayload{}
		if err := proto.DecodePayload(got.Payload, chat); err != nil {
			t.Fatal(err)
		}
		if chat.Content != "hello from an embedded client" {
			t.Fatalf("content mismatch: %q", chat.Content)
		}
	case <-ctx.Done():
		t.Fatal("the seed peer never received the embedded client's message")
	}
}

// TestPrivateGroupSharesAcrossTwoIndependentEmbeddedSessions is the real
// proof for "share secret chats": two separate identities, two separate
// local databases, federating only through a relay — the same topology
// as two strangers both talking to the public seed. Alice creates a
// group and sends a message; Bob, who has never touched this group or
// this device before, joins using only the invite string (which
// contains no key material — see gandrcrypto.DeriveGroupKey) and reads
// Alice's message. The relay in between only ever handles an opaque
// ciphertext blob under a random channel id.
func TestPrivateGroupSharesAcrossTwoIndependentEmbeddedSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full-stack test in -short mode")
	}

	hub := startRawDaemonPeer(t, []string{"tcp://127.0.0.1:0"})
	hubKey := hex.EncodeToString(hub.transport.LocalAddr().YggKey)
	hubAddr := "tcp://" + hub.transport.ListenAddrs()[0]

	aliceSubsystem := NewAt(filepath.Join(t.TempDir(), "alice", "identity.key"))
	if err := aliceSubsystem.Create("alices-losenord"); err != nil {
		t.Fatal(err)
	}
	alice, err := aliceSubsystem.ConnectEmbedded(EmbeddedOptions{
		SeedYggdrasilKey: hubKey,
		BootstrapPeers:   []string{hubAddr},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer alice.Close()

	bobSubsystem := NewAt(filepath.Join(t.TempDir(), "bob", "identity.key"))
	if err := bobSubsystem.Create("bobs-helt-egna-losenord"); err != nil {
		t.Fatal(err)
	}
	bob, err := bobSubsystem.ConnectEmbedded(EmbeddedOptions{
		SeedYggdrasilKey: hubKey,
		BootstrapPeers:   []string{hubAddr},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer bob.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// wait for both to federate with the hub
	for _, s := range []*Session{alice, bob} {
		deadline := time.Now().Add(20 * time.Second)
		for {
			peers, err := s.Peers(ctx)
			if err == nil && len(peers) == 1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("a session never federated with the hub")
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	group, err := alice.CreatePrivateGroup("Hemligt möte", "gruppens-losenord")
	if err != nil {
		t.Fatal(err)
	}
	invite, err := EncodeGroupInvite(group.ID, group.Name, "gruppens-losenord")
	if err != nil {
		t.Fatal(err)
	}

	// Bob has done nothing with this group before this exact call — no
	// local row, no cached key, nothing — proving the invite alone is
	// sufficient.
	id, name, password, err := DecodeGroupInvite(invite)
	if err != nil {
		t.Fatal(err)
	}
	if err := bob.UnlockPrivateGroup(id, password, name); err != nil {
		t.Fatalf("bob kunde inte gå med i gruppen via inbjudan: %v", err)
	}

	if _, err := alice.SendPrivateGroup(ctx, group.ID, "mötet är imorgon 18:00"); err != nil {
		t.Fatalf("alice kunde inte skicka gruppmeddelandet: %v", err)
	}

	select {
	case env := <-bob.Incoming():
		msg, ok := bob.DecryptGroupMessage(env)
		if !ok {
			t.Fatal("bob kunde inte dekryptera alices gruppmeddelande")
		}
		if msg.Content != "mötet är imorgon 18:00" {
			t.Fatalf("fel innehåll: %q", msg.Content)
		}
		if msg.GroupID != group.ID {
			t.Fatal("meddelandet dök upp under fel grupp-id")
		}
	case <-ctx.Done():
		t.Fatal("bob mottog aldrig alices gruppmeddelande")
	}

	// The relay only ever saw an opaque blob under a random channel id —
	// never plaintext, never the group name or password.
	select {
	case env := <-hub.sink.push:
		payload := &proto.ChatPayload{}
		if err := proto.DecodePayload(env.Payload, payload); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(payload.Content, "mötet") || strings.Contains(payload.Content, "18:00") {
			t.Fatal("relayen såg klartext — gruppmeddelandet krypterades inte korrekt på tråden")
		}
	default:
		t.Fatal("relayen fick aldrig pushen (bytt kanal-id mellan de två kontrollerna?)")
	}
}

// TestEnsureDefaultChannelsResubscribesAfterReconnect pins the actual bug
// behind "messages don't route": a fresh transport session always starts
// with zero subscriptions, regardless of what channels are already known
// from local storage. EnsureDefaultChannels used to return early once
// channels existed locally, silently leaving every session after the
// very first one subscribed to nothing — sends still worked, nothing was
// ever received. This simulates exactly that: connect, join, close (an
// app restart), reconnect with the same identity, and confirm a message
// from someone else still arrives on the new connection.
func TestEnsureDefaultChannelsResubscribesAfterReconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full-stack test in -short mode")
	}

	hub := startRawDaemonPeer(t, []string{"tcp://127.0.0.1:0"})
	hubKey := hex.EncodeToString(hub.transport.LocalAddr().YggKey)
	hubAddr := "tcp://" + hub.transport.ListenAddrs()[0]

	identityPath := filepath.Join(t.TempDir(), "identity.key")
	first := NewAt(identityPath)
	if err := first.Create("password"); err != nil {
		t.Fatal(err)
	}
	firstSession, err := first.ConnectEmbedded(EmbeddedOptions{
		SeedYggdrasilKey: hubKey,
		BootstrapPeers:   []string{hubAddr},
	})
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := firstSession.EnsureDefaultChannels(ctx); err != nil {
		t.Fatal(err)
	}
	if err := firstSession.Close(); err != nil {
		t.Fatal(err)
	}

	// Simulate the app restarting: a fresh Subsystem unlocking the exact
	// same identity file, and a fresh embedded daemon with its own zero
	// subscription state — the local DB already knows about the default
	// channels from the first session, this connection does not.
	second := NewAt(identityPath)
	if err := second.Unlock("password"); err != nil {
		t.Fatal(err)
	}
	secondSession, err := second.ConnectEmbedded(EmbeddedOptions{
		SeedYggdrasilKey: hubKey,
		BootstrapPeers:   []string{hubAddr},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer secondSession.Close()

	channels, err := secondSession.EnsureDefaultChannels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) == 0 {
		t.Fatal("channels should already have been known locally from the first session")
	}

	deadline := time.Now().Add(20 * time.Second)
	for {
		peers, err := secondSession.Peers(ctx)
		if err == nil && len(peers) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second session never federated with the hub")
		}
		time.Sleep(100 * time.Millisecond)
	}

	// The hub relaying a message from some other, unrelated user — this
	// only reaches secondSession's Incoming() if EnsureDefaultChannels
	// actually resubscribed on this fresh connection.
	sender, err := gandridentity.Generate("annan-anvandare")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := proto.EncodePayload(&proto.ChatPayload{ChannelID: channels[0].ID, Content: "message after a restart"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := proto.NewEnvelope(sender.PrivateKey, proto.MsgChat, channels[0].ID, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := hub.daemon.HandleSend(env); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-secondSession.Incoming():
		msg, err := DecodeChat(got)
		if err != nil {
			t.Fatal(err)
		}
		if msg.Content != "message after a restart" {
			t.Fatalf("fel innehåll: %q", msg.Content)
		}
	case <-ctx.Done():
		t.Fatal("the reconnected session never received the message — EnsureDefaultChannels did not resubscribe")
	}
}
