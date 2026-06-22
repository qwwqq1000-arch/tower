package api

import (
	"context"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func TestBanAnalysis(t *testing.T) {
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
	_ = q.InsertBanEpisode(ctx, sqlc.InsertBanEpisodeParams{NodeID: "n", ProfileID: "p", BannedAt: 1718000000000, Detail: []byte("{}")})
	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q)
	req := httptest.NewRequest("GET", "/api/admin/ban-analysis", nil)
	req.AddCookie(adminCookie(t, ctx, q, secret))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "byWeekday") {
		t.Fatalf("ban-analysis=%d %s", rec.Code, rec.Body)
	}
}
