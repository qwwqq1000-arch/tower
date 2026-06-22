package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/provision"
)

func startProvisionHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Host, User, Password, Name, OwnerId string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Host == "" || body.Password == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "host/password required"})
			return
		}
		if body.Name == "" {
			body.Name = nextNodeName(r.Context(), q)
		}
		now := func() int64 { return time.Now().UnixMilli() }
		jobID := randHex("job_")
		if _, err := q.CreateProvisionJob(r.Context(), sqlc.CreateProvisionJobParams{
			ID: jobID, Host: body.Host, Name: body.Name, OwnerID: body.OwnerId, CreatedAt: now(),
		}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		user := body.User
		if user == "" {
			user = "root"
		}
		// async — password stays in this closure only
		go func(host, user, password, name, ownerID string) {
			ctx := context.Background()
			ex, closer, err := provision.Dial(host, user, password)
			if err != nil {
				_ = q.AppendProvisionLog(ctx, sqlc.AppendProvisionLogParams{ID: jobID, Chunk: "✗ SSH 连接失败: " + err.Error() + "\n", UpdatedAt: now()})
				_ = q.SetProvisionStatus(ctx, sqlc.SetProvisionStatusParams{ID: jobID, Status: "failed", Step: "ssh", UpdatedAt: now()})
				return
			}
			defer closer()
			provision.Provision(ctx, q, ex, jobID, name, host, ownerID, now)
		}(body.Host, user, body.Password, body.Name, body.OwnerId)

		writeJSON(w, http.StatusAccepted, map[string]string{"jobId": jobID})
	}
}

func getProvisionHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		j, err := q.GetProvisionJob(r.Context(), r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if owner, all := scope(r); !all && j.OwnerID != owner {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": j.ID, "host": j.Host, "name": j.Name, "status": j.Status, "step": j.Step, "log": j.Log})
	}
}
