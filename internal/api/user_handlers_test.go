package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func TestUserMgmtAndChangePassword(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" { t.Skip("TEST_DATABASE_URL not set") }
	ctx := context.Background()
	if err := db.Migrate(ctx, url); err != nil { t.Fatalf("migrate: %v", err) }
	pool, err := db.Connect(ctx, url)
	if err != nil { t.Fatalf("connect: %v", err) }
	defer pool.Close()
	q := sqlc.New(pool)
	_, _ = pool.Exec(ctx, "DELETE FROM tenants")

	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q)
	ck := adminCookie(t, ctx, q, secret) // sub=u_admin role=admin
	do := func(m, p, b string, c *http.Cookie) *httptest.ResponseRecorder {
		var r *http.Request
		if b != "" { r = httptest.NewRequest(m, p, strings.NewReader(b)) } else { r = httptest.NewRequest(m, p, nil) }
		if c != nil { r.AddCookie(c) }
		rec := httptest.NewRecorder(); router.ServeHTTP(rec, r); return rec
	}
	// create a tenant user
	if rec := do("POST", "/api/admin/users", `{"username":"tenant1","password":"pw12345678","role":"tenant"}`, ck); rec.Code != 200 {
		t.Fatalf("create user=%d %s", rec.Code, rec.Body)
	}
	// list
	if rec := do("GET", "/api/admin/users", "", ck); rec.Code != 200 || !strings.Contains(rec.Body.String(), "tenant1") {
		t.Fatalf("list=%d %s", rec.Code, rec.Body)
	}
	u, err := q.GetTenantByUsername(ctx, "tenant1")
	if err != nil { t.Fatal(err) }
	// change role → admin
	if rec := do("PATCH", "/api/admin/users/"+u.ID+"/role", `{"role":"admin"}`, ck); rec.Code != 200 {
		t.Fatalf("role=%d", rec.Code)
	}
	// set hosting rate
	if rec := do("PATCH", "/api/admin/users/"+u.ID+"/hosting-rate", `{"rate":1.5}`, ck); rec.Code != 200 {
		t.Fatalf("rate=%d", rec.Code)
	}
	// change own password (session sub = tenant1's id). The role change above
	// bumped tenant1's session_epoch, so the token must carry the current epoch
	// for requireSession to accept it (auth-session-1).
	ep, _ := q.GetSessionEpoch(ctx, u.ID)
	tok := auth.IssueSession(secret, u.ID, "admin", ep, nowUnix(), 3600)
	uck := &http.Cookie{Name: "tower_session", Value: tok}
	if rec := do("POST", "/auth/change-password", `{"oldPassword":"pw12345678","newPassword":"newpw123456"}`, uck); rec.Code != 200 {
		t.Fatalf("change-pw=%d %s", rec.Code, rec.Body)
	}
	u2, _ := q.GetTenantByID(ctx, u.ID)
	if !auth.VerifyPassword("newpw123456", u2.PwHash, u2.Salt) {
		t.Fatal("password not changed")
	}
	// The password change bumped the epoch again, revoking the old token. Re-issue
	// at the new epoch so the next request reaches the handler's wrong-password
	// check (rather than being rejected by the epoch check).
	ep2, _ := q.GetSessionEpoch(ctx, u.ID)
	uck = &http.Cookie{Name: "tower_session", Value: auth.IssueSession(secret, u.ID, "admin", ep2, nowUnix(), 3600)}
	// wrong old password → 401
	if rec := do("POST", "/auth/change-password", `{"oldPassword":"WRONG","newPassword":"another12345"}`, uck); rec.Code != 401 {
		t.Fatalf("wrong-old=%d want 401", rec.Code)
	}
	// delete user
	if rec := do("DELETE", "/api/admin/users/"+u.ID, "", ck); rec.Code != 200 {
		t.Fatalf("delete=%d", rec.Code)
	}
	// non-admin (viewer) cannot list users → 403 (seeded real user).
	if rec := do("GET", "/api/admin/users", "", seedSessionCookie(t, ctx, q, secret, "u_v", "viewer")); rec.Code != 403 {
		t.Fatalf("viewer list=%d want 403", rec.Code)
	}
}
