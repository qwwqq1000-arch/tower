package auth

import "testing"

func TestSession_RoundTrip(t *testing.T) {
	const secret = "test-secret-at-least-32-chars-xx"
	tok := IssueSession(secret, "u1", "admin", 7, 1000, 3600)
	p, ok := VerifySession(secret, tok, 1500)
	if !ok {
		t.Fatal("verify should succeed within ttl")
	}
	if p.Sub != "u1" || p.Role != "admin" || p.Exp != 4600 {
		t.Fatalf("payload = %+v", p)
	}
}

// TestSession_Epoch verifies the session epoch is carried in the signed payload
// and survives a verify round-trip, so the middleware can compare it against the
// user's current DB epoch to revoke stale tokens.
func TestSession_Epoch(t *testing.T) {
	const secret = "test-secret-at-least-32-chars-xx"
	tok := IssueSession(secret, "u1", "admin", 7, 1000, 3600)
	p, ok := VerifySession(secret, tok, 1500)
	if !ok {
		t.Fatal("verify should succeed within ttl")
	}
	if p.Epoch != 7 {
		t.Fatalf("epoch = %d, want 7", p.Epoch)
	}
	if p.Exp != 4600 {
		t.Fatalf("exp = %d, want 4600", p.Exp)
	}
}

func TestSession_Expired(t *testing.T) {
	const secret = "test-secret-at-least-32-chars-xx"
	tok := IssueSession(secret, "u1", "admin", 0, 1000, 3600)
	if _, ok := VerifySession(secret, tok, 99999); ok {
		t.Fatal("verify should fail after expiry")
	}
}

func TestSession_BadSignature(t *testing.T) {
	tok := IssueSession("secret-a-padding-to-32-chars-xxx", "u1", "admin", 0, 1000, 3600)
	if _, ok := VerifySession("secret-b-padding-to-32-chars-xxx", tok, 1500); ok {
		t.Fatal("verify should fail with wrong secret")
	}
	if _, ok := VerifySession("secret-a-padding-to-32-chars-xxx", tok+"x", 1500); ok {
		t.Fatal("verify should fail with tampered token")
	}
}
