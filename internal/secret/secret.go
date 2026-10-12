// Package secret implements envelope encryption for credentials stored in
// SEMAR's database (ADR-0004).
//
// Every secret gets its own random data key (DEK). The plaintext is
// encrypted with the DEK, and the DEK is wrapped by a key-encryption key
// (KEK) identified by a version. Rotating the KEK only rewraps DEKs; the
// encrypted data itself is never re-encrypted. Additional authenticated data
// (AAD) binds each ciphertext to its database row so ciphertexts cannot be
// swapped between rows.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
)

// Sealed is the stored form of one secret.
type Sealed struct {
	KEKVersion string
	// WrappedDEK is nonce || AES-GCM(KEK, DEK).
	WrappedDEK []byte
	// Ciphertext is nonce || AES-GCM(DEK, plaintext).
	Ciphertext []byte
}

// Store seals and opens secrets. Keyring is the built-in implementation; a
// Vault-backed implementation can satisfy the same interface later.
type Store interface {
	Seal(plaintext, aad []byte) (Sealed, error)
	Open(s Sealed, aad []byte) ([]byte, error)
	// Rewrap rewraps s's DEK with the current KEK. It reports false when s
	// already uses the current KEK.
	Rewrap(s Sealed, aad []byte) (Sealed, bool, error)
	CurrentVersion() string
}

// ErrUnknownKEK is returned for secrets sealed with a KEK the keyring lacks.
var ErrUnknownKEK = errors.New("secret: unknown KEK version")

// AAD builds the additional authenticated data that binds a secret to its
// table, row and purpose.
func AAD(table, rowID, purpose string) []byte {
	return []byte(strings.Join([]string{table, rowID, purpose}, "|"))
}

// Keyring holds the current KEK and older KEKs kept for decryption.
type Keyring struct {
	current string
	keks    map[string]cipher.AEAD
}

var _ Store = (*Keyring)(nil)

// NewKeyring builds a keyring that seals with keks[current]. Every KEK must
// be 32 bytes (AES-256).
func NewKeyring(current string, keks map[string][]byte) (*Keyring, error) {
	if _, ok := keks[current]; !ok {
		return nil, fmt.Errorf("secret: current KEK %q not provided", current)
	}
	k := &Keyring{current: current, keks: make(map[string]cipher.AEAD, len(keks))}
	for version, key := range keks {
		if version == "" {
			return nil, errors.New("secret: empty KEK version")
		}
		aead, err := newAEAD(key)
		if err != nil {
			return nil, fmt.Errorf("secret: KEK %q: %w", version, err)
		}
		k.keks[version] = aead
	}
	return k, nil
}

// CurrentVersion returns the version new secrets are sealed with.
func (k *Keyring) CurrentVersion() string { return k.current }

// Seal encrypts plaintext with a fresh DEK wrapped by the current KEK.
func (k *Keyring) Seal(plaintext, aad []byte) (Sealed, error) {
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return Sealed{}, fmt.Errorf("secret: generate DEK: %w", err)
	}
	dekAEAD, err := newAEAD(dek)
	if err != nil {
		return Sealed{}, err
	}
	ciphertext, err := seal(dekAEAD, plaintext, aad)
	if err != nil {
		return Sealed{}, err
	}
	wrapped, err := seal(k.keks[k.current], dek, wrapAAD(k.current, aad))
	if err != nil {
		return Sealed{}, err
	}
	return Sealed{KEKVersion: k.current, WrappedDEK: wrapped, Ciphertext: ciphertext}, nil
}

// Open decrypts s. It fails if aad differs from the one used to seal.
func (k *Keyring) Open(s Sealed, aad []byte) ([]byte, error) {
	dek, err := k.unwrap(s, aad)
	if err != nil {
		return nil, err
	}
	dekAEAD, err := newAEAD(dek)
	if err != nil {
		return nil, err
	}
	plaintext, err := open(dekAEAD, s.Ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("secret: decrypt data: %w", err)
	}
	return plaintext, nil
}

// Rewrap rewraps the DEK with the current KEK, leaving Ciphertext unchanged.
func (k *Keyring) Rewrap(s Sealed, aad []byte) (Sealed, bool, error) {
	if s.KEKVersion == k.current {
		return s, false, nil
	}
	dek, err := k.unwrap(s, aad)
	if err != nil {
		return Sealed{}, false, err
	}
	wrapped, err := seal(k.keks[k.current], dek, wrapAAD(k.current, aad))
	if err != nil {
		return Sealed{}, false, err
	}
	return Sealed{KEKVersion: k.current, WrappedDEK: wrapped, Ciphertext: s.Ciphertext}, true, nil
}

func (k *Keyring) unwrap(s Sealed, aad []byte) ([]byte, error) {
	kek, ok := k.keks[s.KEKVersion]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownKEK, s.KEKVersion)
	}
	dek, err := open(kek, s.WrappedDEK, wrapAAD(s.KEKVersion, aad))
	if err != nil {
		return nil, fmt.Errorf("secret: unwrap DEK: %w", err)
	}
	return dek, nil
}

// wrapAAD binds the wrapped DEK to both the KEK version and the row.
func wrapAAD(version string, aad []byte) []byte {
	return append([]byte("kek:"+version+"|"), aad...)
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func seal(aead cipher.AEAD, plaintext, aad []byte) ([]byte, error) {
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("secret: nonce: %w", err)
	}
	return aead.Seal(nonce, nonce, plaintext, aad), nil
}

func open(aead cipher.AEAD, data, aad []byte) ([]byte, error) {
	if len(data) < aead.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	return aead.Open(nil, data[:aead.NonceSize()], data[aead.NonceSize():], aad)
}
