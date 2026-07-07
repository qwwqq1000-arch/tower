package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// sessionCookie issues a token at epoch 0. Callers seed the subject as a real
// tenant row (default session_epoch 0) so requireSession's epoch check accepts it.
func sessionCookie(secret, sub, role string) *http.Cookie {
	tok := auth.IssueSession(secret, sub, role, 0, nowUnix(), 3600)
	return &http.Cookie{Name: "tower_session", Value: tok}
}

// TestMeEndpointsOwnerScoping verifies strict owner isolation across all
// /api/me/* endpoints: tenant A sees only A's data and cannot mutate B's.
func TestMeEndpointsOwnerScoping(t *testing.T) {
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
	// Close the pool via Cleanup (registered first → runs last in LIFO) so the
	// row-cleanup registered later still has an open pool.
	t.Cleanup(pool.Close)
	q := sqlc.New(pool)
	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q, false, nil, "")

	ownerA := randHex("owA_")
	ownerB := randHex("owB_")

	// real tenant rows so fallback_limit enforcement does not block fixtures.
	// ownerA needs a limit >1 because this test creates 1 channel via DB fixture
	// and another via the handler.
	for _, o := range []string{ownerA, ownerB} {
		if _, err := q.CreateTenant(ctx, sqlc.CreateTenantParams{ID: o, Username: o, PwHash: "h", Salt: "s", Role: "tenant", IngestKey: randHex("ik_")}); err != nil {
			t.Fatalf("create tenant %s: %v", o, err)
		}
	}
	if err := q.SetTenantFallbackLimit(ctx, sqlc.SetTenantFallbackLimitParams{ID: ownerA, FallbackLimit: 10}); err != nil {
		t.Fatalf("set fallback limit: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_ = q.DeleteTenant(cctx, ownerA)
		_ = q.DeleteTenant(cctx, ownerB)
	})

	// nodes (each owned by respective tenant)
	nodeA := randHex("nA_")
	nodeB := randHex("nB_")
	for _, nc := range []sqlc.CreateNodeParams{
		{ID: nodeA, Name: "nodeA", BaseUrl: "http://a", ApiKey: "k", OwnerID: ownerA},
		{ID: nodeB, Name: "nodeB", BaseUrl: "http://b", ApiKey: "k", OwnerID: ownerB},
	} {
		if _, err := q.CreateNode(ctx, nc); err != nil {
			t.Fatalf("create node: %v", err)
		}
	}

	// accounts
	now := time.Now()
	accA := randHex("acA_")
	accB := randHex("acB_")
	for _, ac := range []sqlc.CreateAccountParams{
		{ID: accA, OwnerID: ownerA, Email: "a@x.com", ExpiresAt: now.Add(time.Hour).UnixMilli()},
		{ID: accB, OwnerID: ownerB, Email: "b@x.com", ExpiresAt: now.Add(time.Hour).UnixMilli()},
	} {
		if _, err := q.CreateAccount(ctx, ac); err != nil {
			t.Fatalf("create account: %v", err)
		}
	}
	// node_accounts
	if _, err := q.AssignAccount(ctx, sqlc.AssignAccountParams{NodeID: nodeA, AccountID: accA, ProfileID: "pA", Weight: 100, Role: "baseline"}); err != nil {
		t.Fatalf("assign A: %v", err)
	}
	if _, err := q.AssignAccount(ctx, sqlc.AssignAccountParams{NodeID: nodeB, AccountID: accB, ProfileID: "pB", Weight: 100, Role: "baseline"}); err != nil {
		t.Fatalf("assign B: %v", err)
	}

	// dispatch logs + events
	ts := now.UnixMilli()
	for _, lp := range []sqlc.InsertDispatchLogParams{
		{Ts: ts, OwnerID: ownerA, Model: "m", Target: nodeA + ":pA", ProfileID: "pA", Status: "ok", CostUsd: 1.0},
		{Ts: ts, OwnerID: ownerB, Model: "m", Target: nodeB + ":pB", ProfileID: "pB", Status: "ok", CostUsd: 2.0},
	} {
		if err := q.InsertDispatchLog(ctx, lp); err != nil {
			t.Fatalf("log: %v", err)
		}
	}
	for _, ep := range []sqlc.InsertDispatchEventParams{
		{Ts: ts, Type: "evtA", Target: nodeA, OwnerID: ownerA, Detail: []byte(`{}`)},
		{Ts: ts, Type: "evtB", Target: nodeB, OwnerID: ownerB, Detail: []byte(`{}`)},
	} {
		if err := q.InsertDispatchEvent(ctx, ep); err != nil {
			t.Fatalf("event: %v", err)
		}
	}

	// fallback channels
	chA, err := q.CreateFallbackChannel(ctx, sqlc.CreateFallbackChannelParams{ID: randHex("fc_"), OwnerID: ownerA, Name: "chA", BaseUrl: "http://a"})
	if err != nil {
		t.Fatalf("chA: %v", err)
	}
	chB, err := q.CreateFallbackChannel(ctx, sqlc.CreateFallbackChannelParams{ID: randHex("fc_"), OwnerID: ownerB, Name: "chB", BaseUrl: "http://b"})
	if err != nil {
		t.Fatalf("chB: %v", err)
	}
	// Clean up this test's fixtures so sibling tests sharing the database (which
	// assume empty/recent-row state) are not polluted by these rows.
	t.Cleanup(func() {
		cctx := context.Background()
		for _, o := range []string{ownerA, ownerB} {
			if rows, err := q.ListFallbackChannelsByOwner(cctx, o); err == nil {
				for _, c := range rows {
					_ = q.DeleteFallbackChannel(cctx, c.ID)
				}
			}
			_, _ = pool.Exec(cctx, "DELETE FROM dispatch_events WHERE owner_id=$1", o)
			_, _ = pool.Exec(cctx, "DELETE FROM dispatch_logs WHERE owner_id=$1", o)
			_, _ = pool.Exec(cctx, "DELETE FROM billing_ledger WHERE tenant_id=$1", o)
		}
	})

	ckA := sessionCookie(secret, ownerA, "tenant")
	do := func(ck *http.Cookie, m, p, b string) *httptest.ResponseRecorder {
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

	// 1. /api/me/accounts → only A's account; never B's.
	rec := do(ckA, "GET", "/api/me/accounts", "")
	if rec.Code != 200 {
		t.Fatalf("me/accounts=%d %s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	if !strings.Contains(body, accA) || strings.Contains(body, accB) {
		t.Fatalf("accounts leak: %s", body)
	}
	if strings.Contains(body, "b@x.com") {
		t.Fatalf("accounts leaked B email: %s", body)
	}

	// 2. pause: A cannot pause B's account (403); can pause own.
	if rec := do(ckA, "POST", "/api/me/accounts/"+accB+"/pause", `{"enabled":false}`); rec.Code != 403 {
		t.Fatalf("pause B by A = %d want 403; body=%s", rec.Code, rec.Body)
	}
	if rec := do(ckA, "POST", "/api/me/accounts/"+accA+"/pause", `{"enabled":false}`); rec.Code != 200 {
		t.Fatalf("pause own = %d want 200; body=%s", rec.Code, rec.Body)
	}
	// confirm B's node_account still enabled (untouched)
	if nas, _ := q.ListNodeAccountsByAccount(ctx, accB); len(nas) != 1 || !nas[0].Enabled {
		t.Fatalf("B node_account altered: %+v", nas)
	}
	if nas, _ := q.ListNodeAccountsByAccount(ctx, accA); len(nas) != 1 || nas[0].Enabled {
		t.Fatalf("A node_account not paused: %+v", nas)
	}

	// 3. dashboard scoped to A: today.costUsd == 1.0 (A only), accounts.total==1
	rec = do(ckA, "GET", "/api/me/dashboard", "")
	if rec.Code != 200 {
		t.Fatalf("dashboard=%d %s", rec.Code, rec.Body)
	}
	var dash struct {
		Accounts struct{ Total, Active int }
		Today    struct {
			Requests int64
			CostUsd  float64 `json:"costUsd"`
		}
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &dash); err != nil {
		t.Fatalf("dashboard json: %v %s", err, rec.Body)
	}
	if dash.Accounts.Total != 1 {
		t.Fatalf("dashboard accounts.total=%d want 1", dash.Accounts.Total)
	}
	if dash.Today.CostUsd != 1.0 || dash.Today.Requests != 1 {
		t.Fatalf("dashboard today cost=%v req=%v want 1.0/1 (B leaked?)", dash.Today.CostUsd, dash.Today.Requests)
	}

	// 4. logs scoped to A
	rec = do(ckA, "GET", "/api/me/logs", "")
	if b := rec.Body.String(); strings.Contains(b, nodeB+":pB") || !strings.Contains(b, nodeA+":pA") {
		t.Fatalf("logs leak: %s", b)
	}

	// 5. events scoped to A
	rec = do(ckA, "GET", "/api/me/events", "")
	if b := rec.Body.String(); strings.Contains(b, "evtB") || !strings.Contains(b, "evtA") {
		t.Fatalf("events leak: %s", b)
	}

	// 6. fallback channels scoped to A
	rec = do(ckA, "GET", "/api/me/fallback-channels", "")
	if b := rec.Body.String(); strings.Contains(b, "chB") || !strings.Contains(b, "chA") {
		t.Fatalf("channels leak: %s", b)
	}
	// A cannot edit / enable / delete B's channel (403)
	if rec := do(ckA, "PATCH", "/api/me/fallback-channels/"+chB.ID, `{"name":"hax"}`); rec.Code != 403 {
		t.Fatalf("edit B chan by A=%d want 403", rec.Code)
	}
	if rec := do(ckA, "PATCH", "/api/me/fallback-channels/"+chB.ID+"/enabled", `{"enabled":false}`); rec.Code != 403 {
		t.Fatalf("enable B chan by A=%d want 403", rec.Code)
	}
	if rec := do(ckA, "DELETE", "/api/me/fallback-channels/"+chB.ID, ""); rec.Code != 403 {
		t.Fatalf("delete B chan by A=%d want 403", rec.Code)
	}
	// B's channel must still exist with its name intact
	if got, err := q.GetFallbackChannel(ctx, chB.ID); err != nil || got.Name != "chB" {
		t.Fatalf("B channel tampered: %+v err=%v", got, err)
	}
	// A can create + edit own channel; created channel is owned by A (body ownerId ignored)
	rec = do(ckA, "POST", "/api/me/fallback-channels", `{"name":"newA","baseUrl":"http://n","ownerId":"`+ownerB+`"}`)
	if rec.Code != 200 {
		t.Fatalf("create own chan=%d %s", rec.Code, rec.Body)
	}
	var created struct{ ID string }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if got, _ := q.GetFallbackChannel(ctx, created.ID); got.OwnerID != ownerA {
		t.Fatalf("created channel owner=%s want %s (body override leaked)", got.OwnerID, ownerA)
	}
	if rec := do(ckA, "PATCH", "/api/me/fallback-channels/"+chA.ID, `{"name":"chA2","baseUrl":"http://a"}`); rec.Code != 200 {
		t.Fatalf("edit own chan=%d %s", rec.Code, rec.Body)
	}

	// 7. ledger scoped to A — entry for B must not appear.
	if _, err := q.AppendLedger(ctx, sqlc.AppendLedgerParams{TenantID: ownerB, Ts: ts, Type: "charge", AmountUsd: 9.99, Ref: "refB"}); err != nil {
		t.Fatalf("append ledger B: %v", err)
	}
	if _, err := q.AppendLedger(ctx, sqlc.AppendLedgerParams{TenantID: ownerA, Ts: ts, Type: "charge", AmountUsd: 1.11, Ref: "refA"}); err != nil {
		t.Fatalf("append ledger A: %v", err)
	}
	rec = do(ckA, "GET", "/api/me/ledger", "")
	if b := rec.Body.String(); strings.Contains(b, "refB") || !strings.Contains(b, "refA") {
		t.Fatalf("ledger leak: %s", b)
	}
}

// TestMeSettingsOwnerScoping verifies strict owner isolation on tenant settings
// endpoints: slots + dispatch-keys. Owner A creates resources; A lists only A's;
// A cannot delete B's (403). Reads/writes never cross tenants.
func TestMeSettingsOwnerScoping(t *testing.T) {
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
	t.Cleanup(pool.Close)
	q := sqlc.New(pool)
	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q, false, nil, "")

	ownerA := randHex("owA_")
	ownerB := randHex("owB_")
	// Seed real tenant rows so requireSession's epoch check (auth-session-1)
	// accepts the sessions; tokens are issued at the default epoch 0.
	for _, o := range []string{ownerA, ownerB} {
		if _, err := q.CreateTenant(ctx, sqlc.CreateTenantParams{ID: o, Username: o, PwHash: "h", Salt: "s", Role: "tenant", IngestKey: randHex("ik_")}); err != nil {
			t.Fatalf("create tenant %s: %v", o, err)
		}
	}
	t.Cleanup(func() {
		cctx := context.Background()
		_ = q.DeleteTenant(cctx, ownerA)
		_ = q.DeleteTenant(cctx, ownerB)
	})
	ckA := sessionCookie(secret, ownerA, "tenant")
	ckB := sessionCookie(secret, ownerB, "tenant")

	do := func(ck *http.Cookie, m, p, b string) *httptest.ResponseRecorder {
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

	// Track created ids for cleanup.
	var slotIDs, keyIDs []string
	t.Cleanup(func() {
		cctx := context.Background()
		for _, id := range slotIDs {
			_ = q.DeleteSlot(cctx, id)
		}
		for _, id := range keyIDs {
			_ = q.DisableDispatchKey(cctx, id)
		}
	})

	// ---- slots ----
	// A creates a slot; B creates a slot.
	recA := do(ckA, "POST", "/api/me/slots", `{"name":"slotA","startMin":0,"endMin":600}`)
	if recA.Code != 200 {
		t.Fatalf("A create slot=%d %s", recA.Code, recA.Body)
	}
	var sa struct{ ID string }
	_ = json.Unmarshal(recA.Body.Bytes(), &sa)
	slotIDs = append(slotIDs, sa.ID)

	recB := do(ckB, "POST", "/api/me/slots", `{"name":"slotB","startMin":0,"endMin":600}`)
	if recB.Code != 200 {
		t.Fatalf("B create slot=%d %s", recB.Code, recB.Body)
	}
	var sb struct{ ID string }
	_ = json.Unmarshal(recB.Body.Bytes(), &sb)
	slotIDs = append(slotIDs, sb.ID)

	// created slot is owned by the creator.
	if got, _ := q.GetSlot(ctx, sa.ID); got.OwnerID != ownerA {
		t.Fatalf("slot A owner=%s want %s", got.OwnerID, ownerA)
	}

	// A lists only A's slot.
	rec := do(ckA, "GET", "/api/me/slots", "")
	if b := rec.Body.String(); !strings.Contains(b, sa.ID) || strings.Contains(b, sb.ID) {
		t.Fatalf("slots leak: %s", b)
	}

	// A cannot delete B's slot (403); B's slot survives.
	if rec := do(ckA, "DELETE", "/api/me/slots/"+sb.ID, ""); rec.Code != 403 {
		t.Fatalf("A delete B slot=%d want 403; body=%s", rec.Code, rec.Body)
	}
	if _, err := q.GetSlot(ctx, sb.ID); err != nil {
		t.Fatalf("B slot deleted by A: %v", err)
	}
	// A cannot toggle B's slot (403).
	if rec := do(ckA, "PATCH", "/api/me/slots/"+sb.ID+"/enabled", `{"enabled":false}`); rec.Code != 403 {
		t.Fatalf("A toggle B slot=%d want 403", rec.Code)
	}
	// A can delete own slot.
	if rec := do(ckA, "DELETE", "/api/me/slots/"+sa.ID, ""); rec.Code != 200 {
		t.Fatalf("A delete own slot=%d %s", rec.Code, rec.Body)
	}

	// ---- dispatch keys ----
	recA = do(ckA, "POST", "/api/me/dispatch-keys", `{"label":"keyA"}`)
	if recA.Code != 200 {
		t.Fatalf("A create key=%d %s", recA.Code, recA.Body)
	}
	var ka struct{ ID, Key string }
	_ = json.Unmarshal(recA.Body.Bytes(), &ka)
	keyIDs = append(keyIDs, ka.ID)
	if ka.Key == "" {
		t.Fatalf("create key returned empty plaintext")
	}

	recB = do(ckB, "POST", "/api/me/dispatch-keys", `{"label":"keyB"}`)
	if recB.Code != 200 {
		t.Fatalf("B create key=%d %s", recB.Code, recB.Body)
	}
	var kb struct{ ID string }
	_ = json.Unmarshal(recB.Body.Bytes(), &kb)
	keyIDs = append(keyIDs, kb.ID)

	// created key carries the creator's owner_id (dispatch isolation).
	if rows, _ := q.ListDispatchKeysByOwner(ctx, ownerA); func() bool {
		for _, k := range rows {
			if k.ID == ka.ID {
				return false
			}
		}
		return true
	}() {
		t.Fatalf("A's key not owned by A")
	}

	// A lists only A's key.
	rec = do(ckA, "GET", "/api/me/dispatch-keys", "")
	if b := rec.Body.String(); !strings.Contains(b, ka.ID) || strings.Contains(b, kb.ID) {
		t.Fatalf("keys leak: %s", b)
	}

	// A cannot delete B's key (403); B's key stays enabled.
	if rec := do(ckA, "DELETE", "/api/me/dispatch-keys/"+kb.ID, ""); rec.Code != 403 {
		t.Fatalf("A delete B key=%d want 403; body=%s", rec.Code, rec.Body)
	}
	if rows, _ := q.ListDispatchKeysByOwner(ctx, ownerB); len(rows) == 0 || !rows[0].Enabled {
		t.Fatalf("B key disabled by A: %+v", rows)
	}
	// A can delete own key.
	if rec := do(ckA, "DELETE", "/api/me/dispatch-keys/"+ka.ID, ""); rec.Code != 200 {
		t.Fatalf("A delete own key=%d %s", rec.Code, rec.Body)
	}
}

// TestFallbackChannelLimit verifies a tenant with fallback_limit=1 can create one
// channel but is rejected (403) when creating a second.
func TestFallbackChannelLimit(t *testing.T) {
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
	t.Cleanup(pool.Close)
	q := sqlc.New(pool)
	const secret = "test-secret-padding-to-32-chars!"
	router := NewRouter(pool, secret, nil, q, false, nil, "")

	owner := randHex("owL_")
	// tenant created with default fallback_limit=1.
	if _, err := q.CreateTenant(ctx, sqlc.CreateTenantParams{ID: owner, Username: owner, PwHash: "h", Salt: "s", Role: "tenant", IngestKey: randHex("ik_")}); err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	t.Cleanup(func() {
		cctx := context.Background()
		if rows, err := q.ListFallbackChannelsByOwner(cctx, owner); err == nil {
			for _, c := range rows {
				_ = q.DeleteFallbackChannel(cctx, c.ID)
			}
		}
		_ = q.DeleteTenant(cctx, owner)
	})

	ck := sessionCookie(secret, owner, "tenant")
	do := func(m, p, b string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(m, p, strings.NewReader(b))
		r.AddCookie(ck)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, r)
		return rec
	}

	// first channel succeeds.
	if rec := do("POST", "/api/me/fallback-channels", `{"name":"c1","baseUrl":"http://a"}`); rec.Code != 200 {
		t.Fatalf("create #1 = %d want 200; body=%s", rec.Code, rec.Body)
	}
	// second channel hits the limit → 403.
	rec := do("POST", "/api/me/fallback-channels", `{"name":"c2","baseUrl":"http://b"}`)
	if rec.Code != 403 {
		t.Fatalf("create #2 = %d want 403; body=%s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "fallback channel limit reached") {
		t.Fatalf("create #2 body=%s want limit error", rec.Body)
	}
}
