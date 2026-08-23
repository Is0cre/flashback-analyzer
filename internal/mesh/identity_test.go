package mesh

import (
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
