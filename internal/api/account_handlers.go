package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
	"github.com/qwwqq1000-arch/tower/internal/events"
	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
)

// effectiveProfiles returns the node's named profiles, or — when none exist but
// the node is logged in (default account) — a synthetic "default" profile from /health.
func effectiveProfiles(ctx context.Context, cl *nodeclient.Client) ([]nodeclient.Profile, error) {
	ps, err := cl.ProfilesList(ctx)
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		h, herr := cl.Health(ctx)
		if herr == nil && h.Auth.LoggedIn {
			return []nodeclient.Profile{{ID: "default", Email: h.Auth.Email, SubscriptionType: h.Auth.SubscriptionType, LoggedIn: true}}, nil
		}
	}
	return ps, nil
}

func nodeClientFor(q *sqlc.Queries, cipher *crypto.Cipher, r *http.Request, id string) (*nodeclient.Client, sqlc.Node, bool) {
	n, err := q.GetNode(r.Context(), id)
	if err != nil {
		return nil, sqlc.Node{}, false
	}
	// owner scoping: non-superadmin may only operate on nodes they own.
	if owner, all := scope(r); !all && n.OwnerID != owner {
		return nil, sqlc.Node{}, false
	}
	// Decrypt the stored api_key transparently (vault-crypto-3): ciphertext rows
	// decrypt, legacy plaintext rows pass through unchanged.
	return nodeclient.New(n.BaseUrl, cipher.DecryptOrPlaintext(n.ApiKey)), n, true
}

func oauthStartHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, _, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
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

func oauthExchangeHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, n, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
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
			ExpiresAt:        time.Now().Add(30 * 24 * time.Hour).UnixMilli(),
			OnboardedAt:      time.Now().UnixMilli(),
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

func importProfileHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		nodeID := r.PathValue("id")
		n, err := q.GetNode(r.Context(), nodeID)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		if owner, all := scope(r); !all && n.OwnerID != owner {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var body struct {
			ProfileID string `json:"profileId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ProfileID == "" {
			writeJSON(w, 400, map[string]string{"error": "profileId required"})
			return
		}
		cl := nodeclient.New(n.BaseUrl, cipher.DecryptOrPlaintext(n.ApiKey))
		profiles, _ := effectiveProfiles(r.Context(), cl)
		var matched *nodeclient.Profile
		for i := range profiles {
			if profiles[i].ID == body.ProfileID {
				matched = &profiles[i]
				break
			}
		}
		if matched == nil || !matched.LoggedIn {
			writeJSON(w, 400, map[string]string{"error": "profile not found or not logged in"})
			return
		}
		// Idempotent: reuse existing assignment if present.
		existing, _ := q.ListNodeAccountsByNode(r.Context(), n.ID)
		for _, a := range existing {
			if a.ProfileID == matched.ID {
				writeJSON(w, 200, map[string]any{"ok": true, "reused": true, "profileId": matched.ID, "email": matched.Email})
				return
			}
		}
		accID := randHex("acc_")
		if _, err := q.CreateAccount(r.Context(), sqlc.CreateAccountParams{
			ID:               accID,
			OwnerID:          n.OwnerID,
			Email:            matched.Email,
			SubscriptionType: "",
			OauthAccessEnc:   "",
			OauthRefreshEnc:  "",
			ExpiresAt:        time.Now().Add(30 * 24 * time.Hour).UnixMilli(),
			OnboardedAt:      time.Now().UnixMilli(),
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if _, err := q.AssignAccount(r.Context(), sqlc.AssignAccountParams{
			NodeID:    n.ID,
			AccountID: accID,
			ProfileID: matched.ID,
			Egress:    "",
			Weight:    100,
			Role:      "baseline",
			SlotID:    "",
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "profileId": matched.ID, "email": matched.Email})
	}
}

func listProfilesHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, _, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		ps, err := effectiveProfiles(r.Context(), cl)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, ps)
	}
}

func listAccountsHandler(q *sqlc.Queries, svc *dispatch.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		owner, all := scope(r)
		rows, err := q.ListNodeAccountsAll(ctx)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		// CPA per-account quota (5h/7d/7d-sonnet utilization), keyed by account id.
		quotaByAccount := map[string]sqlc.CpaAccountQuotum{}
		if qrows, qerr := q.ListCpaQuota(ctx); qerr == nil {
			for _, qr := range qrows {
				quotaByAccount[qr.AccountID] = qr
			}
		}
		// Live status overlay (banned/half_open/permanent/...) from the in-memory store.
		liveStatus := map[string]string{}
		if svc != nil && svc.Store != nil {
			now := int64(0)
			if svc.Now != nil {
				now = svc.Now()
			}
			for _, snap := range svc.Store.Snapshot(now) {
				liveStatus[snap.Key] = snap.Status
			}
		}
		// Build today cost map: target -> cost
		todayCostMap := map[string]float64{}
		if todayRows, err := q.CostByTargetSince(ctx, startOfTodayMs()); err == nil {
			for _, r := range todayRows {
				todayCostMap[r.Target] = r.Cost
			}
		}
		// Build total cost map: target -> cost
		totalCostMap := map[string]float64{}
		if totalRows, err := q.CostByTargetTotal(ctx); err == nil {
			for _, r := range totalRows {
				totalCostMap[r.Target] = r.Cost
			}
		}
		out := make([]map[string]any, 0, len(rows))
		for _, a := range rows {
			if !all && a.AcctOwnerID != owner { // owner scoping: non-superadmin sees only own
				continue
			}
			key := a.NodeID + ":" + a.ProfileID
			status := a.AcctStatus
			if ls, ok := liveStatus[key]; ok {
				status = ls // live ban/half_open/permanent state wins over stored value
			}
			var quota map[string]any
			if qr, ok := quotaByAccount[a.AccountID]; ok {
				quota = map[string]any{
					"fiveHourUtil": qr.FiveHourUtil, "fiveHourResetsAt": qr.FiveHourResetsAt,
					"sevenDayUtil": qr.SevenDayUtil, "sevenDayResetsAt": qr.SevenDayResetsAt,
					"sevenDaySonnetUtil": qr.SevenDaySonnetUtil, "sevenDaySonnetResetsAt": qr.SevenDaySonnetResetsAt,
					"updatedAt": qr.UpdatedAt,
				}
			}
			out = append(out, map[string]any{
				"nodeId":           a.NodeID,
				"nodeName":         a.NodeName,
				"baseUrl":          a.BaseUrl,
				"accountId":        a.AccountID,
				"profileId":        a.ProfileID,
				"enabled":          a.Enabled,
				"weight":           a.Weight,
				"role":             a.Role,
				"egress":           a.Egress,
				"email":            a.Email,
				"status":           status,
				"todayCostUsd":     todayCostMap[key],
				"totalCostUsd":     totalCostMap[key],
				"expiresAt":        a.ExpiresAt,
				"subscriptionType": a.SubscriptionType,
				"ownerId":          a.AcctOwnerID,
				"cpaQuota":         quota,
			})
		}
		writeJSON(w, 200, out)
	}
}

// recoverAccountHandler clears all ban/cooldown/permanent state for one account
// (across every node it is assigned to), re-enables it, and records an event.
func recoverAccountHandler(q *sqlc.Queries, svc *dispatch.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accountID := r.PathValue("accountId")
		acc, err := q.GetAccount(r.Context(), accountID)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "account not found"})
			return
		}
		// owner scoping: non-superadmin may only recover their own accounts.
		if owner, all := scope(r); !all && acc.OwnerID != owner {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		rows, err := q.ListNodeAccountsByAccount(r.Context(), accountID)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		var now int64
		if svc != nil && svc.Now != nil {
			now = svc.Now()
		}
		if svc != nil && svc.Store != nil {
			for _, na := range rows {
				svc.Store.Recover(na.NodeID + ":" + na.ProfileID)
				// Persist the cleared verdict immediately so a restart in the
				// periodic-persist window cannot reload permanent=true and silently
				// re-ban the account the operator just recovered.
				_ = q.UpsertAccountState(r.Context(), sqlc.UpsertAccountStateParams{
					NodeID: na.NodeID, ProfileID: na.ProfileID, Status: "active",
					CooldownUntil: 0, BanStreak: 0, FailCount: 0, Permanent: false, UpdatedAt: now,
				})
			}
		}
		_ = q.SetNodeAccountEnabledByAccount(r.Context(), sqlc.SetNodeAccountEnabledByAccountParams{AccountID: accountID, Enabled: true})
		_ = q.SetAccountStatus(r.Context(), sqlc.SetAccountStatusParams{ID: accountID, Status: "active"})
		_ = events.Record(r.Context(), q, now, events.Event{Type: "account_recovered", Target: accountID, OwnerID: acc.OwnerID, Detail: map[string]any{"email": acc.Email}})
		writeJSON(w, 200, map[string]any{"ok": true, "accountId": accountID})
	}
}

func setAccountExpiryHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		accountID := r.PathValue("accountId")
		if !ownsAccountID(r, q, accountID) {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var body struct {
			ExpiresAt int64 `json:"expiresAt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		if err := q.SetAccountExpiry(r.Context(), sqlc.SetAccountExpiryParams{
			ID:        accountID,
			ExpiresAt: body.ExpiresAt,
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func setAccountOwnerHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Reassigning ownership is a superadmin-only operation: allowing a
		// non-superadmin to set an arbitrary owner_id is an account-theft vector.
		if _, all := scope(r); !all {
			writeJSON(w, 403, map[string]string{"error": "superadmin required"})
			return
		}
		accountID := r.PathValue("accountId")
		var body struct {
			OwnerID string `json:"ownerId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		if err := q.SetAccountOwner(r.Context(), sqlc.SetAccountOwnerParams{
			ID:      accountID,
			OwnerID: body.OwnerID,
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func unassignAccountHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ownsAccountID(r, q, r.PathValue("accountId")) {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
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
