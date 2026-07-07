# Tower 审计修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the 53 confirmed-actionable findings (7 HIGH / 14 MEDIUM / 32 LOW) from the 2026-06-23 full audit of Tower, without changing observable behavior except where the audit says current behavior is wrong.

**Architecture:** Tower is a Go single-binary control tower (`cmd/tower`) + Postgres, with a React SPA (`web/spa`). Backend is layered: `api` (HTTP) → `dispatch`/`billing`/`policy`/`state`/`telemetry`/`fallback`/`auth` → `db/sqlc` (generated). Fixes are localized per subsystem; each phase is independently testable and committable.

**Tech Stack:** Go 1.26.4, pgx v5, sqlc v1.31.1 (generated queries), goose migrations (embedded, applied at startup), React + Vite + TypeScript SPA.

**Spec:** `docs/audit-2026-06-23-full.md` — every task below references finding IDs whose full detail/evidence/fix live there.

## Global Constraints

- **Branch:** `fix/audit-2026-06-23` (already created). Commit per task. **Do NOT push or deploy** — deployment is the user's decision after review.
- **Module path:** `github.com/qwwqq1000-arch/tower`.
- **sqlc:** after editing `queries/*.sql` or adding a query, run `sqlc generate` (config `sqlc.yaml`) to regenerate `internal/db/sqlc/*`. **Never hand-edit generated `*.sql.go`.** Verify `sqlc version` == v1.31.1.
- **Migrations:** goose, embedded, applied at startup. New schema → new file `migrations/202606230000NN_<name>.sql` with `-- +goose Up` / `-- +goose Down`, continuing the existing `20260622000NN` numbering. Add the new file to `migrations/migrations.go` embed only if files aren't globbed (check `migrations.go`).
- **Build/test gate per task:** `go build ./...` then `go test ./...`. DB-backed tests skip unless `TEST_DATABASE_URL` is set — prefer pure unit tests for new logic so they always run.
- **Live verification (read-only):** `http://23.237.28.170:8088`, superadmin cookie at `/tmp/tower_cookies.txt`. GET only — **never** POST/PATCH/DELETE/`/v1/messages` against production.
- **Do not break plain-HTTP access:** the live box is plain HTTP. Any cookie `Secure` flag must be env-gated (default off), never hardcoded true.
- **Decision policy for dead features (audit "wire-or-delete"):** user-facing/advertised features are **wired to take effect**; purely internal vestigial code (`SelectWLR`, `pickElastic`, unused `rbac.Can`) is **deleted**.

---

## Phase 1 — Billing correctness & settle authz (HIGH)

Covers: billing-1, billing-2, billing-3 (= rbac-scoping-1), billing-4, billing-5.
**Status:** edits to `settle.sql`, `fee.go`, `settle.go`, `service.go` (Day), `router.go` (settle route) are already staged in the working tree; tasks below finish and verify them.

### Task 1.1: Settle the outstanding delta + record day (billing-1, billing-2)

**Files:**
- Modify: `queries/settle.sql` (added `SumSettledForOwner` — done)
- Modify: `internal/billing/fee.go` (added `OutstandingToSettle`, `RoundUSD` — done)
- Modify: `internal/billing/settle.go` (settle outstanding — done)
- Modify: `internal/dispatch/service.go:524,639` (`Day: ""` → `Day: todayDayStr()` — done)
- Regenerate: `internal/db/sqlc/*` via `sqlc generate`
- Test: `internal/billing/settle_logic_test.go` (new, pure)

**Interfaces produced:** `billing.OutstandingToSettle(gross, alreadySettled float64) float64`, `billing.RoundUSD(v float64) float64`, `q.SumSettledForOwner(ctx, tenantID) (float64, error)`.

- [ ] **Step 1: Run `sqlc generate`** so `q.SumSettledForOwner` exists.
  Run: `cd /Users/leo/总控台/tower && sqlc generate`
  Expected: no error; `git status` shows `internal/db/sqlc/*` changed.
- [ ] **Step 2: Write the failing pure test** `internal/billing/settle_logic_test.go`:
```go
package billing

import "testing"

func TestOutstandingToSettle(t *testing.T) {
	if got := OutstandingToSettle(10, 4); got != 6 {
		t.Fatalf("first settle: got %v want 6", got)
	}
	// re-settle after fully settled → 0 (no double charge)
	if got := OutstandingToSettle(10, 10); got != 0 {
		t.Fatalf("re-settle: got %v want 0", got)
	}
	// over-settled clamps to 0
	if got := OutstandingToSettle(10, 12); got != 0 {
		t.Fatalf("clamp: got %v want 0", got)
	}
}

func TestRoundUSD(t *testing.T) {
	if got := RoundUSD(63.34780276499996); got != 63.35 {
		t.Fatalf("got %v want 63.35", got)
	}
}
```
- [ ] **Step 3: Run** `go test ./internal/billing/ -run 'TestOutstandingToSettle|TestRoundUSD' -v` → PASS (impl already added).
- [ ] **Step 4: Build** `go build ./...` → OK.
- [ ] **Step 5: Commit**
```bash
git add -A && git commit -m "fix(billing): settle outstanding delta only + record rollup day (billing-1,2)"
```

### Task 1.2: Restrict settlement to superadmin + defensive scope (billing-3 / rbac-scoping-1)

**Files:**
- Modify: `internal/api/router.go:38` (`requireAdmin` → `requireSuperadmin` — done)
- Modify: `internal/api/billing_handlers.go` (add belt-and-suspenders scope check)
- Test: `internal/api/owner_scope_test.go` or a new `billing_handlers_test.go` asserting non-superadmin is rejected.

