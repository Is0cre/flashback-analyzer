package mesh

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func TestPeerObjectTransferMarksPeerOnlyAndPersists(t *testing.T) {
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
	o := NewObject(ThreadPageSnapshot, "flashback", "t123:1", time.Unix(1, 0), []byte("public page"), OriginVerified)
	if err := aStore.Put(o); err != nil {
		t.Fatal(err)
	}
	b.Peer = HandlerTransport{Handler: a.Serve}
	got, err := b.Get(o.HashString())
	if err != nil {
		t.Fatal(err)
	}
	if got.Provenance != PeerOnly {
		t.Fatalf("provenans blev %q, väntade PEER_ONLY", got.Provenance)
	}
	if _, err := bStore.Get(o.HashString()); err != nil {
		t.Fatalf("peerobjekt sparades inte: %v", err)
	}
}

func TestPeerObjectWithWrongHashIsRejected(t *testing.T) {
	store, err := OpenObjectStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	n := &Node{Store: store, Peer: HandlerTransport{Handler: func(Message) (Message, error) {
		wrong := NewObject(ThreadPageSnapshot, "flashback", "other", time.Now(), []byte("wrong"), OriginVerified)
		b, err := marshalForTest(wrong)
		return Message{Type: Object, Hash: "not-the-requested-hash", Body: b}, err
	}}}
	if _, err := n.Get("requested-hash"); err == nil {
		t.Fatal("felaktig peer-hash accepterades")
	}
}

func TestObjectStoreRejectsUnsafeHash(t *testing.T) {
	store, err := OpenObjectStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("../../etc/passwd"); err == nil {
		t.Fatal("osäker meshadress accepterades")
	}
}

func marshalForTest(o CacheObject) ([]byte, error) { return json.Marshal(o) }
