package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/provision"
)

func startProvisionHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// PinnedKey is the base64-encoded SSH wire-format public key of the target
		// host (provision-4). When supplied, DialWithHostKey is used and the
		// connection is rejected if the server presents a different key. When
		// omitted (e.g. fresh machine whose host key is not yet known), Dial is
		// used with InsecureIgnoreHostKey — operators must accept the MITM risk.
		var body struct{ Host, User, Password, Name, OwnerId, PinnedKey string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Host == "" || body.Password == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "host/password required"})
			return
		}
		// For non-superadmin callers, force the node's owner_id to the caller's
		// own sub, ignoring any ownerId supplied in the body (provision-3).
		// A superadmin may explicitly specify an ownerId (e.g. to provision on
		// behalf of a tenant); if the body omits it the empty string is kept.
		if callerOwner, all := scope(r); !all {
			body.OwnerId = callerOwner
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
		go func(host, user, password, pinnedKey, name, ownerID string) {
			ctx := context.Background()
			var (
				ex     provision.Executor
				closer func() error
				err    error
			)
			if pinnedKey != "" {
				// provision-4: use host-key pinning to prevent MITM attacks.
				ex, closer, err = provision.DialWithHostKey(host, user, password, pinnedKey)
			} else {
				// No pinned key supplied — fall through to insecure dial.
				// WARNING: this path is vulnerable to MITM attacks. Operators
				// should supply pinnedKey for all non-fresh machines.
				log.Printf("[provision] WARNING: provisioning %s without a pinned host key (MITM risk) — supply pinnedKey to enable host-key verification", host)
				ex, closer, err = provision.Dial(host, user, password)
			}
			if err != nil {
				_ = q.AppendProvisionLog(ctx, sqlc.AppendProvisionLogParams{ID: jobID, Chunk: "✗ SSH 连接失败: " + err.Error() + "\n", UpdatedAt: now()})
				_ = q.SetProvisionStatus(ctx, sqlc.SetProvisionStatusParams{ID: jobID, Status: "failed", Step: "ssh", UpdatedAt: now()})
				return
			}
			defer closer()
			provision.Provision(ctx, q, ex, jobID, name, host, ownerID, now)
		}(body.Host, user, body.Password, body.PinnedKey, body.Name, body.OwnerId)

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
