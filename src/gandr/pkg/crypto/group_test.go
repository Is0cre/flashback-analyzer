package crypto

import (
	"bytes"
	"testing"
)

func TestGroupPasswordWrapAndMessageEncryption(t *testing.T) {
	groupID := Digest([]byte("group"))
	key, err := NewGroupKey()
	if err != nil {
		t.Fatal(err)
	}
	salt, err := NewGroupSalt()
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := WrapGroupKey([]byte("hemligt"), salt, groupID, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnwrapGroupKey([]byte("fel"), salt, groupID, wrapped); err == nil {
		t.Fatal("fel lösenord accepterades")
	}
	opened, err := UnwrapGroupKey([]byte("hemligt"), salt, groupID, wrapped)
	if err != nil || opened != key {
		t.Fatalf("kunde inte öppna gruppnyckel: %v", err)
	}
	blob, err := EncryptGroup(key, groupID, []byte("privat gruppmeddelande"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := DecryptGroup(key, groupID, blob)
	if err != nil || !bytes.Equal(plain, []byte("privat gruppmeddelande")) {
		t.Fatalf("kunde inte dekryptera gruppmeddelande: %v", err)
	}
}
