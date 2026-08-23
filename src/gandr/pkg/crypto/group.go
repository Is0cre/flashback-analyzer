package crypto

import (
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
)

const (
	GroupSaltSize    = 16
	groupArgonTime   = 3
	groupArgonMemory = 64 * 1024
	groupArgonLanes  = 4
)

// NewGroupKey creates an independent symmetric key. It is never derived from
// a GANDR identity or a BACKFLASH mesh identity.
func NewGroupKey() ([KeySize]byte, error) {
	var key [KeySize]byte
	if _, err := rand.Read(key[:]); err != nil {
		return key, fmt.Errorf("crypto: generating group key: %w", err)
	}
	return key, nil
}

// NewGroupSalt creates the public salt used for password-based group key
// wrapping. The salt is not secret.
func NewGroupSalt() ([GroupSaltSize]byte, error) {
	var salt [GroupSaltSize]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return salt, fmt.Errorf("crypto: generating group salt: %w", err)
	}
	return salt, nil
}

func deriveGroupPasswordKey(password, salt []byte) [KeySize]byte {
	return [KeySize]byte(argon2.IDKey(password, salt, groupArgonTime, groupArgonMemory, groupArgonLanes, KeySize))
}

// WrapGroupKey protects a random group key with a password. The returned value
// contains nonce || ciphertext and is safe to store as an opaque blob.
func WrapGroupKey(password []byte, salt [GroupSaltSize]byte, groupID [32]byte, groupKey [KeySize]byte) ([]byte, error) {
	if len(password) == 0 {
		return nil, errors.New("crypto: group password is empty")
	}
	kek := deriveGroupPasswordKey(password, salt[:])
	nonce, ciphertext, err := Encrypt(kek, groupKey[:], groupID[:])
	if err != nil {
		return nil, err
	}
	return append(nonce[:], ciphertext...), nil
}

// UnwrapGroupKey reverses WrapGroupKey and rejects a wrong password.
func UnwrapGroupKey(password []byte, salt [GroupSaltSize]byte, groupID [32]byte, wrapped []byte) ([KeySize]byte, error) {
	var groupKey [KeySize]byte
	if len(password) == 0 {
		return groupKey, errors.New("crypto: group password is empty")
	}
	if len(wrapped) < NonceSize+Overhead {
		return groupKey, errors.New("crypto: wrapped group key is too short")
	}
	var nonce [NonceSize]byte
	copy(nonce[:], wrapped[:NonceSize])
	kek := deriveGroupPasswordKey(password, salt[:])
	plain, err := Decrypt(kek, nonce, wrapped[NonceSize:], groupID[:])
	if err != nil {
		return groupKey, err
	}
	if len(plain) != KeySize {
		return groupKey, errors.New("crypto: invalid group key")
	}
	copy(groupKey[:], plain)
	return groupKey, nil
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
