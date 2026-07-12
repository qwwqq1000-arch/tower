package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/provision"
)

// updateNodeCodeHandler SSHes into a provisioned node and pulls+rebuilds the
// latest meridian-mirror image (git pull → docker compose build → up). Tower does
// not store node SSH passwords, so the caller supplies it per batch run. Used by
// the node list's "更新代码" batch action to bring already-installed nodes up to the
// latest code without re-provisioning.
func updateNodeCodeHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n, err := q.GetNode(r.Context(), r.PathValue("id"))
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		if owner, all := scope(r); !all && n.OwnerID != owner {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var b struct{ User, Password string }
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Password == "" {
			writeJSON(w, 400, map[string]string{"error": "password required"})
			return
		}
		u, perr := url.Parse(n.BaseUrl)
		if perr != nil || u.Hostname() == "" {
			writeJSON(w, 400, map[string]string{"error": "bad base_url"})
			return
		}
		user := b.User
		if user == "" {
			user = "root"
		}
		ex, closer, derr := provision.Dial(u.Hostname(), user, b.Password)
		if derr != nil {
			writeJSON(w, 502, map[string]string{"error": "ssh: " + derr.Error()})
			return
		}
		defer func() { _ = closer() }()
		ctx, cancel := context.WithTimeout(r.Context(), 8*time.Minute)
		defer cancel()
		// Detect the install dir (one-click provision uses /opt/meridian; some
		// hand-deployed nodes use /opt/new-meridian). Pull latest main + rebuild.
		// Force the correct repo (older nodes were cloned from new-meridian) and
		// hard-reset to meridian-mirror main, then rebuild. reset --hard tolerates
		// the unrelated-history switch from new-meridian → meridian-mirror.
		cmd := `if [ -d /opt/meridian/.git ]; then cd /opt/meridian; ` +
			`elif [ -d /opt/new-meridian/.git ]; then cd /opt/new-meridian; ` +
			`else echo "no git install dir"; exit 1; fi; ` +
			`git remote set-url origin https://github.com/qwwqq1000-arch/meridian-mirror.git && ` +
			`git fetch origin main && git reset --hard origin/main && ` +
			`docker compose build && docker compose up -d`
		res, xerr := ex.Exec(ctx, cmd)
		log := res.Stdout + res.Stderr
		if len(log) > 4000 {
			log = log[len(log)-4000:]
		}
		if xerr != nil {
			writeJSON(w, 500, map[string]string{"error": xerr.Error(), "log": log})
			return
		}
		if res.Code != 0 {
			writeJSON(w, 500, map[string]string{"error": "update failed (exit " + strconv.Itoa(res.Code) + ")", "log": log})
			return
		}
		recordAudit(r, q, "node.update-code", "node:"+n.ID, nil, map[string]any{"host": u.Hostname()})
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}
