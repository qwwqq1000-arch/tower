package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwwqq1000-arch/tower/internal/cpaclient"
	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
)

// defaultCpaMgmtKey is the fleet-standard CLIProxyAPI management key applied when a
// CPA push omits mgmtKey (the value normalize-mgmt-key unifies nodes to).
const defaultCpaMgmtKey = "5ee9014ab72e97308d9a24f366c874cf"

// normalizeNodeBaseURL turns a user-supplied URL or IP (optionally host:port, with or
// without an http:// prefix / trailing path) into the node base URL plus the host:port
// used for IP de-dup. Defaults: scheme http, port 8080.
func normalizeNodeBaseURL(in string) (baseURL, hostPort string, err error) {
	s := strings.TrimSpace(in)
	if s == "" {
		return "", "", fmt.Errorf("url required")
	}
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "https://")
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i] // drop any path
	}
	if s == "" {
		return "", "", fmt.Errorf("url required")
	}
	if !strings.Contains(s, ":") {
		s += ":8080"
	}
	return "http://" + s + "/", s, nil
}

// nodeHostPort extracts host:port from a stored node base_url for IP de-dup.
func nodeHostPort(baseURL string) string {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Host
}

// pushNodeHandler registers a node (a whole upstream server) and auto-discovers its
// accounts into the pool. Two kinds:
//   - "cpa" (default): params url + mgmtKey(+ apiKey); passthrough ON; accounts read
//     from the CLIProxyAPI management API.
//   - "meridian" (kind="mer"|"meridian"): params url + apiKey; accounts = the node's
//     logged-in profiles.
//
// The NODE is owned by 超级管理员 (owner_id=""); only the discovered ACCOUNTS default to
// the tenant (username, default "test"). Duplicate node IP → the existing node is
// OVERWRITTEN (response "重复已覆盖"). Returns {ok, kind, registered, [message|error]}.
func pushNodeHandler(pool *pgxpool.Pool, q *sqlc.Queries, cipher *crypto.Cipher, svc *dispatch.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Kind     string `json:"kind"`
			URL      string `json:"url"`
			BaseUrl  string `json:"baseUrl"`
			MgmtKey  string `json:"mgmtKey"`
			ApiKey   string `json:"apiKey"`
			Username string `json:"username"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "invalid json: " + err.Error()})
			return
		}
		kind := "cpa"
		if k := strings.ToLower(strings.TrimSpace(body.Kind)); k == "mer" || k == "meridian" {
			kind = "meridian"
		}
		rawURL := strings.TrimSpace(body.URL)
		if rawURL == "" {
			rawURL = strings.TrimSpace(body.BaseUrl)
		}
		if rawURL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "url required"})
			return
		}
		apiKey := strings.TrimSpace(body.ApiKey)
		mgmtKey := strings.TrimSpace(body.MgmtKey)
		if kind == "cpa" && mgmtKey == "" {
			mgmtKey = defaultCpaMgmtKey
		}
		username := strings.TrimSpace(body.Username)
		if username == "" {
			username = "test"
		}
		tenant, err := q.GetTenantByUsername(r.Context(), username)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "用户名不存在: " + username})
			return
		}
		baseURL, hostPort, verr := normalizeNodeBaseURL(rawURL)
		if verr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": verr.Error()})
			return
		}
		if uerr := validateUpstreamURL(baseURL, true); uerr != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": uerr.Error()})
			return
		}
		// Verify reachability + auth before committing (so a bad node never persists).
		var merProfiles []nodeclient.Profile
		if kind == "cpa" {
			if _, lerr := cpaclient.New(baseURL, mgmtKey).ListAccounts(r.Context()); lerr != nil {
				writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": "连接或认证失败: " + lerr.Error()})
				return
			}
		} else {
			p, perr := effectiveProfiles(r.Context(), nodeclient.New(baseURL, apiKey))
			if perr != nil {
				writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": "连接或认证失败: " + perr.Error()})
				return
			}
			merProfiles = p
		}
		// De-dup by node IP → overwrite, else create. NODE owner = "" (超级管理员).
		nodes, err := q.ListNodes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		nodeID, overwritten := "", false
		for _, n := range nodes {
			if nodeHostPort(n.BaseUrl) == hostPort {
				nodeID, overwritten = n.ID, true
				break
			}
		}
		passthrough := kind == "cpa" // 透传仅 cpa,默认开
		if overwritten {
			if _, uerr := pool.Exec(r.Context(),
				`UPDATE nodes SET base_url=$2, api_key=$3, mgmt_key=$4, owner_id='', kind=$5, passthrough=$6, account_owner_id=$7, enabled=TRUE WHERE id=$1`,
				nodeID, baseURL, cipher.EncryptStr(apiKey), cipher.EncryptStr(mgmtKey), kind, passthrough, tenant.ID); uerr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": uerr.Error()})
				return
			}
		} else {
			n, cerr := q.CreateNode(r.Context(), sqlc.CreateNodeParams{
				ID: randHex("n_"), Name: nextNodeName(r.Context(), q), BaseUrl: baseURL,
				ApiKey: cipher.EncryptStr(apiKey), MgmtKey: cipher.EncryptStr(mgmtKey),
				OwnerID: "", Kind: kind, Passthrough: passthrough, AccountOwnerID: tenant.ID,
			})
			if cerr != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": cerr.Error()})
				return
			}
			nodeID = n.ID
		}
		// Discover accounts → owned by the tenant (node stays 超级管理员).
		registered := 0
		if kind == "cpa" {
			if node, gerr := q.GetNode(r.Context(), nodeID); gerr == nil {
				rot := &cpaclient.RotateConfig{Store: svc.Store, BaseCapacity: svc.Base.MaxConcurrent, DefaultTTLMs: 3600000, Cipher: cipher}
				registered, _ = cpaclient.Sync(r.Context(), q, node, rot)
			}
		} else {
			existing, _ := q.ListNodeAccountsByNode(r.Context(), nodeID)
			have := make(map[string]bool, len(existing))
			for _, a := range existing {
				have[a.ProfileID] = true
			}
			now := time.Now().UnixMilli()
			for _, p := range merProfiles {
				if !p.LoggedIn || have[p.ID] {
					continue
				}
				accID := randHex("acc_")
				if _, cerr := q.CreateAccount(r.Context(), buildImportAccountParams(accID, tenant.ID, p.Email, now+30*24*3600*1000, now)); cerr != nil {
					continue
				}
				if _, aerr := q.AssignAccount(r.Context(), sqlc.AssignAccountParams{
					NodeID: nodeID, AccountID: accID, ProfileID: p.ID, Egress: "", Weight: 100, Role: "baseline", SlotID: "",
				}); aerr != nil {
					continue
				}
				registered++
			}
		}
		recordAudit(r, q, "node.push", "node:"+nodeID, nil, map[string]any{"ip": hostPort, "kind": kind, "owner": tenant.Username, "overwritten": overwritten})
		resp := map[string]any{"ok": true, "kind": kind, "registered": registered}
		if overwritten {
			resp["message"] = "重复已覆盖"
		}
		writeJSON(w, http.StatusOK, resp)
	}
}
