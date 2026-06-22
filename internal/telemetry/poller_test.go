package telemetry

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

func sfx(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func TestPoller_ThresholdFromPolicy(t *testing.T) {
	const def = 0.95
	cases := []struct {
		name  string
		json  []byte
		want  float64
	}{
		{
			name: "valid 0.8",
			json: []byte(`{"QuotaRotateThreshold":0.8}`),
			want: 0.8,
		},
		{
			name: "nil pointer in patch (field absent)",
			json: []byte(`{}`),
			want: def,
		},
		{
			name: "value 1.5 out of range",
			json: []byte(`{"QuotaRotateThreshold":1.5}`),
			want: def,
		},
		{
			name: "value 0 out of range",
			json: []byte(`{"QuotaRotateThreshold":0}`),
			want: def,
		},
		{
			name: "empty JSON",
			json: []byte(``),
			want: def,
		},
		{
			name: "invalid JSON",
			json: []byte(`not-json`),
			want: def,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := policy.PickThreshold(tc.json, def)
			if got != tc.want {
				t.Fatalf("policy.PickThreshold(%q, %v) = %v, want %v", tc.json, def, got, tc.want)
			}
		})
	}
}

func TestPollOnce_SetsLimitedFromQuota(t *testing.T) {
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
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"healthy","version":"1.45.0","auth":{"loggedIn":true,"email":"a@b.com"}}`))
		case "/v1/usage/quota/all":
			_, _ = w.Write([]byte(`{"activeProfile":"default","profiles":[{"id":"default","isActive":true,"windows":[{"type":"five_hour","status":"rejected","utilization":1.0,"resetsAt":9999999999999}]}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer node.Close()

	s := sfx(t)
	nodeID := "n_" + s
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{ID: nodeID, Name: "n", BaseUrl: node.URL, ApiKey: "k", OwnerID: "o_" + s}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := q.AssignAccount(ctx, sqlc.AssignAccountParams{NodeID: nodeID, AccountID: "a_" + s, ProfileID: "default", Weight: 100, Role: "baseline"}); err != nil {
		t.Fatalf("assign: %v", err)
	}

	store := state.NewStore(func() int64 { return 1000 }, func(a, b int64) int64 { return a })
	p := &Poller{Q: q, Store: store, Threshold: 0.95, DefaultTTLMs: 3600000, Capacity: 2, Now: func() int64 { return 1000 }}
	if err := p.PollOnce(ctx); err != nil {
		t.Fatalf("PollOnce: %v", err)
	}

	cfg := state.BreakerCfg{PersistStreak: 3, BaseMs: 1, MaxMs: 1, Mult: 2}
	key := nodeID + ":default"
	if store.TryDispatch(key, "opus", cfg) {
		t.Fatal("five_hour rejected → all models limited → no dispatch")
	}
	// node version updated
	n, _ := q.GetNode(ctx, nodeID)
	if n.Version != "1.45.0" {
		t.Fatalf("version=%q, want 1.45.0", n.Version)
	}
}

func TestPollOnce_OfflineWhenNodeDown(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	_ = db.Migrate(ctx, url)
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := sqlc.New(pool)

	s := sfx(t)
	nodeID := "n_" + s
	// unreachable base URL
	_, _ = q.CreateNode(ctx, sqlc.CreateNodeParams{ID: nodeID, Name: "n", BaseUrl: "http://127.0.0.1:1", ApiKey: "k", OwnerID: "o_" + s})
	_, _ = q.AssignAccount(ctx, sqlc.AssignAccountParams{NodeID: nodeID, AccountID: "a_" + s, ProfileID: "default", Weight: 100, Role: "baseline"})

	store := state.NewStore(func() int64 { return 1000 }, func(a, b int64) int64 { return a })
	p := &Poller{Q: q, Store: store, Threshold: 0.95, DefaultTTLMs: 3600000, Capacity: 2, Now: func() int64 { return 1000 }}
	_ = p.PollOnce(ctx)

	cfg := state.BreakerCfg{PersistStreak: 3, BaseMs: 1, MaxMs: 1, Mult: 2}
	if store.TryDispatch(nodeID+":default", "opus", cfg) {
		t.Fatal("unreachable node → offline → no dispatch")
	}
}