- [ ] **Step 1:** In `settleHandler` (`billing_handlers.go:13`), after decoding `body`, add:
```go
		if owner, all := scope(r); !all && body.TenantId != owner {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
```
(Route is now superadmin-only so `all` is always true here; this guard prevents regressions if the route wrapper ever changes.)
- [ ] **Step 2: Write test** that a session with role `admin` calling `settleHandler` gets 403 (mirror the pattern in `owner_scope_test.go` — build a request with an admin session payload in context, assert status). If the existing test harness lacks a settle helper, assert via the router that `POST /api/admin/settle` with an admin cookie returns 403.
- [ ] **Step 3: Run** the test → PASS. `go build ./...` → OK.
- [ ] **Step 4: Commit**
```bash
git add -A && git commit -m "fix(billing): settle is superadmin-only + scope guard (billing-3)"
```

### Task 1.3: Round money at API boundary + flag unknown models (billing-4, billing-5)

**Files:**
- Modify: `internal/api/admin_handlers.go:353-356` and `internal/api/me_handlers.go:166-184` — pass real `settled` and round fee outputs.
- Modify: `internal/billing/cost.go` — add `KnownModel(model) bool`.
- Modify: `internal/dispatch/service.go` `logOK`/`logStream` — when `!billing.KnownModel(model)`, set the log row `FallbackReason`/note to mark unknown-model pricing (so it is not silent). Implementer: read current `InsertDispatchLogParams` fields first.
- Test: `internal/billing/cost_test.go` (add `TestKnownModel`).

- [ ] **Step 1:** In `cost.go` add:
```go
// KnownModel reports whether the model maps to an explicit price family
// (opus/haiku/sonnet). Unknown models fall back to sonnet pricing in ModelPrice;
// callers should surface that fallback rather than bill silently.
func KnownModel(model string) bool {
	m := strings.ToLower(model)
	return strings.Contains(m, "opus") || strings.Contains(m, "haiku") || strings.Contains(m, "sonnet")
}
```
- [ ] **Step 2:** In `admin_handlers.go:353` replace `ComputeHostingFee(consumption, 0, rate)` with:
```go
				settled, _ := q.SumSettledForOwner(ctx, t.ID)
				unsettled, accumulated := billing.ComputeHostingFee(consumption, settled, rate)
```
and round the emitted money in the appended map: `"feeUsd": billing.RoundUSD(accumulated)`, `"unsettledUsd": billing.RoundUSD(unsettled)`, `"channelFeeUsd": billing.RoundUSD(channelFee)`, `"consumptionUsd": billing.RoundUSD(consumption)`, `"channelConsumptionUsd": billing.RoundUSD(channelConsumption)`.
- [ ] **Step 3:** In `me_handlers.go:164-184` do the same: add `settled, _ := q.SumSettledForOwner(ctx, owner)`, use it in `ComputeHostingFee`, and `RoundUSD` the `unsettledUsd`/`accumulatedUsd`/`channelHostingFeeUsd`/`consumptionUsd`/`channelConsumptionUsd` outputs.
- [ ] **Step 4:** Add `TestKnownModel` asserting `KnownModel("claude-opus-4-8")==true` and `KnownModel("gpt-4")==false`. Run → PASS.
- [ ] **Step 5:** Wire the unknown-model marker in `service.go` `logOK`/`logStream` (set reason to `"unknown-model-pricing"` when `!billing.KnownModel(model)` and reason is empty). `go build ./... && go test ./...` → OK.
- [ ] **Step 6: Commit**
```bash
git add -A && git commit -m "fix(billing): real settled in fee view, round USD, flag unknown-model pricing (billing-4,5)"
```

---

## Phase 2 — Auth, session & authz scoping (HIGH + MEDIUM + LOW)

Covers: auth-session-2,1,3,4,5,6; policy-effectiveness-5; rbac-scoping-2; provision-3; fallback-4.

### Task 2.1: Login rate-limit + lockout (auth-session-2, HIGH)

**Files:**
- Create: `internal/auth/throttle.go` — in-memory per-key failure tracker with sweeper.
- Create: `internal/auth/throttle_test.go`.
- Modify: `internal/api/auth_handlers.go` `loginHandler` — consult/record the throttle keyed by `username + clientIP`.
- Modify: `internal/api/middleware.go` or a helper for `clientIP(r)` (read `X-Forwarded-For` first hop, else `RemoteAddr`).

**Interfaces produced:** `auth.NewThrottle(maxFails int, window, lockout time.Duration) *Throttle`; `(*Throttle).Allowed(key string, now time.Time) bool`; `(*Throttle).RecordFailure(key string, now time.Time)`; `(*Throttle).Reset(key string)`.

