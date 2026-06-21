package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func adminCookie(t *testing.T, secret string) *http.Cookie {
	t.Helper()
	tok := auth.IssueSession(secret, "u_admin", "admin", nowUnix(), 3600)
	return &http.Cookie{Name: "tower_session", Value: tok}
}

func TestAdminNodesAndKeys(t *testing.T) {
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
	ck := adminCookie(t, secret)

	// create node
	req := httptest.NewRequest(http.MethodPost, "/api/admin/nodes", strings.NewReader(`{"name":"n1","baseUrl":"http://x:3456","apiKey":"k"}`))
	req.AddCookie(ck)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create node status=%d body=%s", rec.Code, rec.Body.String())
	}

	// list nodes
	req2 := httptest.NewRequest(http.MethodGet, "/api/admin/nodes", nil)
	req2.AddCookie(ck)
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	var nodes []map[string]any
	_ = json.NewDecoder(rec2.Body).Decode(&nodes)
	if len(nodes) < 1 {
		t.Fatalf("expected >=1 node, got %d", len(nodes))
	}

	// create dispatch key → plaintext returned once
	req3 := httptest.NewRequest(http.MethodPost, "/api/admin/dispatch-keys", strings.NewReader(`{"label":"new-api"}`))
	req3.AddCookie(ck)
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, req3)
	var keyResp struct{ ID, Key string }
	_ = json.NewDecoder(rec3.Body).Decode(&keyResp)
	if !strings.HasPrefix(keyResp.Key, "dk_") {
		t.Fatalf("expected dk_ plaintext, got %q (status %d)", keyResp.Key, rec3.Code)
	}

	// unauthorized (no cookie) → 401
	rec4 := httptest.NewRecorder()
	router.ServeHTTP(rec4, httptest.NewRequest(http.MethodGet, "/api/admin/nodes", nil))
	if rec4.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie status=%d, want 401", rec4.Code)
	}

	// non-admin role → 403
	tok := auth.IssueSession(secret, "u_v", "viewer", nowUnix(), 3600)
	req5 := httptest.NewRequest(http.MethodGet, "/api/admin/nodes", nil)
	req5.AddCookie(&http.Cookie{Name: "tower_session", Value: tok})
	rec5 := httptest.NewRecorder()
	router.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d, want 403", rec5.Code)
	}
}
