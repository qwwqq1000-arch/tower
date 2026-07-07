// Package crypto provides envelope encryption (AES-256-GCM) for secrets at rest,
// such as OAuth credentials stored in the central account vault.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// Cipher wraps an AES-256-GCM AEAD keyed by the master key.
type Cipher struct {
	aead cipher.AEAD
}

// NewCipher builds a Cipher from a base64-encoded 32-byte master key.
func NewCipher(masterKeyB64 string) (*Cipher, error) {
	key, err := base64.StdEncoding.DecodeString(masterKeyB64)
	if err != nil {
		return nil, fmt.Errorf("decode master key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("master key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt returns base64( nonce || ciphertext||tag ).
func (c *Cipher) Encrypt(plaintext []byte) (string, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	sealed := c.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// EncryptStr encrypts plain and returns base64 ciphertext. It is a string-typed
// convenience over Encrypt for secret columns. A nil Cipher (plaintext-mode
// deployments / tests without a master key) or an empty input returns plain
// unchanged, so callers can encrypt-on-write unconditionally. On encryption
// error it falls back to returning plain rather than persisting a lost secret.
func (c *Cipher) EncryptStr(plain string) string {
	if c == nil || plain == "" {
		return plain
	}
	enc, err := c.Encrypt([]byte(plain))
	if err != nil {
		return plain
	}
	return enc
}

// DecryptOrPlaintext is the transparent read shim for secret columns
// (vault-crypto-3). It returns the decrypted plaintext when s is a ciphertext
// produced by this cipher, and otherwise returns s unchanged. This makes reads
// tolerant of legacy plaintext rows written before encryption-at-rest was
// enabled: an admin re-save upgrades a row to ciphertext, but un-migrated rows
// keep working. A nil Cipher (plaintext-mode) returns s unchanged.
func (c *Cipher) DecryptOrPlaintext(s string) string {
	if c == nil || s == "" {
		return s
	}
	plain, err := c.Decrypt(s)
	if err != nil {
		return s // not our ciphertext (or wrong key) → treat as plaintext.
	}
	return string(plain)
}

// Decrypt reverses Encrypt; returns an error on tamper or wrong key.
func (c *Cipher) Decrypt(s string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns {
		return nil, fmt.Errorf("ciphertext too short: need at least %d bytes, got %d", ns, len(raw))
	}
	nonce, ct := raw[:ns], raw[ns:]
	plain, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt: %w", err)
	}
	return plain, nil
}
