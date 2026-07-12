package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/billing"
	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
)

// nodeHealthCache stores background-probed health results so listNodes returns
// instantly instead of blocking on N concurrent HTTP probes per request.
var nodeHealthCache struct {
	sync.RWMutex
	data map[string]cachedHealth // keyed by node ID
	at   time.Time
}

type cachedHealth struct {
	LoggedIn              bool
	Email                 string
	LiveVersion           string
	SubscriptionType      string
	SubscriptionCreatedAt string
	AccountCreatedAt      string
}

func getHealthCache(nodeID string) (cachedHealth, bool) {
	nodeHealthCache.RLock()
	defer nodeHealthCache.RUnlock()
	h, ok := nodeHealthCache.data[nodeID]
	return h, ok
}

// setHealthCache updates one node's cached health entry so the manual node
// refresh reflects a new login state immediately (status badge + email),
// instead of waiting for the next 30s background prober tick.
func setHealthCache(nodeID string, h cachedHealth) {
	nodeHealthCache.Lock()
	defer nodeHealthCache.Unlock()
	if nodeHealthCache.data == nil {
		nodeHealthCache.data = make(map[string]cachedHealth)
	}
	nodeHealthCache.data[nodeID] = h
}

// StartHealthProber runs a background loop that probes all nodes every 30s.
func StartHealthProber(q *sqlc.Queries, cipher *crypto.Cipher) {
	probe := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		rows, err := q.ListNodes(ctx)
		if err != nil {
			return
		}
		m := make(map[string]cachedHealth, len(rows))
		var mu sync.Mutex
		var wg sync.WaitGroup
		for _, n := range rows {
			if strings.EqualFold(n.Kind, "cpa") {
				accs, _ := q.ListNodeAccountsByNode(ctx, n.ID)
				email := ""
				switch {
				case len(accs) == 1:
					email = "1 个账户"
				case len(accs) > 1:
					email = fmt.Sprintf("%d 个账户", len(accs))
				}
				mu.Lock()
				m[n.ID] = cachedHealth{LoggedIn: len(accs) > 0, Email: email}
				mu.Unlock()
				continue
			}
			nodeAccounts, _ := q.ListNodeAccountsByNode(ctx, n.ID)
			wg.Add(1)
			go func(id, baseURL, apiKey string, accs []sqlc.NodeAccount) {
				defer wg.Done()
				pctx, pcancel := context.WithTimeout(ctx, 5*time.Second)
				defer pcancel()
				c := nodeclient.New(baseURL, apiKey)
				h, herr := c.Health(pctx)
				if herr != nil {
					return
				}
				mu.Lock()
				m[id] = cachedHealth{
					LoggedIn: h.Auth.LoggedIn, Email: h.Auth.Email, LiveVersion: h.Version,
					SubscriptionType: h.Auth.SubscriptionType, SubscriptionCreatedAt: h.Auth.SubscriptionCreatedAt,
					AccountCreatedAt: h.Auth.AccountCreatedAt,
				}
				mu.Unlock()
				// Fetch quota from meridian and persist so the accounts list shows it.
				qa, qerr := c.QuotaAll(pctx)
				if qerr != nil || len(qa.Profiles) == 0 {
					return
				}
				profToAcct := make(map[string]string, len(accs))
				for _, a := range accs {
					profToAcct[a.ProfileID] = a.AccountID
				}
				now := time.Now().UnixMilli()
				for _, p := range qa.Profiles {
					aid, ok := profToAcct[p.ID]
					if !ok {
						continue
					}
					var fhUtil, sdUtil, ssUtil float64
					var fhReset, sdReset, ssReset string
					for _, w := range p.Windows {
						resetStr := ""
						if w.ResetsAt > 0 {
							resetStr = time.UnixMilli(w.ResetsAt).UTC().Format(time.RFC3339)
						}
						switch w.Type {
						case "five_hour":
							fhUtil, fhReset = w.Utilization, resetStr
						case "seven_day":
							sdUtil, sdReset = w.Utilization, resetStr
						case "seven_day_sonnet":
							ssUtil, ssReset = w.Utilization, resetStr
						}
					}
					_ = q.UpsertCpaQuota(pctx, sqlc.UpsertCpaQuotaParams{
						AccountID: aid, FiveHourUtil: fhUtil, FiveHourResetsAt: fhReset,
						SevenDayUtil: sdUtil, SevenDayResetsAt: sdReset,
						SevenDaySonnetUtil: ssUtil, SevenDaySonnetResetsAt: ssReset,
						UpdatedAt: now,
					})
				}
			}(n.ID, n.BaseUrl, cipher.DecryptOrPlaintext(n.ApiKey), nodeAccounts)
		}
		wg.Wait()
		nodeHealthCache.Lock()
		nodeHealthCache.data = m
		nodeHealthCache.at = time.Now()
		nodeHealthCache.Unlock()
	}
	probe()
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			probe()
		}
	}()
}

