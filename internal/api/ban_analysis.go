package api

import (
	"net/http"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func banAnalysisHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		total, _ := q.BanTotal(r.Context())
		wd, _ := q.BanCountsByWeekday(r.Context())
		hr, _ := q.BanCountsByHour(r.Context())
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
