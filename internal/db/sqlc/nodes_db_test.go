package sqlc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
)

func suffixC(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func TestNodeCRUD(t *testing.T) {
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
	id := "n_" + suffixC(t)

	created, err := q.CreateNode(ctx, CreateNodeParams{
		ID: id, Name: "node-a", BaseUrl: "http://1.2.3.4:3456", ApiKey: "k", OwnerID: "o1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.BaseUrl != "http://1.2.3.4:3456" {
		t.Fatalf("base_url = %q", created.BaseUrl)
	}
	got, err := q.GetNode(ctx, id)
	if err != nil || got.ID != id {
		t.Fatalf("get: %v %q", err, got.ID)
	}
	if err := q.UpdateNodeEnabled(ctx, UpdateNodeEnabledParams{ID: id, Enabled: false}); err != nil {
		t.Fatalf("update: %v", err)
	}
	g2, _ := q.GetNode(ctx, id)
	if g2.Enabled {
		t.Fatal("enabled should be false after update")
	}
	if err := q.DeleteNode(ctx, id); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := q.GetNode(ctx, id); err == nil {
		t.Fatal("get after delete should error")
	}
}
