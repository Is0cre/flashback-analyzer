package ygg

import (
	"context"
	"crypto/ed25519"
	"path/filepath"
	"testing"
	"time"

	"github.com/backflash-cli/backflash/internal/mesh"
)

func TestTwoNodeCacheTransferOverYggdrasil(t *testing.T) {
	if testing.Short() {
		t.Skip("Yggdrasil integrationstest hoppas över i kort läge")
	}
	_, keyA, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_, keyB, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	a, err := New(Config{PrivateKey: keyA, Listen: []string{"tcp://127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := New(Config{PrivateKey: keyB, Listen: []string{"tcp://127.0.0.1:0"}, Peers: []string{"tcp://" + a.ListenAddrs()[0]}, PeerKey: a.PublicKey()})
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	aStore, err := mesh.OpenObjectStore(filepath.Join(t.TempDir(), "a"))
	if err != nil {
		t.Fatal(err)
	}
	bStore, err := mesh.OpenObjectStore(filepath.Join(t.TempDir(), "b"))
	if err != nil {
		t.Fatal(err)
	}
	object := mesh.NewObject(mesh.ThreadPageSnapshot, "flashback", "t-ygg:1", time.Now(), []byte("peer-cache"), mesh.OriginVerified)
	if err := aStore.Put(object); err != nil {
		t.Fatal(err)
	}
	cacheA := &mesh.Node{Store: aStore}
	cacheB := &mesh.Node{Store: bStore, Peer: b}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() { serveDone <- a.Serve(ctx, cacheA) }()

	deadline := time.Now().Add(10 * time.Second)
	for b.PeerCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if b.PeerCount() == 0 {
		t.Fatal("Yggdrasil-peer anslöt inte")
	}
	fetched, err := cacheB.Get(object.HashString())
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Provenance != mesh.PeerOnly {
		t.Fatalf("oväntad provenans: %s", fetched.Provenance)
	}
	if _, err := bStore.Get(object.HashString()); err != nil {
		t.Fatal(err)
	}
	cancel()
	_ = a.Close()
	_ = <-serveDone
}