- [ ] **Step 1: Write failing test** `throttle_test.go`:
```go
package auth

import (
	"testing"
	"time"
)

func TestThrottleLocksAfterN(t *testing.T) {
	now := time.Unix(1000, 0)
	th := NewThrottle(5, time.Minute, 15*time.Minute)
	k := "alice|1.2.3.4"
	for i := 0; i < 5; i++ {
		if !th.Allowed(k, now) {
			t.Fatalf("attempt %d should be allowed", i)
		}
		th.RecordFailure(k, now)
	}
	if th.Allowed(k, now) {
		t.Fatal("should be locked after 5 fails")
	}
	if !th.Allowed(k, now.Add(16*time.Minute)) {
		t.Fatal("should unlock after lockout window")
	}
}

func TestThrottleResetOnSuccess(t *testing.T) {
	now := time.Unix(1000, 0)
	th := NewThrottle(5, time.Minute, 15*time.Minute)
	k := "bob|1.2.3.4"
	for i := 0; i < 4; i++ {
		th.RecordFailure(k, now)
	}
	th.Reset(k)
	if !th.Allowed(k, now) {
		t.Fatal("reset should clear failures")
	}
}
```
- [ ] **Step 2: Run** `go test ./internal/auth/ -run TestThrottle -v` → FAIL (undefined).
- [ ] **Step 3: Implement** `throttle.go`: a `sync.Mutex`-guarded `map[string]*bucket{fails int, first, lockUntil time.Time}`. `Allowed` returns false while `now < lockUntil`; resets the bucket when `now-first > window`. `RecordFailure` increments and sets `lockUntil = now+lockout` once `fails >= maxFails`. `Reset` deletes the key. Add a `Sweep(now)` that drops idle buckets, called opportunistically inside `RecordFailure`.
- [ ] **Step 4: Run** test → PASS.
- [ ] **Step 5: Wire into loginHandler:** construct one `*auth.Throttle` in `NewRouter` (or a package var) and pass to `loginHandler`. At the top: `key := body.Username + "|" + clientIP(r); if !throttle.Allowed(key, time.Now()) { writeJSON(w, http.StatusTooManyRequests, map[string]string{"error":"too many attempts"}); return }`. On invalid creds: `throttle.RecordFailure(key, time.Now())`. On success: `throttle.Reset(key)`. Keep timing uniform (still run `DummyVerify` on unknown user).
- [ ] **Step 6:** `go build ./... && go test ./...` → OK. **Commit**
```bash
git add -A && git commit -m "fix(auth): per-username+IP login rate-limit and lockout (auth-session-2)"
```

### Task 2.2: Session epoch / revocation (auth-session-1, MEDIUM)

**Files:**
- Create migration: `migrations/20260623000030_session_epoch.sql` — `ALTER TABLE tenants ADD COLUMN session_epoch BIGINT NOT NULL DEFAULT 0;`
- Modify: `internal/auth/session.go` — add `Epoch int64` to `SessionPayload`; `IssueSession` takes epoch; `VerifySession` stays signature/expiry only (epoch compared in middleware against DB).
- Modify: `internal/api/auth_handlers.go` `loginHandler` — fetch `u.SessionEpoch`, pass to `IssueSession`.
- Modify: `internal/api/middleware.go` `requireSession` — after `VerifySession`, look up the user's current `session_epoch`; reject if `payload.Epoch != dbEpoch` OR user row missing.
- Modify: queries `users.sql` — add `GetSessionEpoch` and `BumpSessionEpoch`; call `BumpSessionEpoch` in `setUserRoleHandler`, `changePasswordHandler`, and on delete (delete already invalidates via missing row).
- Test: `internal/auth/session_test.go` add an epoch round-trip test.

- [ ] **Step 1: Write migration** and `sqlc generate` after adding queries:
```sql
-- name: GetSessionEpoch :one
SELECT session_epoch FROM tenants WHERE id = $1;

-- name: BumpSessionEpoch :exec
UPDATE tenants SET session_epoch = session_epoch + 1 WHERE id = $1;
```
- [ ] **Step 2: Write failing test** in `session_test.go`: issue a token with epoch 7, verify payload carries `Exp` and decode shows `Epoch==7`.
- [ ] **Step 3: Implement** `SessionPayload.Epoch`, thread through `IssueSession(secret, sub, role string, epoch, nowUnix, ttlSec int64)`. Update all callers (login). `VerifySession` unchanged except it now returns the epoch in the payload.
- [ ] **Step 4: Middleware DB check** — `requireSession` gains access to `pool`/`q` (it already wraps handlers that have `q`; pass a `*pgxpool.Pool` or `*sqlc.Queries` into `requireSession`). Reject when `GetSessionEpoch(sub)` errors (no row) or differs.
  > Note: `requireSession` currently takes only `(secret, next)`. Changing its signature touches every route in `router.go`. Implementer: add `q` as a param and update `NewRouter` wiring in one mechanical pass; keep behavior identical for valid sessions.
- [ ] **Step 5: Call `BumpSessionEpoch`** in `setUserRoleHandler` and `changePasswordHandler`.
- [ ] **Step 6:** `go build ./... && go test ./...` → OK. **Commit**
```bash
git add -A && git commit -m "fix(auth): session epoch invalidates tokens on role/password change (auth-session-1)"
```

### Task 2.3: Secure cookie flag (env-gated) + secret strength (auth-session-3)

**Files:**
- Modify: `internal/config/config.go` — add `SecureCookies bool` from env `TOWER_SECURE_COOKIES` (default false); validate `SessionSecret` length `>= 32` (error at startup if shorter, matching master-key validation).
- Modify: `internal/api/auth_handlers.go` `loginHandler` cookie — set `Secure: cfg.SecureCookies`. Pass the flag into the handler/router.
- Test: `internal/config/config_test.go` — secret too short → error; `TOWER_SECURE_COOKIES=1` → true.

- [ ] Steps: write config test (short secret rejected, env parsed) → fail → implement → pass → wire cookie `Secure` → build/test → commit `fix(auth): env-gated Secure cookie + min session-secret length (auth-session-3)`.

### Task 2.4: Enforce or remove decorative RBAC perms (auth-session-4)

