package api

import (
	"encoding/json"
	"net/http"

	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// getNodeProxyHandler reads the node's current egress proxy (GET
// /settings/api/proxy). meridian-only; CPA nodes return 409.
func getNodeProxyHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, n, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		if cpaNotApplicable(w, n.Kind) {
			return
		}
		pi, err := cl.GetProxy(r.Context())
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, pi)
	}
}

// testNodeProxyHandler asks the node to dial the pasted proxy and report
// reachability + egress IP without persisting anything.
func testNodeProxyHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, n, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		if cpaNotApplicable(w, n.Kind) {
			return
		}
		var body struct {
			Raw string `json:"raw"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		out, err := cl.TestProxy(r.Context(), body.Raw)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, out)
	}
}

// setNodeProxyHandler persists the node's egress proxy (empty raw clears it).
// The node restarts to activate redsocks; the response carries restarting=true.
// audit records only whether a proxy is set — never the plaintext proxy string
// (which contains credentials).
func setNodeProxyHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, n, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		if cpaNotApplicable(w, n.Kind) {
			return
		}
		var body struct {
			Raw string `json:"raw"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		out, err := cl.SetProxy(r.Context(), body.Raw)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		recordAudit(r, q, "node.proxy.set", "node:"+n.ID, nil, map[string]any{"hasProxy": body.Raw != ""})
		writeJSON(w, 200, out)
	}
}
