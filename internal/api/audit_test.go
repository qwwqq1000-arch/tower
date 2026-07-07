package api

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// auditFrom returns the caller's subject id from the session, or "" when no
// session is attached (pure, no DB).
func TestAuditFrom(t *testing.T) {
	// no session → empty
	r := httptest.NewRequest("POST", "/x", nil)
	if got := auditFrom(r); got != "" {
		t.Fatalf("no-session auditFrom=%q want empty", got)
	}
	// with a session payload in context → its Sub
	r = r.WithContext(context.WithValue(r.Context(), ctxKeySession, auth.SessionPayload{Sub: "u_alice", Role: "admin"}))
	if got := auditFrom(r); got != "u_alice" {
		t.Fatalf("auditFrom=%q want u_alice", got)
	}
}

// A mutating admin action (setUserRole) records exactly one audit row attributed
// to the caller. DB-backed; skips without TEST_DATABASE_URL.
func TestSetUserRoleRecordsAudit(t *testing.T) {
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
	_, _ = pool.Exec(ctx, "DELETE FROM audit_log")
	_, _ = pool.Exec(ctx, "DELETE FROM tenants")

	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q, false, nil, "")
	ck := adminCookie(t, ctx, q, secret) // sub=u_admin role=superadmin

	// create a tenant to act on
	target, err := q.CreateTenant(ctx, sqlc.CreateTenantParams{
		ID: "u_target", Username: "target", PwHash: "h", Salt: "s", Role: "tenant", IngestKey: "ik_t",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	req := httptest.NewRequest("PATCH", "/api/admin/users/"+target.ID+"/role", strings.NewReader(`{"role":"admin"}`))
	req.AddCookie(ck)
	req.Header.Set("X-Requested-With", "tower")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("setRole=%d %s", rec.Code, rec.Body)
	}

	rows, err := q.ListRecentAudit(ctx, 10)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("audit rows=%d want 1", len(rows))
	}
	if rows[0].Actor != "u_admin" {
		t.Fatalf("actor=%q want u_admin", rows[0].Actor)
	}
	if rows[0].Action != "user.role" {
		t.Fatalf("action=%q want user.role", rows[0].Action)
	}
	if !strings.Contains(rows[0].Target, target.ID) {
		t.Fatalf("target=%q want to contain %s", rows[0].Target, target.ID)
	}
}
