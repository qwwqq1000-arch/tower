package sqlc

import (
	"context"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
)

func TestNodeAccountAssign(t *testing.T) {
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
	nid := "n_" + suffixC(t)
	aid := "a_" + suffixC(t)

	_, err = q.AssignAccount(ctx, AssignAccountParams{
		NodeID: nid, AccountID: aid, ProfileID: "default", Egress: "", Weight: 100, Role: "baseline", SlotID: "",
	})
	if err != nil {
		t.Fatalf("assign: %v", err)
	}
	rows, err := q.ListNodeAccountsByNode(ctx, nid)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list = %v rows, err %v", len(rows), err)
	}
	if err := q.UnassignAccount(ctx, UnassignAccountParams{NodeID: nid, AccountID: aid}); err != nil {
		t.Fatalf("unassign: %v", err)
	}
	rows2, _ := q.ListNodeAccountsByNode(ctx, nid)
	if len(rows2) != 0 {
		t.Fatalf("after unassign rows = %d", len(rows2))
	}
}
