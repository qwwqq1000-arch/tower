package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db"
)

func TestHealthz_OKWithoutDB(t *testing.T) {
	// nil pool → handler must still respond (degraded), never panic.
	h := NewRouter(nil, "test-secret-padding-to-32-chars!", nil, nil, false)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (nil pool degraded)", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
}

func TestHealthz_OKWithPool(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}

	ctx := context.Background()
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("db.Connect: %v", err)
	}
	defer pool.Close()

	h := NewRouter(pool, "test-secret-padding-to-32-chars!", nil, nil, false)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body[status] = %q, want \"ok\"", body["status"])
	}
}

// TestPolicyRoutesSuperadminOnly verifies that policy mutation routes
// (PUT /api/admin/policies/global, POST /api/admin/policies/dry-run)
// are restricted to superadmin and return 403 for admin role.
//
// We test via the middleware stack directly (not the full router) so the test
// is pure and does not need a database. The requireSuperadmin check is entirely
// in-memory: it examines the role field in the session token.
func TestPolicyRoutesSuperadminOnly(t *testing.T) {
	const secret = "test-secret-padding-to-32-chars!"

	// Build the handlers the same way the router does: wrap with requireSuperadmin.
	putPolicyHandler := requireSuperadmin(secret, nil, putGlobalPolicyHandler(nil))
	dryRunHandler := requireSuperadmin(secret, nil, dryRunPolicyHandler(nil))

	doRequest := func(handler http.HandlerFunc, method, path, sub, role string) int {
		r := httptest.NewRequest(method, path, nil)
		r.AddCookie(&http.Cookie{
			Name:  "tower_session",
			Value: auth.IssueSession(secret, sub, role, 0, nowUnix(), 3600),
		})
		// CSRF header required on non-GET cookie-auth mutations.
		if method != http.MethodGet && method != http.MethodHead {
			r.Header.Set("X-Requested-With", "tower")
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		return rec.Code
	}

	// Admin role must get 403 on policy PUT.
	if code := doRequest(putPolicyHandler, "PUT", "/api/admin/policies/global", "admin_user", "admin"); code != 403 {
		t.Fatalf("admin on PUT /api/admin/policies/global: code=%d, want 403", code)
	}

	// Admin role must get 403 on policy dry-run.
	if code := doRequest(dryRunHandler, "POST", "/api/admin/policies/dry-run", "admin_user", "admin"); code != 403 {
		t.Fatalf("admin on POST /api/admin/policies/dry-run: code=%d, want 403", code)
	}

	// Superadmin should not get 403 (implementation may return 400/200 depending on body).
	// We're testing the authz layer, not the handler logic, so we just check it's not 403.
	if code := doRequest(putPolicyHandler, "PUT", "/api/admin/policies/global", "super_user", "superadmin"); code == 403 {
		t.Fatalf("superadmin on PUT /api/admin/policies/global: code=%d, should not be 403", code)
	}
	if code := doRequest(dryRunHandler, "POST", "/api/admin/policies/dry-run", "super_user", "superadmin"); code == 403 {
		t.Fatalf("superadmin on POST /api/admin/policies/dry-run: code=%d, should not be 403", code)
	}
}
