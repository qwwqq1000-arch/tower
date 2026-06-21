package dispatch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

func sfxJ(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func TestDispatchStream_ToNode(t *testing.T) {
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

	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		fl, _ := w.(http.Flusher)
		_, _ = io.WriteString(w, "data: hello\n\n")
		if fl != nil {
			fl.Flush()
		}
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer node.Close()

	s := sfxJ(t)
	nodeID := "n_" + s
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{ID: nodeID, Name: "n", BaseUrl: node.URL, ApiKey: "k", OwnerID: "o_" + s}); err != nil {
		t.Fatalf("node: %v", err)
	}
	if _, err := q.AssignAccount(ctx, sqlc.AssignAccountParams{NodeID: nodeID, AccountID: "a_" + s, ProfileID: "default", Weight: 100, Role: "baseline"}); err != nil {
		t.Fatalf("assign: %v", err)
	}

	store := state.NewStore(func() int64 { return 0 }, func(a, b int64) int64 { return a })
	svc := &Service{Q: q, Store: store, Base: policy.Defaults(), Now: func() int64 { return 0 }}

	rec := httptest.NewRecorder()
	out := svc.DispatchStream(ctx, rec, "o_"+s, "opus", []byte(`{"model":"opus","stream":true}`))
	if out.Status != 200 {
		t.Fatalf("status=%d target=%s", out.Status, out.Target)
	}
	if body := rec.Body.String(); !strings.Contains(body, "data: hello") || !strings.Contains(body, "[DONE]") {
		t.Fatalf("streamed body=%q", body)
	}
	// slot released after stream completes
	cfg := state.BreakerCfg{PersistStreak: 3, BaseMs: 1, MaxMs: 1, Mult: 2}
	if !store.TryDispatch(nodeID+":default", "opus", cfg) {
		t.Fatal("slot should be released after stream completion")
	}
}
