package api

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

func TestDispatchStatusJSON(t *testing.T) {
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
	store := state.NewStore(func() int64 { return 0 }, func(a, b int64) int64 { return a })
	store.Ensure("n1:default", 2)
	svc := &dispatch.Service{Q: q, Store: store, Base: policy.Defaults(), Now: func() int64 { return 0 }}

	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, svc, q, false, nil, "")
	req := httptest.NewRequest("GET", "/api/admin/dispatch/status", nil)
	req.AddCookie(adminCookie(t, ctx, q, secret))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d %s", rec.Code, rec.Body)
	}
	for _, k := range []string{"accounts", "traffic", "events", "nodes", "fallbackChannels"} {
		if !strings.Contains(rec.Body.String(), k) {
			t.Fatalf("missing %s in %s", k, rec.Body)
		}
	}
}
