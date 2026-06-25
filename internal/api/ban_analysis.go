package api

import (
	"net/http"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func banAnalysisHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, all := scope(r)
		byWeekday := make([]map[string]any, 0)
		byHour := make([]map[string]any, 0)
		byAccount := make([]map[string]any, 0)
		var total int64
		if all {
			total, _ = q.BanTotal(r.Context())
			wd, _ := q.BanCountsByWeekday(r.Context())
			hr, _ := q.BanCountsByHour(r.Context())
			ac, _ := q.BanCountsPerAccount(r.Context())
			for _, x := range wd {
				byWeekday = append(byWeekday, map[string]any{"bucket": x.Bucket, "count": x.N})
			}
			for _, x := range hr {
				byHour = append(byHour, map[string]any{"bucket": x.Bucket, "count": x.N})
			}
			for _, x := range ac {
				byAccount = append(byAccount, map[string]any{"email": x.Email, "count": x.N})
			}
		} else {
			total, _ = q.BanTotalByOwner(r.Context(), owner)
			wd, _ := q.BanCountsByWeekdayForOwner(r.Context(), owner)
			hr, _ := q.BanCountsByHourForOwner(r.Context(), owner)
			ac, _ := q.BanCountsPerAccountForOwner(r.Context(), owner)
			for _, x := range wd {
				byWeekday = append(byWeekday, map[string]any{"bucket": x.Bucket, "count": x.N})
			}
			for _, x := range hr {
				byHour = append(byHour, map[string]any{"bucket": x.Bucket, "count": x.N})
			}
			for _, x := range ac {
				byAccount = append(byAccount, map[string]any{"email": x.Email, "count": x.N})
			}
		}
		writeJSON(w, 200, map[string]any{"total": total, "byWeekday": byWeekday, "byHour": byHour, "byAccount": byAccount})
	}
}
