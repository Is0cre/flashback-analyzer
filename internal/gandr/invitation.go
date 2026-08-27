package gandr

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const invitationVersion = 1

type contactInvitation struct {
	Version   int    `json:"v"`
	PublicKey string `json:"p"`
	CreatedAt int64  `json:"c"`
	ExpiresAt int64  `json:"e"`
	Signature string `json:"s,omitempty"`
}

func invitationPayload(inv contactInvitation) []byte {
	inv.Signature = ""
	b, _ := json.Marshal(inv)
	return b
}

// CreateInvitation returns a short-lived, signed contact invitation. It is
// safe to pass through BACKFLASH as an opaque string, but BACKFLASH never
// stores or interprets it as a public cache object.
func (s *Session) CreateInvitation() (string, error) {
	if s == nil || s.id == nil {
		return "", errors.New("E2E-CHATT-sessionen är inte aktiv")
	}
	now := time.Now().Unix()
	inv := contactInvitation{
		Version:   invitationVersion,
		PublicKey: hex.EncodeToString(s.id.PublicKey),
		CreatedAt: now,
		ExpiresAt: now + int64((7*24*time.Hour)/time.Second),
	}
	signature := ed25519.Sign(s.id.PrivateKey, invitationPayload(inv))
	inv.Signature = hex.EncodeToString(signature)
	b, err := json.Marshal(inv)
	if err != nil {
		return "", err
	}
	return "BFI1." + base64.RawURLEncoding.EncodeToString(b), nil
}

// AcceptInvitation verifies a signed invitation before adding the public key
// to the encrypted local contact database. The recipient chooses the local
// petname; the sender cannot force a name into the recipient's client.
func (s *Session) AcceptInvitation(token, name string) ([32]byte, error) {
	if s == nil || s.db == nil {
		return [32]byte{}, errors.New("E2E-CHATT-sessionen är inte aktiv")
	}
	if !strings.HasPrefix(token, "BFI1.") {
		return [32]byte{}, errors.New("ogiltig E2E-CHATT-inbjudan")
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "BFI1."))
	if err != nil {
		return [32]byte{}, errors.New("inbjudan kunde inte avkodas")
	}
	var inv contactInvitation
	if err := json.Unmarshal(b, &inv); err != nil || inv.Version != invitationVersion {
		return [32]byte{}, errors.New("inbjudan har ogiltigt format")
	}
	if inv.ExpiresAt < time.Now().Unix() || inv.CreatedAt > time.Now().Add(2*time.Minute).Unix() {
		return [32]byte{}, errors.New("inbjudan har gått ut eller har felaktig tid")
	}
	pub, err := hex.DecodeString(inv.PublicKey)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return [32]byte{}, errors.New("inbjudan innehåller ingen giltig publik nyckel")
	}
	sig, err := hex.DecodeString(inv.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize || !ed25519.Verify(pub, invitationPayload(inv), sig) {
		return [32]byte{}, errors.New("inbjudans signatur kunde inte verifieras")
	}
	var pubkey [32]byte
	copy(pubkey[:], pub)
	if strings.TrimSpace(name) == "" {
		name = "~" + hex.EncodeToString(pub[:4])
	}
	if err := s.AddContact(pubkey, name, "via signerad E2E-CHATT-inbjudan"); err != nil {
		return [32]byte{}, fmt.Errorf("kontakten kunde inte sparas: %w", err)
	}
	return pubkey, nil
}
