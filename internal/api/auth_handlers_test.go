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

// TestMeHandlerMustChangePwField is a pure (no-DB) test that asserts the /auth/me
// response includes a "mustChangePw" field. When the pool is nil, the field
// defaults to false (no forced change). The DB-backed case is covered by the
// integration test TestLoginMeLogout.
func TestMeHandlerMustChangePwField(t *testing.T) {
	const secret = "test-secret-padding-to-32-chars!"
	// Build handler with nil pool (skips DB calls → must_change_pw defaults false).
	handler := requireSession(secret, nil, meHandler(nil))

	r := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	r.AddCookie(&http.Cookie{
		Name:  "tower_session",
		Value: auth.IssueSession(secret, "u_test", "admin", 0, nowUnix(), 3600),
	})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, ok := body["mustChangePw"]; !ok {
		t.Fatal("response missing mustChangePw field")
	}
}

func TestLoginMeLogout(t *testing.T) {
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

	// seed an admin user
	h, s, _ := auth.HashPassword("pw12345678")
	q := sqlc.New(pool)
	_, err = q.CreateTenant(ctx, sqlc.CreateTenantParams{
		ID: "u_admin_test", Username: "admin_b6", PwHash: h, Salt: s, Role: "admin", IngestKey: "ik_b6",
	})
	if err != nil && !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("seed: %v", err)
	}

	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, nil, false)

	// login
	body := strings.NewReader(`{"username":"admin_b6","password":"pw12345678"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, want 200", rec.Code)
	}
	cookie := rec.Result().Cookies()
	if len(cookie) == 0 {
		t.Fatal("no session cookie set")
	}

	// me (with cookie)
	req2 := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	req2.AddCookie(cookie[0])
	rec2 := httptest.NewRecorder()
	router.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("me status = %d, want 200", rec2.Code)
	}
	var me map[string]any
	_ = json.NewDecoder(rec2.Body).Decode(&me)
	if me["role"] != "admin" {
		t.Fatalf("me role = %v", me["role"])
	}

	// me without cookie → 401
	rec3 := httptest.NewRecorder()
	router.ServeHTTP(rec3, httptest.NewRequest(http.MethodGet, "/auth/me", nil))
	if rec3.Code != http.StatusUnauthorized {
		t.Fatalf("me-no-cookie status = %d, want 401", rec3.Code)
	}

	// wrong password → 401
	rec4 := httptest.NewRecorder()
	router.ServeHTTP(rec4, httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(`{"username":"admin_b6","password":"nope"}`)))
	if rec4.Code != http.StatusUnauthorized {
		t.Fatalf("bad-login status = %d, want 401", rec4.Code)
	}
}
