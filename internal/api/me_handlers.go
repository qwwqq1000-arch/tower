package api

import (
	"encoding/json"
	"net/http"

	"github.com/qwwqq1000-arch/tower/internal/billing"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// ownerFrom returns the caller's user id (session sub). The boolean is false
// when no session is present (should not happen behind requireSession).
func ownerFrom(r *http.Request) (string, bool) {
	p, ok := sessionFrom(r)
	if !ok || p.Sub == "" {
		return "", false
	}
	return p.Sub, true
}

// meAccountsHandler returns ONLY the accounts owned by the caller. Read-only.
func meAccountsHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		ctx := r.Context()
		rows, err := q.ListNodeAccountsAll(ctx)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		todayCostMap := map[string]float64{}
		if todayRows, err := q.CostByTargetSince(ctx, startOfTodayMs()); err == nil {
			for _, t := range todayRows {
				todayCostMap[t.Target] = t.Cost
			}
		}
		totalCostMap := map[string]float64{}
		if totalRows, err := q.CostByTargetTotal(ctx); err == nil {
			for _, t := range totalRows {
				totalCostMap[t.Target] = t.Cost
			}
		}
		out := make([]map[string]any, 0)
		for _, a := range rows {
			if a.AcctOwnerID != owner { // strict owner scoping
				continue
			}
			key := a.NodeID + ":" + a.ProfileID
			out = append(out, map[string]any{
				"accountId":        a.AccountID,
				"nodeName":         a.NodeName,
				"email":            a.Email,
				"expiresAt":        a.ExpiresAt,
				"subscriptionType": a.SubscriptionType,
				"weight":           a.Weight,
				"role":             a.Role,
				"enabled":          a.Enabled,
				"todayCostUsd":     todayCostMap[key],
				"totalCostUsd":     totalCostMap[key],
			})
		}
		writeJSON(w, 200, out)
	}
}

// mePauseAccountHandler pauses/resumes ALL node_accounts of an account the
// caller owns. Tenant cannot modify weight/role — enabled only.
func mePauseAccountHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		accountID := r.PathValue("accountId")
		acc, err := q.GetAccount(r.Context(), accountID)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "account not found"})
			return
		}
		if acc.OwnerID != owner { // ownership check — never touch others' accounts
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var body struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		if err := q.SetNodeAccountEnabledByAccount(r.Context(), sqlc.SetNodeAccountEnabledByAccountParams{
			AccountID: accountID,
			Enabled:   body.Enabled,
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "enabled": body.Enabled})
	}
}

// meDashboardHandler returns an owner-scoped overview.
func meDashboardHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		ctx := r.Context()
		// own accounts: count distinct accounts + active (enabled) ones.
		accTotal, accActive := 0, 0
		if rows, err := q.ListNodeAccountsAll(ctx); err == nil {
			seen := map[string]bool{}
			seenActive := map[string]bool{}
			for _, a := range rows {
				if a.AcctOwnerID != owner {
					continue
				}
				if !seen[a.AccountID] {
					seen[a.AccountID] = true
					accTotal++
				}
				if a.Enabled && !seenActive[a.AccountID] {
					seenActive[a.AccountID] = true
					accActive++
				}
			}
		}
		// today usage scoped to owner.
		var todayReq int64
		var todayCost float64
		if td, err := q.TodayDispatchForOwner(ctx, sqlc.TodayDispatchForOwnerParams{
			OwnerID: owner,
			Ts:      startOfTodayMs(),
		}); err == nil {
			todayReq = td.Requests
			todayCost = td.Cost
		}
		consumption, _ := q.SumCostForOwner(ctx, owner)
		rate, _ := q.GetHostingRate(ctx, owner)
		unsettled, accumulated := billing.ComputeHostingFee(consumption, 0, rate)
		writeJSON(w, 200, map[string]any{
			"accounts": map[string]any{"total": accTotal, "active": accActive},
			"today":    map[string]any{"requests": todayReq, "costUsd": todayCost},
			"consumptionUsd": consumption,
			"hostingRate":    rate,
			"unsettledUsd":   unsettled,
			"accumulatedUsd": accumulated,
		})
	}
}

// meLogsHandler returns dispatch logs scoped to the caller.
func meLogsHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		rows, err := q.ListLogsByOwner(r.Context(), sqlc.ListLogsByOwnerParams{
			OwnerID: owner,
			Limit:   limitParam(r, 100),
		})
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, l := range rows {
			out = append(out, map[string]any{
				"ts": l.Ts, "model": l.Model, "target": l.Target,
				"status": l.Status, "httpStatus": l.HttpStatus,
				"latencyMs": l.LatencyMs, "tokensIn": l.TokensIn,
				"tokensOut": l.TokensOut, "fallbackReason": l.FallbackReason,
				"ttfbMs": l.TtfbMs, "stream": l.Stream, "costUsd": l.CostUsd,
			})
		}
		writeJSON(w, 200, out)
	}
}

// meEventsHandler returns dispatch events scoped to the caller.
func meEventsHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		rows, err := q.ListEventsByOwner(r.Context(), sqlc.ListEventsByOwnerParams{
			OwnerID: owner,
			Limit:   limitParam(r, 100),
		})
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, e := range rows {
			out = append(out, map[string]any{
				"ts": e.Ts, "type": e.Type, "target": e.Target,
				"detail": json.RawMessage(e.Detail),
			})
		}
		writeJSON(w, 200, out)
	}
}

