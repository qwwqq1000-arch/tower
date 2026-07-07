package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/auth"
)

// TestRequirePerm enforces that requirePerm gates a route by the caller's
// seeded role permissions: a role lacking the capability gets 403, while a
// role holding it (or the superadmin wildcard) passes through.
func TestRequirePerm(t *testing.T) {
	const secret = "test-secret-padding-to-32-chars!"

	// Fake perm loader keyed by role, standing in for the DB-backed loadPerms.
	// Mirrors the seeded perms: superadmin holds the "*" wildcard; admin holds
	// the capability but is not IsAdmin scope; operator lacks the capability.
	perms := map[string][]string{
		"superadmin": {"*"},
		"admin":      {"nodes:read", "billing:settle"},
		"operator":   {"nodes:read"}, // lacks billing:settle
	}
	load := func(_ *http.Request, role string) []string { return perms[role] }

	reached := false
	next := func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}
	// q is nil: requireSession only checks signature/expiry (no epoch check).
	h := requirePerm(secret, nil, load, "billing:settle", next)

	call := func(role string) (int, bool) {
		reached = false
		r := httptest.NewRequest(http.MethodPost, "/api/admin/settle", nil)
		r.AddCookie(&http.Cookie{Name: "tower_session", Value: auth.IssueSession(secret, "u_"+role, role, 0, nowUnix(), 3600)})
		// CSRF header required on non-GET cookie-auth mutations.
		r.Header.Set("X-Requested-With", "tower")
		rec := httptest.NewRecorder()
		h(rec, r)
		return rec.Code, reached
	}

	// operator lacks billing:settle → 403, handler not reached.
	if code, ran := call("operator"); code != http.StatusForbidden || ran {
		t.Fatalf("operator: code=%d ran=%v, want 403 and not reached", code, ran)
	}
	// admin holds the capability but, on a global (non-owner) route, scope is
	// IsAdmin only for superadmin; rbac.Can therefore denies → 403. This is the
	// defense-in-depth behaviour: only superadmin clears the global gate.
	if code, ran := call("admin"); code != http.StatusForbidden || ran {
		t.Fatalf("admin: code=%d ran=%v, want 403 and not reached (global scope)", code, ran)
	}
	// superadmin wildcard → passes.
	if code, ran := call("superadmin"); code != http.StatusOK || !ran {
		t.Fatalf("superadmin: code=%d ran=%v, want 200 and reached", code, ran)
	}

	// no cookie → 401 (handled by requireSession underneath).
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/api/admin/settle", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no cookie: code=%d, want 401", rec.Code)
	}
}
