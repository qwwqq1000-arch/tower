package auth

import (
	"strings"
	"testing"
)

func TestNewDispatchKey(t *testing.T) {
	pt, prefix, hash, salt, err := NewDispatchKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pt, "dk_") {
		t.Fatalf("plaintext %q missing dk_ prefix", pt)
	}
	if len(pt) != 3+64 {
		t.Fatalf("plaintext len = %d, want 67", len(pt))
	}
	if prefix != PrefixOf(pt) {
		t.Fatalf("prefix %q != PrefixOf %q", prefix, PrefixOf(pt))
	}
	if len(prefix) != 8 {
		t.Fatalf("prefix len = %d, want 8", len(prefix))
	}
	if !VerifyDispatchKey(pt, hash, salt) {
		t.Fatal("verify should succeed")
	}
	if VerifyDispatchKey("dk_"+strings.Repeat("0", 64), hash, salt) {
		t.Fatal("verify should fail for wrong key")
	}
}
