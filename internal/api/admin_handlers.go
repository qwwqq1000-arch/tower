package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/billing"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
)

func randHex(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func createNodeHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Name, BaseUrl, ApiKey, OwnerId string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.BaseUrl == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name/baseUrl required"})
			return
		}
		n, err := q.CreateNode(r.Context(), sqlc.CreateNodeParams{
			ID: randHex("n_"), Name: body.Name, BaseUrl: body.BaseUrl, ApiKey: body.ApiKey, OwnerID: body.OwnerId,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": n.ID, "name": n.Name, "baseUrl": n.BaseUrl, "ownerId": n.OwnerID, "enabled": n.Enabled})
	}
}

func listNodesHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := q.ListNodes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, n := range rows {
			out = append(out, map[string]any{"id": n.ID, "name": n.Name, "baseUrl": n.BaseUrl, "ownerId": n.OwnerID, "enabled": n.Enabled, "version": n.Version})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func deleteNodeHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
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
		rows, err := q.ListAllDispatchKeys(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, k := range rows {
			out = append(out, map[string]any{"id": k.ID, "prefix": k.Prefix, "label": k.Label, "ownerId": k.OwnerID, "enabled": k.Enabled})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func deleteDispatchKeyHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := q.DisableDispatchKey(r.Context(), r.PathValue("id")); err != nil {
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
		// nodes
		rows, err := q.ListNodes(ctx)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
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

		// accounts count
		accTotal := 0
		if accs, err := q.ListNodeAccountsAll(ctx); err == nil {
			accTotal = len(accs)
		}

		// today consumption
		since := startOfTodayMs()
		var reqN, okN int
		var inTok, outTok int64
		var cost float64
		byModel := []map[string]any{}
		if modelRows, err := q.TodayDispatchByModel(ctx, since); err == nil {
			for _, mr := range modelRows {
				c := billing.CostUsd(mr.Model, mr.TokensIn, mr.TokensOut, 0, 0)
				reqN += int(mr.Requests)
				okN += int(mr.Ok)
				inTok += mr.TokensIn
				outTok += mr.TokensOut
				cost += c
				byModel = append(byModel, map[string]any{"model": mr.Model, "requests": mr.Requests, "tokensIn": mr.TokensIn, "tokensOut": mr.TokensOut, "costUsd": c})
			}
		}
		successRate := 0.0
		if reqN > 0 {
			successRate = float64(okN) / float64(reqN)
		}
		today := map[string]any{"requests": reqN, "ok": okN, "successRate": successRate, "tokensIn": inTok, "tokensOut": outTok, "costUsd": cost, "byModel": byModel}

		// hosting fees per tenant
		hosting := []map[string]any{}
		if ts, err := q.ListTenantsBasic(ctx); err == nil {
			for _, t := range ts {
				consumption, _ := q.SumCostForOwner(ctx, t.ID)
				rate, _ := q.GetHostingRate(ctx, t.ID)
				unsettled, accumulated := billing.ComputeHostingFee(consumption, 0, rate)
				hosting = append(hosting, map[string]any{"tenantId": t.ID, "username": t.Username, "role": t.Role, "consumptionUsd": consumption, "rate": rate, "feeUsd": accumulated, "unsettledUsd": unsettled})
			}
		}

		writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes, "accounts": map[string]any{"total": accTotal}, "today": today, "hosting": hosting})
	}
}
