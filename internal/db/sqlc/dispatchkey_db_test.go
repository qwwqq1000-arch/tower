package sqlc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
)

func TestDispatchKeyCreateAndLookup(t *testing.T) {
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
	q := New(pool)

	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	suffix := hex.EncodeToString(buf)

	_, err = q.CreateDispatchKey(ctx, CreateDispatchKeyParams{
		ID: "dk_" + suffix, KeyHash: "h", Salt: "s", Prefix: suffix[:8], OwnerID: "owner1", Label: "test",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	rows, err := q.GetDispatchKeysByPrefix(ctx, suffix[:8])
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if len(rows) != 1 || rows[0].OwnerID != "owner1" {
		t.Fatalf("lookup rows = %+v", rows)
	}
}
