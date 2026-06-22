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

func TestUpdateNodeAccount(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" { t.Skip("TEST_DATABASE_URL not set") }
	ctx := context.Background()
	if err := db.Migrate(ctx, url); err != nil { t.Fatalf("migrate: %v", err) }
	pool, err := db.Connect(ctx, url)
	if err != nil { t.Fatalf("connect: %v", err) }
	defer pool.Close()
	q := sqlc.New(pool)
	nid, aid := "n_ua", "a_ua"
	_, _ = q.CreateNode(ctx, sqlc.CreateNodeParams{ID: nid, Name: "n", BaseUrl: "http://x", ApiKey: "k", OwnerID: "o"})
	_, _ = q.AssignAccount(ctx, sqlc.AssignAccountParams{NodeID: nid, AccountID: aid, ProfileID: "default", Weight: 100, Role: "baseline"})

	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q, false)
	req := httptest.NewRequest("PATCH", "/api/admin/accounts/"+nid+"/"+aid, strings.NewReader(`{"egress":"1.2.3.4","weight":50,"role":"reserve","enabled":false}`))
	req.AddCookie(adminCookie(t, ctx, q, secret))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 { t.Fatalf("patch=%d %s", rec.Code, rec.Body) }
	rows, _ := q.ListNodeAccountsByNode(ctx, nid)
	if len(rows) != 1 || rows[0].Weight != 50 || rows[0].Role != "reserve" || rows[0].Egress != "1.2.3.4" || rows[0].Enabled {
		t.Fatalf("updated row = %+v", rows[0])
	}
}
