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

// TestNodeQuotaKindRouting verifies that nodeQuotaHandler dispatches to the CPA
// usage API (not the meridian /v1/usage/quota/all) when the node kind is "cpa",
// and to the meridian endpoint when the kind is anything else.
func TestNodeQuotaKindRouting(t *testing.T) {
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

	// Track which endpoints were called on the fake upstream server.
	var calledCPAAuthFiles bool
	var calledMeridianQuota bool

	// Fake upstream that serves both CPA management endpoints and the meridian
	// /v1/usage/quota/all endpoint. We record which branch was exercised.
	fakeNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v0/management/auth-files":
			calledCPAAuthFiles = true
			_, _ = w.Write([]byte(`{"files":[]}`))
		case strings.HasPrefix(r.URL.Path, "/v0/management/account-usage"):
			_, _ = w.Write([]byte(`{}`))
		case r.URL.Path == "/v1/usage/quota/all":
			calledMeridianQuota = true
			_, _ = w.Write([]byte(`{"profiles":[]}`))
		// nodeclient.New also checks health for token auth setup on some paths;
		// return a sensible default.
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

	// Create a CPA node.
	cpaNodeID := "n_cpa_" + sfx
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{
		ID: cpaNodeID, Name: "cpa-node", BaseUrl: fakeNode.URL,
		ApiKey: "apikey", MgmtKey: "mgmtkey", OwnerID: "o", Kind: "cpa",
	}); err != nil {
		t.Fatalf("create cpa node: %v", err)
	}

	// Create a meridian node.
	meridianNodeID := "n_mer_" + sfx
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{
		ID: meridianNodeID, Name: "mer-node", BaseUrl: fakeNode.URL,
		ApiKey: "apikey2", MgmtKey: "", OwnerID: "o", Kind: "meridian",
	}); err != nil {
		t.Fatalf("create meridian node: %v", err)
	}

	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q, false, nil)
	ck := adminCookie(t, ctx, q, secret)
	do := func(p string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", p, nil)
		r.AddCookie(ck)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		return rec
	}

	// CPA node: should call CPA auth-files endpoint, not meridian quota.
	calledCPAAuthFiles = false
	calledMeridianQuota = false
	rec := do("/api/admin/nodes/" + cpaNodeID + "/quota")
	if rec.Code != 200 {
		t.Fatalf("cpa quota: status=%d body=%s", rec.Code, rec.Body)
	}
	if !calledCPAAuthFiles {
		t.Error("cpa quota: expected CPA /v0/management/auth-files to be called")
	}
	if calledMeridianQuota {
		t.Error("cpa quota: expected meridian /v1/usage/quota/all NOT to be called")
	}

	// Meridian node: should call meridian quota endpoint, not CPA auth-files.
	calledCPAAuthFiles = false
	calledMeridianQuota = false
	rec = do("/api/admin/nodes/" + meridianNodeID + "/quota")
	if rec.Code != 200 {
		t.Fatalf("meridian quota: status=%d body=%s", rec.Code, rec.Body)
	}
	if !calledMeridianQuota {
		t.Error("meridian quota: expected meridian /v1/usage/quota/all to be called")
	}
	if calledCPAAuthFiles {
		t.Error("meridian quota: expected CPA /v0/management/auth-files NOT to be called")
	}
}

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
	router := NewRouter(pool, secret, nil, q, false, nil)
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
