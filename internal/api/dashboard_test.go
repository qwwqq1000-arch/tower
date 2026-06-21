package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func TestDashboardComprehensive(t *testing.T) {
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
	// seed a node + a today dispatch log (opus, tokens) for cost
	_, _ = q.CreateNode(ctx, sqlc.CreateNodeParams{ID: "n_dash", Name: "n", BaseUrl: "http://x", ApiKey: "k"})
	now := time.Now().UnixMilli()
	_ = q.InsertDispatchLog(ctx, sqlc.InsertDispatchLogParams{Ts: now, OwnerID: "o", Model: "claude-opus-4-8", Target: "node", Status: "ok", HttpStatus: 200, TokensIn: 1000, TokensOut: 2000, CostUsd: 0.055})

	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q)
	req := httptest.NewRequest("GET", "/api/dashboard", nil)
	req.AddCookie(adminCookie(t, secret))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d %s", rec.Code, rec.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	for _, k := range []string{"nodes", "accounts", "today", "hosting", "totalCostUsd"} {
		if _, ok := out[k]; !ok {
			t.Fatalf("missing %s in dashboard", k)
		}
	}
	today := out["today"].(map[string]any)
	if today["costUsd"].(float64) <= 0 {
		t.Fatalf("today cost should be > 0, got %v", today["costUsd"])
	}
}
