package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
)

func todayDayStr() string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.UTC
	}
	return time.Now().In(loc).Format("2006-01-02")
}

type fallbackBody struct {
	Name, BaseUrl, ApiKey, ModelAllowlist, OwnerId, GroupId string
	Priority, MaxConcurrent                                 int32
	Weight                                                  int32
	CooldownMs                                              int64
	PriceThreshold                                          float64
	BalanceToken                                            string
	BalanceUserId                                           string
	BalanceAlertUsd                                         float64
	SpendCapDailyMinUsd                                     float64
	SpendCapDailyMaxUsd                                     float64
	SpendCapTotalMinUsd                                     float64
	SpendCapTotalMaxUsd                                     float64
	SpendCapAction                                          string
}

func listFallbackHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, all := scope(r)
		rows, err := q.ListAllFallbackChannels(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		today := todayDayStr()
		out := make([]map[string]any, 0, len(rows))
		for _, c := range rows {
			if !all && c.OwnerID != owner { // owner scoping: non-superadmin sees only own
				continue
			}
			todaySpend, _ := q.GetFallbackSpendToday(r.Context(), sqlc.GetFallbackSpendTodayParams{ChannelID: c.ID, Day: today})
			totalSpend, _ := q.GetFallbackSpendTotal(r.Context(), c.ID)
			out = append(out, map[string]any{
				"id":                  c.ID,
				"name":                c.Name,
				"baseUrl":             c.BaseUrl,
				"hasKey":              c.ApiKey != "",
				"priority":            c.Priority,
				"weight":              c.Weight,
				"maxConcurrent":       c.MaxConcurrent,
				"cooldownMs":          c.CooldownMs,
				"priceThreshold":      c.PriceThreshold,
				"modelAllowlist":      c.ModelAllowlist,
				"enabled":             c.Enabled,
				"ownerId":             c.OwnerID,
				"todayCostUsd":        todaySpend.Cost,
				"todayRequests":       todaySpend.Requests,
				"totalCostUsd":        totalSpend.Cost,
				"totalRequests":       totalSpend.Requests,
				"balanceUsd":          c.BalanceUsd,
				"balanceAlertUsd":     c.BalanceAlertUsd,
				"hasBalanceToken":     c.BalanceToken != "",
				"balanceUserId":       c.BalanceUserID,
				"balanceCheckedAt":    c.BalanceCheckedAt,
				"balanceError":        c.BalanceError,
				"spendCapDailyMinUsd": c.SpendCapDailyMinUsd,
				"spendCapDailyMaxUsd": c.SpendCapDailyMaxUsd,
				"spendCapTotalMinUsd": c.SpendCapTotalMinUsd,
				"spendCapTotalMaxUsd": c.SpendCapTotalMaxUsd,
				"spendCapAction":      c.SpendCapAction,
			})
		}
		writeJSON(w, 200, out)
	}
}

func createFallbackHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b fallbackBody
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.BaseUrl == "" {
			writeJSON(w, 400, map[string]string{"error": "name/baseUrl required"})
			return
		}
		// SSRF guard: a fallback channel is a proxy target; block loopback/metadata/
		// private so a relay can't be pointed at internal infra (security-audit).
		if verr := validateUpstreamURL(b.BaseUrl, false); verr != nil {
			writeJSON(w, 400, map[string]string{"error": verr.Error()})
			return
		}
		// owner default: a non-superadmin owns the channels it creates (cannot
		// create channels for another tenant, bypassing per-tenant limits).
		if owner, all := scope(r); !all {
			b.OwnerId = owner
		}
		// Enforce per-tenant fallback channel limit when the channel is
		// owner-scoped (b.OwnerId set). Default (or 0) = max 1. This mirrors
		// meCreateFallbackHandler so a superadmin cannot bypass the limit on
		// behalf of a tenant by using the admin endpoint.
		if b.OwnerId != "" {
			limit := int32(1)
			if t, err := q.GetTenantByID(r.Context(), b.OwnerId); err == nil && t.FallbackLimit > 0 {
				limit = t.FallbackLimit
			}
			if existing, err := q.ListFallbackChannelsByOwner(r.Context(), b.OwnerId); err == nil && int32(len(existing)) >= limit {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "fallback channel limit reached"})
				return
			}
		}
		// Encrypt channel secrets at rest (vault-crypto-3): the upstream api_key and
		// the balance-query token are stored as ciphertext; read paths decrypt
		// transparently. A nil cipher (plaintext-mode) is a no-op.
		spendCapAction := b.SpendCapAction
		if spendCapAction == "" {
			spendCapAction = "skip"
		}
		c, err := q.CreateFallbackChannel(r.Context(), sqlc.CreateFallbackChannelParams{
			ID:                  randHex("fc_"),
			OwnerID:             b.OwnerId,
			GroupID:             b.GroupId,
			Name:                b.Name,
			BaseUrl:             b.BaseUrl,
			ApiKey:              cipher.EncryptStr(b.ApiKey),
			Priority:            b.Priority,
			Weight:              b.Weight,
			MaxConcurrent:       b.MaxConcurrent,
			CooldownMs:          b.CooldownMs,
			PriceThreshold:      b.PriceThreshold,
			ModelAllowlist:      b.ModelAllowlist,
			BalanceToken:        cipher.EncryptStr(b.BalanceToken),
			BalanceUserID:       b.BalanceUserId,
			BalanceAlertUsd:     b.BalanceAlertUsd,
			SpendCapDailyMinUsd: b.SpendCapDailyMinUsd,
			SpendCapDailyMaxUsd: b.SpendCapDailyMaxUsd,
			SpendCapTotalMinUsd: b.SpendCapTotalMinUsd,
			SpendCapTotalMaxUsd: b.SpendCapTotalMaxUsd,
			SpendCapAction:      spendCapAction,
		})
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		recordAudit(r, q, "fallback.create", "channel:"+c.ID, nil, map[string]any{"name": b.Name, "baseUrl": b.BaseUrl, "ownerId": b.OwnerId})
		writeJSON(w, 200, map[string]string{"id": c.ID})
	}
}

func updateFallbackHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ownsFallbackID(r, q, r.PathValue("id")) {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var b fallbackBody
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		// Secrets at rest (vault-crypto-3): a non-blank incoming value is freshly
		// encrypted; a blank value (留空表示不更改) preserves the stored ciphertext
		// as-is (cur.* is already encrypted, so re-store verbatim — do NOT re-encrypt).
		apiKeyEnc := cipher.EncryptStr(b.ApiKey)
		balTokenEnc := cipher.EncryptStr(b.BalanceToken)
		if cur, err := q.GetFallbackChannel(r.Context(), r.PathValue("id")); err == nil {
			if b.ApiKey == "" {
				apiKeyEnc = cur.ApiKey
			}
			if b.BalanceToken == "" {
				balTokenEnc = cur.BalanceToken
			}
		}
		updateSpendCapAction := b.SpendCapAction
		if updateSpendCapAction == "" {
			updateSpendCapAction = "skip"
		}
		if b.BaseUrl != "" { // SSRF guard on update (security-audit)
			if verr := validateUpstreamURL(b.BaseUrl, false); verr != nil {
				writeJSON(w, 400, map[string]string{"error": verr.Error()})
				return
			}
		}
		if err := q.UpdateFallbackChannel(r.Context(), sqlc.UpdateFallbackChannelParams{
			ID:                  r.PathValue("id"),
			Name:                b.Name,
			BaseUrl:             b.BaseUrl,
			ApiKey:              apiKeyEnc,
			Priority:            b.Priority,
			Weight:              b.Weight,
			MaxConcurrent:       b.MaxConcurrent,
			CooldownMs:          b.CooldownMs,
			PriceThreshold:      b.PriceThreshold,
			ModelAllowlist:      b.ModelAllowlist,
			BalanceToken:        balTokenEnc,
			BalanceUserID:       b.BalanceUserId,
			BalanceAlertUsd:     b.BalanceAlertUsd,
			SpendCapDailyMinUsd: b.SpendCapDailyMinUsd,
			SpendCapDailyMaxUsd: b.SpendCapDailyMaxUsd,
			SpendCapTotalMinUsd: b.SpendCapTotalMinUsd,
			SpendCapTotalMaxUsd: b.SpendCapTotalMaxUsd,
			SpendCapAction:      updateSpendCapAction,
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		recordAudit(r, q, "fallback.update", "channel:"+r.PathValue("id"), nil, map[string]any{"name": b.Name, "baseUrl": b.BaseUrl})
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func enableFallbackHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ownsFallbackID(r, q, r.PathValue("id")) {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var b struct{ Enabled bool }
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		if err := q.SetFallbackChannelEnabled(r.Context(), sqlc.SetFallbackChannelEnabledParams{
			ID:      r.PathValue("id"),
			Enabled: b.Enabled,
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		recordAudit(r, q, "fallback.enable", "channel:"+r.PathValue("id"), nil, map[string]any{"enabled": b.Enabled})
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func deleteFallbackHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ownsFallbackID(r, q, r.PathValue("id")) {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		if err := q.DeleteFallbackChannel(r.Context(), r.PathValue("id")); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		recordAudit(r, q, "fallback.delete", "channel:"+r.PathValue("id"), nil, nil)
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func fetchFallbackBalanceHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		ch, err := q.GetFallbackChannel(r.Context(), id)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "channel not found"})
			return
		}
		if owner, all := scope(r); !all && ch.OwnerID != owner {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}

		now := time.Now().UnixMilli()
		// Decrypt the balance token transparently for the upstream balance query.
		usd, fetchErr := dispatch.FetchChannelBalance(r.Context(), ch.BaseUrl, cipher.DecryptOrPlaintext(ch.BalanceToken), ch.BalanceUserID)

		errStr := ""
		if fetchErr != nil {
			errStr = fetchErr.Error()
		}

		_ = q.SetFallbackBalance(r.Context(), sqlc.SetFallbackBalanceParams{
			ID:               id,
			BalanceUsd:       usd,
			BalanceCheckedAt: now,
			BalanceError:     errStr,
		})

		writeJSON(w, 200, map[string]any{
			"balanceUsd": usd,
			"error":      errStr,
		})
	}
}
