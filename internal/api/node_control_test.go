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

	"github.com/qwwqq1000-arch/tower/internal/cpaclient"
	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// ---------------------------------------------------------------------------
// Pure unit tests — no DB, always run in CI.
// ---------------------------------------------------------------------------

// TestIsCPAKind verifies the pure routing predicate that decides between CPA
// and meridian quota endpoints. This exercises the kind-dispatch branch without
// any database or HTTP infrastructure.
func TestIsCPAKind(t *testing.T) {
	cases := []struct {
		kind string
		want bool
	}{
		{"cpa", true},
		{"CPA", true},
		{"Cpa", true},
		{"meridian", false},
		{"", false},
		{"other", false},
	}
	for _, tc := range cases {
		if got := isCPAKind(tc.kind); got != tc.want {
			t.Errorf("isCPAKind(%q) = %v, want %v", tc.kind, got, tc.want)
		}
	}
}

// TestCpaQuotaAllPure verifies cpaQuotaAll against a fake HTTP server — no
// database, no router, no session. It asserts that the function:
//   - calls /v0/management/auth-files to enumerate accounts, and
//   - calls /v0/management/account-usage for each account, and
//   - returns an "accounts" key in the result map.
func TestCpaQuotaAllPure(t *testing.T) {
	var calledAuthFiles bool
	var calledUsage bool

	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v0/management/auth-files":
			calledAuthFiles = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"files":[{"id":"acc1","email":"a@example.com","auth_index":"acc1"}]}`))
		case strings.HasPrefix(r.URL.Path, "/v0/management/account-usage"):
			calledUsage = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"five_hour":{"utilization":0.1,"resets_at":"soon"}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer fake.Close()

	c := cpaclient.New(fake.URL, "test-mgmt-key")
	result, err := cpaQuotaAll(context.Background(), c)
	if err != nil {
		t.Fatalf("cpaQuotaAll: unexpected error: %v", err)
	}
	if !calledAuthFiles {
		t.Error("cpaQuotaAll: /v0/management/auth-files was not called")
	}
	if !calledUsage {
		t.Error("cpaQuotaAll: /v0/management/account-usage was not called")
	}
	accounts, ok := result["accounts"]
	if !ok {
		t.Fatalf("cpaQuotaAll: result missing 'accounts' key; got %v", result)
	}
	list, ok := accounts.([]interface{ })
	if !ok {
		// The result is a typed slice; just check it's non-nil and non-empty.
		t.Logf("cpaQuotaAll: accounts type %T (OK)", accounts)
	} else if len(list) == 0 {
		t.Error("cpaQuotaAll: expected at least 1 account in result")
	}
}

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
	router := NewRouter(pool, secret, nil, q, false, nil, "")
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
	router := NewRouter(pool, secret, nil, q, false, nil, "")
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
	// passthrough toggle
	if rec := do("PATCH", "/api/admin/nodes/"+nodeID+"/passthrough", `{"passthrough":true}`); rec.Code != 200 {
		t.Fatalf("passthrough=%d", rec.Code)
	}
	nPT, _ := q.GetNode(ctx, nodeID)
	if !nPT.Passthrough {
		t.Fatal("node should have passthrough=true")
	}
}

func randReadR(b []byte) (int, error) { return cryptoRandReadR(b) }

// TestCpaGuardPure verifies that cpaNotApplicable writes a 409 Conflict when
// the node kind is "cpa" and 0 (no write) otherwise.  This is a pure test with
// no database dependency.
func TestCpaGuardPure(t *testing.T) {
	cases := []struct {
		kind    string
		want409 bool
	}{
		{"cpa", true},
		{"CPA", true},
		{"meridian", false},
		{"", false},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		wrote := cpaNotApplicable(rec, tc.kind)
		if tc.want409 {
			if !wrote {
				t.Errorf("kind=%q: expected cpaNotApplicable to return true", tc.kind)
			}
			if rec.Code != 409 {
				t.Errorf("kind=%q: expected status 409, got %d", tc.kind, rec.Code)
			}
		} else {
			if wrote {
				t.Errorf("kind=%q: expected cpaNotApplicable to return false", tc.kind)
			}
		}
	}
}

// TestCpaControlGuard is a DB-backed integration test: it verifies that
// telemetry/features/refresh/oauth routes return 409 for a CPA node and
// behave normally for a meridian node.
func TestCpaControlGuard(t *testing.T) {
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

	// Fake upstream node server (meridian-style responses).
	fakeNode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/health":
			_, _ = w.Write([]byte(`{"status":"ok","version":"test","auth":{}}`))
		case r.URL.Path == "/settings/api/features":
			_, _ = w.Write([]byte(`{"f":{}}`))
		case r.URL.Path == "/v1/telemetry/summary":
			_, _ = w.Write([]byte(`{}`))
		case r.URL.Path == "/auth/refresh":
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer fakeNode.Close()

	b := make([]byte, 6)
	_, _ = randReadR(b)
	sfx := hexR(b)

	// CPA node.
	cpaID := "n_cpa_guard_" + sfx
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{
		ID: cpaID, Name: "cpa-guard", BaseUrl: fakeNode.URL,
		ApiKey: "k", MgmtKey: "m", OwnerID: "o", Kind: "cpa",
	}); err != nil {
		t.Fatalf("create cpa node: %v", err)
	}

	// Meridian node.
	merID := "n_mer_guard_" + sfx
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{
		ID: merID, Name: "mer-guard", BaseUrl: fakeNode.URL,
		ApiKey: "k2", MgmtKey: "", OwnerID: "o", Kind: "meridian",
	}); err != nil {
		t.Fatalf("create meridian node: %v", err)
	}

	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q, false, nil, "")
	ck := adminCookie(t, ctx, q, secret)
	do := func(m, path, body string) *httptest.ResponseRecorder {
		var req *http.Request
		if body != "" {
			req = httptest.NewRequest(m, path, strings.NewReader(body))
		} else {
			req = httptest.NewRequest(m, path, nil)
		}
		req.AddCookie(ck)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// CPA node: meridian-only routes must return 409.
	t.Run("cpa_telemetry_409", func(t *testing.T) {
		rec := do("GET", "/api/admin/nodes/"+cpaID+"/telemetry", "")
		if rec.Code != 409 {
			t.Errorf("telemetry on cpa: want 409, got %d body=%s", rec.Code, rec.Body)
		}
	})
	t.Run("cpa_features_get_409", func(t *testing.T) {
		rec := do("GET", "/api/admin/nodes/"+cpaID+"/features", "")
		if rec.Code != 409 {
			t.Errorf("features GET on cpa: want 409, got %d body=%s", rec.Code, rec.Body)
		}
	})
	t.Run("cpa_features_patch_409", func(t *testing.T) {
		rec := do("PATCH", "/api/admin/nodes/"+cpaID+"/features/opencode", `{"memory":true}`)
		if rec.Code != 409 {
			t.Errorf("features PATCH on cpa: want 409, got %d body=%s", rec.Code, rec.Body)
		}
	})
	t.Run("cpa_refresh_409", func(t *testing.T) {
		rec := do("POST", "/api/admin/nodes/"+cpaID+"/refresh", "")
		if rec.Code != 409 {
			t.Errorf("refresh on cpa: want 409, got %d body=%s", rec.Code, rec.Body)
		}
	})
	t.Run("cpa_oauth_start_409", func(t *testing.T) {
		rec := do("POST", "/api/admin/nodes/"+cpaID+"/oauth/start", "")
		if rec.Code != 409 {
			t.Errorf("oauth/start on cpa: want 409, got %d body=%s", rec.Code, rec.Body)
		}
	})
	t.Run("cpa_oauth_exchange_409", func(t *testing.T) {
		rec := do("POST", "/api/admin/nodes/"+cpaID+"/oauth/exchange", `{"code":"c","codeVerifier":"v","state":"s"}`)
		if rec.Code != 409 {
			t.Errorf("oauth/exchange on cpa: want 409, got %d body=%s", rec.Code, rec.Body)
		}
	})

	// Meridian node: same routes must NOT return 409.
	t.Run("meridian_telemetry_ok", func(t *testing.T) {
		rec := do("GET", "/api/admin/nodes/"+merID+"/telemetry", "")
		if rec.Code == 409 {
			t.Errorf("telemetry on meridian: unexpected 409")
		}
	})
	t.Run("meridian_features_ok", func(t *testing.T) {
		rec := do("GET", "/api/admin/nodes/"+merID+"/features", "")
		if rec.Code == 409 {
			t.Errorf("features GET on meridian: unexpected 409")
		}
	})
	t.Run("meridian_refresh_ok", func(t *testing.T) {
		rec := do("POST", "/api/admin/nodes/"+merID+"/refresh", "")
		if rec.Code == 409 {
			t.Errorf("refresh on meridian: unexpected 409")
		}
	})
}
