package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/auth"
)

// TestSettleRequiresSuperadmin verifies that POST /api/admin/settle is
// rejected for any non-superadmin role (the route is wrapped with
// requireSuperadmin and the handler has a belt-and-suspenders scope guard).
//
// We test via the middleware stack directly (not the full router) so the test
// is pure and does not need a database.
func TestSettleRequiresSuperadmin(t *testing.T) {
	const secret = "test-secret-padding-to-32-chars!"

	// Build the handler the same way the router does.
	handler := requireSuperadmin(secret, settleHandler(nil, nil))

	cases := []struct {
		role string
		want int
	}{
		{"admin", http.StatusForbidden},
		{"operator", http.StatusForbidden},
		{"tenant", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.role, func(t *testing.T) {
			body := `{"tenantId":"tenant-abc","periodStart":0,"periodEnd":9999}`
			r := httptest.NewRequest(http.MethodPost, "/api/admin/settle", strings.NewReader(body))
			r.AddCookie(&http.Cookie{
				Name:  "tower_session",
				Value: auth.IssueSession(secret, "some-user", tc.role, nowUnix(), 3600),
			})
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, r)
			if rec.Code != tc.want {
				t.Fatalf("role=%s: status=%d, want %d (body: %s)", tc.role, rec.Code, tc.want, rec.Body)
			}
		})
	}
}

// TestSettleScopeGuard verifies the belt-and-suspenders scope guard inside
// settleHandler: even if the requireSuperadmin wrapper is somehow bypassed (or
// removed in future), a non-all-scope session whose tenantId does not match the
// caller is rejected with 403.
func TestSettleScopeGuard(t *testing.T) {
	const secret = "test-secret-padding-to-32-chars!"

	// Inject a session directly via context (bypassing requireSuperadmin) so
	// we can test the handler's own guard in isolation.
	payload := auth.SessionPayload{Sub: "caller-tenant", Role: "admin", Exp: nowUnix() + 3600}

	// settleHandler with nil pool/q — the scope guard runs before pool is used.
	h := settleHandler(nil, nil)

	// Request settling a *different* tenantId — should be rejected by the guard.
	body := `{"tenantId":"other-tenant","periodStart":0,"periodEnd":9999}`
	r := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	ctx := context.WithValue(r.Context(), ctxKeySession, payload)
	r = r.WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("scope guard: status=%d, want 403 (body: %s)", rec.Code, rec.Body)
	}
}
