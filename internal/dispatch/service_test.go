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