func randHex(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// nextNodeName returns the next sequential numeric node name (>=1001).
func nextNodeName(ctx context.Context, q *sqlc.Queries) string {
	// Atomic allocation via a Postgres sequence — concurrent batch provisioning
	// (Promise.all on the client) otherwise all read the same max before any node
	// row committed and collided on the same name.
	if v, err := q.NextNodeNameSeq(ctx); err == nil {
		return strconv.Itoa(int(v))
	}
	// Fallback (sequence missing): best-effort max+1 over existing node names.
	max := 1000
	if rows, err := q.ListNodes(ctx); err == nil {
		for _, n := range rows {
			if v, err := strconv.Atoi(strings.TrimSpace(n.Name)); err == nil && v > max {
				max = v
			}
		}
	}
	return strconv.Itoa(max + 1)
}

func createNodeHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name, BaseUrl, ApiKey, OwnerId, Kind, MgmtKey, AccountOwnerId string
			Passthrough bool
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.BaseUrl == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "baseUrl required"})
			return
		}
		// SSRF guard: http(s) only, no loopback/metadata. allowPrivate=true since an
		// admin may legitimately register an internal-network node (security-audit).
		if verr := validateUpstreamURL(body.BaseUrl, true); verr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": verr.Error()})
			return
		}
		if body.Name == "" {
			body.Name = nextNodeName(r.Context(), q)
		}
		// Node kind: "meridian" (default) or "cpa" (CLIProxyAPI).
		kind := strings.ToLower(strings.TrimSpace(body.Kind))
		if kind != "cpa" {
			kind = "meridian"
		}
		// Owner enforcement: a non-superadmin must own the node they create.
		// Non-superadmin: force owner to caller's owner regardless of body.
		// Superadmin: use body value (may be empty for global) or leave empty.
		if owner, all := scope(r); !all {
			body.OwnerId = owner
		}
		// Resolve account owner: use body value if present, else default to the
		// "yanghao" tenant (新号导入默认归属).
		acctOwner := strings.TrimSpace(body.AccountOwnerId)
		if acctOwner == "" {
			if tn, terr := q.GetTenantByUsername(r.Context(), "yanghao"); terr == nil {
				acctOwner = tn.ID
			}
		}
		// Encrypt secrets at rest (vault-crypto-3). The meridian api_key and the
		// CPA mgmt_key are stored as ciphertext; read paths decrypt transparently
		// via cipher.DecryptOrPlaintext. A nil cipher (plaintext-mode) is a no-op.
		n, err := q.CreateNode(r.Context(), sqlc.CreateNodeParams{
			ID: randHex("n_"), Name: body.Name, BaseUrl: body.BaseUrl, ApiKey: cipher.EncryptStr(body.ApiKey),
			MgmtKey: cipher.EncryptStr(body.MgmtKey), OwnerID: body.OwnerId, Kind: kind,
			Passthrough: body.Passthrough, AccountOwnerID: acctOwner,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		recordAudit(r, q, "node.create", "node:"+n.ID, nil, map[string]any{"name": n.Name, "baseUrl": n.BaseUrl, "ownerId": n.OwnerID, "kind": n.Kind})
		writeJSON(w, http.StatusOK, map[string]any{"id": n.ID, "name": n.Name, "baseUrl": n.BaseUrl, "ownerId": n.OwnerID, "enabled": n.Enabled, "kind": n.Kind, "passthrough": n.Passthrough})
	}
}

func updateNodeHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		n, err := q.GetNode(r.Context(), id)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		if owner, all := scope(r); !all && n.OwnerID != owner {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var body struct {
			BaseUrl string `json:"baseUrl"`
			ApiKey  string `json:"apiKey"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		baseUrl := strings.TrimSpace(body.BaseUrl)
		if baseUrl == "" {
			baseUrl = n.BaseUrl
		} else if verr := validateUpstreamURL(baseUrl, true); verr != nil {
			writeJSON(w, 400, map[string]string{"error": verr.Error()})
			return
		}
		apiKey := n.ApiKey
		if strings.TrimSpace(body.ApiKey) != "" {
			apiKey = cipher.EncryptStr(strings.TrimSpace(body.ApiKey))
		}
		old := map[string]any{"baseUrl": n.BaseUrl}
		if err := q.UpdateNode(r.Context(), sqlc.UpdateNodeParams{ID: id, BaseUrl: baseUrl, ApiKey: apiKey}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		recordAudit(r, q, "node.update", "node:"+id, old, map[string]any{"baseUrl": baseUrl})
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

func listNodesHandler(q *sqlc.Queries, cipher *crypto.Cipher, svc *dispatch.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, all := scope(r)
		allRows, err := q.ListNodes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// owner scoping: non-superadmin sees only nodes they own.
		rows := allRows[:0:0]
		for _, n := range allRows {
			if all || n.OwnerID == owner {
				rows = append(rows, n)
			}
		}

		type healthResult struct {
			loggedIn              bool
			email                 string
			liveVersion           string
			subscriptionType      string
			subscriptionCreatedAt string
			accountCreatedAt      string
		}
		results := make([]healthResult, len(rows))
		for i, n := range rows {
			if h, ok := getHealthCache(n.ID); ok {
				results[i] = healthResult{
					loggedIn: h.LoggedIn, email: h.Email, liveVersion: h.LiveVersion,
					subscriptionType: h.SubscriptionType, subscriptionCreatedAt: h.SubscriptionCreatedAt,
					accountCreatedAt: h.AccountCreatedAt,
				}
			}
		}

		// Per-node ban overlay (节点封号): a node is "banned" when it has accounts in
		// the dispatch store and ALL of them are banned/permanent — a node with any
		// still-working account keeps serving, so it stays 正常. Store keys are
		// "<nodeID>:<profile>", so the node id is the prefix before the first colon.
		banned := make([]bool, len(rows))
		if svc != nil && svc.Store != nil {
			type cnt struct{ total, ban int }
			counts := make(map[string]*cnt, len(rows))
			idxOf := make(map[string]int, len(rows))
			for i, n := range rows {
				counts[n.ID] = &cnt{}
				idxOf[n.ID] = i
			}
			for _, s := range svc.Store.Snapshot(time.Now().UnixMilli()) {
				ci := strings.IndexByte(s.Key, ':')
				if ci <= 0 {
					continue
				}
				if c, ok := counts[s.Key[:ci]]; ok {
					c.total++
					if s.Status == "banned" || s.Status == "permanent" {
						c.ban++
					}
				}
			}
			for id, c := range counts {
				if c.total > 0 && c.ban == c.total {
					banned[idxOf[id]] = true
				}
			}
		}

		out := make([]map[string]any, 0, len(rows))
		for i, n := range rows {
			var createdAtMs int64
			if n.CreatedAt.Valid {
				createdAtMs = n.CreatedAt.Time.UnixMilli()
			}
			out = append(out, map[string]any{
				"id":                    n.ID,
				"name":                  n.Name,
				"baseUrl":              n.BaseUrl,
				"ownerId":             n.OwnerID,
				"enabled":             n.Enabled,
				"kind":                n.Kind,
				"passthrough":         n.Passthrough,
				"version":             n.Version,
				"createdAt":           createdAtMs,
				"loggedIn":            results[i].loggedIn,
				"email":               results[i].email,
				"liveVersion":         results[i].liveVersion,
				"banned":              banned[i],
				"subscriptionType":      results[i].subscriptionType,
				"subscriptionCreatedAt": results[i].subscriptionCreatedAt,
				"accountCreatedAt":      results[i].accountCreatedAt,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// normalizeMgmtKeyHandler sets every CPA node's mgmt_key to the given value (stored
// encrypted), updating ONLY the nodes whose current (decrypted) key differs. One-shot
// maintenance to unify the CPA management key across the fleet. Returns which changed.
func normalizeMgmtKeyHandler(pool *pgxpool.Pool, q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	// fingerprint masks a key for display so a typo is recognizable without
	// leaking the full secret: first 8 + last 4 chars + length.
	fingerprint := func(s string) string {
		if s == "" {
			return "(empty)"
		}
		if len(s) <= 12 {
			return fmt.Sprintf("len=%d", len(s))
		}
		return fmt.Sprintf("%s…%s len=%d", s[:8], s[len(s)-4:], len(s))
	}
	return func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Key    string `json:"key"`
			DryRun bool   `json:"dryRun"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil || strings.TrimSpace(b.Key) == "" {
			writeJSON(w, 400, map[string]string{"error": "key required"})
			return
		}
		key := strings.TrimSpace(b.Key)
		nodes, err := q.ListNodes(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		changed := []string{}
		mismatched := []map[string]string{}
		alreadyOk := 0
		for _, n := range nodes {
			if !strings.EqualFold(n.Kind, "cpa") {
				continue
			}
			cur := cipher.DecryptOrPlaintext(n.MgmtKey)
			if cur == key {
				alreadyOk++
				continue
			}
			mismatched = append(mismatched, map[string]string{
				"id": n.ID, "name": n.Name, "current": fingerprint(cur),
			})
			if b.DryRun {
				continue
			}
			if _, err := pool.Exec(r.Context(), "UPDATE nodes SET mgmt_key=$2 WHERE id=$1", n.ID, cipher.EncryptStr(key)); err != nil {
				continue
			}
			changed = append(changed, n.Name)
		}
		if !b.DryRun {
			recordAudit(r, q, "node.normalize_mgmt_key", "global", nil, map[string]any{"changed": changed})
		}
		writeJSON(w, 200, map[string]any{
			"dryRun": b.DryRun, "target": fingerprint(key),
			"alreadyOk": alreadyOk, "mismatched": mismatched,
			"changed": len(changed), "changedNodes": changed,
		})
	}
}

func deleteNodeHandler(pool *pgxpool.Pool, q *sqlc.Queries, svc *dispatch.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !ownsNodeID(r, q, id) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		tx, err := pool.Begin(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer tx.Rollback(r.Context())

		cascade := []string{
			"DELETE FROM account_state WHERE node_id = $1",
			"DELETE FROM ban_episodes WHERE node_id = $1",
			"DELETE FROM account_spend_threshold WHERE key LIKE $1 || ':%'",
			"DELETE FROM account_limit_state WHERE key LIKE $1 || ':%'",
			"DELETE FROM cpa_account_quota WHERE account_id IN (SELECT account_id FROM node_accounts WHERE node_id = $1)",
			"DELETE FROM accounts WHERE id IN (SELECT account_id FROM node_accounts WHERE node_id = $1) AND id NOT IN (SELECT account_id FROM node_accounts WHERE node_id != $1)",
			"DELETE FROM node_accounts WHERE node_id = $1",
			"DELETE FROM nodes WHERE id = $1",
		}
		for _, stmt := range cascade {
			if _, err := tx.Exec(r.Context(), stmt, id); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		if err := tx.Commit(r.Context()); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// Drop the node's in-memory accounts so they don't linger as ghost rows.
		if svc != nil && svc.Store != nil {
			svc.Store.RemoveNode(id)
		}
		recordAudit(r, q, "node.delete", "node:"+id, nil, nil)
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}
}

func createDispatchKeyHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Label, OwnerId string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		// owner default: a non-superadmin owns the keys it creates (cannot mint
		// keys for another tenant).
		if owner, all := scope(r); !all {
			body.OwnerId = owner
		}
		plaintext, prefix, hash, salt, err := auth.NewDispatchKey()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		id := randHex("k_")
		if _, err := q.CreateDispatchKey(r.Context(), sqlc.CreateDispatchKeyParams{
			ID: id, KeyHash: hash, Salt: salt, Prefix: prefix, OwnerID: body.OwnerId, Label: body.Label,
		}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		recordAudit(r, q, "dispatchKey.create", "key:"+id, nil, map[string]any{"label": body.Label, "ownerId": body.OwnerId, "prefix": prefix})
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "key": plaintext})
	}
}

func listDispatchKeysHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, all := scope(r)
		rows, err := q.ListAllDispatchKeys(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, k := range rows {
			if !all && k.OwnerID != owner { // owner scoping: non-superadmin sees only own
				continue
			}
			out = append(out, map[string]any{"id": k.ID, "prefix": k.Prefix, "label": k.Label, "ownerId": k.OwnerID, "enabled": k.Enabled})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func deleteDispatchKeyHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		// owner scoping: non-superadmin may only disable keys they own.
		if owner, all := scope(r); !all {
			rows, err := q.ListAllDispatchKeys(r.Context())
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			owned := false
			for _, k := range rows {
				if k.ID == id {
					owned = k.OwnerID == owner
					break
				}
			}
			if !owned {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return
			}
		}
		if err := q.DisableDispatchKey(r.Context(), id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		recordAudit(r, q, "dispatchKey.delete", "key:"+id, nil, nil)
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}
}

func startOfTodayMs() int64 {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	y, m, d := now.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc).UnixMilli()
}

func dashboardHandler(q *sqlc.Queries, svc *dispatch.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		owner, all := scope(r)
		// nodes (owner-scoped: non-superadmin sees only own)
		allNodes, err := q.ListNodes(ctx)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		rows := allNodes[:0:0]
		for _, n := range allNodes {
			if all || n.OwnerID == owner {
				rows = append(rows, n)
			}
		}
		var store interface{ NodeStatus(string) string }
		if svc != nil && svc.Store != nil {
			store = svc.Store
		}
		byStatus := map[string]int{}
		enabled := 0
		list := make([]map[string]any, 0, len(rows))
		for _, n := range rows {
			if n.Enabled {
				enabled++
			}
			st := ""
			if store != nil {
				st = store.NodeStatus(n.ID)
				if st != "" {
					byStatus[st]++
				}
			}
			list = append(list, map[string]any{"id": n.ID, "name": n.Name, "baseUrl": n.BaseUrl, "enabled": n.Enabled, "status": st, "version": n.Version, "region": n.Region})
		}
		nodes := map[string]any{"total": len(rows), "enabled": enabled, "byStatus": byStatus, "list": list}

		// accounts count + target set (owner-scoped for non-superadmin)
		accTotal := 0
		accEnabled := 0
		accTargets := map[string]bool{}
		if accs, err := q.ListNodeAccountsAll(ctx); err == nil {
			for _, a := range accs {
				if all || a.AcctOwnerID == owner {
					accTotal++
					accTargets[a.NodeID+":"+a.ProfileID] = true
					if a.Enabled {
						accEnabled++
					}
				}
			}
		}

		// elastic status — count actually usable active accounts (not limited,
		// not permanent, not reserve). Limited accounts are effectively dead and
		// should not inflate the elastic "当前" display.
		elasticCurrent := 0
		elasticMax := 0
		if svc != nil {
			cfg := svc.ResolveConfigForOwner(ctx, owner)
			if cfg.ElasticEnabled {
				if cfg.ElasticMaxActive > 0 {
					elasticMax = cfg.ElasticMaxActive
				} else {
					elasticMax = cfg.ElasticBaselineCount + cfg.ElasticMaxReserve
				}
				reserveKeys := map[string]bool{}
				owners := map[string]bool{}
				if all {
					for _, a := range accTargets {
						_ = a // accTargets is a set of keys
					}
					// collect unique owners from node_accounts
					if accs, aerr := q.ListNodeAccountsAll(ctx); aerr == nil {
						for _, a := range accs {
							if a.AcctOwnerID != "" {
								owners[a.AcctOwnerID] = true
							}
						}
					}
				} else {
					owners[owner] = true
				}
				for o := range owners {
					ocfg := svc.ResolveConfigForOwner(ctx, o)
					for k := range svc.ReserveKeys(ctx, o, ocfg) {
						reserveKeys[k] = true
					}
				}
				nowMs := time.Now().UnixMilli()
				if svc.Store != nil {
					for _, s := range svc.Store.Snapshot(nowMs) {
						if strings.HasPrefix(s.Key, "fb:") {
							continue
						}
						if !accTargets[s.Key] {
							continue
						}
						if s.Status == "permanent" || s.Limited {
							continue
						}
						if reserveKeys[s.Key] {
							continue
						}
						elasticCurrent++
					}
				}
				if elasticCurrent > elasticMax {
					elasticCurrent = elasticMax
				}
			}
		}

		// today consumption
		since := startOfTodayMs()
		var reqN, okN int
		var inTok, outTok int64
		var cost float64
		byModel := []map[string]any{}
		if all {
			if modelRows, err := q.TodayDispatchByModel(ctx, since); err == nil {
				for _, mr := range modelRows {
					reqN += int(mr.Requests)
					okN += int(mr.Ok)
					inTok += mr.TokensIn
					outTok += mr.TokensOut
					cost += mr.Cost
					byModel = append(byModel, map[string]any{"model": mr.Model, "requests": mr.Requests, "tokensIn": mr.TokensIn, "tokensOut": mr.TokensOut, "costUsd": mr.Cost})
				}
			}
		} else if td, err := q.TodayDispatchForOwner(ctx, sqlc.TodayDispatchForOwnerParams{OwnerID: owner, Ts: since}); err == nil {
			// owner-scoped totals (per-model breakdown is superadmin-only)
			reqN = int(td.Requests)
			okN = int(td.Requests)
			cost = td.Cost
		}
		successRate := 0.0
		if reqN > 0 {
			successRate = float64(okN) / float64(reqN)
		}
		// Override cost with 号库-only cost (sum per-target, filtered by current node_accounts)
		var accTodayCost float64
		if todayTargets, err := q.CostByTargetSince(ctx, since); err == nil {
			for _, r := range todayTargets {
				if accTargets[r.Target] {
					accTodayCost += r.Cost
				}
			}
		}
		today := map[string]any{"requests": reqN, "ok": okN, "successRate": successRate, "tokensIn": inTok, "tokensOut": outTok, "costUsd": accTodayCost, "byModel": byModel}

		// total accumulated cost — only for accounts currently in 号库
		var totalCost float64
		if totalRows, err := q.CostByTargetTotal(ctx); err == nil {
			for _, r := range totalRows {
				if accTargets[r.Target] {
					totalCost += r.Cost
				}
			}
		}
		// channel (fallback) cost: today + total
		todayStr := time.UnixMilli(since).UTC().Format("2006-01-02")
		var channelTodayCost, channelTotalCost float64
		if all {
			channelTodayCost, _ = q.SumFallbackSpendToday(ctx, todayStr)
			channelTotalCost, _ = q.SumAllFallbackSpend(ctx)
		} else {
			channelTodayCost, _ = q.SumFallbackSpendTodayByOwner(ctx, sqlc.SumFallbackSpendTodayByOwnerParams{OwnerID: owner, Day: todayStr})
			channelTotalCost, _ = q.SumFallbackSpendByOwner(ctx, owner)
		}

		// hosting fees per tenant
		hosting := []map[string]any{}
		if ts, err := q.ListTenantsBasic(ctx); err == nil {
			for _, t := range ts {
				if !all && t.ID != owner { // owner scoping: non-superadmin sees only own billing row
					continue
				}
				consumption, _ := q.SumCostForOwner(ctx, t.ID)
				rate, _ := q.GetHostingRate(ctx, t.ID)
				settled, _ := q.SumSettledForOwner(ctx, t.ID) // settled FEE (billing-fee-1)
				channelConsumption, _ := q.SumFallbackSpendByOwner(ctx, t.ID)
				nodeFee := consumption * rate
				channelFee := channelConsumption * t.ChannelRate
				// Unsettled = combined outstanding fee (node + channel) minus settled fee.
				unsettled, _ := billing.ComputeHostingFee(nodeFee+channelFee, settled)
				hosting = append(hosting, map[string]any{"tenantId": t.ID, "username": t.Username, "role": t.Role, "consumptionUsd": billing.RoundUSD(consumption), "rate": rate, "feeUsd": billing.RoundUSD(nodeFee), "unsettledUsd": billing.RoundUSD(unsettled), "channelRate": t.ChannelRate, "channelConsumptionUsd": billing.RoundUSD(channelConsumption), "channelFeeUsd": billing.RoundUSD(channelFee)})
			}
		}

		// cached average utilization (0..1 fractions) — display-only (nodeclient-telemetry-3).
		// This is a GLOBAL average across all accounts, so only superadmin may see it.
		// It does not drive dispatch, scaling, or rate-limit decisions (those use
		// per-account quotas and account-level limits instead).
		var a5h, a7d float64
		if all {
			a5h, a7d = cpaQuotaAvg(r.Context(), q, svc)
		}

		writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "accounts": map[string]any{"total": accTotal, "enabled": accEnabled}, "elastic": map[string]any{"current": elasticCurrent, "max": elasticMax}, "today": today, "hosting": hosting, "totalCostUsd": totalCost, "channelTodayCostUsd": channelTodayCost, "channelTotalCostUsd": channelTotalCost, "quota5hAvg": a5h, "quota7dAvg": a7d})
	}
}
