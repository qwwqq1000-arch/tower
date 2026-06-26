package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// TestOwnerFilterBeforeLimit verifies that the owner filter is pushed into SQL
// before the LIMIT clause (events-audit-4). A scoped admin must receive a full
// page of their own rows even when another owner dominates the most-recent rows.
func TestOwnerFilterBeforeLimit(t *testing.T) {
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

	const secret = "test-secret-padding-to-32-chars!"
	const limit = 5

	// Seed two tenant rows so requireSession's epoch check accepts them.
	const ownerA = "audit4_ownerA"
	const ownerB = "audit4_ownerB"
	for _, s := range []struct{ id, role string }{
		{ownerA, "admin"},
		{ownerB, "admin"},
	} {
		if _, err := q.CreateTenant(ctx, sqlc.CreateTenantParams{
			ID: s.id, Username: s.id, PwHash: "h", Salt: "s",
			Role: s.role, IngestKey: "ik_" + s.id,
		}); err != nil {
			_ = q.SetTenantRole(ctx, sqlc.SetTenantRoleParams{ID: s.id, Role: s.role})
		}
	}
	t.Cleanup(func() {
		cctx := context.Background()
		for _, id := range []string{ownerA, ownerB} {
			_ = q.DeleteTenant(cctx, id)
		}
	})

	router := NewRouter(pool, secret, nil, q, false, nil, "")

	// Seed limit rows for ownerA and limit+5 rows for ownerB (ownerB dominates).
	// Use unique targets so we can count ownerA rows in the response.
	ownerATarget := "audit4_target_a"
	for i := 0; i < limit; i++ {
		if err := q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
			Ts:      int64(1000 + i),
			OwnerID: ownerA,
			Model:   "claude-sonnet",
			Target:  ownerATarget,
			Status:  "ok",
		}); err != nil {
			t.Fatalf("insert log ownerA %d: %v", i, err)
		}
	}
	for i := 0; i < limit+5; i++ {
		if err := q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{
			Ts:      int64(2000 + i), // later → ownerB dominates recency
			OwnerID: ownerB,
			Model:   "claude-sonnet",
			Target:  "audit4_target_b",
			Status:  "ok",
		}); err != nil {
			t.Fatalf("insert log ownerB %d: %v", i, err)
		}
	}

	// Seed limit events for ownerA and limit+5 events for ownerB.
	for i := 0; i < limit; i++ {
		if err := q.InsertDispatchEvent(ctx, sqlc.InsertDispatchEventParams{
			Ts:      int64(1000 + i),
			Type:    "ban",
			Target:  ownerATarget,
			OwnerID: ownerA,
			Detail:  []byte(`{}`),
		}); err != nil {
			t.Fatalf("insert event ownerA %d: %v", i, err)
		}
	}
	for i := 0; i < limit+5; i++ {
		if err := q.InsertDispatchEvent(ctx, sqlc.InsertDispatchEventParams{
			Ts:      int64(2000 + i),
			Type:    "ban",
			Target:  "audit4_target_b",
			OwnerID: ownerB,
			Detail:  []byte(`{}`),
		}); err != nil {
			t.Fatalf("insert event ownerB %d: %v", i, err)
		}
	}

	epoch, _ := q.GetSessionEpoch(ctx, ownerA)
	cookieA := &http.Cookie{
		Name:  "tower_session",
		Value: auth.IssueSession(secret, ownerA, "admin", epoch, nowUnix(), 3600),
	}

	get := func(path string) []map[string]any {
		req := httptest.NewRequest("GET", fmt.Sprintf("%s?limit=%d", path, limit), nil)
		req.AddCookie(cookieA)
		req.Header.Set("X-Requested-With", "tower")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s: code=%d body=%s", path, rec.Code, rec.Body)
		}
		var out []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
		return out
	}

	// ownerA should get exactly limit rows, all belonging to ownerA,
	// even though ownerB's more-recent rows would dominate a pre-limited query.
	logs := get("/api/admin/logs")
	if len(logs) != limit {
		t.Fatalf("logs: got %d rows, want %d (owner filter must happen in SQL before LIMIT)", len(logs), limit)
	}
	for _, row := range logs {
		if target, _ := row["target"].(string); target != ownerATarget {
			t.Fatalf("logs: unexpected row target=%q (ownerB row leaked)", target)
		}
	}

	events := get("/api/admin/events")
	if len(events) != limit {
		t.Fatalf("events: got %d rows, want %d (owner filter must happen in SQL before LIMIT)", len(events), limit)
	}
	for _, row := range events {
		if target, _ := row["target"].(string); target != ownerATarget {
			t.Fatalf("events: unexpected row target=%q (ownerB event leaked)", target)
		}
	}
}
