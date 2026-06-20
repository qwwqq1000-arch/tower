package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
)

// SessionPayload is the signed body of a session cookie.
type SessionPayload struct {
	Sub  string `json:"sub"`
	Role string `json:"role"`
	Exp  int64  `json:"exp"`
}

func sign(secret, msg string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// IssueSession builds a signed token: base64url(payload) + "." + hex(HMAC).
func IssueSession(secret, sub, role string, nowUnix, ttlSec int64) string {
	p := SessionPayload{Sub: sub, Role: role, Exp: nowUnix + ttlSec}
	raw, err := json.Marshal(p)
	if err != nil {
		panic("auth: IssueSession marshal: " + err.Error())
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	return body + "." + sign(secret, body)
}

// VerifySession checks signature and expiry; returns the payload on success.
func VerifySession(secret, token string, nowUnix int64) (SessionPayload, bool) {
	body, mac, found := strings.Cut(token, ".")
	if !found {
		return SessionPayload{}, false
	}
	if !hmac.Equal([]byte(mac), []byte(sign(secret, body))) {
		return SessionPayload{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return SessionPayload{}, false
	}
	var p SessionPayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return SessionPayload{}, false
	}
	if nowUnix >= p.Exp {
		return SessionPayload{}, false
	}
	return p, true
}
