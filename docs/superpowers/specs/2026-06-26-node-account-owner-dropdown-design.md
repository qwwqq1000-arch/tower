# Node 号库归属用户 Dropdown (#137) — Design

**Goal:** Replace the Add-Node form's free-text "归属用户 ID(选填)" with a "号库归属用户" dropdown that loads the user list and defaults to `test`. The node is owned by 超级管理员; only its discovered accounts (号库) are owned by the selected user.

**Architecture:** Decouple *node owner* from *account owner*. Add a nullable `account_owner_id` column to `nodes`; account discovery (CPA `Sync`, meridian profile import) assigns each account to `account_owner_id` when set, else falls back to the node's `owner_id` (today's behavior). Both the manual Add-Node form and the existing push-node API set it, so the inline `UPDATE accounts …` override in the push handler is removed in favor of the column (also fixes accounts discovered *later* on a pushed node, which currently revert to the node owner).

**Tech stack:** Go (sqlc/pgx, goose migration) + React/Vite SPA.

## Global Constraints
- New behavior must be **owner-scoping safe**: a non-superadmin who can already create a node continues to own the node they create (`scope(r)` enforcement unchanged); only the *account* owner is the selected user.
- Default account owner = the `test` tenant when the dropdown is left at its default.
- `listUsers` is `requireSuperadmin`. The dropdown must degrade gracefully for a non-superadmin caller (show only `test` + 超级管理员, no crash).
- No regression to existing nodes: `account_owner_id` defaults to `''` → discovery falls back to `owner_id`, identical to today.

---

## Component 1: DB migration — `nodes.account_owner_id`

**File:** `migrations/<next>_node_account_owner.sql`

Add `account_owner_id TEXT NOT NULL DEFAULT ''` to `nodes`. Empty string = "inherit node owner" (back-compat).

## Component 2: sqlc — carry the column

**Files:** `internal/db/queries/nodes.sql` (+ regenerated `internal/db/sqlc/nodes.sql.go`)

- `CreateNode` gains an `account_owner_id` parameter and returns it.
- Add `SetNodeAccountOwner(id, account_owner_id)` for the push/overwrite path.
- The `Node` struct gains `AccountOwnerID string`.

If `sqlc` is unavailable in the environment, hand-edit `nodes.sql.go` to match the generated shape (the project already contains hand-consistent generated files).

## Component 3: discovery uses account_owner_id

**File:** `internal/cpaclient/discovery.go` (`Sync`, ~line 190)

```go
acctOwner := node.AccountOwnerID
if acctOwner == "" {
    acctOwner = node.OwnerID
}
// UpsertCpaAccount(... OwnerID: acctOwner ...)
```

Meridian import path (`internal/api/account_handlers.go` `buildImportAccountParams`) likewise uses `account_owner_id` when the caller is the push/manual flow (passed through, not read from the node there — see Component 5).

## Component 4: createNodeHandler accepts the account owner

**File:** `internal/api/admin_handlers.go` (`createNodeHandler`)

- Request body gains `accountOwnerId string`.
- Resolve: if empty → the `test` tenant id (looked up once via `GetTenantByUsername(ctx,"test")`; if `test` is absent, fall back to `''`).
- Node `owner_id`: unchanged (existing `scope(r)` logic — superadmin may pass `ownerId` or leave empty=global; non-superadmin forced to caller).
- Persist `account_owner_id` on `CreateNode`.

## Component 5: push-node API uses the column

**File:** `internal/api/cpa_push_handler.go`

- Set `account_owner_id = tenant.ID` on create/overwrite (via `CreateNodeParams.AccountOwnerID` / `SetNodeAccountOwner`).
- **Remove** the inline `UPDATE accounts SET owner_id … WHERE id LIKE 'cpa:'||$2||':%'` (discovery now applies the column). The meridian branch passes `tenant.ID` to `CreateAccount` as today.

## Component 6: frontend dropdown

**File:** `web/spa/src/pages/Nodes.tsx` (`AddNodeForm`)

- On mount, `listUsers()` → `UserRow[]`; on failure, set an empty list (degrade).
- Replace the `ownerId` text input with a `<select>` labeled "号库归属用户":
  - options: `超级管理员(全局)` (value `""`) + each user's `username` (value = user `id`)
  - default selected: the user whose `username === 'test'` (else `""`)
- Submit sends `accountOwnerId` (selected value). The node `ownerId` is no longer collected from this field (node stays superadmin/global).

## Error Handling
- `listUsers` 403/failure → dropdown shows only 超级管理员 + (if known) test; submit still works.
- `test` tenant missing → account owner falls back to `''` (superadmin); node creation never fails on this.

## Testing
- `discovery_test`: a node with `account_owner_id` set → discovered CPA account has that owner; empty → inherits `owner_id`.
- `admin_handlers_test` (TEST_DATABASE_URL): create node with `accountOwnerId` → node `owner_id` per scope, `account_owner_id` persisted.
- Frontend: manual check — dropdown renders users, defaults to test.
