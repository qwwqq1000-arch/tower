package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
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
	Priority, Weight, MaxConcurrent                         int32
	CooldownMs                                              int64
	PriceThreshold                                          float64
}

func listFallbackHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := q.ListAllFallbackChannels(r.Context())
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
				"id":             c.ID,
				"name":           c.Name,
				"baseUrl":        c.BaseUrl,
				"hasKey":         c.ApiKey != "",
				"priority":       c.Priority,
				"weight":         c.Weight,
				"maxConcurrent":  c.MaxConcurrent,
				"cooldownMs":     c.CooldownMs,
				"priceThreshold": c.PriceThreshold,
				"modelAllowlist": c.ModelAllowlist,
				"enabled":        c.Enabled,
				"ownerId":        c.OwnerID,
				"todayCostUsd":   todaySpend.Cost,
				"todayRequests":  todaySpend.Requests,
				"totalCostUsd":   totalSpend.Cost,
				"totalRequests":  totalSpend.Requests,
			})
		}
		writeJSON(w, 200, out)
	}
}

func createFallbackHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b fallbackBody
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.BaseUrl == "" {
			writeJSON(w, 400, map[string]string{"error": "name/baseUrl required"})
			return
		}
		c, err := q.CreateFallbackChannel(r.Context(), sqlc.CreateFallbackChannelParams{
			ID:             randHex("fc_"),
			OwnerID:        b.OwnerId,
			GroupID:        b.GroupId,
			Name:           b.Name,
			BaseUrl:        b.BaseUrl,
			ApiKey:         b.ApiKey,
			Priority:       b.Priority,
			Weight:         b.Weight,
			MaxConcurrent:  b.MaxConcurrent,
			CooldownMs:     b.CooldownMs,
			PriceThreshold: b.PriceThreshold,
			ModelAllowlist: b.ModelAllowlist,
		})
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"id": c.ID})
	}
}

func updateFallbackHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b fallbackBody
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		if err := q.UpdateFallbackChannel(r.Context(), sqlc.UpdateFallbackChannelParams{
			ID:             r.PathValue("id"),
			Name:           b.Name,
			BaseUrl:        b.BaseUrl,
			ApiKey:         b.ApiKey,
			Priority:       b.Priority,
			Weight:         b.Weight,
			MaxConcurrent:  b.MaxConcurrent,
			CooldownMs:     b.CooldownMs,
			PriceThreshold: b.PriceThreshold,
			ModelAllowlist: b.ModelAllowlist,
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func enableFallbackHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func deleteFallbackHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := q.DeleteFallbackChannel(r.Context(), r.PathValue("id")); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}
