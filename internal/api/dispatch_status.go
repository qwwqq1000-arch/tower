package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
)

// totalCostCache caches the expensive CostByTargetTotal query (full-table scan
// of dispatch_logs). Refreshed at most every 30 seconds; SSE pushes read the
// cached value so the 2-second tick doesn't hammer the DB.
var totalCostCache struct {
	sync.Mutex
	data map[string]float64
	at   time.Time
}

// totalCountCache caches CountDispatchLogs + CountDispatchLogsByStatus("ok"/"error").
var totalCountCache struct {
	sync.Mutex
	total, ok, err int64
	at             time.Time
}

func cachedTotalCounts(ctx context.Context, q *sqlc.Queries) (total, okc, errc int64) {
	totalCountCache.Lock()
	defer totalCountCache.Unlock()
	if time.Since(totalCountCache.at) < 10*time.Second {
		return totalCountCache.total, totalCountCache.ok, totalCountCache.err
	}
	if t, e := q.CountDispatchLogs(ctx); e == nil {
		totalCountCache.total = t
	}
	if c, e := q.CountDispatchLogsByStatus(ctx, "ok"); e == nil {
		totalCountCache.ok = c
	}
	if c, e := q.CountDispatchLogsByStatus(ctx, "error"); e == nil {
		totalCountCache.err = c
	}
	totalCountCache.at = time.Now()
	return totalCountCache.total, totalCountCache.ok, totalCountCache.err
}

func cachedTotalCostMap(ctx context.Context, q *sqlc.Queries) map[string]float64 {
	totalCostCache.Lock()
	defer totalCostCache.Unlock()
	if time.Since(totalCostCache.at) < 30*time.Second && totalCostCache.data != nil {
		return totalCostCache.data
	}
	m := map[string]float64{}
	if rows, err := q.CostByTargetTotal(ctx); err == nil {
		for _, r := range rows {
			m[r.Target] = r.Cost
		}
	}
	totalCostCache.data = m
	totalCostCache.at = time.Now()
	return m
}

// cpaQuotaAvg computes the average 5h/7d utilization (0..1 fractions) across CPA
// accounts that have usable quota data, EXCLUDING banned accounts and failed fetches.
// It is read-only: it reflects whatever cpa_account_quota currently holds (populated
// by the manual 刷新 button and reactive limit pulls) and never triggers a fetch — so
// the homepage cards update whenever a refresh writes the table, but the page itself
// does not actively poll. Utilization is stored 0..100, so we divide by 100 here to
// match the 0..1 fraction convention the dashboard cards render with (*100 → %).
func cpaQuotaAvg(ctx context.Context, q *sqlc.Queries, svc *dispatch.Service) (float64, float64) {
	rows, err := q.ListCpaQuota(ctx)
	if err != nil || len(rows) == 0 {
		return 0, 0
	}
	// Banned accounts (封号的不计入): map account_id → banned via the live store status.
	banned := map[string]bool{}
	if svc != nil && svc.Store != nil {
		nowMs := int64(0)
		if svc.Now != nil {
			nowMs = svc.Now()
		}
		statusByKey := map[string]string{}
		for _, snap := range svc.Store.Snapshot(nowMs) {
			statusByKey[snap.Key] = snap.Status
		}
		if nas, nerr := q.ListNodeAccountsAll(ctx); nerr == nil {
			for _, na := range nas {
				switch statusByKey[na.NodeID+":"+na.ProfileID] {
				case "banned", "permanent", "half_open", "cooldown":
					banned[na.AccountID] = true
				}
			}
		}
	}
	var sum5, sum7 float64
	var n int
	for _, r := range rows {
		if r.QuotaFetchError != "" || r.UpdatedAt == 0 { // 无数据/拉取失败 → 跳过
			continue
		}
		if banned[r.AccountID] {
			continue
		}
		sum5 += r.FiveHourUtil
		sum7 += r.SevenDayUtil
		n++
	}
	if n == 0 {
		return 0, 0
	}
	return (sum5 / float64(n)) / 100, (sum7 / float64(n)) / 100
}

