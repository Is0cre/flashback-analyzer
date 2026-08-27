package gandr

import (
	"context"
	"encoding/hex"
	"path/filepath"
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
