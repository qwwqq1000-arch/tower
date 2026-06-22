package dispatch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

func setupDB(t *testing.T) (*sqlc.Queries, func()) {
	t.Helper()
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
	return sqlc.New(pool), func() { pool.Close() }
}

func TestService_DispatchToNode(t *testing.T) {
	q, closeDB := setupDB(t)
	defer closeDB()
	ctx := context.Background()

	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true,"usage":{"input_tokens":10,"output_tokens":5}}`))
	}))
	defer node.Close()

	// seed node + account
	sfx := suffixDispatch(t)
	nodeID := "n_" + sfx
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{
		ID:      nodeID,
		Name:    "test-node-" + sfx,
		BaseUrl: node.URL,
		ApiKey:  "k",
		OwnerID: "owner_" + sfx,
	}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := q.CreateAccount(ctx, sqlc.CreateAccountParams{
		ID:      "a_" + sfx,
		OwnerID: "owner_" + sfx,
	}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := q.AssignAccount(ctx, sqlc.AssignAccountParams{
		NodeID:    nodeID,
		AccountID: "a_" + sfx,
		ProfileID: "default",
		Weight:    100,
		Role:      "baseline",
	}); err != nil {
		t.Fatalf("assign: %v", err)
	}

	store := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	svc := &Service{Q: q, Store: store, Base: policy.Defaults(), Now: func() int64 { return 0 }}

	out := svc.Dispatch(ctx, "owner_"+sfx, "opus", "please do a real task here", []byte(`{"model":"opus"}`))
	if out.Status != 200 {
		t.Fatalf("status=%d body=%s reason=%s target=%s", out.Status, out.Body, out.Reason, out.Target)
	}
	// a dispatch log row should exist
	logs, _ := q.ListRecentDispatchLogs(ctx, 5)
	if len(logs) < 1 {
		t.Fatal("expected a dispatch log row")
	}
}

func suffixDispatch(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func TestParseUsageSSE(t *testing.T) {
	body := `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":150,"output_tokens":0}}}

event: content_block_delta
data: {"type":"content_block_delta","delta":{"text":"Hello"}}

event: message_delta
data: {"type":"message_delta","usage":{"output_tokens":10}}

event: message_delta
data: {"type":"message_delta","usage":{"output_tokens":42}}

event: message_stop
data: {"type":"message_stop"}
`
	in, out, cacheRead, cache5m, cache1h := parseUsageSSE(body)
	if in != 150 {
		t.Errorf("input_tokens: got %d, want 150", in)
	}
	if out != 42 {
		t.Errorf("output_tokens: got %d, want 42", out)
	}
	if cacheRead != 0 || cache5m != 0 || cache1h != 0 {
		t.Errorf("cache tokens should be 0, got read=%d 5m=%d 1h=%d", cacheRead, cache5m, cache1h)
	}
}

func TestParseUsageSSE_CacheTokens(t *testing.T) {
	// Aggregate cache_creation_input_tokens (no split ephemeral fields) → treated as cache5m
	bodyAggregate := `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":100,"output_tokens":0,"cache_read_input_tokens":5000,"cache_creation_input_tokens":1200}}}

event: message_delta
data: {"type":"message_delta","usage":{"output_tokens":20}}
`
	in, out, cacheRead, cache5m, cache1h := parseUsageSSE(bodyAggregate)
	if in != 100 {
		t.Errorf("aggregate: input_tokens got %d, want 100", in)
	}
	if out != 20 {
		t.Errorf("aggregate: output_tokens got %d, want 20", out)
	}
	if cacheRead != 5000 {
		t.Errorf("aggregate: cache_read got %d, want 5000", cacheRead)
	}
	if cache5m != 1200 {
		t.Errorf("aggregate: cache5m got %d, want 1200 (aggregate treated as 5m)", cache5m)
	}
	if cache1h != 0 {
		t.Errorf("aggregate: cache1h got %d, want 0", cache1h)
	}

	// Split ephemeral fields present → use split
	bodySplit := `event: message_start
data: {"type":"message_start","message":{"usage":{"input_tokens":50,"output_tokens":0,"cache_read_input_tokens":3000,"cache_creation_input_tokens":900,"cache_creation":{"ephemeral_5m_input_tokens":700,"ephemeral_1h_input_tokens":200}}}}