// meLedgerHandler returns the caller's own billing ledger.
func meLedgerHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		rows, err := q.ListLedgerByTenant(r.Context(), owner)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, e := range rows {
			out = append(out, map[string]any{"ts": e.Ts, "type": e.Type, "amount": e.AmountUsd, "ref": e.Ref, "note": e.Note})
		}
		writeJSON(w, 200, out)
	}
}

// meListFallbackHandler lists fallback channels owned by the caller.
func meListFallbackHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		rows, err := q.ListFallbackChannelsByOwner(r.Context(), owner)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		today := todayDayStr()
		out := make([]map[string]any, 0, len(rows))
		for _, c := range rows {
			todaySpend, _ := q.GetFallbackSpendToday(r.Context(), sqlc.GetFallbackSpendTodayParams{ChannelID: c.ID, Day: today})
			totalSpend, _ := q.GetFallbackSpendTotal(r.Context(), c.ID)
			out = append(out, map[string]any{
				"id":              c.ID,
				"name":            c.Name,
				"baseUrl":         c.BaseUrl,
				"hasKey":          c.ApiKey != "",
				"priority":        c.Priority,
				"weight":          c.Weight,
				"maxConcurrent":   c.MaxConcurrent,
				"cooldownMs":      c.CooldownMs,
				"priceThreshold":  c.PriceThreshold,
				"modelAllowlist":  c.ModelAllowlist,
				"enabled":         c.Enabled,
				"todayCostUsd":    todaySpend.Cost,
				"todayRequests":   todaySpend.Requests,
				"totalCostUsd":    totalSpend.Cost,
				"totalRequests":   totalSpend.Requests,
				"balanceUsd":      c.BalanceUsd,
				"balanceAlertUsd": c.BalanceAlertUsd,
			})
		}
		writeJSON(w, 200, out)
	}
}

// meCreateFallbackHandler creates a fallback channel owned by the caller.
// owner_id is forced to the session sub — the request body cannot override it.
func meCreateFallbackHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		var b fallbackBody
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.BaseUrl == "" {
			writeJSON(w, 400, map[string]string{"error": "name/baseUrl required"})
			return
		}
		c, err := q.CreateFallbackChannel(r.Context(), sqlc.CreateFallbackChannelParams{
			ID:              randHex("fc_"),
			OwnerID:         owner, // forced — never trust body ownerId
			GroupID:         b.GroupId,
			Name:            b.Name,
			BaseUrl:         b.BaseUrl,
			ApiKey:          b.ApiKey,
			Priority:        b.Priority,
			Weight:          b.Weight,
			MaxConcurrent:   b.MaxConcurrent,
			CooldownMs:      b.CooldownMs,
			PriceThreshold:  b.PriceThreshold,
			ModelAllowlist:  b.ModelAllowlist,
			BalanceToken:    b.BalanceToken,
			BalanceUserID:   b.BalanceUserId,
			BalanceAlertUsd: b.BalanceAlertUsd,
		})
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"id": c.ID})
	}
}

// meOwnFallback loads a channel and verifies the caller owns it. Returns false
// (after writing the response) when missing or not owned.
func meOwnFallback(q *sqlc.Queries, w http.ResponseWriter, r *http.Request) (sqlc.FallbackChannel, string, bool) {
	owner, ok := ownerFrom(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return sqlc.FallbackChannel{}, "", false
	}
	ch, err := q.GetFallbackChannel(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "channel not found"})
		return sqlc.FallbackChannel{}, "", false
	}
	if ch.OwnerID != owner {
		writeJSON(w, 403, map[string]string{"error": "forbidden"})
		return sqlc.FallbackChannel{}, "", false
	}
	return ch, owner, true
}

func meUpdateFallbackHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ch, _, ok := meOwnFallback(q, w, r)
		if !ok {
			return
		}
		var b fallbackBody
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		if err := q.UpdateFallbackChannel(r.Context(), sqlc.UpdateFallbackChannelParams{
			ID:              ch.ID,
			Name:            b.Name,
			BaseUrl:         b.BaseUrl,
			ApiKey:          b.ApiKey,
			Priority:        b.Priority,
			Weight:          b.Weight,
			MaxConcurrent:   b.MaxConcurrent,
			CooldownMs:      b.CooldownMs,
			PriceThreshold:  b.PriceThreshold,
			ModelAllowlist:  b.ModelAllowlist,
			BalanceToken:    b.BalanceToken,
			BalanceUserID:   b.BalanceUserId,
			BalanceAlertUsd: b.BalanceAlertUsd,
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func meEnableFallbackHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ch, _, ok := meOwnFallback(q, w, r)
		if !ok {
			return
		}
		var b struct{ Enabled bool }
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		if err := q.SetFallbackChannelEnabled(r.Context(), sqlc.SetFallbackChannelEnabledParams{
			ID:      ch.ID,
			Enabled: b.Enabled,
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func meDeleteFallbackHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ch, _, ok := meOwnFallback(q, w, r)
		if !ok {
			return
		}
		if err := q.DeleteFallbackChannel(r.Context(), ch.ID); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}
