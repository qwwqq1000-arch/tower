package sqlc

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwwqq1000-arch/tower/internal/db"
)

func TestCreateAndGetTenant(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	if err := db.Migrate(ctx, url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	var pool *pgxpool.Pool
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	q := New(pool)
	created, err := q.CreateTenant(ctx, CreateTenantParams{
		ID:        "t_test_1",
		Username:  "alice_" + os.Getenv("RANDOM_SUFFIX"),
		PwHash:    "h", Salt: "s", Role: "tenant",
		IngestKey: "ik_test_1",
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