// pinnedModelOf returns the model an account is currently pinned to (sticky model-pin),
// or "" when unpinned/expired. Surfaced in the status so the UI shows each号's模型.
func pinnedModelOf(svc *dispatch.Service, key string, ttl int64) string {
	if svc == nil || svc.Store == nil || ttl <= 0 {
		return ""
	}
	if pm, ok := svc.Store.PinnedModel(key, ttl); ok {
		return pm
	}
	return ""
}

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

	// Resolve elastic reserve set: for each owner, compute which keys are reserve
	// (not yet scaled up and beyond the baseline count). Used to display 待命 (standby).
	// For superadmin (all=true) we need to resolve per unique owner; for a tenant,
	// only that owner's policy matters.
	reserveKeys := map[string]bool{}
	if svc != nil {
		owners := map[string]bool{}
		if all {
			for _, o := range keyOwner {
				if o != "" {
					owners[o] = true
				}
			}
		} else {
			owners[owner] = true
		}
		for o := range owners {
			cfg := svc.ResolveConfigForOwner(ctx, o)
			for k := range svc.ReserveKeys(ctx, o, cfg) {
				reserveKeys[k] = true
			}
		}
	}
	// An account is "known" only while its node_accounts row still exists; a deleted
	// node can leave stale in-memory store entries that must not surface as ghost rows.
	known := make(map[string]bool, len(keyOwner))
	for k := range keyOwner {
		known[k] = true
	}
	visible := func(key string) bool { return known[key] && (all || keyOwner[key] == owner) }
	// Build bound_at map for cost scoping after account replacement.
	boundAtMap := map[string]int64{}
	if baRows, err := q.ListBoundAtByTarget(ctx); err == nil {
		for _, r := range baRows {
			boundAtMap[r.Target] = r.BoundAt
		}
	}
	todayStart := startOfTodayMs()
	// Build today/total cost maps for accounts
	todayCostMap := map[string]float64{}
	if todayRows, err := q.CostByTargetSince(ctx, todayStart); err == nil {
		for _, r := range todayRows {
			todayCostMap[r.Target] = r.Cost
		}
	}
	for target, ba := range boundAtMap {
		if ba > todayStart {
			if cost, err := q.CostByTargetSinceOne(ctx, sqlc.CostByTargetSinceOneParams{Target: target, Ts: ba}); err == nil {
				todayCostMap[target] = cost
			}
		}
	}
	totalCostMap := cachedTotalCostMap(ctx, q)
	for target, ba := range boundAtMap {
		if ba > 0 {
			if cost, err := q.CostByTargetSinceOne(ctx, sqlc.CostByTargetSinceOneParams{Target: target, Ts: ba}); err == nil {
				totalCostMap[target] = cost
			}
		}
	}
	// Build per-account RPM map: count dispatch_logs for each target in the last 60s.
	rpmMap := map[string]int64{}
	if rpmRows, err := q.CountRecentByTarget(ctx, now-60000); err == nil {
		for _, r := range rpmRows {
			rpmMap[r.Target] = r.N
		}
	}
	// Model-pin TTL for surfacing each account's currently-pinned model (model-aware-
	// elastic visibility). Resolved once from the owner's (or global) config.
	pinTTL := int64(0)
	if svc != nil {
		pinTTL = int64(svc.ResolveConfigForOwner(ctx, owner).AffinityTTLSec) * 1000
	}
	// owner_id -> tenant username so the concurrency panel shows which tenant
	// owns each account (few unique owners, so per-owner lookup is cheap).
	tenantNames := map[string]string{}
	seenOwner := map[string]bool{}
	for _, o := range keyOwner {
		if o == "" || seenOwner[o] {
			continue
		}
		seenOwner[o] = true
		if t, err := q.GetTenantByID(ctx, o); err == nil {
			tenantNames[o] = t.Username
		}
	}
	accounts := []map[string]any{}
	if svc != nil && svc.Store != nil {
		for _, s := range svc.Store.Snapshot(now) {
			if strings.HasPrefix(s.Key, "fb:") {
				continue
			}
			owner, known := keyOwner[s.Key]
			if !known { // node/account deleted → drop stale ghost row
				continue
			}
			// yanghao is the warm-up/holding tenant: its fresh accounts are parked
			// there deliberately kept OUT of rotation so they are not hammered while
			// aging, so they must not appear in the live concurrency panel.
			if tenantNames[owner] == "yanghao" {
				continue
			}
			if !visible(s.Key) { // owner scoping
				continue
			}
			if s.Status == "permanent" { // permanently banned → out of rotation, hide from the live pool
				continue
			}
			status := s.Status
			if s.Paused {
				status = "paused"
			} else if s.Limited {
				status = "limited"
			}
			// Elastic reserve overlay: accounts beyond the baseline that haven't been
			// scaled up yet show as "reserve" (待命) instead of "active". Banned/limited
			// accounts keep their real status so they still sort to the end.
			isReserve := reserveKeys[s.Key]
			if isReserve && status == "active" {
				status = "reserve"
				// Affinity override: if a conversation is currently pinned to this reserve
				// account (affinity > elastic), show 亲和 so the operator can see it is
				// actively routing — not truly idle.
				if svc.HasActiveAffinity(s.Key, now) {
					status = "affinity"
				}
			}
			acct := map[string]any{
				"key":          s.Key,
				"label":        labels[s.Key],
				"status":       status,
				"reserve":      isReserve,
				"limitedUntil": s.LimitedUntil,
				"limitReason":  s.LimitReason,
				"pausedUntil":  s.PausedUntil,
				"inflight":     s.Inflight,
				"available":    s.Available,
				"recoverAt":    s.RecoverAt,
				"todayCostUsd": todayCostMap[s.Key],
				"totalCostUsd": totalCostMap[s.Key],
				"rpm":          rpmMap[s.Key],
				"pinnedModel":  pinnedModelOf(svc, s.Key, pinTTL),
				"tenant":       tenantNames[keyOwner[s.Key]],
			}
			if ci := strings.IndexByte(s.Key, ':'); ci > 0 {
				if h, ok := getHealthCache(s.Key[:ci]); ok {
					acct["subscriptionType"] = h.SubscriptionType
					acct["subscriptionCreatedAt"] = h.SubscriptionCreatedAt
					acct["accountCreatedAt"] = h.AccountCreatedAt
				}
			}
			accounts = append(accounts, acct)
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
		total, okc, errc = cachedTotalCounts(ctx, q)
		if r, rerr := q.CountDispatchLogsSince(ctx, now-60000); rerr == nil {
			rpm = r
		}
	} else {
		// owner-scoped traffic. ok/error + token breakdown come from the owner's recent
		// logs so the tenant dispatch panel shows real 成功/错误 (not just totals); this
		// is the same computation the old per-tenant handler did, now shared here.
		total, _ = q.CountDispatchLogsByOwner(ctx, owner)
		rpm, _ = q.CountDispatchLogsByOwnerSince(ctx, sqlc.CountDispatchLogsByOwnerSinceParams{OwnerID: owner, Ts: now - 60000})
		if logs, lerr := q.ListLogsByOwner(ctx, sqlc.ListLogsByOwnerParams{OwnerID: owner, Limit: 200}); lerr == nil {
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
	acctTotal, acctEnabled := 0, 0
	if accs, err := q.ListNodeAccountsAll(ctx); err == nil {
		for _, a := range accs {
			if !all && a.AcctOwnerID != owner {
				continue
			}
			acctTotal++
			if a.Enabled {
				acctEnabled++
			}
		}
	}
	elasticCurrent, elasticMax := 0, 0
	if svc != nil {
		cfg := svc.ResolveConfigForOwner(ctx, owner)
		if cfg.ElasticEnabled {
			if cfg.ElasticMaxActive > 0 {
				elasticMax = cfg.ElasticMaxActive
			} else {
				elasticMax = cfg.ElasticBaselineCount + cfg.ElasticMaxReserve
			}
			// Count actually usable active accounts: not in reserve, not
			// permanent, not limited. Limited accounts are effectively dead
			// (quota exhausted) and should not occupy elastic slots.
			if svc.Store != nil {
				for _, s := range svc.Store.Snapshot(now) {
					if strings.HasPrefix(s.Key, "fb:") {
						continue
					}
					if !visible(s.Key) {
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
				"priority": ch.Priority,
				"todayRequests": todayReq, "todayCostUsd": todayCost,
				"inflight": inflight, "available": available,
				"balanceUsd": ch.BalanceUsd,
			})
		}
	}
	// global average across all accounts → superadmin only (no cross-owner leak).
	// Display-only metric (nodeclient-telemetry-3); does not drive dispatch or scaling.
	var a5h, a7d float64
	if all {
		a5h, a7d = cpaQuotaAvg(ctx, q, svc)
	}
	return map[string]any{
		"accounts": accounts, "traffic": traffic, "events": events,
		"nodes":            map[string]any{"total": acctTotal, "enabled": acctEnabled},
		"elastic":          map[string]any{"current": elasticCurrent, "max": elasticMax},
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
