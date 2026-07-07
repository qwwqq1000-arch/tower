# Node 号库归属用户 Dropdown (#137) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The Add-Node form's owner field becomes a "号库归属用户" dropdown (loads users, default `test`); the node stays superadmin-owned while its discovered accounts go to the selected user, via a new `nodes.account_owner_id` column.

**Architecture:** Add `account_owner_id` to `nodes`. CPA discovery (`Sync`) assigns each account to `account_owner_id` when set, else the node's `owner_id` (back-compat). The manual form and the push-node API both persist `account_owner_id`; the push API's inline `UPDATE accounts …` override is removed in favor of the column.

**Tech Stack:** Go (goose migrations, sqlc/pgx), React/Vite SPA, Postgres.

## Global Constraints
- Owner-scoping unchanged: `scope(r)` still forces a non-superadmin's *node* owner to themselves; only the *account* owner is the selected user.
- Default account owner = the `test` tenant id when the dropdown is left default; if `test` is absent, fall back to `''`.
- `account_owner_id` defaults to `''` → discovery falls back to `owner_id` → existing nodes behave exactly as today.
- `listUsers` is `requireSuperadmin`; the dropdown must not crash for a non-superadmin (degrade to 超级管理员 + test).
- Migrations are goose, timestamped `YYYYMMDDHHMMSS_name.sql`, auto-applied via `internal/db/migrate.go` (embedded `migrations.FS`). Never renumber an applied migration.

---

### Task 1: Migration + sqlc — `nodes.account_owner_id`

**Files:**
- Create: `migrations/20260626120000_node_account_owner.sql`
- Modify: `internal/db/queries/nodes.sql` (CreateNode insert/returning; add SetNodeAccountOwner; ListNodes/GetNode already `SELECT *`-style — verify they return the new column)
- Modify (generated): `internal/db/sqlc/nodes.sql.go`, `internal/db/sqlc/models.go`
- Test: `internal/db/migrate_test.go`

**Interfaces:**
- Produces: `sqlc.Node.AccountOwnerID string`; `CreateNodeParams.AccountOwnerID string`; `Queries.SetNodeAccountOwner(ctx, SetNodeAccountOwnerParams{ID, AccountOwnerID}) error`.

- [ ] **Step 1: Write the migration**

```sql
-- +goose Up
ALTER TABLE nodes ADD COLUMN account_owner_id TEXT NOT NULL DEFAULT '';

-- +goose Down
ALTER TABLE nodes DROP COLUMN account_owner_id;
```

- [ ] **Step 2: Add a failing migration test asserting the column exists**

Append to `internal/db/migrate_test.go`:

