package sqlc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
)

func TestCreateAndGetTenant(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	buf := make([]byte, 6)
	_, _ = rand.Read(buf)
	suffix := hex.EncodeToString(buf)

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
	created, err := q.CreateTenant(ctx, CreateTenantParams{
		ID:        "t_" + suffix,
		Username:  "alice_" + suffix,
		PwHash:    "h", Salt: "s", Role: "tenant",
		IngestKey: "ik_" + suffix,
	})
	if err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	got, err := q.GetTenantByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTenantByID: %v", err)
	}
	if got.Username != created.Username {
		t.Fatalf("got %q want %q", got.Username, created.Username)
	}
}
