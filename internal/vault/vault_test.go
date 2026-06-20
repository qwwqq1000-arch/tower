package vault

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db"
)

func keyB64(t *testing.T) string {
	t.Helper()
	k := make([]byte, 32)
	_, _ = rand.Read(k)
	return base64.StdEncoding.EncodeToString(k)
}

func suffix(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func TestVault_StoreLoad_EncryptsAtRest(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	if err := db.Migrate(ctx, url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	c, err := crypto.NewCipher(keyB64(t))
	if err != nil {
		t.Fatal(err)
	}
	v := New(pool, c)
	id := "acc_" + suffix(t)
	cr := Creds{Access: "access-tok-123", Refresh: "refresh-tok-456", ExpiresAt: 1781000000}

	if err := v.Store(ctx, id, "owner1", "a@b.com", "max", cr, 1780000000); err != nil {
		t.Fatalf("store: %v", err)
	}

	// at-rest must be ciphertext, not plaintext
	var accessEnc string
	if err := pool.QueryRow(ctx, `SELECT oauth_access_enc FROM accounts WHERE id=$1`, id).Scan(&accessEnc); err != nil {
		t.Fatal(err)
	}
	if accessEnc == cr.Access || accessEnc == "" {
		t.Fatalf("access stored in plaintext or empty: %q", accessEnc)
	}

	got, err := v.Load(ctx, id)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Access != cr.Access || got.Refresh != cr.Refresh || got.ExpiresAt != cr.ExpiresAt {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestVault_Load_WrongKeyFails(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	_ = db.Migrate(ctx, url)
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	id := "acc_" + suffix(t)
	c1, _ := crypto.NewCipher(keyB64(t))
	if err := New(pool, c1).Store(ctx, id, "o", "e", "max", Creds{Access: "x", Refresh: "y"}, 0); err != nil {
		t.Fatal(err)
	}
	c2, _ := crypto.NewCipher(keyB64(t))
	if _, err := New(pool, c2).Load(ctx, id); err == nil {
		t.Fatal("load with wrong key should error")
	}
}
