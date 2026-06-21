package api

import (
	"encoding/json"
	"net/http"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
)

func nodeClientFor(q *sqlc.Queries, r *http.Request, id string) (*nodeclient.Client, sqlc.Node, bool) {
	n, err := q.GetNode(r.Context(), id)
	if err != nil {
		return nil, sqlc.Node{}, false
	}
	return nodeclient.New(n.BaseUrl, n.ApiKey), n, true
}

func oauthStartHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, _, ok := nodeClientFor(q, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		lu, err := cl.LoginURL(r.Context())
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"authorizeUrl": lu.AuthorizeURL, "codeVerifier": lu.CodeVerifier, "state": lu.State})
	}
}

func oauthExchangeHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, n, ok := nodeClientFor(q, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		var body struct {
			CodeVerifier string `json:"codeVerifier"`
			State        string `json:"state"`
			Code         string `json:"code"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
			writeJSON(w, 400, map[string]string{"error": "code required"})
			return
		}
		if err := cl.Exchange(r.Context(), body.CodeVerifier, body.State, body.Code); err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		// register the now-logged-in profile
		profiles, _ := cl.ProfilesList(r.Context())
		profileID, email := "default", ""
		for _, p := range profiles {
			if p.LoggedIn {
				profileID, email = p.ID, p.Email
				break
			}
		}
		// Idempotent per (node, profile): reuse existing assignment if present.
		existing, _ := q.ListNodeAccountsByNode(r.Context(), n.ID)
		for _, a := range existing {
			if a.ProfileID == profileID {
				writeJSON(w, 200, map[string]string{"ok": "true", "profileId": profileID, "email": email, "reused": "true"})
				return
			}
		}
		accID := randHex("acc_")
		if _, err := q.CreateAccount(r.Context(), sqlc.CreateAccountParams{
			ID:               accID,
			OwnerID:          n.OwnerID,
			Email:            email,
			SubscriptionType: "",
			OauthAccessEnc:   "",
			OauthRefreshEnc:  "",
			ExpiresAt:        0,
			OnboardedAt:      0,
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if _, err := q.AssignAccount(r.Context(), sqlc.AssignAccountParams{
			NodeID:    n.ID,
			AccountID: accID,
			ProfileID: profileID,
			Egress:    "",
			Weight:    100,
			Role:      "baseline",
			SlotID:    "",
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true", "profileId": profileID, "email": email})
	}
}

func listProfilesHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, _, ok := nodeClientFor(q, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		ps, err := cl.ProfilesList(r.Context())
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, ps)
	}
}

func listAccountsHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := q.ListNodeAccountsAll(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, a := range rows {
			out = append(out, map[string]any{
				"nodeId":    a.NodeID,
				"nodeName":  a.NodeName,
				"baseUrl":   a.BaseUrl,
				"accountId": a.AccountID,
				"profileId": a.ProfileID,
				"enabled":   a.Enabled,
				"weight":    a.Weight,
				"role":      a.Role,
				"egress":    a.Egress,
			})
		}
		writeJSON(w, 200, out)
	}
}

func unassignAccountHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := q.UnassignAccount(r.Context(), sqlc.UnassignAccountParams{
			NodeID:    r.PathValue("nodeId"),
			AccountID: r.PathValue("accountId"),
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}
