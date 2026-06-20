package db

import (
	"context"
	"os"
	"testing"
)

func TestMigrate_RBACSeedsRoles(t *testing.T) {
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
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM roles WHERE builtin = true`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 5 {
		t.Fatalf("builtin roles = %d, want 5", n)
	}
	var perms string
	if err := pool.QueryRow(ctx, `SELECT permissions::text FROM roles WHERE name='superadmin'`).Scan(&perms); err != nil {
		t.Fatalf("query superadmin: %v", err)
	}
	if perms != `["*"]` {
		t.Fatalf("superadmin perms = %s", perms)
	}
}
