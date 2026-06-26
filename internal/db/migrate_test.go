package db

import (
	"context"
	"os"
	"testing"
)

func TestMigrate_NodeAccountOwnerColumn(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	if err := Migrate(ctx, url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.columns WHERE table_name='nodes' AND column_name='account_owner_id'`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Fatalf("account_owner_id column missing (got %d)", n)
	}
}

func testURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	return url
}

func TestMigrate_CreatesTenants(t *testing.T) {
	url := testURL(t)
	ctx := context.Background()

	if err := Migrate(ctx, url); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := Connect(ctx, url)
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer pool.Close()

	var exists bool
	err = pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name='tenants')`,
	).Scan(&exists)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if !exists {
		t.Fatal("tenants table not created")
	}
}
