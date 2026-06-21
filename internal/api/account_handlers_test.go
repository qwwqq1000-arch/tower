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

// suffixDispatch returns a short random hex suffix for test isolation.
func suffixDispatch(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return hex.EncodeToString(b)
}

func TestAccountOAuthFlow(t *testing.T) {
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
		case "/auth/login-url":
			_, _ = w.Write([]byte(`{"authorizeUrl":"https://claude.ai/x","codeVerifier":"v1","state":"s1"}`))
		case "/auth/exchange":
			_, _ = w.Write([]byte(`{"success":true}`))
		case "/profiles/list":
			_, _ = w.Write([]byte(`{"activeProfile":"default","profiles":[{"id":"default","email":"acc@x.com","loggedIn":true}]}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer node.Close()

	sfx := suffixDispatch(t)
	nodeID := "n_" + sfx
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{
		ID:      nodeID,
		Name:    "n",
		BaseUrl: node.URL,
		ApiKey:  "k",
		OwnerID: "o_" + sfx,
	}); err != nil {
		t.Fatalf("node: %v", err)
	}

	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q)
	ck := adminCookie(t, secret)
	do := func(m, p, b string) *httptest.ResponseRecorder {
		var r *http.Request
		if b != "" {
			r = httptest.NewRequest(m, p, strings.NewReader(b))
		} else {
			r = httptest.NewRequest(m, p, nil)
		}
		r.AddCookie(ck)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		return rec
	}

	if rec := do("POST", "/api/admin/nodes/"+nodeID+"/oauth/start", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "authorizeUrl") {
		t.Fatalf("start=%d %s", rec.Code, rec.Body)
	}
	if rec := do("POST", "/api/admin/nodes/"+nodeID+"/oauth/exchange", `{"codeVerifier":"v1","state":"s1","code":"c"}`); rec.Code != 200 {
		t.Fatalf("exchange=%d %s", rec.Code, rec.Body)
	}
	// account registered + assigned
	accs, _ := q.ListNodeAccountsByNode(ctx, nodeID)
	if len(accs) != 1 || accs[0].ProfileID != "default" {
		t.Fatalf("assigned=%+v", accs)
	}
	if rec := do("GET", "/api/admin/accounts", ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), "default") {
		t.Fatalf("list accounts=%d %s", rec.Code, rec.Body)
	}
}
