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

// TestAdminCreateFallbackLimit verifies that POST /api/admin/fallback-channels
// enforces the per-tenant FallbackLimit when the created channel is owner-scoped.
// A superadmin creating a second channel for an owner that has already reached its
// limit must receive 403 — the same gate meCreateFallbackHandler applies.
func TestAdminCreateFallbackLimit(t *testing.T) {
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
	t.Cleanup(pool.Close)
	q := sqlc.New(pool)
	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q, false, nil, "")

	// Create a tenant with fallback_limit=1.
	owner := randHex("owFL_")
	if _, err := q.CreateTenant(ctx, sqlc.CreateTenantParams{
		ID: owner, Username: owner, PwHash: "h", Salt: "s",
		Role: "tenant", IngestKey: randHex("ik_"),
	}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	if err := q.SetTenantFallbackLimit(ctx, sqlc.SetTenantFallbackLimitParams{
		ID: owner, FallbackLimit: 1,
	}); err != nil {
		t.Fatalf("set fallback limit: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		if rows, err := q.ListFallbackChannelsByOwner(cctx, owner); err == nil {
			for _, c := range rows {
				_ = q.DeleteFallbackChannel(cctx, c.ID)
			}
		}
		_ = q.DeleteTenant(cctx, owner)
	})

	// Use a superadmin cookie so the admin route is accessible.
	ck := adminCookie(t, ctx, q, secret)
	doPost := func(body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/admin/fallback-channels", strings.NewReader(body))
		r.AddCookie(ck)
		r.Header.Set("X-Requested-With", "tower")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		return rec
	}

	// First channel for this owner — must succeed.
	body1 := `{"name":"ch1","baseUrl":"http://a","ownerId":"` + owner + `"}`
	if rec := doPost(body1); rec.Code != 200 {
		t.Fatalf("create #1 = %d want 200; body=%s", rec.Code, rec.Body)
	}

	// Second channel for the same owner — must be rejected (limit=1).
	body2 := `{"name":"ch2","baseUrl":"http://b","ownerId":"` + owner + `"}`
	rec := doPost(body2)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("create #2 = %d want 403; body=%s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "fallback channel limit reached") {
		t.Fatalf("create #2 body=%s want limit error", rec.Body)
	}
}

func TestFallbackChannelCRUD(t *testing.T) {
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
	router := NewRouter(pool, secret, nil, q, false, nil, "")
	ck := adminCookie(t, ctx, q, secret)
	do := func(m, p, b string) *httptest.ResponseRecorder {
		var r *http.Request
		if b != "" {
			r = httptest.NewRequest(m, p, strings.NewReader(b))
		} else {
			r = httptest.NewRequest(m, p, nil)
		}
		r.AddCookie(ck)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		return rec
	}
	// create
	rec := do("POST", "/api/admin/fallback-channels", `{"name":"relay1","baseUrl":"http://relay:8080","apiKey":"sk-x","priority":50,"weight":100,"priceThreshold":0.01}`)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "id") {
		t.Fatalf("create=%d %s", rec.Code, rec.Body)
	}
	// list (no api_key leaked)
	rec2 := do("GET", "/api/admin/fallback-channels", "")
	if rec2.Code != 200 || !strings.Contains(rec2.Body.String(), "relay1") {
		t.Fatalf("list=%d %s", rec2.Code, rec2.Body)
	}
	if strings.Contains(rec2.Body.String(), "sk-x") {
		t.Fatal("api_key must not be leaked in list")
	}
	// grab id
	rows, _ := q.ListAllFallbackChannels(ctx)
	if len(rows) < 1 {
		t.Fatal("no channel row")
	}
	id := rows[0].ID
	// disable
	if rec := do("PATCH", "/api/admin/fallback-channels/"+id+"/enabled", `{"enabled":false}`); rec.Code != 200 {
		t.Fatalf("disable=%d", rec.Code)
	}
	// delete
	if rec := do("DELETE", "/api/admin/fallback-channels/"+id, ""); rec.Code != 200 {
		t.Fatalf("delete=%d", rec.Code)
	}
	rows2, _ := q.ListAllFallbackChannels(ctx)
	if len(rows2) != 0 {
		t.Fatalf("after delete rows=%d", len(rows2))
	}
	// 403 for viewer (seeded real user so the epoch check passes and the role
	// gate is what rejects it).
	r5 := httptest.NewRequest("GET", "/api/admin/fallback-channels", nil)
	r5.AddCookie(seedSessionCookie(t, ctx, q, secret, "u", "viewer"))
	rec5 := httptest.NewRecorder()
	router.ServeHTTP(rec5, r5)
	if rec5.Code != 403 {
		t.Fatalf("viewer=%d want 403", rec5.Code)
	}
}
