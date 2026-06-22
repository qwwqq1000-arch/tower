package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwwqq1000-arch/tower/internal/billing"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func settleHandler(pool *pgxpool.Pool, q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			TenantId    string `json:"tenantId"`
			PeriodStart int64  `json:"periodStart"`
			PeriodEnd   int64  `json:"periodEnd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TenantId == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenantId required"})
			return
		}
		if owner, all := scope(r); !all && body.TenantId != owner {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		now := time.Now().UnixMilli()
		st, err := billing.Settle(r.Context(), pool, body.TenantId, body.PeriodStart, body.PeriodEnd, now, randHex("s_"))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": st.ID, "tenantId": st.TenantID, "gross": st.GrossUsd, "status": st.Status})
	}
}

func ledgerHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := r.URL.Query().Get("tenantId")
		// owner scoping: a non-superadmin may only read their own ledger regardless
		// of the tenantId query param.
		if owner, all := scope(r); !all {
			tenant = owner
		}
		if tenant == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenantId required"})
			return
		}
		rows, err := q.ListLedgerByTenant(r.Context(), tenant)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, e := range rows {
			out = append(out, map[string]any{"ts": e.Ts, "type": e.Type, "amount": e.AmountUsd, "ref": e.Ref, "note": e.Note})
		}
		writeJSON(w, http.StatusOK, out)
	}
}
