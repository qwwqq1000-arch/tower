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

// seedSessionCookie upserts a real tenant row for sub (session_epoch reset to 0)
// and returns a cookie carrying a token issued at epoch 0. Sessions must
// correspond to a live user row at the matching epoch now that requireSession
// validates the token's epoch against the DB (auth-session-1); test helpers seed
// the subject so the middleware accepts the session.
func seedSessionCookie(t *testing.T, ctx context.Context, q *sqlc.Queries, secret, sub, role string) *http.Cookie {
	t.Helper()
	if _, err := q.CreateTenant(ctx, sqlc.CreateTenantParams{
		ID: sub, Username: sub, PwHash: "h", Salt: "s", Role: role, IngestKey: "ik_" + sub,
	}); err != nil {
		// Already exists from a previous test/iteration; ensure the role matches.
		_ = q.SetTenantRole(ctx, sqlc.SetTenantRoleParams{ID: sub, Role: role})
	}
	// Issue the token at the row's current epoch so the middleware's epoch check
	// accepts it even if a prior test bumped the epoch.
	epoch, _ := q.GetSessionEpoch(ctx, sub)
	tok := auth.IssueSession(secret, sub, role, epoch, nowUnix(), 3600)
	return &http.Cookie{Name: "tower_session", Value: tok}
}

// adminCookie issues a superadmin session — the all-access role used by tests
// that exercise global management. Owner-scoping (admin sees only own) is tested
// separately with explicit non-superadmin sessions.
func adminCookie(t *testing.T, ctx context.Context, q *sqlc.Queries, secret string) *http.Cookie {
	t.Helper()
	return seedSessionCookie(t, ctx, q, secret, "u_admin", "superadmin")
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
	router := NewRouter(pool, secret, nil, q, false, nil)
	ck := adminCookie(t, ctx, q, secret)

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

	// non-admin role → 403 (viewer is a real seeded user so the session passes
	// the epoch check and the role gate is what rejects it).
	req5 := httptest.NewRequest(http.MethodGet, "/api/admin/nodes", nil)
	req5.AddCookie(seedSessionCookie(t, ctx, q, secret, "u_v", "viewer"))
	rec5 := httptest.NewRecorder()
	router.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusForbidden {
		t.Fatalf("viewer status=%d, want 403", rec5.Code)
	}
}

func TestCreateNodeForceOwner(t *testing.T) {
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
	router := NewRouter(pool, secret, nil, q, false, nil)

	// Create an admin session with a specific owner ID
	adminCookie := seedSessionCookie(t, ctx, q, secret, "u_admin1", "admin")

	// Try to create a node with a different ownerId in the body
	foreignOwner := "u_admin2"
	req := httptest.NewRequest(http.MethodPost, "/api/admin/nodes",
		strings.NewReader(`{"name":"n1","baseUrl":"http://x:3456","apiKey":"k","ownerId":"`+foreignOwner+`"}`))
	req.AddCookie(adminCookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create node status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Verify the node was created with the caller's owner, not the foreign one
	var nodeResp struct{ ID, OwnerId string }
	if err := json.NewDecoder(rec.Body).Decode(&nodeResp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if nodeResp.OwnerId != "u_admin1" {
		t.Fatalf("expected ownerId=u_admin1 (caller), got %q (body supplied %q)", nodeResp.OwnerId, foreignOwner)
	}
}
