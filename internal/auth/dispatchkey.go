package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"

	"golang.org/x/crypto/scrypt"
)

// NewDispatchKey generates a dk_ plaintext key plus its prefix, scrypt hash, salt.
func NewDispatchKey() (plaintext, prefix, hash, salt string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return
	}
	body := hex.EncodeToString(raw) // 64 hex
	plaintext = "dk_" + body
	prefix = body[:8]
	s := make([]byte, saltLen)
	if _, err = rand.Read(s); err != nil {
		return
	}
	dk, e := scrypt.Key([]byte(plaintext), s, scryptN, scryptR, scryptP, scryptKeyLen)
	if e != nil {
		err = e
		return
	}
	hash = hex.EncodeToString(dk)
	salt = hex.EncodeToString(s)
	return
}

// PrefixOf returns the 8-hex lookup prefix of a dk_ plaintext key ("" if malformed).
func PrefixOf(plaintext string) string {
	if len(plaintext) < 3+8 || plaintext[:3] != "dk_" {
		return ""
	}
	return plaintext[3:11]
}

// VerifyDispatchKey checks a plaintext dk_ key against a stored hash+salt.
func VerifyDispatchKey(plaintext, hash, salt string) bool {
	s, err := hex.DecodeString(salt)
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(hash)
	if err != nil {
		return false
	}
	dk, err := scrypt.Key([]byte(plaintext), s, scryptN, scryptR, scryptP, scryptKeyLen)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(dk, want) == 1
}
