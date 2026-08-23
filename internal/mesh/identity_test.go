package mesh

import (
	"crypto/ed25519"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestIdentityPersistsAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mesh", "identity.key")
	first, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("meshidentiteten ändrades efter omladdning")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("meshidentiteten har för öppna rättigheter: %o", info.Mode().Perm())
	}
}

func TestPublicKeyHexDoesNotExposePrivateSeed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity.key")
	private, err := LoadOrCreateIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	public := PublicKeyHex(private)
	if len(public) != ed25519.PublicKeySize*2 {
		t.Fatalf("publik nyckel har fel längd: %d", len(public))
	}
	if public == hex.EncodeToString(private) {
		t.Fatal("publika nyckeln får inte vara privata nyckeln")
	}
	loaded, err := LoadIdentity(path)
	if err != nil {
		t.Fatal(err)
	}
	if PublicKeyHex(loaded) != public {
		t.Fatal("publika nyckeln ändrades vid omladdning")
	}
}
