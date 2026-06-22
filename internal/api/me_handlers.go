package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/billing"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
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
		// separate channel hosting billing at the tenant's channel_rate.
		channelConsumption, _ := q.SumFallbackSpendByOwner(ctx, owner)
		var channelRate float64
		if t, err := q.GetTenantByID(ctx, owner); err == nil {
			channelRate = t.ChannelRate
		}
		channelHostingFee := channelConsumption * channelRate
		writeJSON(w, 200, map[string]any{
			"accounts": map[string]any{"total": accTotal, "active": accActive},
			"today":    map[string]any{"requests": todayReq, "costUsd": todayCost},
			"consumptionUsd":        consumption,
			"hostingRate":           rate,
			"unsettledUsd":          unsettled,
			"accumulatedUsd":        accumulated,
			"channelConsumptionUsd": channelConsumption,
			"channelRate":           channelRate,
			"channelHostingFeeUsd":  channelHostingFee,
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
		// Enforce per-tenant fallback channel limit. Default (or 0) = max 1.
		limit := int32(1)
		if t, err := q.GetTenantByID(r.Context(), owner); err == nil && t.FallbackLimit > 0 {
			limit = t.FallbackLimit
		}
		if existing, err := q.ListFallbackChannelsByOwner(r.Context(), owner); err == nil && int32(len(existing)) >= limit {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "fallback channel limit reached"})
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
		// Preserve existing secrets when blank (留空表示不更改).
		if b.ApiKey == "" {
			b.ApiKey = ch.ApiKey
		}
		if b.BalanceToken == "" {
			b.BalanceToken = ch.BalanceToken
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

// ---- Tenant settings: slots (owner-scoped) ----

// meListSlotsHandler lists ONLY the slots owned by the caller.
func meListSlotsHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		rows, err := q.ListSlotsByOwner(r.Context(), owner)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, s := range rows {
			out = append(out, map[string]any{
				"id": s.ID, "name": s.Name, "startMin": s.StartMin,
				"endMin": s.EndMin, "enabled": s.Enabled,
			})
		}
		writeJSON(w, 200, out)
	}
}

// meCreateSlotHandler creates a slot owned by the caller. owner_id is forced to
// the session sub — the body cannot override it.
func meCreateSlotHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		var b struct {
			Name     string `json:"name"`
			StartMin int32  `json:"startMin"`
			EndMin   int32  `json:"endMin"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		s, err := q.CreateSlotOwned(r.Context(), sqlc.CreateSlotOwnedParams{
			ID:       randHex("slot_"),
			Name:     b.Name,
			StartMin: b.StartMin,
			EndMin:   b.EndMin,
			OwnerID:  owner, // forced — never trust body
		})
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"id": s.ID})
	}
}

// meOwnSlot loads a slot and verifies the caller owns it. Writes the response
// and returns false when missing or not owned.
func meOwnSlot(q *sqlc.Queries, w http.ResponseWriter, r *http.Request) (sqlc.Slot, bool) {
	owner, ok := ownerFrom(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return sqlc.Slot{}, false
	}
	s, err := q.GetSlot(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]string{"error": "slot not found"})
		return sqlc.Slot{}, false
	}
	if s.OwnerID != owner {
		writeJSON(w, 403, map[string]string{"error": "forbidden"})
		return sqlc.Slot{}, false
	}
	return s, true
}

func meDeleteSlotHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := meOwnSlot(q, w, r)
		if !ok {
			return
		}
		if err := q.DeleteSlot(r.Context(), s.ID); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func meSetSlotEnabledHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, ok := meOwnSlot(q, w, r)
		if !ok {
			return
		}
		var b struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		if err := q.SetSlotEnabled(r.Context(), sqlc.SetSlotEnabledParams{ID: s.ID, Enabled: b.Enabled}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

// ---- Tenant settings: dispatch keys (owner-scoped) ----

// meListDispatchKeysHandler lists ONLY dispatch keys owned by the caller.
func meListDispatchKeysHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		rows, err := q.ListDispatchKeysByOwner(r.Context(), owner)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, k := range rows {
			out = append(out, map[string]any{
				"id": k.ID, "prefix": k.Prefix, "label": k.Label, "enabled": k.Enabled,
			})
		}
		writeJSON(w, 200, out)
	}
}

// meCreateDispatchKeyHandler mints a dispatch key with owner_id = caller. The
// owner_id binds dispatch isolation to the tenant's accounts. Plaintext is
// returned exactly once.
func meCreateDispatchKeyHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		var body struct {
			Label string `json:"label"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		plaintext, prefix, hash, salt, err := auth.NewDispatchKey()
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		id := randHex("k_")
		if _, err := q.CreateDispatchKey(r.Context(), sqlc.CreateDispatchKeyParams{
			ID: id, KeyHash: hash, Salt: salt, Prefix: prefix,
			OwnerID: owner, // forced — tenant owns the key
			Label:   body.Label,
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"id": id, "key": plaintext})
	}
}

