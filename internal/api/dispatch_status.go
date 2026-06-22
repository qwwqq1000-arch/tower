package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
)

func buildDispatchStatus(ctx context.Context, q *sqlc.Queries, svc *dispatch.Service, now int64, owner string, all bool) map[string]any {
	// account labels + owner map (owner scoping: non-superadmin sees only own)
	labels := map[string]string{}
	keyOwner := map[string]string{}
	if accs, err := q.ListNodeAccountsAll(ctx); err == nil {
		for _, a := range accs {
			label := a.NodeName
			if a.Email != "" {
				label = a.Email
			}
			labels[a.NodeID+":"+a.ProfileID] = label
			keyOwner[a.NodeID+":"+a.ProfileID] = a.AcctOwnerID
		}
	}
	// An account is "known" only while its node_accounts row still exists; a deleted
	// node can leave stale in-memory store entries that must not surface as ghost rows.
	known := make(map[string]bool, len(keyOwner))
	for k := range keyOwner {
		known[k] = true
	}
	visible := func(key string) bool { return known[key] && (all || keyOwner[key] == owner) }
	// Build today/total cost maps for accounts
	todayCostMap := map[string]float64{}
	if todayRows, err := q.CostByTargetSince(ctx, startOfTodayMs()); err == nil {
		for _, r := range todayRows {
			todayCostMap[r.Target] = r.Cost
		}
	}
	totalCostMap := map[string]float64{}
	if totalRows, err := q.CostByTargetTotal(ctx); err == nil {
		for _, r := range totalRows {
			totalCostMap[r.Target] = r.Cost
		}
	}
	accounts := []map[string]any{}
	if svc != nil && svc.Store != nil {
		for _, s := range svc.Store.Snapshot(now) {
			if strings.HasPrefix(s.Key, "fb:") {
				continue
			}
			if _, known := keyOwner[s.Key]; !known { // node/account deleted → drop stale ghost row
				continue
			}
			if !visible(s.Key) { // owner scoping
				continue
			}
			if s.Status == "permanent" { // permanently banned → out of rotation, hide from the live pool
				continue
			}
			accounts = append(accounts, map[string]any{
				"key":          s.Key,
				"label":        labels[s.Key],
				"status":       s.Status,
				"inflight":     s.Inflight,
				"available":    s.Available,
				"recoverAt":    s.RecoverAt,
				"todayCostUsd": todayCostMap[s.Key],
				"totalCostUsd": totalCostMap[s.Key],
			})
		}
	}
	// tokensIn/tokensOut come from the recent-200 window; ok/error/total/rpm are REAL totals.
	var in, out int64
	var total, rpm, okc, errc int64
	if all {
		if logs, err := q.ListRecentDispatchLogs(ctx, 200); err == nil {
			for _, l := range logs {
				in += l.TokensIn
				out += l.TokensOut
			}
		}
		if t, terr := q.CountDispatchLogs(ctx); terr == nil {
			total = t
		}
		if r, rerr := q.CountDispatchLogsSince(ctx, now-60000); rerr == nil {
			rpm = r
		}
		if c, cerr := q.CountDispatchLogsByStatus(ctx, "ok"); cerr == nil {
			okc = c
		}
		if c, cerr := q.CountDispatchLogsByStatus(ctx, "error"); cerr == nil {
			errc = c
		}
	} else {
		// owner-scoped traffic (per-status / token breakdown is superadmin-only)
		total, _ = q.CountDispatchLogsByOwner(ctx, owner)
		rpm, _ = q.CountDispatchLogsByOwnerSince(ctx, sqlc.CountDispatchLogsByOwnerSinceParams{OwnerID: owner, Ts: now - 60000})
	}
	traffic := map[string]any{"total": total, "rpm": rpm, "ok": okc, "error": errc, "tokensIn": in, "tokensOut": out}
	events := []map[string]any{}
	if all {
		if evs, err := q.ListRecentEvents(ctx, 20); err == nil {
			for _, e := range evs {
				events = append(events, map[string]any{"ts": e.Ts, "type": e.Type, "target": e.Target, "detail": json.RawMessage(e.Detail)})
			}
		}
	} else if evs, err := q.ListEventsByOwner(ctx, sqlc.ListEventsByOwnerParams{OwnerID: owner, Limit: 20}); err == nil {
		for _, e := range evs {
			events = append(events, map[string]any{"ts": e.Ts, "type": e.Type, "target": e.Target, "detail": json.RawMessage(e.Detail)})
		}
	}
	nodesTotal, nodesEnabled := 0, 0
	if ns, err := q.ListNodes(ctx); err == nil {
		for _, n := range ns {
			if !all && n.OwnerID != owner {
				continue
			}
			nodesTotal++
			if n.Enabled {
				nodesEnabled++
			}
		}
	}
	// Build a snapshot map keyed by account key for O(1) lookup.
	snapMap := map[string]struct{ Inflight, Available int }{}
	if svc != nil && svc.Store != nil {
		for _, s := range svc.Store.Snapshot(now) {
			snapMap[s.Key] = struct{ Inflight, Available int }{s.Inflight, s.Available}
		}
	}

	fallbackChannels := []map[string]any{}
	var chs []sqlc.FallbackChannel
	if all {
		chs, _ = q.ListAllFallbackChannels(ctx)
	} else {
		chs, _ = q.ListFallbackChannelsByOwner(ctx, owner)
	}
	{
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
	// global average across all accounts → superadmin only (no cross-owner leak).
	var a5h, a7d float64
	if all && svc != nil && svc.Store != nil {
		a5h, a7d = svc.Store.QuotaAvg()
	}
	return map[string]any{
		"accounts": accounts, "traffic": traffic, "events": events,
		"nodes":            map[string]any{"total": nodesTotal, "enabled": nodesEnabled},
		"fallbackChannels": fallbackChannels,
		"asOf":             now,
		"quota5hAvg":       a5h,
		"quota7dAvg":       a7d,
	}
}

func dispatchStatusHandler(q *sqlc.Queries, svc *dispatch.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, all := scope(r)
		writeJSON(w, 200, buildDispatchStatus(r.Context(), q, svc, time.Now().UnixMilli(), owner, all))
	}
}

func dispatchStreamHandler(q *sqlc.Queries, svc *dispatch.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		fl, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, 500, map[string]string{"error": "stream unsupported"})
			return
		}
		owner, all := scope(r)
		push := func() {
			b, _ := json.Marshal(buildDispatchStatus(r.Context(), q, svc, time.Now().UnixMilli(), owner, all))
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(b)
			_, _ = w.Write([]byte("\n\n"))
			fl.Flush()
		}
		push()
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-t.C:
				push()
			}
		}
	}
}
