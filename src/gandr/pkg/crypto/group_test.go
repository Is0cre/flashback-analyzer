package crypto

import (
	"bytes"
	"testing"
)

func TestGroupKeyDerivationIsDeterministicAndMessageEncryptionRoundTrips(t *testing.T) {
	groupID := Digest([]byte("group"))

	key, err := DeriveGroupKey([]byte("hemligt"), groupID)
	if err != nil {
		t.Fatal(err)
	}
	// The whole point: anyone who knows (groupID, password) reaches the
	// same key independently, with no prior local state — that's what
	// makes an invite carrying just those two things enough to join.
	again, err := DeriveGroupKey([]byte("hemligt"), groupID)
	if err != nil || again != key {
		t.Fatal("samma (grupp-id, lösenord) gav olika nycklar mellan två anrop")
	}
	wrongPassword, err := DeriveGroupKey([]byte("fel"), groupID)
	if err != nil {
		t.Fatal(err)
	}
	if wrongPassword == key {
		t.Fatal("olika lösenord gav samma nyckel")
	}
	otherGroup := Digest([]byte("annan grupp"))
	wrongGroup, err := DeriveGroupKey([]byte("hemligt"), otherGroup)
	if err != nil {
		t.Fatal(err)
	}
	if wrongGroup == key {
		t.Fatal("samma lösenord i en annan grupp gav samma nyckel")
	}
	if _, err := DeriveGroupKey(nil, groupID); err == nil {
		t.Fatal("tomt lösenord accepterades")
	}

	blob, err := EncryptGroup(key, groupID, []byte("privat gruppmeddelande"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := DecryptGroup(key, groupID, blob)
	if err != nil || !bytes.Equal(plain, []byte("privat gruppmeddelande")) {
		t.Fatalf("kunde inte dekryptera gruppmeddelande: %v", err)
	}
	if _, err := DecryptGroup(wrongPassword, groupID, blob); err == nil {
		t.Fatal("dekrypterade med fel lösenords-härledda nyckel")
	}
}
