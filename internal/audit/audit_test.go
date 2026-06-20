package audit

import (
	"context"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func TestRecordAndList(t *testing.T) {
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
	q := sqlc.New(pool)

	err = Record(ctx, q, 1000, Entry{
		Actor: "admin1", Action: "policy.update", Target: "global",
		Before: map[string]any{"MaxConcurrent": 3},
		After:  map[string]any{"MaxConcurrent": 5},
	})
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	rows, err := q.ListRecentAudit(ctx, 10)
	if err != nil || len(rows) < 1 {
		t.Fatalf("list: %v n=%d", err, len(rows))
	}
	if rows[0].Actor != "admin1" || rows[0].Action != "policy.update" {
		t.Fatalf("row=%+v", rows[0])
	}
}
