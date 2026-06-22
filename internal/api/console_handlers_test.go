package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/policy"
)

// TestDryRunUsesDefaults_WhenNoQ verifies that dryRunPolicyHandler falls back
// to policy.Defaults() as the base when q is nil (no DB). This is the
// backward-compatible degraded path and is a pure unit test.
func TestDryRunUsesDefaults_WhenNoQ(t *testing.T) {
	handler := dryRunPolicyHandler(nil)
	// Patch MaxConcurrent to a value different from Defaults (default=3).
	body := `{"MaxConcurrent":10}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/policies/dry-run", strings.NewReader(body))
	req.Header.Set("X-Requested-With", "tower")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body)
	}
	var resp struct {
		Final policy.Config `json:"final"`
		Diffs []struct {
			Field string `json:"Field"`
			From  string `json:"From"`
			To    string `json:"To"`
		} `json:"diffs"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Final.MaxConcurrent != 10 {
		t.Fatalf("final.MaxConcurrent=%d, want 10", resp.Final.MaxConcurrent)
	}
	// With q=nil, the base is Defaults (MaxConcurrent=3), so the diff should
	// show From="3".
	defaults := policy.Defaults()
	var mcDiff *struct {
		Field string `json:"Field"`
		From  string `json:"From"`
		To    string `json:"To"`
	}
	for i := range resp.Diffs {
		if resp.Diffs[i].Field == "MaxConcurrent" {
			mcDiff = &resp.Diffs[i]
			break
		}
	}
	if mcDiff == nil {
		t.Fatal("MaxConcurrent diff missing from diffs")
	}
	wantFrom := fmt.Sprintf("%d", defaults.MaxConcurrent)
	if mcDiff.From != wantFrom {
		t.Fatalf("MaxConcurrent diff From=%q, want %q (Defaults value)", mcDiff.From, wantFrom)
	}
}

// TestDryRunUsesStoredPolicy verifies that dryRunPolicyHandler uses the stored
// effective global policy as the base when a DB is available and a global policy
// row exists. This is the key fix: the preview should show diffs relative to
// what the running system already has configured, not hard-coded Defaults.
func TestDryRunUsesStoredPolicy(t *testing.T) {
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

	// Store a global policy with MaxConcurrent=7 (different from Defaults=3).
	if err := q.UpsertPolicy(ctx, sqlc.UpsertPolicyParams{
		ScopeType: "global",
		ScopeID:   "_",
		Params:    []byte(`{"MaxConcurrent":7}`),
		UpdatedAt: 1,
	}); err != nil {
		t.Fatalf("upsert policy: %v", err)
	}

	handler := dryRunPolicyHandler(q)
	// Patch MaxConcurrent to 10 — the diff should show From="7" (stored), not From="3" (Defaults).
	body := `{"MaxConcurrent":10}`
	req := httptest.NewRequest(http.MethodPost, "/api/admin/policies/dry-run", strings.NewReader(body))
	req.Header.Set("X-Requested-With", "tower")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200; body=%s", rec.Code, rec.Body)
	}
	var resp struct {
		Final policy.Config `json:"final"`
		Diffs []struct {
			Field string `json:"Field"`
			From  string `json:"From"`
			To    string `json:"To"`
		} `json:"diffs"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Final.MaxConcurrent != 10 {
		t.Fatalf("final.MaxConcurrent=%d, want 10", resp.Final.MaxConcurrent)
	}
	// With stored MaxConcurrent=7, the base is 7 so diff should show From="7".
	var mcDiff *struct {
		Field string `json:"Field"`
		From  string `json:"From"`
		To    string `json:"To"`
	}
	for i := range resp.Diffs {
		if resp.Diffs[i].Field == "MaxConcurrent" {
			mcDiff = &resp.Diffs[i]
			break
		}
	}
	if mcDiff == nil {
		t.Fatal("MaxConcurrent diff missing")
	}
	if mcDiff.From != "7" {
		t.Fatalf("MaxConcurrent diff From=%q, want \"7\" (stored value, not Defaults)", mcDiff.From)
	}
}

func TestConsoleAPIs(t *testing.T) {
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
	router := NewRouter(pool, secret, nil, q, false)
	ck := adminCookie(t, ctx, q, secret)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		var r *http.Request
		if body != "" {
			r = httptest.NewRequest(method, path, strings.NewReader(body))
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		r.AddCookie(ck)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		return rec
	}

	if rec := do("PUT", "/api/admin/policies/global", `{"MaxConcurrent":7}`); rec.Code != 200 {
		t.Fatalf("put policy=%d %s", rec.Code, rec.Body)
	}
	if rec := do("GET", "/api/admin/policies", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "global") {
		t.Fatalf("list policy=%d %s", rec.Code, rec.Body)
	}
	if rec := do("POST", "/api/admin/policies/dry-run", `{"MaxConcurrent":10}`); rec.Code != 200 || !strings.Contains(rec.Body.String(), "MaxConcurrent") {
		t.Fatalf("dry-run=%d %s", rec.Code, rec.Body)
	}
	if rec := do("PUT", "/api/admin/desired", `{"opencode":{"memory":true}}`); rec.Code != 200 {
		t.Fatalf("put desired=%d", rec.Code)
	}
	if rec := do("GET", "/api/admin/desired", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "opencode") {
		t.Fatalf("get desired=%d %s", rec.Code, rec.Body)
	}
	for _, p := range []string{"/api/admin/logs", "/api/admin/events", "/api/admin/audit"} {
		if rec := do("GET", p, ""); rec.Code != 200 {
			t.Fatalf("GET %s = %d", p, rec.Code)
		}
	}
	// guard: no cookie → 401
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("GET", "/api/admin/policies", nil))
	if rec.Code != 401 {
		t.Fatalf("no-cookie=%d want 401", rec.Code)
	}
}
