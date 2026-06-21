package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
)

func buildDispatchStatus(ctx context.Context, q *sqlc.Queries, svc *dispatch.Service, now int64) map[string]any {
	// account labels
	labels := map[string]string{}
	if accs, err := q.ListNodeAccountsAll(ctx); err == nil {
		for _, a := range accs {
			label := a.NodeName
			if a.Email != "" {
				label = a.Email
			}
			labels[a.NodeID+":"+a.ProfileID] = label
		}
	}
	accounts := []map[string]any{}
	if svc != nil && svc.Store != nil {
		for _, s := range svc.Store.Snapshot(now) {
			accounts = append(accounts, map[string]any{"key": s.Key, "label": labels[s.Key], "status": s.Status, "inflight": s.Inflight, "available": s.Available})
		}
	}
	traffic := map[string]any{"total": 0, "ok": 0, "error": 0, "tokensIn": int64(0), "tokensOut": int64(0)}
	if logs, err := q.ListRecentDispatchLogs(ctx, 200); err == nil {
		var ok, errc int
		var in, out int64
		for _, l := range logs {
			if l.Status == "ok" {
				ok++
			} else if l.Status == "error" {
				errc++
			}
			in += l.TokensIn
			out += l.TokensOut
		}
		traffic = map[string]any{"total": len(logs), "ok": ok, "error": errc, "tokensIn": in, "tokensOut": out}
	}
	events := []map[string]any{}
	if evs, err := q.ListRecentEvents(ctx, 20); err == nil {
		for _, e := range evs {
			events = append(events, map[string]any{"ts": e.Ts, "type": e.Type, "target": e.Target})
		}
	}
	nodesTotal, nodesEnabled := 0, 0
	if ns, err := q.ListNodes(ctx); err == nil {
		nodesTotal = len(ns)
		for _, n := range ns {
			if n.Enabled {
				nodesEnabled++
			}
		}
	}
	return map[string]any{
		"accounts": accounts, "traffic": traffic, "events": events,
		"nodes": map[string]any{"total": nodesTotal, "enabled": nodesEnabled},
		"asOf":  now,
	}
}

func dispatchStatusHandler(q *sqlc.Queries, svc *dispatch.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, buildDispatchStatus(r.Context(), q, svc, time.Now().UnixMilli()))
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
		push := func() {
			b, _ := json.Marshal(buildDispatchStatus(r.Context(), q, svc, time.Now().UnixMilli()))
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
