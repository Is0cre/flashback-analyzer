package gandr

import (
	"context"
	"strings"
	"testing"
)

func TestParseYggKeyRejectsWrongLength(t *testing.T) {
	if _, err := parseYggKey("abcd"); err == nil {
		t.Fatal("för kort nyckel accepterades")
	}
}

func TestParseYggKeyRejectsInvalidHex(t *testing.T) {
	notHex := strings.Repeat("zz", 32) // 64 chars, not valid hex
	if _, err := parseYggKey(notHex); err == nil {
		t.Fatal("ogiltig hex accepterades")
	}
}

func TestParseYggKeyAcceptsValid64CharHex(t *testing.T) {
	valid := strings.Repeat("ab", 32)
	key, err := parseYggKey(valid)
	if err != nil {
		t.Fatal(err)
	}
	if key[0] != 0xab || key[31] != 0xab {
		t.Fatalf("nyckeln avkodades fel: %x", key)
	}
	// Leading/trailing whitespace (e.g. from a pasted invite) must not
	// break parsing.
	if _, err := parseYggKey("  " + valid + "  "); err != nil {
		t.Fatalf("nyckel med omgivande whitespace avvisades: %v", err)
	}
}

func TestConnectPeerRequiresActiveSession(t *testing.T) {
	var nilSession *Session
	if err := nilSession.ConnectPeer(context.Background(), strings.Repeat("ab", 32)); err == nil {
		t.Fatal("nil-session tillät ConnectPeer")
	}
}

func TestConnectPeerRequiresDaemonConnection(t *testing.T) {
	// A session opened without a socket path (subsystem.Connect("")) has no
	// IPC client at all — the same "gandrd not running" offline mode used
	// elsewhere in this package's tests — so ConnectPeer must fail clearly
	// instead of nil-pointer-dereferencing into s.client.
	subsystem := NewAt(t.TempDir() + "/identity.key")
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	session, err := subsystem.Connect("")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if err := session.ConnectPeer(context.Background(), strings.Repeat("ab", 32)); err == nil {
		t.Fatal("ConnectPeer lyckades utan en ansluten gandrd-daemon")
	}
}

func TestConnectPeerValidatesKeyBeforeTouchingClient(t *testing.T) {
	subsystem := NewAt(t.TempDir() + "/identity.key")
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	session, err := subsystem.Connect("")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	err = session.ConnectPeer(context.Background(), "not-a-key")
	if err == nil || !strings.Contains(err.Error(), "hextecken") {
		t.Fatalf("förväntade ett tydligt fel om nyckelformatet, fick: %v", err)
	}
}