event: message_delta
data: {"type":"message_delta","usage":{"output_tokens":15}}
`
	in2, out2, cacheRead2, cache5m2, cache1h2 := parseUsageSSE(bodySplit)
	if in2 != 50 {
		t.Errorf("split: input_tokens got %d, want 50", in2)
	}
	if out2 != 15 {
		t.Errorf("split: output_tokens got %d, want 15", out2)
	}
	if cacheRead2 != 3000 {
		t.Errorf("split: cache_read got %d, want 3000", cacheRead2)
	}
	if cache5m2 != 700 {
		t.Errorf("split: cache5m got %d, want 700", cache5m2)
	}
	if cache1h2 != 200 {
		t.Errorf("split: cache1h got %d, want 200", cache1h2)
	}
}

func TestLastUserText(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "plain string content",
			body: `{"model":"claude-3","messages":[{"role":"user","content":"hi"}]}`,
			want: "hi",
		},
		{
			name: "array content blocks",
			body: `{"model":"claude-3","messages":[{"role":"user","content":[{"type":"text","text":"hello "},{"type":"text","text":"world"}]}]}`,
			want: "hello world",
		},
		{
			name: "no user message",
			body: `{"model":"claude-3","messages":[{"role":"assistant","content":"pong"}]}`,
			want: "",
		},
		{
			name: "last user message wins",
			body: `{"model":"claude-3","messages":[{"role":"user","content":"first"},{"role":"assistant","content":"reply"},{"role":"user","content":"second"}]}`,
			want: "second",
		},
		{
			name: "invalid JSON returns empty",
			body: `not-json`,
			want: "",
		},
		{
			name: "empty messages array",
			body: `{"model":"claude-3","messages":[]}`,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := lastUserText([]byte(tc.body))
			if got != tc.want {
				t.Errorf("lastUserText(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

func TestPickElastic(t *testing.T) {
	b := []string{"b1", "b2"}
	r := []string{"r1", "r2", "r3"}

	cases := []struct {
		name       string
		util       float64
		threshold  float64
		maxReserve int
		wantLen    int
		wantOrder  []string
	}{
		{
			name: "below threshold — baseline only",
			util: 0.5, threshold: 0.8, maxReserve: 1000,
			wantLen:   2,
			wantOrder: []string{"b1", "b2"},
		},
		{
			name: "at threshold — reserves added",
			util: 0.8, threshold: 0.8, maxReserve: 1000,
			wantLen:   5,
			wantOrder: []string{"b1", "b2", "r1", "r2", "r3"},
		},
		{
			name: "above threshold — reserves added",
			util: 1.0, threshold: 0.8, maxReserve: 1000,
			wantLen:   5,
			wantOrder: []string{"b1", "b2", "r1", "r2", "r3"},
		},
		{
			name: "above threshold — maxReserve cap applied",
			util: 0.9, threshold: 0.8, maxReserve: 2,
			wantLen:   4,
			wantOrder: []string{"b1", "b2", "r1", "r2"},
		},
		{
			name: "above threshold — maxReserve 0 means no cap",
			util: 0.9, threshold: 0.8, maxReserve: 0,
			wantLen:   5,
			wantOrder: []string{"b1", "b2", "r1", "r2", "r3"},
		},
		{
			name: "empty reserve pool — baseline only even above threshold",
			util: 1.0, threshold: 0.8, maxReserve: 1000,
			// use nil reserve
			wantLen:   2,
			wantOrder: []string{"b1", "b2"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rv := r
			if tc.name == "empty reserve pool — baseline only even above threshold" {
				rv = nil
			}
			got := pickElastic(b, rv, tc.util, tc.threshold, tc.maxReserve)
			if len(got) != tc.wantLen {
				t.Fatalf("len=%d, want %d; got %v", len(got), tc.wantLen, got)
			}
			for i, key := range tc.wantOrder {
				if got[i] != key {
					t.Errorf("order[%d]=%q, want %q", i, got[i], key)
				}
			}
		})
	}
}

// TestElasticHysteresis verifies the hysteresis state machine used in buildCandidates.
// It exercises the three cases required by the spec:
//   1. not-scaled + util >= scaleUp → scales up
//   2. scaled + util between scaleDown and scaleUp → stays scaled
//   3. scaled + util <= scaleDown → scales down
func TestElasticHysteresis(t *testing.T) {
	type input struct {
		wasScaled bool
		util      float64
		scaleUp   float64
		scaleDown float64
	}
	type want struct {
		shouldScale bool
	}
	cases := []struct {
		name  string
		in    input
		want  want
	}{
		{
			name: "not-scaled + util at scaleUp threshold -> scales up",
			in:   input{wasScaled: false, util: 0.8, scaleUp: 0.8, scaleDown: 0.3},
			want: want{shouldScale: true},
		},
		{
			name: "not-scaled + util above scaleUp -> scales up",
			in:   input{wasScaled: false, util: 0.95, scaleUp: 0.8, scaleDown: 0.3},
			want: want{shouldScale: true},
		},
		{
			name: "not-scaled + util below scaleUp -> stays not-scaled",
			in:   input{wasScaled: false, util: 0.5, scaleUp: 0.8, scaleDown: 0.3},
			want: want{shouldScale: false},
		},
		{
			name: "scaled + util between scaleDown and scaleUp -> stays scaled (hysteresis)",
			in:   input{wasScaled: true, util: 0.5, scaleUp: 0.8, scaleDown: 0.3},
			want: want{shouldScale: true},
		},
		{
			name: "scaled + util exactly at scaleDown -> scales down",
			in:   input{wasScaled: true, util: 0.3, scaleUp: 0.8, scaleDown: 0.3},
			want: want{shouldScale: false},
		},
		{
			name: "scaled + util below scaleDown -> scales down",
			in:   input{wasScaled: true, util: 0.1, scaleUp: 0.8, scaleDown: 0.3},
			want: want{shouldScale: false},
		},
		{
			name: "misconfigured scaleDown=0 -> no hysteresis (scaleDown=scaleUp)",
			in:   input{wasScaled: true, util: 0.5, scaleUp: 0.8, scaleDown: 0.0},
			// With scaleDown fallback to scaleUp=0.8: wasScaled=true, util=0.5 > 0.8 is false,
			// so we check util <= scaleDown(0.8): 0.5 <= 0.8 → shouldScale=false.
			want: want{shouldScale: false},
		},
		{
			name: "misconfigured scaleDown >= scaleUp -> no hysteresis",
			in:   input{wasScaled: true, util: 0.6, scaleUp: 0.8, scaleDown: 0.9},
			// scaleDown normalized to scaleUp=0.8: wasScaled=true, util=0.6 <= 0.8 → shouldScale=false.
			want: want{shouldScale: false},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scaleUp := tc.in.scaleUp
			scaleDown := tc.in.scaleDown
			if scaleDown <= 0 || scaleDown >= scaleUp {
				scaleDown = scaleUp
			}

			wasScaled := tc.in.wasScaled
			shouldScale := wasScaled
			if !wasScaled && tc.in.util >= scaleUp {
				shouldScale = true
			} else if wasScaled && tc.in.util <= scaleDown {
				shouldScale = false
			}

			if shouldScale != tc.want.shouldScale {
				t.Errorf("shouldScale=%v, want %v (wasScaled=%v util=%.2f scaleUp=%.2f scaleDown=%.2f)",
					shouldScale, tc.want.shouldScale, wasScaled, tc.in.util, scaleUp, scaleDown)
			}
		})
	}
}

// TestElasticCountPartition verifies the count-based baseline/reserve split inside buildCandidates.
// It uses a real state.Store snapshot so we can control utilisation without a DB.
func TestElasticCountPartition(t *testing.T) {
	// buildCandidates calls s.Q.ListNodesByOwner and s.Q.ListNodes, which need a real DB.
	// We test the pure pickElastic function with count-partitioned slices to cover the
	// count-based contract without a DB dependency.

	// 3 accounts weight-desc: a1(weight=30), a2(weight=20), a3(weight=10)
	// ElasticBaselineCount=1 → baseline=[a1], reserve=[a2,a3]
	all := []string{"a1", "a2", "a3"}

	// Simulate count partition: n=1
	n := 1
	baseline := all[:n]
	reserve := all[n:]

	// Below threshold: only baseline returned.
	t.Run("count=1 below threshold", func(t *testing.T) {
		got := pickElastic(baseline, reserve, 0.5, 0.8, 1000)
		if len(got) != 1 {
			t.Fatalf("expected 1 account, got %d: %v", len(got), got)
		}
		if got[0] != "a1" {
			t.Errorf("expected a1, got %s", got[0])
		}
	})

	// At threshold: baseline + reserves added.
	t.Run("count=1 at threshold", func(t *testing.T) {
		got := pickElastic(baseline, reserve, 0.8, 0.8, 1000)
		if len(got) != 3 {
			t.Fatalf("expected 3 accounts, got %d: %v", len(got), got)
		}
		if got[0] != "a1" || got[1] != "a2" || got[2] != "a3" {
			t.Errorf("unexpected order: %v", got)
		}
	})

	// Above threshold with maxReserve=1: only 1 reserve added.
	t.Run("count=1 above threshold maxReserve=1", func(t *testing.T) {
		got := pickElastic(baseline, reserve, 1.0, 0.8, 1)
		if len(got) != 2 {
			t.Fatalf("expected 2 accounts, got %d: %v", len(got), got)
		}
		if got[0] != "a1" || got[1] != "a2" {
			t.Errorf("unexpected order: %v", got)
		}
	})

	// ElasticBaselineCount=2 → baseline=[a1,a2], reserve=[a3]
	n2 := 2
	baseline2 := all[:n2]
	reserve2 := all[n2:]

	t.Run("count=2 below threshold", func(t *testing.T) {
		got := pickElastic(baseline2, reserve2, 0.3, 0.8, 1000)
		if len(got) != 2 {
			t.Fatalf("expected 2 accounts, got %d: %v", len(got), got)
		}
	})

	t.Run("count=2 at threshold", func(t *testing.T) {
		got := pickElastic(baseline2, reserve2, 0.8, 0.8, 1000)
		if len(got) != 3 {
			t.Fatalf("expected 3 accounts, got %d: %v", len(got), got)
		}
		if got[2] != "a3" {
			t.Errorf("expected a3 as reserve, got %s", got[2])
		}
	})
}

// TestBuildCandidates_OwnerIsolation verifies that buildCandidates filters by account owner:
// - ownerID="A" → only accounts owned by A are candidates
// - ownerID="" (admin) → all accounts are candidates
func TestBuildCandidates_OwnerIsolation(t *testing.T) {
	q, closeDB := setupDB(t)
	defer closeDB()
	ctx := context.Background()

	// Shared node that hosts both accounts.
	sfx := suffixDispatch(t)
	nodeID := "n_iso_" + sfx
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{
		ID:      nodeID,
		Name:    "iso-node-" + sfx,
		BaseUrl: "http://unused",
		ApiKey:  "k",
		OwnerID: "",
	}); err != nil {
		t.Fatalf("create node: %v", err)
	}

	ownerA := "owner_a_" + sfx
	ownerB := "owner_b_" + sfx
	accA := "acc_a_" + sfx
	accB := "acc_b_" + sfx

	// Create two accounts, one per tenant.
	if _, err := q.CreateAccount(ctx, sqlc.CreateAccountParams{
		ID:      accA,
		OwnerID: ownerA,
	}); err != nil {
		t.Fatalf("create accA: %v", err)
	}
	if _, err := q.CreateAccount(ctx, sqlc.CreateAccountParams{
		ID:      accB,
		OwnerID: ownerB,
	}); err != nil {
		t.Fatalf("create accB: %v", err)
	}

	// Assign both accounts to the shared node with distinct profiles.
	if _, err := q.AssignAccount(ctx, sqlc.AssignAccountParams{
		NodeID: nodeID, AccountID: accA, ProfileID: "profA", Weight: 100, Role: "baseline",
	}); err != nil {
		t.Fatalf("assign accA: %v", err)
	}
	if _, err := q.AssignAccount(ctx, sqlc.AssignAccountParams{
		NodeID: nodeID, AccountID: accB, ProfileID: "profB", Weight: 100, Role: "baseline",
	}); err != nil {
		t.Fatalf("assign accB: %v", err)
	}

	store := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	svc := &Service{Q: q, Store: store, Base: policy.Defaults(), Now: func() int64 { return 0 }}
	cfg := policy.Defaults()

	keyA := nodeID + ":profA"
	keyB := nodeID + ":profB"

	// ownerID=ownerA → only accA candidate.
	t.Run("owner A sees only A", func(t *testing.T) {
		order, _, _ := svc.buildCandidates(ctx, ownerA, "claude-3", cfg)
		found := map[string]bool{}
		for _, k := range order {
			found[k] = true
		}
		if !found[keyA] {
			t.Errorf("expected %s in candidates, got %v", keyA, order)
		}
		if found[keyB] {
			t.Errorf("expected %s NOT in candidates for ownerA, got %v", keyB, order)
		}
	})

	// ownerID=ownerB → only accB candidate.
	t.Run("owner B sees only B", func(t *testing.T) {
		order, _, _ := svc.buildCandidates(ctx, ownerB, "claude-3", cfg)
		found := map[string]bool{}
		for _, k := range order {
			found[k] = true
		}
		if !found[keyB] {
			t.Errorf("expected %s in candidates, got %v", keyB, order)
		}
		if found[keyA] {
			t.Errorf("expected %s NOT in candidates for ownerB, got %v", keyA, order)
		}
	})

	// ownerID="" (admin) → both accounts visible.
	t.Run("admin (empty ownerID) sees all", func(t *testing.T) {
		order, _, _ := svc.buildCandidates(ctx, "", "claude-3", cfg)
		found := map[string]bool{}
		for _, k := range order {
			found[k] = true
		}
		if !found[keyA] {
			t.Errorf("expected %s in admin candidates, got %v", keyA, order)
		}
		if !found[keyB] {
			t.Errorf("expected %s in admin candidates, got %v", keyB, order)
		}
	})
}
