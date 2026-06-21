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
		writeJSON(w, http.StatusOK, map[string]any{
			"uptimeSec":  int64(time.Since(startedAt).Seconds()),
			"goroutines": runtime.NumGoroutine(),
			"memAllocMB": float64(m.Alloc) / 1024 / 1024,
			"memSysMB":   float64(m.Sys) / 1024 / 1024,
			"numGC":      m.NumGC,
			"goVersion":  runtime.Version(),
			"numCPU":     runtime.NumCPU(),
			"startedAt":  startedAt.UnixMilli(),
		})
	}
}
