package runtime

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net"
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

// TestRuntimeStartsWithBootstrapPeersButNoPeerKey pins the exact deployment
// this used to break: a seed/bootstrap node given general Yggdrasil
// transport peers (needed just to reach the overlay) but no PeerKey (which
// only matters for syncing content from one specific other BACKFLASH-cache
// node — orthogonal, and optional). Start() must not reject this; the
// downstream ygg.Node methods that actually need PeerKey already fail
// gracefully per-call when it's unset.
func TestRuntimeStartsWithBootstrapPeersButNoPeerKey(t *testing.T) {
	cfg := mesh.Config{
		Enabled:      true,
		IdentityPath: filepath.Join(t.TempDir(), "identity.key"),
		ObjectPath:   filepath.Join(t.TempDir(), "objects"),
		Peers:        []string{"tcp://127.0.0.1:1"}, // registered for background dialing, never expected to connect
	}
	r := New(cfg)
	if err := r.Start(context.Background()); err != nil {
		t.Fatalf("Start() med Peers men utan PeerKey gav fel: %v", err)
	}
	defer r.Stop()
	if r.State() != Running && r.State() != Degraded {
		t.Fatalf("status blev %s, väntade RUNNING eller DEGRADED", r.State())
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
	fetched, err := b.GetResource(context.Background(), "flashback", "t-runtime:1", mesh.ThreadPageSnapshot)
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

func TestLocalObjectIsOpportunisticallyReplicatedToPeer(t *testing.T) {
	root := t.TempDir()
	aAddr := freeMeshAddress(t)
	bAddr := freeMeshAddress(t)
	aIdentity, err := mesh.LoadOrCreateIdentity(filepath.Join(root, "a.key"))
	if err != nil {
		t.Fatal(err)
	}
	bIdentity, err := mesh.LoadOrCreateIdentity(filepath.Join(root, "b.key"))
	if err != nil {
		t.Fatal(err)
	}
	a := New(mesh.Config{Enabled: true, ShareCache: true, IdentityPath: filepath.Join(root, "a.key"), ObjectPath: filepath.Join(root, "a-objects"), Listen: []string{"tcp://" + aAddr}, Peers: []string{"tcp://" + bAddr}, PeerKey: bIdentity.Public().(ed25519.PublicKey)})
	b := New(mesh.Config{Enabled: true, ShareCache: false, IdentityPath: filepath.Join(root, "b.key"), ObjectPath: filepath.Join(root, "b-objects"), Listen: []string{"tcp://" + bAddr}, Peers: []string{"tcp://" + aAddr}, PeerKey: aIdentity.Public().(ed25519.PublicKey)})
	if err := a.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer a.Stop()
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer b.Stop()
	waitFor(t, 10*time.Second, func() bool { return b.Snapshot().Peers > 0 })
	object := mesh.NewObject(mesh.ThreadPageSnapshot, "flashback", "t-publish:1", time.Now(), []byte("opportunistic"), mesh.OriginVerified)
	if err := a.PutLocal(object); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := b.Get(object.HashString()); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	fetched, err := b.Get(object.HashString())
	if err != nil {
		t.Fatalf("HAVE-replikering misslyckades: %v A=%+v B=%+v", err, a.Snapshot(), b.Snapshot())
	}
	if fetched.Provenance != mesh.PeerOnly {
		t.Fatalf("annonserat objekt fick provenance %s, väntade PEER_ONLY", fetched.Provenance)
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

func freeMeshAddress(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}
