package mesh

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

func TestConnTransportTransfersObjectOverBidirectionalConnection(t *testing.T) {
	aStore, err := OpenObjectStore(filepath.Join(t.TempDir(), "a"))
	if err != nil {
		t.Fatal(err)
	}
	bStore, err := OpenObjectStore(filepath.Join(t.TempDir(), "b"))
	if err != nil {
		t.Fatal(err)
	}
	a := &Node{Store: aStore}
	b := &Node{Store: bStore}
	o := NewObject(ThreadPageSnapshot, "flashback", "t999:2", time.Now(), []byte("svar från peer"), OriginVerified)
	if err := aStore.Put(o); err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	serverDone := make(chan error, 1)
	go func() { serverDone <- ServeConn(left, a) }()
	b.Peer = NewConnTransport(right)
	got, err := b.Get(o.HashString())
	if err != nil {
		t.Fatal(err)
	}
	if got.Provenance != PeerOnly {
		t.Fatalf("förväntade PEER_ONLY, fick %q", got.Provenance)
	}
	_ = right.Close()
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}
