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
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
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
func TestPolicyRoutesSuperadminOnly(t *testing.T) {
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
	router := NewRouter(pool, secret, nil, q, false)

	// Seed test tenants: one admin, one superadmin.
	for _, s := range []struct{ id, role string }{
		{"admin_user", "admin"},
		{"super_user", "superadmin"},
	} {
		if _, err := q.CreateTenant(ctx, sqlc.CreateTenantParams{
			ID:        s.id,
			Username:  s.id,
			PwHash:    "h",
			Salt:      "s",
			Role:      s.role,
			IngestKey: "ik_" + s.id,
		}); err != nil {
			_ = q.SetTenantRole(ctx, sqlc.SetTenantRoleParams{ID: s.id, Role: s.role})
		}
	}
	t.Cleanup(func() {
		cctx := context.Background()
		for _, id := range []string{"admin_user", "super_user"} {
			_ = q.DeleteTenant(cctx, id)
		}
	})

	doRequest := func(method, path, sub, role string) int {
		r := httptest.NewRequest(method, path, nil)
		r.AddCookie(&http.Cookie{
			Name:  "tower_session",
			Value: auth.IssueSession(secret, sub, role, 0, nowUnix(), 3600),
		})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		return rec.Code
	}

	// Admin role must get 403 on policy PUT.
	if code := doRequest("PUT", "/api/admin/policies/global", "admin_user", "admin"); code != 403 {
		t.Fatalf("admin on PUT /api/admin/policies/global: code=%d, want 403", code)
	}

	// Admin role must get 403 on policy dry-run.
	if code := doRequest("POST", "/api/admin/policies/dry-run", "admin_user", "admin"); code != 403 {
		t.Fatalf("admin on POST /api/admin/policies/dry-run: code=%d, want 403", code)
	}

	// Superadmin should not get 403 (implementation may return 400/200 depending on body).
	// We're testing the authz layer, not the handler logic, so we just check it's not 403.
	if code := doRequest("PUT", "/api/admin/policies/global", "super_user", "superadmin"); code == 403 {
		t.Fatalf("superadmin on PUT /api/admin/policies/global: code=%d, should not be 403", code)
	}
	if code := doRequest("POST", "/api/admin/policies/dry-run", "super_user", "superadmin"); code == 403 {
		t.Fatalf("superadmin on POST /api/admin/policies/dry-run: code=%d, should not be 403", code)
	}
}
