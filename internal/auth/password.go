// Package auth provides password hashing, session tokens, and dispatch keys.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"

	"golang.org/x/crypto/scrypt"
)

const (
	scryptN      = 32768
	scryptR      = 8
	scryptP      = 1
	scryptKeyLen = 32
	saltLen      = 16
)

// HashPassword returns a hex-encoded scrypt hash and the hex-encoded random salt.
func HashPassword(pw string) (hash, salt string, err error) {
	s := make([]byte, saltLen)
	if _, err = rand.Read(s); err != nil {
		return "", "", err
	}
	dk, err := scrypt.Key([]byte(pw), s, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return "", "", err
	}
	return hex.EncodeToString(dk), hex.EncodeToString(s), nil
}

// VerifyPassword reports whether pw matches the stored hex hash+salt (constant time).
func VerifyPassword(pw, hash, salt string) bool {
	s, err := hex.DecodeString(salt)
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(hash)
	if err != nil {
		return false
	}
	dk, err := scrypt.Key([]byte(pw), s, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(dk, want) == 1
}
