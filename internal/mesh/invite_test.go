package mesh

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestEncodeInviteContainsOnlyPublicConnectionData(t *testing.T) {
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	invite, err := EncodeInvite(private.Public().(ed25519.PublicKey), []string{"tcp://example.test:4242"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(invite, "BFM1.") {
		t.Fatalf("oväntat inbjudningsformat: %q", invite)
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(invite, "BFM1."))
	if err != nil {
		t.Fatal(err)
	}
	var parsed NetworkInvite
	if err := json.Unmarshal(payload, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Network != "backflash-public-cache" || len(parsed.Endpoints) != 1 || len(parsed.PeerKey) != 64 {
		t.Fatalf("felaktig inbjudan: %+v", parsed)
	}
	if strings.Contains(string(payload), string(private)) {
		t.Fatal("inbjudan innehåller privat nyckel")
	}
}
