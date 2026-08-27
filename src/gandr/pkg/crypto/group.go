package crypto

import (
	"errors"

	"golang.org/x/crypto/argon2"
)

const (
	groupArgonTime   = 3
	groupArgonMemory = 64 * 1024
	groupArgonLanes  = 4
)

// DeriveGroupKey computes a private group's message-encryption key
// directly from its password via Argon2id, using the group's own random
// id as the salt (already unique per group, already known to anyone who
// has the invite — a separate stored salt would add nothing). This is
// deliberately deterministic: the same (groupID, password) pair always
// derives the same key, on any device, with no prior local state and no
// separate key material to transmit or store. That's what makes an
// invite carrying just the id, name, and password (see
// internal/gandr.EncodeGroupInvite) enough on its own to actually join a
// group — an earlier design generated an independent random key and
// merely wrapped it with the password for local at-rest protection,
// which meant a password by itself was never sufficient to reach the
// same key as anyone else, and there was no way to grant a new member
// access without also transmitting that random key out of band.
func DeriveGroupKey(password []byte, groupID [32]byte) ([KeySize]byte, error) {
	if len(password) == 0 {
		return [KeySize]byte{}, errors.New("crypto: group password is empty")
	}
	return [KeySize]byte(argon2.IDKey(password, groupID[:], groupArgonTime, groupArgonMemory, groupArgonLanes, KeySize)), nil
}

// EncryptGroup encrypts one group message. The returned blob is nonce ||
// ciphertext and contains no plaintext metadata.
func EncryptGroup(groupKey [KeySize]byte, groupID [32]byte, plaintext []byte) ([]byte, error) {
	nonce, ciphertext, err := Encrypt(groupKey, plaintext, groupID[:])
	if err != nil {
		return nil, err
	}
	return append(nonce[:], ciphertext...), nil
}

func DecryptGroup(groupKey [KeySize]byte, groupID [32]byte, blob []byte) ([]byte, error) {
	if len(blob) < NonceSize+Overhead {
		return nil, errors.New("crypto: group ciphertext is too short")
	}
	var nonce [NonceSize]byte
	copy(nonce[:], blob[:NonceSize])
	return Decrypt(groupKey, nonce, blob[NonceSize:], groupID[:])
}
