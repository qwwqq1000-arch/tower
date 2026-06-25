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

// TestReserveKeys_MatchesBuildCandidatesPartition asserts that ReserveKeys and
// buildCandidates produce the SAME baseline / reserve partition for the same owner
// and time. This guards against partition divergence (e.g. one function applying
// slot-window or other filters that the other misses), which is the confirmed root
// cause of non-affinity requests reaching reserve accounts without scale-up.
//
// Scenario: 3 accounts assigned to a shared node. One account has an INACTIVE
// slot window. ElasticBaselineCount=1. buildCandidates must see 2 eligible accounts
// (the slot-inactive one is excluded) and put the second one as reserve.
// ReserveKeys must identify exactly that same second account as reserve.
func TestReserveKeys_MatchesBuildCandidatesPartition(t *testing.T) {
	q, closeDB := setupDB(t)
	defer closeDB()
	ctx := context.Background()

	sfx := suffixDispatch(t)
	nodeID := "n_rsv_" + sfx
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{
		ID: nodeID, Name: "rsv-node-" + sfx, BaseUrl: "http://unused", ApiKey: "k", OwnerID: "",
	}); err != nil {
		t.Fatalf("create node: %v", err)
	}

	owner := "owner_rsv_" + sfx
	// Three accounts: A (weight 300), B (weight 200), C (weight 100).
	// C has an inactive slot window → excluded by buildCandidates.
	// With ElasticBaselineCount=1: baseline=[A], reserve=[B], C is excluded entirely.
	for _, acc := range []struct {
		id     string
		weight int32
	}{
		{"acc_a_rsv_" + sfx, 300},
		{"acc_b_rsv_" + sfx, 200},
		{"acc_c_rsv_" + sfx, 100},
	} {
		if _, err := q.CreateAccount(ctx, sqlc.CreateAccountParams{ID: acc.id, OwnerID: owner}); err != nil {
			t.Fatalf("create account %s: %v", acc.id, err)
		}
	}

	// Create a slot with a window that is currently INACTIVE: startMin=0, endMin=1
	// (only active 00:00–00:01 UTC). nowMs will be well after midnight.
	slotID := "slot_rsv_" + sfx
	if _, err := q.CreateSlot(ctx, sqlc.CreateSlotParams{
		ID: slotID, Name: "inactive-window-" + sfx, StartMin: 0, EndMin: 1,
	}); err != nil {
		t.Fatalf("create slot: %v", err)
	}
	if err := q.SetSlotEnabled(ctx, sqlc.SetSlotEnabledParams{ID: slotID, Enabled: true}); err != nil {
		t.Fatalf("enable slot: %v", err)
	}

	// Assign accounts to node: A and B without slot (always active), C with inactive slot.
	if _, err := q.AssignAccount(ctx, sqlc.AssignAccountParams{
		NodeID: nodeID, AccountID: "acc_a_rsv_" + sfx, ProfileID: "profA", Weight: 300, Role: "baseline",
	}); err != nil {
		t.Fatalf("assign A: %v", err)
	}
	if _, err := q.AssignAccount(ctx, sqlc.AssignAccountParams{
		NodeID: nodeID, AccountID: "acc_b_rsv_" + sfx, ProfileID: "profB", Weight: 200, Role: "baseline",
	}); err != nil {
		t.Fatalf("assign B: %v", err)
	}
	if _, err := q.AssignAccount(ctx, sqlc.AssignAccountParams{
		NodeID: nodeID, AccountID: "acc_c_rsv_" + sfx, ProfileID: "profC", Weight: 100, Role: "baseline",
		SlotID: slotID, // inactive → excluded from dispatch candidates
	}); err != nil {
		t.Fatalf("assign C: %v", err)
	}

	keyA := nodeID + ":profA"
	keyB := nodeID + ":profB"
	keyC := nodeID + ":profC"

	// nowMs is 12:00 UTC — well outside the slot window [00:00, 00:01) → C is inactive.
	noonUTC := int64(12 * 3600 * 1000) // 1970-01-01 12:00 UTC (ms)
	store := state.NewStore(func() int64 { return noonUTC }, func(min, max int64) int64 { return min })
	svc := &Service{Q: q, Store: store, Base: policy.Defaults(), Now: func() int64 { return noonUTC }, scaledUp: make(map[string]bool)}

	cfg := policy.Defaults()
	cfg.ElasticEnabled = true
	cfg.ElasticBaselineCount = 1
	cfg.ElasticScaleUpUtil = 0.8
	cfg.ElasticScaleDownUtil = 0.3

	// buildCandidates should see A and B (C filtered by slot), baseline=[A], reserve=[B].
	order, _, _, _, _ := svc.buildCandidates(ctx, owner, "claude-3", cfg)

	dispatchBaseline := make(map[string]bool)
	for _, k := range order {
		dispatchBaseline[k] = true
	}

	// Verify dispatch: A in baseline, B in order (either baseline or reserve), C excluded.
	if !dispatchBaseline[keyA] {
		t.Errorf("buildCandidates: expected keyA=%s in baseline order, got %v", keyA, order)
	}
	if dispatchBaseline[keyC] {
		t.Errorf("buildCandidates: keyC=%s (inactive slot) must NOT appear in dispatch order, got %v", keyC, order)
	}

	// ReserveKeys must produce a reserve set that matches: keyB=reserve, keyA=not-reserve, keyC=not-reserve.
	reserveKeys := svc.ReserveKeys(ctx, owner, cfg)

	if reserveKeys[keyC] {
		t.Errorf("ReserveKeys: keyC=%s (inactive slot) must NOT appear in reserve set — partition divergence from buildCandidates", keyC)
	}
	if !reserveKeys[keyB] {
		t.Errorf("ReserveKeys: keyB=%s must be reserve (beyond baseline after slot filter), got reserve=%v", keyB, reserveKeys)
	}
	if reserveKeys[keyA] {
		t.Errorf("ReserveKeys: keyA=%s must be in baseline (not reserve), got reserve=%v", keyA, reserveKeys)
	}

	// Cross-check: the union of (dispatch order) and (reserve keys) must equal A+B;
	// C must appear in neither (slot-inactive, excluded from both).
	allInDispatch := make(map[string]bool)
	for _, k := range order {
		allInDispatch[k] = true
	}
	for k := range reserveKeys {
		allInDispatch[k] = true
	}
	if allInDispatch[keyC] {
		t.Errorf("keyC=%s appears in dispatch+reserve union but has inactive slot — partition mismatch", keyC)
	}
	if !allInDispatch[keyA] || !allInDispatch[keyB] {
		t.Errorf("expected A+B in combined dispatch+reserve set, got %v", allInDispatch)
	}
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
		order, _, _, _, _ := svc.buildCandidates(ctx, ownerA, "claude-3", cfg)
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
		order, _, _, _, _ := svc.buildCandidates(ctx, ownerB, "claude-3", cfg)
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
		order, _, _, _, _ := svc.buildCandidates(ctx, "", "claude-3", cfg)
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
