package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func cryptoRandReadR(b []byte) (int, error) { return rand.Read(b) }
func hexR(b []byte) string                  { return hex.EncodeToString(b) }

func TestNodeControl(t *testing.T) {
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

	var patched bool
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/settings/api/features" && r.Method == "GET":
			_, _ = w.Write([]byte(`{"opencode":{"memory":false}}`))
		case strings.HasPrefix(r.URL.Path, "/settings/api/features/") && r.Method == "PATCH":
			patched = true
			_, _ = w.Write([]byte(`{"ok":true}`))
		case r.URL.Path == "/auth/refresh":
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer node.Close()

	b := make([]byte, 6)
	_, _ = randReadR(b)
	sfx := hexR(b)
	nodeID := "n_" + sfx
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{ID: nodeID, Name: "n", BaseUrl: node.URL, ApiKey: "k", OwnerID: "o"}); err != nil {
		t.Fatal(err)
	}

	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q)
	ck := adminCookie(t, ctx, q, secret)
	do := func(m, p, body string) *httptest.ResponseRecorder {
		var r *http.Request
		if body != "" {
			r = httptest.NewRequest(m, p, strings.NewReader(body))
		} else {
			r = httptest.NewRequest(m, p, nil)
		}
		r.AddCookie(ck)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		return rec
	}

	if rec := do("GET", "/api/admin/nodes/"+nodeID+"/features", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "opencode") {
		t.Fatalf("features=%d %s", rec.Code, rec.Body)
	}
	if rec := do("PATCH", "/api/admin/nodes/"+nodeID+"/features/opencode", `{"memory":true}`); rec.Code != 200 {
		t.Fatalf("patch=%d", rec.Code)
	}
	if !patched {
		t.Fatal("node not patched")
	}
	if rec := do("POST", "/api/admin/nodes/"+nodeID+"/refresh", ""); rec.Code != 200 {
		t.Fatalf("refresh=%d", rec.Code)
	}
	if rec := do("PATCH", "/api/admin/nodes/"+nodeID+"/enabled", `{"enabled":false}`); rec.Code != 200 {
		t.Fatalf("enable=%d", rec.Code)
	}
	n, _ := q.GetNode(ctx, nodeID)
	if n.Enabled {
		t.Fatal("node should be disabled")
	}
}

func randReadR(b []byte) (int, error) { return cryptoRandReadR(b) }
