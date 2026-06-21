package api

import (
	"net/http"
	"runtime"
	"time"
)

var startedAt = time.Now()

func serverStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		diskTotalGB, diskUsedGB, diskUsedPct := diskStats()
		netRxMBps, netTxMBps, netRxTotalMB, netTxTotalMB := netStats()

		writeJSON(w, http.StatusOK, map[string]any{
			"uptimeSec":    int64(time.Since(startedAt).Seconds()),
			"goroutines":   runtime.NumGoroutine(),
			"memAllocMB":   float64(m.Alloc) / 1024 / 1024,
			"memSysMB":     float64(m.Sys) / 1024 / 1024,
			"numGC":        m.NumGC,
			"goVersion":    runtime.Version(),
			"numCPU":       runtime.NumCPU(),
			"startedAt":    startedAt.UnixMilli(),
			"diskTotalGB":  diskTotalGB,
			"diskUsedGB":   diskUsedGB,
			"diskUsedPct":  diskUsedPct,
			"netRxMBps":    netRxMBps,
			"netTxMBps":    netTxMBps,
			"netRxTotalMB": netRxTotalMB,
			"netTxTotalMB": netTxTotalMB,
		})
	}
}
