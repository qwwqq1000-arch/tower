package events

import (
	"context"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func TestRecordEvent(t *testing.T) {
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

	if err := Record(ctx, q, 100, Event{Type: "ban_detected", Target: "node1:default", OwnerID: "o1", Detail: map[string]any{"status": 401}}); err != nil {
		t.Fatalf("record: %v", err)
	}
	rows, err := q.ListRecentEvents(ctx, 10)
	if err != nil || len(rows) < 1 {
		t.Fatalf("list: %v n=%d", err, len(rows))
	}
	if rows[0].Type != "ban_detected" {
		t.Fatalf("type=%s", rows[0].Type)
	}
}
