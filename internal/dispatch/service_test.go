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
	in, out := parseUsageSSE(body)
	if in != 150 {
		t.Errorf("input_tokens: got %d, want 150", in)
	}
	if out != 42 {
		t.Errorf("output_tokens: got %d, want 42", out)
	}
}
