package db

import (
	"context"
	"os"
	"testing"
)

func TestMigrate_RegistryTables(t *testing.T) {
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
	for _, tbl := range []string{"nodes", "accounts", "node_accounts"} {
		var ok bool
		if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name=$1)`, tbl).Scan(&ok); err != nil {
			t.Fatalf("query %s: %v", tbl, err)
		}
		if !ok {
			t.Fatalf("table %s not created", tbl)
		}
	}
}
