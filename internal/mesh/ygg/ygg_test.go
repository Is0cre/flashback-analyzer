package ygg

import (
	"crypto/ed25519"
	"testing"
)

func TestNewRejectsMissingTransportKey(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("Yggdrasil-adaptern accepterade saknad transportnyckel")
	}
}

func TestPublicKeyIsIndependentValue(t *testing.T) {
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	n, err := New(Config{PrivateKey: private})
	if err != nil {
		t.Fatal(err)
	}
	defer n.Close()
	if len(n.PublicKey()) != ed25519.PublicKeySize {
		t.Fatal("Yggdrasil-noden saknar publik transportnyckel")
	}
}