```go
func TestMigrate_NodeAccountOwnerColumn(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	if err := Migrate(ctx, url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	var n int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM information_schema.columns WHERE table_name='nodes' AND column_name='account_owner_id'`).Scan(&n); err != nil {
		t.Fatalf("query: %v", err)
	}
	if n != 1 {
		t.Fatalf("account_owner_id column missing (got %d)", n)
	}
}
```

- [ ] **Step 3: Run it (skips without DB; with TEST_DATABASE_URL it must pass after the migration)**

Run: `go test ./internal/db/ -run TestMigrate_NodeAccountOwnerColumn -v`
Expected: PASS (or SKIP if no TEST_DATABASE_URL).

- [ ] **Step 4: Update the sqlc query source** in `internal/db/queries/nodes.sql`: add `account_owner_id` to the `CreateNode` INSERT column list, its `$N` value, and the `RETURNING`; ensure `GetNode`/`ListNodes`/`ListNodesByOwner` select lists include `account_owner_id`. Add:

```sql
-- name: SetNodeAccountOwner :exec
UPDATE nodes SET account_owner_id = $2 WHERE id = $1;
```

- [ ] **Step 5: Regenerate or hand-edit sqlc** so `sqlc.Node` gains `AccountOwnerID string`, `CreateNodeParams` gains `AccountOwnerID string` (appended last, wired into the INSERT args + the row Scan in `CreateNode`, `GetNode`, `ListNodes`, `ListNodesByOwner`), and `SetNodeAccountOwner` + `SetNodeAccountOwnerParams{ID, AccountOwnerID string}` exist. If `sqlc` binary is unavailable, hand-edit `internal/db/sqlc/nodes.sql.go` + `models.go` to match the existing generated style (every `SELECT`/`RETURNING` column maps to a `&i.Field` Scan in the same order).

- [ ] **Step 6: Build**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 7: Commit**

```bash
git add migrations/20260626120000_node_account_owner.sql internal/db/queries/nodes.sql internal/db/sqlc/nodes.sql.go internal/db/sqlc/models.go internal/db/migrate_test.go
git commit -m "feat(db): nodes.account_owner_id column + SetNodeAccountOwner query"
```

---

### Task 2: discovery assigns account owner from the column

**Files:**
- Modify: `internal/cpaclient/discovery.go` (`Sync`, the `UpsertCpaAccount` call ~line 188-194)
- Test: `internal/cpaclient/discovery_owner_test.go` (new)

**Interfaces:**
- Consumes: `sqlc.Node.AccountOwnerID` (Task 1).

- [ ] **Step 1: Write the failing test** — `internal/cpaclient/discovery_owner_test.go`. Use an httptest server serving `/v0/management/auth-files` with one account, a fake `syncQuerier` capturing the `UpsertCpaAccountParams`, and assert OwnerID resolves to `account_owner_id` when set, else `owner_id`. Model it on the existing `internal/cpaclient/discovery_test.go` fake querier (read that file first for the exact `syncQuerier` interface + the auth-files JSON shape). The assertion:

```go
// node.AccountOwnerID = "u_acct" , node.OwnerID = "u_node"  → captured account OwnerID == "u_acct"
// node.AccountOwnerID = ""       , node.OwnerID = "u_node"  → captured account OwnerID == "u_node"
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/cpaclient/ -run TestSyncAccountOwner -v`
Expected: FAIL (account owner is currently always `node.OwnerID`).

- [ ] **Step 3: Implement** in `Sync` (replace `OwnerID: node.OwnerID` in the `UpsertCpaAccountParams`):

```go
acctOwner := node.AccountOwnerID
if acctOwner == "" {
	acctOwner = node.OwnerID
}
// ... UpsertCpaAccountParams{ ID: aid, OwnerID: acctOwner, ... }
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cpaclient/ -run TestSyncAccountOwner -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cpaclient/discovery.go internal/cpaclient/discovery_owner_test.go
git commit -m "feat(discovery): CPA accounts inherit nodes.account_owner_id (fallback owner_id)"
```

---

### Task 3: createNodeHandler accepts accountOwnerId

**Files:**
- Modify: `internal/api/admin_handlers.go` (`createNodeHandler`, the body struct + `CreateNodeParams`)
- Test: `internal/api/admin_handlers_test.go`

**Interfaces:**
- Consumes: `CreateNodeParams.AccountOwnerID` (Task 1), `Queries.GetTenantByUsername`.

- [ ] **Step 1: Write the failing test** in `internal/api/admin_handlers_test.go` (guarded by `TEST_DATABASE_URL`, mirror the existing node-create test): POST `/api/admin/nodes` as superadmin with body `{"baseUrl":"http://1.2.3.4:8080/","kind":"cpa","accountOwnerId":"u_test"}`; then read the node row and assert `account_owner_id == "u_test"` and `owner_id == ""`.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/api/ -run TestCreateNodeAccountOwner -v`
Expected: FAIL / SKIP (field not wired).

- [ ] **Step 3: Implement.** In `createNodeHandler` add `AccountOwnerId string` to the request body struct; after the kind/owner logic resolve the account owner:

```go
acctOwner := strings.TrimSpace(body.AccountOwnerId)
if acctOwner == "" {
	if tn, terr := q.GetTenantByUsername(r.Context(), "test"); terr == nil {
		acctOwner = tn.ID
	}
}
```

Pass `AccountOwnerID: acctOwner` in the `CreateNodeParams`.

- [ ] **Step 4: Run tests + build**

