package mesh

import (
	"path/filepath"
	"testing"
	"time"
)

func TestObjectStoreIndexesResourcesAcrossRestart(t *testing.T) {
	root := filepath.Join(t.TempDir(), "objects")
	store, err := OpenObjectStore(root)
	if err != nil {
		t.Fatal(err)
	}
	want := NewObject(ForumSnapshot, "flashback", "forum:f123", time.Now(), []byte("snapshot"), OriginVerified)
	if err := store.Put(want); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenObjectStore(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Find("flashback", "forum:f123", ForumSnapshot)
	if err != nil {
		t.Fatal(err)
	}
	if got.HashString() != want.HashString() {
		t.Fatalf("index pekade på fel objekt: %s != %s", got.HashString(), want.HashString())
	}
}
