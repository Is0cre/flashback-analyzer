package runtime

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/backflash-cli/backflash/internal/mesh"
)

func TestDisabledRuntimeDoesNotCreateIdentity(t *testing.T) {
	identity := filepath.Join(t.TempDir(), "never-created.key")
	r := New(mesh.Config{IdentityPath: identity, ObjectPath: filepath.Join(t.TempDir(), "objects")})
	if err := r.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if r.State() != Disabled {
		t.Fatalf("status blev %s, väntade DISABLED", r.State())
	}
	if _, err := os.Stat(identity); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("disabled runtime skapade identitet: %v", err)
	}
}

func TestRuntimeIdentityAndObjectsSurviveRestart(t *testing.T) {
	identity := filepath.Join(t.TempDir(), "identity.key")
	objects := filepath.Join(t.TempDir(), "objects")
	cfg := mesh.Config{Enabled: true, IdentityPath: identity, ObjectPath: objects}
	first := New(cfg)
	if err := first.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	object := mesh.NewObject(mesh.ThreadPageSnapshot, "flashback", "t-restart:1", time.Now(), []byte("cached"), mesh.OriginVerified)
	if err := first.PutLocal(object); err != nil {
		t.Fatal(err)
	}
	fingerprint := first.Snapshot().Identity
	if err := first.Stop(); err != nil {
		t.Fatal(err)
	}
	second := New(cfg)
	if err := second.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer second.Stop()
	if second.Snapshot().Identity != fingerprint {
		t.Fatalf("identiteten ändrades: %s -> %s", fingerprint, second.Snapshot().Identity)
	}
	if second.Snapshot().Objects != 1 {
		t.Fatalf("cacheobjekt överlevde inte omstart: %d", second.Snapshot().Objects)
	}
}

func TestTwoRuntimeNodesTransferAndReadAfterPeerStops(t *testing.T) {
	root := t.TempDir()
	a := New(mesh.Config{Enabled: true, ShareCache: true, IdentityPath: filepath.Join(root, "a.key"), ObjectPath: filepath.Join(root, "a-objects"), Listen: []string{"tcp://127.0.0.1:0"}})
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer a.Stop()
	object := mesh.NewObject(mesh.ThreadPageSnapshot, "flashback", "t-runtime:1", time.Now(), []byte("runtime cache"), mesh.OriginVerified)
	if err := a.PutLocal(object); err != nil {
		t.Fatal(err)
	}
	b := New(mesh.Config{Enabled: true, ShareCache: false, IdentityPath: filepath.Join(root, "b.key"), ObjectPath: filepath.Join(root, "b-objects"), Listen: []string{"tcp://127.0.0.1:0"}, Peers: []string{"tcp://" + a.ListenAddrs()[0]}, PeerKey: a.PublicKey()})
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer b.Stop()
	waitFor(t, 10*time.Second, func() bool { return b.Snapshot().Peers > 0 })
	if snapshot := b.Snapshot(); snapshot.State == Error {
		t.Fatalf("B gick till ERROR före hämtning: %+v", snapshot)
	}
	fetched, err := b.GetContext(context.Background(), object.HashString())
	if err != nil {
		t.Fatal(err)
	}
	if fetched.Provenance != mesh.PeerOnly {
		t.Fatalf("förväntade PEER_ONLY, fick %s", fetched.Provenance)
	}
	if a.Snapshot().ObjectsServed == 0 || b.Snapshot().ObjectsRecv == 0 {
		t.Fatalf("peerstatistik saknas: A=%+v B=%+v", a.Snapshot(), b.Snapshot())
	}
	if err := a.Stop(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Get(object.HashString()); err != nil {
		t.Fatalf("lokal kopia fungerar inte efter peerstopp: %v", err)
	}
}

func TestShareCacheFalseDoesNotServePeerObjects(t *testing.T) {
	root := t.TempDir()
	a := New(mesh.Config{Enabled: true, ShareCache: false, IdentityPath: filepath.Join(root, "a.key"), ObjectPath: filepath.Join(root, "a-objects"), Listen: []string{"tcp://127.0.0.1:0"}})
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer a.Stop()
	object := mesh.NewObject(mesh.ThreadPageSnapshot, "flashback", "t-share:1", time.Now(), []byte("not shared"), mesh.OriginVerified)
	if err := a.PutLocal(object); err != nil {
		t.Fatal(err)
	}
	b := New(mesh.Config{Enabled: true, IdentityPath: filepath.Join(root, "b.key"), ObjectPath: filepath.Join(root, "b-objects"), Listen: []string{"tcp://127.0.0.1:0"}, Peers: []string{"tcp://" + a.ListenAddrs()[0]}, PeerKey: a.PublicKey()})
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer b.Stop()
	waitFor(t, 10*time.Second, func() bool { return b.Snapshot().Peers > 0 })
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	if _, err := b.GetContext(ctx, object.HashString()); err == nil {
		t.Fatal("share_cache=false serverade ett cacheobjekt")
	}
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !condition() {
		t.Fatal("villkor uppnåddes inte inom timeout")
	}
}