Run: `go test ./internal/api/ -run TestCreateNodeAccountOwner -v && go build ./...`
Expected: PASS (or SKIP) + build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/api/admin_handlers.go internal/api/admin_handlers_test.go
git commit -m "feat(api): createNode accepts accountOwnerId (default test tenant)"
```

---

### Task 4: push-node API uses the column (remove inline override)

**Files:**
- Modify: `internal/api/cpa_push_handler.go` (`pushNodeHandler`)

**Interfaces:**
- Consumes: `CreateNodeParams.AccountOwnerID`, `Queries.SetNodeAccountOwner` (Task 1); discovery now applies the owner (Task 2).

- [ ] **Step 1: Set the column on create.** In the `CreateNode` call add `AccountOwnerID: tenant.ID`.

- [ ] **Step 2: Set the column on overwrite.** In the overwrite `pool.Exec(UPDATE nodes …)` add `, account_owner_id=$N` set to `tenant.ID` (extend the arg list).

- [ ] **Step 3: Remove the inline account override** — delete the line:
```go
_, _ = pool.Exec(r.Context(), `UPDATE accounts SET owner_id=$1 WHERE id LIKE 'cpa:' || $2 || ':%'`, tenant.ID, nodeID)
```
(The CPA branch now relies on `Sync` honoring `account_owner_id`.) The meridian branch keeps passing `tenant.ID` to `CreateAccount`.

- [ ] **Step 4: Build + smoke the existing push tests**

Run: `go build ./... && go test ./internal/api/ -run 'NormalizeNodeBaseURL|NodeHostPort' -v`
Expected: build clean, PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/cpa_push_handler.go
git commit -m "refactor(push): node account owner via account_owner_id column, drop inline override"
```

---

### Task 5: frontend "号库归属用户" dropdown

**Files:**
- Modify: `web/spa/src/pages/Nodes.tsx` (`AddNodeForm`)
- Modify: `web/spa/src/api.ts` (confirm `listUsers` exists — it does; no change expected)

**Interfaces:**
- Consumes: `listUsers(): Promise<UserRow[]>` (api.ts), `createNode` body now takes `accountOwnerId`.

- [ ] **Step 1: Load users on mount.** In `AddNodeForm`, add `const [users, setUsers] = useState<UserRow[]>([]);` and a `useEffect(() => { listUsers().then(setUsers).catch(() => setUsers([])); }, []);`. Add `import { listUsers } from '../api';` and `UserRow` type import if not present.

- [ ] **Step 2: Replace the ownerId text input with a select.** Replace the `<input … value={ownerId} …>` block with:

```tsx
<select
  value={ownerId}
  onChange={(e) => setOwnerId(e.target.value)}
  className={inputCls}
  title="号库归属用户"
>
  <option value="">超级管理员（全局）</option>
  {users.map((u) => (
    <option key={u.id} value={u.id}>{u.username}</option>
  ))}
</select>
```

- [ ] **Step 3: Default to test.** After users load, default-select the test tenant: extend the effect — `listUsers().then((us) => { setUsers(us); const t = us.find((u) => u.username === 'test'); if (t) setOwnerId(t.id); }).catch(() => setUsers([]));`. (Keep the existing `ownerId` state; it now holds the account owner id.)

- [ ] **Step 4: Submit as accountOwnerId.** In the form submit handler, change the create payload to send `accountOwnerId: ownerId` (instead of `ownerId`). Leave node `ownerId` unset so the node stays superadmin/global.

- [ ] **Step 5: Build the SPA**

Run: `cd web/spa && npm run build`
Expected: builds, no TS errors.

- [ ] **Step 6: Commit**

```bash
git add web/spa/src/pages/Nodes.tsx
git commit -m "feat(ui): Add-Node 号库归属用户 dropdown (default test)"
```

---

## Self-Review notes
- Spec coverage: migration (T1), discovery (T2), createNode (T3), push API (T4), frontend (T5) — all spec components covered.
- The `test` tenant id is resolved server-side (T3) and client-side default (T5) by username, never hardcoded.
- Back-compat: empty `account_owner_id` → `owner_id` fallback (T2) keeps existing nodes unchanged.
