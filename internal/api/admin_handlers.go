package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/billing"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
)

func randHex(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// nextNodeName returns the next sequential numeric node name (>=1001).
func nextNodeName(ctx context.Context, q *sqlc.Queries) string {
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

func createNodeHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Name, BaseUrl, ApiKey, OwnerId, Kind, MgmtKey string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.BaseUrl == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "baseUrl required"})
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
		// Owner default: a non-superadmin that does not specify an owner owns the
		// node it creates (so it remains visible under owner scoping). superadmin
		// may leave it empty (global) or assign explicitly.
		if owner, all := scope(r); !all && body.OwnerId == "" {
			body.OwnerId = owner
		}
		n, err := q.CreateNode(r.Context(), sqlc.CreateNodeParams{
			ID: randHex("n_"), Name: body.Name, BaseUrl: body.BaseUrl, ApiKey: body.ApiKey,
			MgmtKey: body.MgmtKey, OwnerID: body.OwnerId, Kind: kind,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": n.ID, "name": n.Name, "baseUrl": n.BaseUrl, "ownerId": n.OwnerID, "enabled": n.Enabled, "kind": n.Kind})
	}
}

func listNodesHandler(q *sqlc.Queries) http.HandlerFunc {
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
			loggedIn    bool
			email       string
			liveVersion string
		}
		results := make([]healthResult, len(rows))
		var wg sync.WaitGroup
		for i, n := range rows {
			wg.Add(1)
			go func(idx int, baseURL, apiKey string) {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				c := nodeclient.New(baseURL, apiKey)
				h, err := c.Health(ctx)
				if err != nil {
					return
				}
				results[idx] = healthResult{
					loggedIn:    h.Auth.LoggedIn,
					email:       h.Auth.Email,
					liveVersion: h.Version,
				}
			}(i, n.BaseUrl, n.ApiKey)
		}
		wg.Wait()

		out := make([]map[string]any, 0, len(rows))
		for i, n := range rows {
			var createdAtMs int64
			if n.CreatedAt.Valid {
				createdAtMs = n.CreatedAt.Time.UnixMilli()
			}
			out = append(out, map[string]any{
				"id":          n.ID,
				"name":        n.Name,
				"baseUrl":     n.BaseUrl,
				"ownerId":     n.OwnerID,
				"enabled":     n.Enabled,
				"kind":        n.Kind,
				"version":     n.Version,
				"createdAt":   createdAtMs,
				"loggedIn":    results[i].loggedIn,
				"email":       results[i].email,
				"liveVersion": results[i].liveVersion,
			})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func deleteNodeHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !ownsNodeID(r, q, id) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		if err := q.DeleteNode(r.Context(), id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
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

		// accounts count (owner-scoped for non-superadmin)
		accTotal := 0
		if accs, err := q.ListNodeAccountsAll(ctx); err == nil {
			for _, a := range accs {
				if all || a.AcctOwnerID == owner {
					accTotal++
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
		today := map[string]any{"requests": reqN, "ok": okN, "successRate": successRate, "tokensIn": inTok, "tokensOut": outTok, "costUsd": cost, "byModel": byModel}

		// total accumulated cost (owner-scoped for non-superadmin)
		var totalCost float64
		if all {
			totalCost, _ = q.SumAllCost(ctx)
		} else {
			totalCost, _ = q.SumCostForOwner(ctx, owner)
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
				unsettled, accumulated := billing.ComputeHostingFee(consumption, 0, rate)
				channelConsumption, _ := q.SumFallbackSpendByOwner(ctx, t.ID)
				channelFee := channelConsumption * t.ChannelRate
				hosting = append(hosting, map[string]any{"tenantId": t.ID, "username": t.Username, "role": t.Role, "consumptionUsd": consumption, "rate": rate, "feeUsd": accumulated, "unsettledUsd": unsettled, "channelRate": t.ChannelRate, "channelConsumptionUsd": channelConsumption, "channelFeeUsd": channelFee})
			}
		}

		// cached average utilization (0..1 fractions)
		var a5h, a7d float64
		if svc != nil && svc.Store != nil {
			a5h, a7d = svc.Store.QuotaAvg()
		}

		writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "accounts": map[string]any{"total": accTotal}, "today": today, "hosting": hosting, "totalCostUsd": totalCost, "quota5hAvg": a5h, "quota7dAvg": a7d})
	}
}
