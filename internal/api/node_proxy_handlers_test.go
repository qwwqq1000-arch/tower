package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// TestNodeProxyHandlers is a DB-backed integration test for the three egress
// proxy admin routes. It verifies: GET/test/set transparently proxy to the
// node's /settings/api/proxy endpoints (and forward the raw body + restarting
// flag), CPA nodes return 409, and a non-owner admin gets 404.
func TestNodeProxyHandlers(t *testing.T) {
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

	var gotTestRaw, gotSetRaw string
	fakeNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/settings/api/proxy" && r.Method == "GET":
			_, _ = w.Write([]byte(`{"proxy":"socks5://h:1:u:p","parsed":null}`))
		case r.URL.Path == "/settings/api/proxy/test" && r.Method == "POST":
			var b struct {
				Raw string `json:"raw"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			gotTestRaw = b.Raw
			_, _ = w.Write([]byte(`{"ok":true,"egressIp":"9.9.9.9"}`))
		case r.URL.Path == "/settings/api/proxy" && r.Method == "POST":
			var b struct {
				Raw string `json:"raw"`
			}
			_ = json.NewDecoder(r.Body).Decode(&b)
			gotSetRaw = b.Raw
			_, _ = w.Write([]byte(`{"ok":true,"restarting":true}`))
		case r.URL.Path == "/health":
			_, _ = w.Write([]byte(`{"status":"ok","version":"test","auth":{}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer fakeNode.Close()

	b := make([]byte, 6)
	_, _ = randReadR(b)
	sfx := hexR(b)

	merID := "n_proxy_mer_" + sfx
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{
		ID: merID, Name: "mer", BaseUrl: fakeNode.URL, ApiKey: "k", OwnerID: "o", Kind: "meridian",
	}); err != nil {
		t.Fatalf("create meridian node: %v", err)
	}
	cpaID := "n_proxy_cpa_" + sfx
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{
		ID: cpaID, Name: "cpa", BaseUrl: fakeNode.URL, ApiKey: "k", MgmtKey: "m", OwnerID: "o", Kind: "cpa",
	}); err != nil {
		t.Fatalf("create cpa node: %v", err)
	}

	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q, false, nil, "")
	admin := adminCookie(t, ctx, q, secret)
	do := func(ck *http.Cookie, m, path, body string) *httptest.ResponseRecorder {
		var req *http.Request
		if body != "" {
			req = httptest.NewRequest(m, path, strings.NewReader(body))
		} else {
			req = httptest.NewRequest(m, path, nil)
		}
		req.AddCookie(ck)
		if m != "GET" && m != "HEAD" {
			req.Header.Set("X-Requested-With", "tower") // CSRF guard (requireSameOrigin)
		}
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// GET proxy transparently proxies the node's current proxy.
	if rec := do(admin, "GET", "/api/admin/nodes/"+merID+"/proxy", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "socks5://h:1:u:p") {
		t.Fatalf("get proxy: code=%d body=%s", rec.Code, rec.Body)
	}
	// test proxy forwards the raw body and returns the node's reachability result.
	if rec := do(admin, "POST", "/api/admin/nodes/"+merID+"/proxy/test", `{"raw":"socks5://x:2:a:b"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), "9.9.9.9") {
		t.Fatalf("test proxy: code=%d body=%s", rec.Code, rec.Body)
	}
	if gotTestRaw != "socks5://x:2:a:b" {
		t.Fatalf("test proxy raw not forwarded: %q", gotTestRaw)
	}
	// set proxy forwards the raw body and returns the restarting flag.
	if rec := do(admin, "POST", "/api/admin/nodes/"+merID+"/proxy", `{"raw":"socks5://y:3:c:d"}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), "restarting") {
		t.Fatalf("set proxy: code=%d body=%s", rec.Code, rec.Body)
	}
	if gotSetRaw != "socks5://y:3:c:d" {
		t.Fatalf("set proxy raw not forwarded: %q", gotSetRaw)
	}

	// CPA node: all three routes must 409.
	for _, tc := range []struct{ m, p, body string }{
		{"GET", "/api/admin/nodes/" + cpaID + "/proxy", ""},
		{"POST", "/api/admin/nodes/" + cpaID + "/proxy/test", `{"raw":"x"}`},
		{"POST", "/api/admin/nodes/" + cpaID + "/proxy", `{"raw":"x"}`},
	} {
		if rec := do(admin, tc.m, tc.p, tc.body); rec.Code != 409 {
			t.Errorf("cpa %s %s: want 409, got %d", tc.m, tc.p, rec.Code)
		}
	}

	// Non-owner admin (role admin, different sub) → 404 owner-scope.
	other := seedSessionCookie(t, ctx, q, secret, "u_other_"+sfx, "admin")
	if rec := do(other, "GET", "/api/admin/nodes/"+merID+"/proxy", ""); rec.Code != 404 {
		t.Errorf("non-owner get proxy: want 404, got %d", rec.Code)
	}
}