**Decision:** keep coarse role gating as the real authz (it works), and **wire `rbac.Can` as a server-side `requirePerm` helper on the sensitive superadmin routes** so the seeded permissions actually take effect (this also resolves rbac-scoping-3's dead `rbac.Can`).

**Files:**
- Modify: `internal/api/middleware.go` — add `requirePerm(secret, q, capability string, next)` that loads the caller's role permissions (`loadPerms`) and calls `rbac.Can(perms, capability, rbac.Scope{IsAdmin: role∈{superadmin}})`.
- Modify: `internal/api/router.go` — wrap `billing:settle`, user-management routes with `requirePerm` in addition to `requireSuperadmin` (defense in depth), OR document perms as advisory if wiring proves too invasive.
- Test: `internal/rbac/rbac_test.go` already covers `Can`; add a middleware test that a role lacking the capability gets 403.

- [ ] Steps: test → implement `requirePerm` → wire on 2-3 sensitive routes → build/test → commit `fix(rbac): enforce seeded role permissions via requirePerm (auth-session-4, rbac-scoping-3)`.

### Task 2.5: Global policy mutation requires superadmin (policy-effectiveness-5)

**Files:** `internal/api/router.go:41-42` — `PUT /api/admin/policies/global` and `POST /api/admin/policies/dry-run` → `requireSuperadmin`.

- [ ] Steps: change wrappers → add/extend a router test asserting admin role gets 403 on policy PUT → build/test → commit `fix(policy): global policy mutation is superadmin-only (policy-effectiveness-5)`.

### Task 2.6: Enforce must_change_pw at login (auth-session-5)

**Files:** `internal/api/auth_handlers.go` `loginHandler` — include `mustChangePw` in the login response (and/or `/auth/me`); SPA shows a forced password-change gate. Implementer: read the `tenants.must_change_pw` column and `meHandler`.

- [ ] Steps: surface `mustChangePw` in `/auth/me` response → SPA `auth.tsx` forces change when true → build/test → commit `fix(auth): surface+enforce must_change_pw (auth-session-5)`.

### Task 2.7: CSRF defense for cookie-auth mutations (auth-session-6)

**Files:** `internal/api/middleware.go` — add a `requireSameOrigin`/custom-header check (e.g. require `X-Requested-With: tower` or an `Origin`/`Referer` allowlist) on state-changing cookie-authed routes; SPA `api.ts` sends the header. SameSite=Lax already mitigates cross-site POST.

- [ ] Steps: add header check helper → SPA sends header on non-GET → test a forged request without header gets 403 → build/test → commit `fix(auth): CSRF header check on cookie-auth mutations (auth-session-6)`.

### Task 2.8: Enforce fallback channel limit on admin create too (fallback-4)

**Files:** `internal/api/fallback_handlers.go` `createFallbackHandler` — apply the same per-tenant `FallbackLimit` check that `meCreateFallbackHandler` uses when the created channel is owner-scoped.

- [ ] Steps: test admin create beyond limit for an owner → 403 → implement → build/test → commit `fix(fallback): enforce per-tenant channel limit on admin create (fallback-4)`.

### Task 2.9: Provision honors caller owner-scope (provision-desired-reconcile-3)

**Files:** `internal/api/provision_handlers.go` `startProvisionHandler` (and node creation it triggers) — for a non-superadmin caller, force the node's `owner_id` to `scope(r)` owner instead of accepting body/global.

- [ ] Steps: test non-superadmin provision assigns owner=caller → implement → build/test → commit `fix(provision): force owner-scope on node creation (provision-3)`.

### Task 2.10: createNode rejects attacker-supplied ownerId (rbac-scoping-2)

**Files:** `internal/api/admin_handlers.go` `createNodeHandler` — for non-superadmin, set `owner_id = scope(r)` owner, ignoring `body.ownerId`.

- [ ] Steps: test admin createNode with foreign ownerId → owner forced to caller → implement → build/test → commit `fix(rbac): createNode forces owner to caller for non-superadmin (rbac-scoping-2)`.

---

## Phase 3 — Quota rotation effectiveness (HIGH + MEDIUM)

Covers: nodeclient-telemetry-1 (= policy-effectiveness-2), policy-effectiveness-3.

### Task 3.1: Project CPA quota into rotation (telemetry-1 / policy-2, HIGH)

**Files:**
- Modify: `internal/cpaclient/discovery.go` (`Sync`, ~:88 where it upserts `cpa_account_quota.five_hour_util/seven_day_util`) — after upserting quota, project utilization into the live store like the meridian poller does.
- Modify/Reuse: `internal/telemetry/map.go` `LimitsFromQuota` (or add a CPA-specific projector that maps `five_hour_util`/`seven_day_util`/`seven_day_opus_util`/`seven_day_sonnet_util` through the same `QuotaRotateThreshold` → `Store.SetLimited(cpaKey, capacity, limits)`).
- Needs access to the `*state.Store` and the threshold from policy inside `cpaclient.Sync` (or do the projection in the caller that already has the store; implementer: trace who calls `cpaclient.Sync` and whether it has the store — likely a discovery loop in `cmd/tower` or a poller; thread the store + threshold in).
- Test: `internal/telemetry/map_test.go` — extend to assert a CPA-style profile at 95% util with threshold 0.9 yields a non-empty limits map keyed by class with a future deadline.

- [ ] **Step 1:** Confirm where CPA quota is fetched and whether a `*state.Store` is reachable there. Read `internal/cpaclient/discovery.go` and its caller.
- [ ] **Step 2: Write failing test** that the projection function (e.g. `LimitsFromCpaQuota(util map, threshold, now, ttl)`) returns `{"all": resetTime}` when 5h util ≥ threshold.
- [ ] **Step 3: Implement** the projector (reuse `windowClass`/threshold semantics from `map.go` so behavior matches meridian) and call `Store.SetLimited(nodeID+":"+profileID, capacity, limits)` after each CPA quota sync.
- [ ] **Step 4: Run** test → PASS. `go build ./...` → OK.
- [ ] **Step 5: Live read-only sanity** (no mutation): `GET /api/admin/accounts` shows CPA `cpaQuota` 5h/7d util; confirm a high-util account would now be gated (logic check, not a live write).
- [ ] **Step 6: Commit**
```bash
git add -A && git commit -m "feat(cpa): drive quota rotation from CPA utilization (telemetry-1, policy-2)"
```

### Task 3.2: Normalize model→class in quota lookup (policy-effectiveness-3)

**Files:**
- Modify: `internal/state/account.go` `limitedFor` — also check `LimitedUntil[classOf(model)]`.
- Add: `classOf(model string) string` (opus/sonnet/haiku→class; else "all") — colocate in `account.go` or `telemetry/map.go` and reuse `windowClass` naming.
- Test: `internal/state/account_test.go` — set `LimitedUntil["opus"]` in the future, assert `CanDispatch(now, "claude-opus-4-8")` returns not-ok.

- [ ] Steps: write failing test → implement `classOf` + extend `limitedFor` → run pass → build/test → commit `fix(policy): match per-class quota limits by model class, not raw id (policy-effectiveness-3)`.

---

## Phase 4 — Policy scope & dry-run (MEDIUM + uncertain)

Covers: policy-effectiveness-1, policy-effectiveness-4, policy-effectiveness-6 (decision).

### Task 4.1: Wire per-tenant policy override (policy-effectiveness-1)

**Decision:** wire the existing variadic three-layer `policy.Resolve`. Add a write route + read in `resolveConfig`.

**Files:**
- Modify: `internal/api/console_handlers.go` — add `putTenantPolicyHandler` (scopeType `owner`, scopeId = tenant) gated `requireSuperadmin`; add route in `router.go` (`PUT /api/admin/policies/tenant/{id}`).
- Modify: `internal/dispatch/service.go` `resolveConfig` (~:228-235) — load global + the dispatch owner's tenant policy row and apply ordered (`policy.Resolve(base, globalPatch, tenantPatch)`).
- Modify: `internal/telemetry/poller.go` if it also resolves policy per account.
- Test: `internal/policy/policy_test.go` — `Resolve(base, global, tenant)` applies tenant over global.

- [ ] Steps: test layered resolve → implement write handler + route + resolveConfig load → build/test → commit `feat(policy): per-tenant policy override resolved over global (policy-effectiveness-1)`.

### Task 4.2: Dry-run against effective stored policy (policy-effectiveness-4)

**Files:** `internal/api/console_handlers.go` `dryRunPolicyHandler` — give it `q`; load the stored global params, unmarshal into a `policy.Config`, then `policy.DryRun(currentEffective, incomingPatch)` so the preview matches the merge `PUT` performs.

- [ ] Steps: test dry-run base == stored (not Defaults) → implement (add `q` param, load row) → build/test → commit `fix(policy): dry-run previews against effective policy, not Defaults (policy-effectiveness-4)`.

### Task 4.3: Document elastic baseline behavior (policy-effectiveness-6, uncertain)

**Files:** `internal/policy/policy.go` doc comment — clarify that `ElasticEnabled` produces reserves only when accounts have non-baseline roles; no code change unless the user wants elastic active on the current fleet.

- [ ] Steps: add doc comment → build → commit `docs(policy): clarify elastic reserve preconditions (policy-effectiveness-6)`.

---

## Phase 5 — Fallback effectiveness (MEDIUM + LOW)

Covers: fallback-1, fallback-2, fallback-3, fallback-5.

### Task 5.1: Enforce ModelAllowlist in dispatch (fallback-1)

**Files:** `internal/dispatch/service.go` `enabledChannels()` (~:457) — skip channels whose non-empty `model_allowlist` does not contain the requested model. Add a helper `channelAllowsModel(allowlist, model string) bool` (comma/space split; substring match against `opus`/`haiku`/`sonnet` family consistent with `billing.ModelPrice`).
- Test: `internal/dispatch/` unit test for `channelAllowsModel` (empty allowlist → all allowed; `"haiku"` → blocks opus).

- [ ] Steps: test helper → implement + filter in `enabledChannels` → build/test → commit `fix(fallback): enforce per-channel model_allowlist (fallback-1)`.

### Task 5.2: Enforce fallback MaxConcurrent (fallback-2)

**Files:** `internal/dispatch/service.go` `viaChannel` (~:474) and `streamChannel` (~:988) — when `Store.TryDispatch` returns `occupied==false` (slot set full), **do not forward**; skip to the next channel or return backpressure (503) if none.
- Test: simulate a full slot set and assert the request is not forwarded (or returns 503). Read current `TryDispatch`/`Slots.Acquire` semantics first.

- [ ] Steps: write test → implement reject/skip-on-full → build/test → commit `fix(fallback): MaxConcurrent actually caps concurrency (fallback-2)`.

### Task 5.3: Honor per-channel price_threshold (fallback-3)

**Files:** `internal/dispatch/service.go` price-trigger logic + `internal/fallback/decide.go` — use the channel's `price_threshold` when set, falling back to the global policy threshold only when the channel value is 0.
- Test: `internal/fallback/decide_test.go` — channel threshold overrides global.

- [ ] Steps: test override → implement → build/test → commit `fix(fallback): per-channel price_threshold overrides global (fallback-3)`.

### Task 5.4: Make balance gating real (fallback-5)

**Files:** `internal/dispatch/service.go` — write the observed balance into `fallback_spend.balance_observed` (currently always 0) and, when `balance_observed < balance_alert_usd` (or <=0), skip routing to that channel rather than only notifying.
- Test: a channel below alert balance is excluded from `enabledChannels`.

- [ ] Steps: test balance exclusion → implement observed-balance write + gate → build/test → commit `fix(fallback): low-balance channel removed from routing, not just alerted (fallback-5)`.

---

## Phase 6 — Secrets at rest (MEDIUM)

Covers: vault-crypto-1, vault-crypto-3, vault-crypto-2.

### Task 6.1: Plumb the Cipher through the app (vault-crypto-1)

**Files:** `cmd/tower/main.go:32` — keep the constructed `*crypto.Cipher` (stop discarding with `_`); pass it into the API router / dispatch service / cpaclient wherever secrets are written/read.
- Test: build-time wiring; add a smoke test that the server constructs with a cipher.

- [ ] Steps: stop discarding cipher → thread into `NewRouter`/services → build → commit `refactor(crypto): plumb master-key Cipher into runtime (vault-crypto-1)`.

### Task 6.2: Encrypt node/channel secrets at rest (vault-crypto-3)

**Files:**
- Modify: `internal/api/admin_handlers.go` `createNodeHandler` and `internal/api/fallback_handlers.go` create/update — `cipher.Encrypt` `api_key`/`mgmt_key`/`balance_token` before insert.
- Modify: read paths — `internal/cpaclient/client.go` (`Bearer `+mgmtKey) and `internal/dispatch/service.go` (`ch.ApiKey`) — `cipher.Decrypt` just before use.
- Migration: add a one-time backfill is NOT auto-run against prod; document that existing plaintext rows must be re-saved. Implementer: support a transparent "decrypt-if-decryptable-else-treat-as-plaintext" read shim to avoid breaking existing rows.
- Test: round-trip encrypt→store→decrypt→use for one node and one channel (unit-level with a test cipher).

- [ ] Steps: test round-trip + plaintext-fallback read shim → implement encrypt-on-write/decrypt-on-read → build/test → commit `fix(crypto): encrypt node/channel secrets at rest with master key (vault-crypto-3)`.

### Task 6.3: Persist OAuth creds encrypted on import (vault-crypto-2)

**Files:** `internal/api/account_handlers.go` import / oauth-exchange path — currently stores `OauthAccessEnc:""`. Encrypt the real creds via `cipher.Encrypt` and store; decrypt on use.
- Test: import stores non-empty ciphertext that decrypts to the original.

- [ ] Steps: test → implement → build/test → commit `fix(crypto): persist OAuth creds encrypted instead of empty (vault-crypto-2)`.

> If the user prefers **delete over wire** for vault/crypto, Phase 6 collapses to: remove `internal/vault`, `internal/crypto`, the enc columns/queries, and document that DB-at-rest encryption is the deployment's responsibility. Confirm before executing Phase 6.

---

## Phase 7 — Audit trail & ban events (MEDIUM + LOW)

Covers: events-audit-1, events-audit-3, events-audit-2 (= ban-classify-1), events-audit-4, events-audit-5, ban-classify-6.

### Task 7.1: Record audit entries on every mutating admin action (events-audit-1)

**Files:** each mutating handler in `internal/api/*_handlers.go` — call `audit.Record(ctx, q, now, audit.Entry{Actor: sub, Action: "...", Target: "...", Before: ..., After: ...})`. Highest priority: `setUserRoleHandler`, `createUserHandler`, `deleteUserHandler`, `setUserHostingRateHandler`/`channelRate`/`fallbackLimit`, `settleHandler`, `putGlobalPolicyHandler`, node create/delete, account recover/owner/expiry, fallback/slot CRUD, dispatch-key create/delete.
- Add a small helper `auditFrom(r)` returning the caller `sub`.
- Test: `internal/api/...` — a known mutation (e.g. setUserRole) produces one audit row (DB test, skips without `TEST_DATABASE_URL`); plus a pure test that `audit.Record` is invoked (via a fake `q` interface) if feasible.

- [ ] Steps: add `auditFrom` helper → add `audit.Record` calls to the listed handlers (one commit per cluster is fine) → build/test → commit `fix(audit): record audit entries for all mutating admin actions (events-audit-1)`.

### Task 7.2: Attribute ban events to the banned account's owner (events-audit-3)

**Files:** `internal/dispatch/service.go` `recordBan`/`recordRetry`/`maybeCooldown` (~:872) — enrich with the **banned account's** owner (`acctOwner[accountID]`) rather than the dispatch caller's owner. Implementer: build/lookup an `accountID→ownerID` map already available in `buildCandidates`.
- Test: ban under an admin/unowned key on a tenant-owned account writes the event with the tenant's owner_id.

- [ ] Steps: test attribution → implement owner lookup in recordBan → build/test → commit `fix(events): ban events attributed to banned account owner (events-audit-3)`.

### Task 7.3: Close ban episodes (events-audit-2 / ban-classify-1)

**Files:** call `RecoverBanEpisode` where an account recovers from half-open→active (in the breaker recovery path or the recover handler) so `recovered_at`/`survival_ms` are populated.
- Test: simulate ban→recover, assert episode `recovered_at != 0`.

- [ ] Steps: test → wire `RecoverBanEpisode` on recovery → build/test → commit `fix(events): close ban episodes on recovery (events-audit-2)`.

### Task 7.4: Admin events/logs owner filter before limit (events-audit-4)

**Files:** the admin events/logs queries (`queries/events.sql`, `dispatch_logs.sql`) and handlers — push the owner filter into the SQL `WHERE` so the `LIMIT` applies after filtering (not a post-filter on a pre-limited page). Regenerate sqlc.
- Test: a scoped admin sees a full page of own rows even when other owners dominate recent rows (DB test).

- [ ] Steps: add owner-scoped queries → use in handlers → sqlc generate → build/test → commit `fix(events): owner filter in SQL before limit (events-audit-4)`.

### Task 7.5: Atomic ban event + episode (events-audit-5) & lock staleness (ban-classify-6)

**Files:** `internal/dispatch/service.go` recordBan — write the event and episode in one transaction (or accept best-effort but stop swallowing errors silently — log them); read streak/permanent under the same breaker lock to avoid the benign staleness in ban-classify-6.
- Test: existing banevents test extended.

- [ ] Steps: test → implement tx/lock fix → build/test → commit `fix(events): atomic ban event+episode, consistent streak read (events-audit-5, ban-classify-6)`.

---

## Phase 8 — Node / telemetry / CPA (MEDIUM + LOW)

Covers: nodeclient-telemetry-4, cpa-1, cpa-3, cpa-2 (uncertain), nodeclient-telemetry-3, nodeclient-telemetry-5.

### Task 8.1: Per-account offline, not node-wide (nodeclient-telemetry-4)

**Files:** `internal/telemetry/poller.go` (~:58-91) + `internal/telemetry/map.go` `OfflineFromHealth` — derive per-account offline from `QuotaAll` per-profile presence/`IsActive`; treat node-level health error as node-down for all, but a logged-out active profile must not offline sibling profiles that QuotaAll reports active.
- Test: `map_test.go`/`poller_test.go` — one logged-out active profile + healthy siblings → only the active profile offline.

- [ ] Steps: test → implement per-profile offline → build/test → commit `fix(telemetry): per-account offline instead of node-wide (nodeclient-telemetry-4)`.

### Task 8.2: CPA quota endpoint uses CPA usage API (cpa-1)

**Files:** `internal/api/node_control_handlers.go` `nodeQuotaHandler` — for `kind=cpa`, call the CPA usage API (via `cpaclient`) instead of meridian `/v1/usage/quota/all`. Read `cpaclient` for the right method.
- Test: handler dispatches by node kind (unit with a faked client) — assert CPA branch taken.

- [ ] Steps: test kind routing → implement CPA branch → build/test → commit `fix(cpa): node quota endpoint uses CPA usage API (cpa-1)`.

### Task 8.3: Surface CPA quota fetch errors (cpa-3)

**Files:** `internal/cpaclient/discovery.go` — stop swallowing quota fetch errors; record a fetch-error marker so the UI can show "quota unavailable" instead of silently null.
- Test: error path sets an error field rather than nil-and-continue.

- [ ] Steps: test → implement → build/test → commit `fix(cpa): surface quota fetch errors (cpa-3)`.

### Task 8.4: Guard node-control routes for CPA (nodeclient-telemetry-5)

**Files:** `internal/api/node_control_handlers.go` — telemetry/quota/features/oauth/refresh handlers: when `kind=cpa`, either route to CPA equivalents or return a clear 409/"not applicable for CPA" instead of calling meridian endpoints.

- [ ] Steps: test CPA guard → implement → build/test → commit `fix(node): guard meridian-only control routes against CPA nodes (nodeclient-telemetry-5)`.

### Task 8.5: QuotaAvg drives elastic or is documented (nodeclient-telemetry-3)

**Files:** decide: wire node-reported `QuotaAvg` into elastic scaling input, or document it as display-only. Default: document (low value).

- [ ] Steps: doc comment (or wire if user wants) → build → commit `docs(telemetry): QuotaAvg is display-only (nodeclient-telemetry-3)`.

### Task 8.6: Discovery must not re-enable manually disabled CPA accounts (cpa-2, uncertain)

**Files:** `internal/cpaclient/discovery.go` `Sync` — do not flip `enabled` back to true for an account an admin manually disabled; only manage discovery of new accounts. Confirm intent with user (uncertain finding).
- Test: a disabled account stays disabled across a sync.

- [ ] Steps: confirm intent → test → implement → build/test → commit `fix(cpa): discovery preserves manual disable (cpa-2)`.

---

## Phase 9 — State persistence (LOW)

Covers: state-store-1, state-store-2.

### Task 9.1: Drop orphan account_state on node delete (state-store-1)

**Files:** `internal/api/admin_handlers.go` `deleteNodeHandler` — also delete `account_state` rows for the node's accounts (add query `DeleteAccountStateByNode`), so deleted nodes don't resurrect ghost accounts / stale permanent bans on restart. Regenerate sqlc.
- Test: DB test — delete node removes its account_state rows.

- [ ] Steps: add query → call in deleteNodeHandler → sqlc generate → build/test → commit `fix(state): delete account_state on node delete (state-store-1)`.

### Task 9.2: Persist status includes CoolUntil/LimitedUntil (state-store-2)

**Files:** `internal/state/persist.go` `PersistAll` — compute the persisted status from the full account (including `CoolUntil`/`LimitedUntil`), not a copy missing those fields.
- Test: `persist_test.go` — an account in cooldown persists status "cooldown".

- [ ] Steps: test → implement → build/test → commit `fix(state): persist cooldown/limited status correctly (state-store-2)`.

---

## Phase 10 — Dispatch correctness & dead-code removal (LOW)

Covers: dispatch-core-1, dispatch-core-4, dispatch-core-6, dispatch-core-2 (delete), dispatch-core-3 (delete), rbac-scoping-3 (if not wired in 2.4 → delete), provision-1, provision-2, provision-4.

### Task 10.1: Forge claude-code headers on non-stream path (dispatch-core-1)

**Files:** `internal/dispatch/proxy.go` `NodeProxy.Send` (~:69-92) — call `ForgeClaudeCodeHeaders(req.Header)` after `setNodeAuthHeaders`, mirroring `OpenStream:156`.
- Test: `proxy_test.go` — Send sets `User-Agent: claude-cli/1.0` / `x-app: cli` (for non-cpa).

- [ ] Steps: test → implement → build/test → commit `fix(dispatch): forge claude-code headers on non-stream path (dispatch-core-1)`.

### Task 10.2: Detect in-body errors on non-stream success (dispatch-core-4)

**Files:** `internal/dispatch/service.go`/`proxy.go` non-stream success path — inspect HTTP-200 bodies for `event: error`/error JSON the stream path already catches, and classify/ban accordingly.
- Test: a 200 response with an in-body error is classified as error.

- [ ] Steps: test → implement → build/test → commit `fix(dispatch): classify in-body errors on non-stream 200 (dispatch-core-4)`.

### Task 10.3: Stream read/idle timeout (dispatch-core-6)

**Files:** `internal/dispatch/proxy.go` `OpenStream` — give the streaming client an idle/read deadline so a silently-stalled upstream releases the slot instead of holding it until the client gives up.
- Test: stalled reader triggers timeout (use a slow test server or a context deadline).

- [ ] Steps: test → implement deadline → build/test → commit `fix(dispatch): idle/read timeout on streaming proxy (dispatch-core-6)`.

### Task 10.4: Delete dead routing code (dispatch-core-2, dispatch-core-3, rbac-scoping-3)

**Files:** delete `internal/dispatch/wlr.go` + `wlr_test.go` (SelectWLR power-of-two never called); delete the dead `pickElastic` helper (elastic is inline in `buildCandidates`); if Task 2.4 did **not** wire `rbac.Can`, delete `internal/rbac` usages note. Confirm zero non-test callers via grep before deleting.

- [ ] Steps: grep to confirm dead → delete files/functions → `go build ./... && go test ./...` → commit `chore(dispatch): remove dead SelectWLR/pickElastic routing code (dispatch-core-2,3)`.

### Task 10.5: Provision sets node kind + secure SSH (provision-2, provision-4) & reconciler note (provision-1)

**Files:**
- `internal/provision/*` node registration — set `kind="meridian"` (not `""`).
- `internal/provision/ssh.go` — enable host-key verification (use `knownhosts` or pinned host key) instead of `InsecureIgnoreHostKey`; if password auth must stay, document the MITM risk and require a pinned key.
- `internal/reconcile/reconciler.go` — document/enable the reconciler loop (currently no-op in prod). Default: document; enable only if user wants active reconciliation.
- Test: provision registers `kind=meridian`; ssh config rejects unknown host key.

- [ ] Steps: tests → implement kind + ssh host-key → build/test → commit `fix(provision): set node kind=meridian + verify SSH host key (provision-2,4)`.

---

## Phase 11 — Frontend (LOW)

Covers: frontend-spa-1, frontend-spa-2, frontend-spa-5, frontend-spa-3.

### Task 11.1: Role guards on client routes (frontend-spa-1)

**Files:** `web/spa/src/App.tsx` / `Shell.tsx` — wrap admin/superadmin-only routes (Users, Policies, Desired, Billing settle, Provision) in a `RequireRole` guard that redirects a tenant/viewer to the dashboard. Source role from `auth.tsx` (`/auth/me`).
- Test: SPA unit/render test (vitest) that a tenant role does not render the Users page; rely on backend authz as the real gate (already enforced).

- [ ] Steps: implement `RequireRole` → wrap routes → `cd web/spa && npm run build` succeeds → commit `fix(web): role guards on admin-only routes (frontend-spa-1)`.

### Task 11.2: Gate the Policies page form (frontend-spa-2)

**Files:** `web/spa/src/pages/Policies.tsx` — hide/disable the editable form + Save/dry-run for non-superadmin; show read-only or an error state.

- [ ] Steps: implement gate → build → commit `fix(web): gate Policies editor to superadmin (frontend-spa-2)`.

### Task 11.3: Robust policy-load array parsing (frontend-spa-5)

**Files:** `web/spa/src/pages/Policies.tsx` — when an array policy field comes back as JSON `null`, default to `[]` instead of aborting all field hydration.

- [ ] Steps: implement null-safe parse → build → commit `fix(web): null-safe policy array hydration (frontend-spa-5)`.

### Task 11.4: Per-second recover countdown (frontend-spa-3)

**Files:** `web/spa/src/pages/Dispatch.tsx` — tick the recover countdown every second (like the Accounts quota countdown) instead of only on the 2s SSE push.

- [ ] Steps: implement local ticking → build → commit `fix(web): per-second recover countdown on Dispatch (frontend-spa-3)`.

---

## Final verification (after all phases)

- [ ] `go build ./...` clean.
- [ ] `go test ./...` green (DB tests may skip without `TEST_DATABASE_URL`).
- [ ] `cd web/spa && npm run build` clean.
- [ ] `git log --oneline` shows one focused commit per task.
- [ ] Re-run a read-only live smoke against the instance (GET dashboard/accounts/policies) to confirm nothing regressed in shapes the SPA consumes.
- [ ] Present the full diff to the user for deploy decision (do NOT deploy).

## Self-Review notes

- **Coverage:** every confirmed-actionable finding (53) maps to a task; INFO/verified-good (26), refuted (2) need no fix. Uncertain (cpa-2, policy-6) are tasks 8.6 / 4.3 pending user confirmation.
- **Sequencing:** Phases 1–3 (money, authz,防封生效) are highest value; do first. Phase 6 (secrets) and Phase 4.1 (per-tenant policy) are the largest builds and carry a wire-vs-delete decision — confirm with user before executing.
- **Shared hot files:** `service.go`, `router.go`, `middleware.go`, `console_handlers.go` are touched by multiple phases — execute phases sequentially (not parallel worktrees) to avoid conflicts, or partition by file if parallelizing.
