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

// TestOwnerScoping verifies that a non-superadmin only sees resources they own,
// while a superadmin sees everything.
func TestOwnerScoping(t *testing.T) {
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

	// Two owners, each with their own node.
	nodeA := randHex("n_")
	nodeB := randHex("n_")
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{ID: nodeA, Name: "nodeA", BaseUrl: "http://a", OwnerID: "ownerA"}); err != nil {
		t.Fatalf("create node A: %v", err)
	}
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{ID: nodeB, Name: "nodeB", BaseUrl: "http://b", OwnerID: "ownerB"}); err != nil {
		t.Fatalf("create node B: %v", err)
	}

	get := func(path, sub, role string) []map[string]any {
		r := httptest.NewRequest("GET", path, nil)
		r.AddCookie(&http.Cookie{Name: "tower_session", Value: auth.IssueSession(secret, sub, role, nowUnix(), 3600)})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		if rec.Code != 200 {
			t.Fatalf("%s as %s/%s: code=%d body=%s", path, sub, role, rec.Code, rec.Body)
		}
		var out []map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return out
	}

	// Contamination-robust helpers (other tests in this package share the DB).
	names := func(rows []map[string]any) map[string]bool {
		m := map[string]bool{}
		for _, n := range rows {
			if s, ok := n["name"].(string); ok {
				m[s] = true
			}
		}
		return m
	}
	allOwnedBy := func(rows []map[string]any, owner string) bool {
		for _, n := range rows {
			if n["ownerId"] != owner {
				return false
			}
		}
		return true
	}

	// ownerA (admin) sees nodeA and ONLY nodes it owns.
	nodesA := get("/api/admin/nodes", "ownerA", "admin")
	if !names(nodesA)["nodeA"] || names(nodesA)["nodeB"] || !allOwnedBy(nodesA, "ownerA") {
		t.Fatalf("admin ownerA nodes = %+v, want only ownerA-owned incl nodeA", nodesA)
	}

	// ownerB (admin) sees nodeB and ONLY nodes it owns.
	nodesB := get("/api/admin/nodes", "ownerB", "admin")
	if !names(nodesB)["nodeB"] || names(nodesB)["nodeA"] || !allOwnedBy(nodesB, "ownerB") {
		t.Fatalf("admin ownerB nodes = %+v, want only ownerB-owned incl nodeB", nodesB)
	}

	// superadmin sees both A and B.
	nodesAll := get("/api/admin/nodes", "root", "superadmin")
	if !names(nodesAll)["nodeA"] || !names(nodesAll)["nodeB"] {
		t.Fatalf("superadmin must see both nodeA and nodeB, got %+v", names(nodesAll))
	}

	// A non-superadmin creating a node without ownerId owns it.
	body := `{"baseUrl":"http://c","name":"nodeC"}`
	r := httptest.NewRequest("POST", "/api/admin/nodes", strings.NewReader(body))
	r.AddCookie(&http.Cookie{Name: "tower_session", Value: auth.IssueSession(secret, "ownerC", "admin", nowUnix(), 3600)})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, r)
	if rec.Code != 200 {
		t.Fatalf("create node C: %d %s", rec.Code, rec.Body)
	}
	nodesC := get("/api/admin/nodes", "ownerC", "admin")
	if !names(nodesC)["nodeC"] || !allOwnedBy(nodesC, "ownerC") {
		t.Fatalf("admin ownerC nodes = %+v, want only ownerC-owned incl nodeC (auto-owned)", nodesC)
	}

	// --- write-path owner enforcement ---
	do := func(method, path, sub, role string) int {
		rr := httptest.NewRequest(method, path, nil)
		rr.AddCookie(&http.Cookie{Name: "tower_session", Value: auth.IssueSession(secret, sub, role, nowUnix(), 3600)})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, rr)
		return rec.Code
	}
	// admin ownerB must NOT delete ownerA's node.
	if code := do("DELETE", "/api/admin/nodes/"+nodeA, "ownerB", "admin"); code != 403 {
		t.Fatalf("ownerB deleting nodeA: code=%d, want 403", code)
	}
	if !names(get("/api/admin/nodes", "root", "superadmin"))["nodeA"] {
		t.Fatal("nodeA should still exist after forbidden delete")
	}
	// user management is superadmin-only (privilege-escalation guard).
	if code := do("GET", "/api/admin/users", "ownerA", "admin"); code != 403 {
		t.Fatalf("admin listing users: code=%d, want 403", code)
	}
	if code := do("GET", "/api/admin/users", "root", "superadmin"); code != 200 {
		t.Fatalf("superadmin listing users: code=%d, want 200", code)
	}
	// reassigning account ownership is superadmin-only.
	if code := do("PATCH", "/api/admin/accounts/whatever/owner", "ownerA", "admin"); code != 403 {
		t.Fatalf("admin reassigning account owner: code=%d, want 403", code)
	}
	// superadmin CAN delete a node.
	if code := do("DELETE", "/api/admin/nodes/"+nodeB, "root", "superadmin"); code != 200 {
		t.Fatalf("superadmin deleting nodeB: code=%d, want 200", code)
	}
}