// meDeleteDispatchKeyHandler disables a dispatch key after verifying ownership.
func meDeleteDispatchKeyHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		id := r.PathValue("id")
		rows, err := q.ListDispatchKeysByOwner(r.Context(), owner)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		owned := false
		for _, k := range rows {
			if k.ID == id {
				owned = true
				break
			}
		}
		if !owned { // missing OR owned by someone else → forbidden, never leak existence
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		if err := q.DisableDispatchKey(r.Context(), id); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

// ---- Tenant dispatch status (own concurrency) ----

// meDispatchStatusHandler returns an owner-scoped dispatch snapshot: accounts
// limited to the caller's, with traffic/events computed from owner-scoped logs.
func meDispatchStatusHandler(q *sqlc.Queries, svc *dispatch.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		ctx := r.Context()
		now := nowUnix() * 1000
		// owner-owned account keys (node:profile) + labels.
		ownKeys := map[string]string{}
		if accs, err := q.ListNodeAccountsAll(ctx); err == nil {
			for _, a := range accs {
				if a.AcctOwnerID != owner {
					continue
				}
				label := a.NodeName
				if a.Email != "" {
					label = a.Email
				}
				ownKeys[a.NodeID+":"+a.ProfileID] = label
			}
		}
		todayCostMap := map[string]float64{}
		if rows, err := q.CostByTargetSince(ctx, startOfTodayMs()); err == nil {
			for _, t := range rows {
				todayCostMap[t.Target] = t.Cost
			}
		}
		totalCostMap := map[string]float64{}
		if rows, err := q.CostByTargetTotal(ctx); err == nil {
			for _, t := range rows {
				totalCostMap[t.Target] = t.Cost
			}
		}
		accounts := []map[string]any{}
		if svc != nil && svc.Store != nil {
			for _, s := range svc.Store.Snapshot(now) {
				if strings.HasPrefix(s.Key, "fb:") {
					continue
				}
				label, mine := ownKeys[s.Key]
				if !mine { // strict: only the caller's accounts
					continue
				}
				accounts = append(accounts, map[string]any{
					"key": s.Key, "label": label, "status": s.Status,
					"inflight": s.Inflight, "available": s.Available,
					"todayCostUsd": todayCostMap[s.Key], "totalCostUsd": totalCostMap[s.Key],
				})
			}
		}
		// traffic scoped to owner via owner-scoped logs.
		var okc, errc int
		var in, out int64
		if logs, err := q.ListLogsByOwner(ctx, sqlc.ListLogsByOwnerParams{OwnerID: owner, Limit: 200}); err == nil {
			for _, l := range logs {
				switch l.Status {
				case "ok":
					okc++
				case "error":
					errc++
				}
				in += l.TokensIn
				out += l.TokensOut
			}
		}
		var total, rpm int64
		if t, err := q.CountDispatchLogsByOwner(ctx, owner); err == nil {
			total = t
		}
		if rr, err := q.CountDispatchLogsByOwnerSince(ctx, sqlc.CountDispatchLogsByOwnerSinceParams{OwnerID: owner, Ts: now - 60000}); err == nil {
			rpm = rr
		}
		traffic := map[string]any{
			"total": total, "rpm": rpm, "ok": okc, "error": errc,
			"tokensIn": in, "tokensOut": out,
		}
		events := []map[string]any{}
		if evs, err := q.ListEventsByOwner(ctx, sqlc.ListEventsByOwnerParams{OwnerID: owner, Limit: 20}); err == nil {
			for _, e := range evs {
				events = append(events, map[string]any{
					"ts": e.Ts, "type": e.Type, "target": e.Target,
					"detail": json.RawMessage(e.Detail),
				})
			}
		}
		// fallbackChannels: the caller's OWN channels (mirrors admin buildDispatchStatus shape).
		snapMap := map[string]struct{ Inflight, Available int }{}
		if svc != nil && svc.Store != nil {
			for _, s := range svc.Store.Snapshot(now) {
				snapMap[s.Key] = struct{ Inflight, Available int }{s.Inflight, s.Available}
			}
		}
		fallbackChannels := []map[string]any{}
		if chs, err := q.ListFallbackChannelsByOwner(ctx, owner); err == nil {
			today := todayDayStr()
			for _, ch := range chs {
				var todayReq int64
				var todayCost float64
				if spend, serr := q.GetFallbackSpendToday(ctx, sqlc.GetFallbackSpendTodayParams{ChannelID: ch.ID, Day: today}); serr == nil {
					todayReq = spend.Requests
					todayCost = spend.Cost
				}
				fbKey := "fb:" + ch.ID
				inflight, available := 0, int(ch.MaxConcurrent)
				if available <= 0 {
					available = 1000
				}
				if snap, ok := snapMap[fbKey]; ok {
					inflight = snap.Inflight
					available = snap.Available
				}
				fallbackChannels = append(fallbackChannels, map[string]any{
					"id": ch.ID, "name": ch.Name, "enabled": ch.Enabled,
					"priority": ch.Priority, "weight": ch.Weight,
					"todayRequests": todayReq, "todayCostUsd": todayCost,
					"inflight": inflight, "available": available,
					"balanceUsd": ch.BalanceUsd,
				})
			}
		}
		writeJSON(w, 200, map[string]any{
			"accounts": accounts, "traffic": traffic, "events": events,
			"fallbackChannels": fallbackChannels, "asOf": now,
		})
	}
}

// ---- Tenant ban analysis (own accounts) ----

// meBanAnalysisHandler returns ban stats scoped to the caller's accounts,
// joining ban_episodes → node_accounts → accounts by owner_id.
func meBanAnalysisHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, ok := ownerFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		ctx := r.Context()
		total, _ := q.BanTotalByOwner(ctx, owner)
		wd, _ := q.BanCountsByWeekdayForOwner(ctx, owner)
		hr, _ := q.BanCountsByHourForOwner(ctx, owner)
		byWeekday := make([]map[string]any, 0, len(wd))
		for _, x := range wd {
			byWeekday = append(byWeekday, map[string]any{"bucket": x.Bucket, "count": x.N})
		}
		byHour := make([]map[string]any, 0, len(hr))
		for _, x := range hr {
			byHour = append(byHour, map[string]any{"bucket": x.Bucket, "count": x.N})
		}
		writeJSON(w, 200, map[string]any{"total": total, "byWeekday": byWeekday, "byHour": byHour})
	}
}
