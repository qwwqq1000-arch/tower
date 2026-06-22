package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func TestConsoleAPIs(t *testing.T) {
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
	router := NewRouter(pool, secret, nil, q)
	ck := adminCookie(t, ctx, q, secret)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		var r *http.Request
		if body != "" {
			r = httptest.NewRequest(method, path, strings.NewReader(body))
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.AddCookie(ck)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		return rec
	}

	if rec := do("PUT", "/api/admin/policies/global", `{"MaxConcurrent":7}`); rec.Code != 200 {
		t.Fatalf("put policy=%d %s", rec.Code, rec.Body)
	}
	if rec := do("GET", "/api/admin/policies", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "global") {
		t.Fatalf("list policy=%d %s", rec.Code, rec.Body)
	}
	if rec := do("POST", "/api/admin/policies/dry-run", `{"MaxConcurrent":10}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), "MaxConcurrent") {
		t.Fatalf("dry-run=%d %s", rec.Code, rec.Body)
	}
	if rec := do("PUT", "/api/admin/desired", `{"opencode":{"memory":true}}`); rec.Code != 200 {
		t.Fatalf("put desired=%d", rec.Code)
	}
	if rec := do("GET", "/api/admin/desired", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "opencode") {
		t.Fatalf("get desired=%d %s", rec.Code, rec.Body)
	}
	for _, p := range []string{"/api/admin/logs", "/api/admin/events", "/api/admin/audit"} {
		if rec := do("GET", p, ""); rec.Code != 200 {
			t.Fatalf("GET %s = %d", p, rec.Code)
		}
	}
	// guard: no cookie → 401
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/admin/policies", nil))
	if rec.Code != 401 {
		t.Fatalf("no-cookie=%d want 401", rec.Code)
	}
}
