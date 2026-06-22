package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/auth"
)

// TestRequireSameOrigin verifies that the CSRF guard rejects state-changing
// cookie-auth requests that lack the X-Requested-With: tower header, while
// passing through requests that carry it and unconditionally passing GET/HEAD.
//
// requireSession delegates to requireSameOrigin internally, so testing
// requireSession exercises the CSRF protection in exactly the same way it
// fires in production — no additional wrapper is needed.
func TestRequireSameOrigin(t *testing.T) {
	const secret = "test-secret-padding-to-32-chars!"

	reached := false
	next := func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	}

	// requireSession owns the CSRF check (via requireSameOrigin). Using it
	// directly here mirrors the production call-site in router.go.
	h := requireSession(secret, nil, next)

	sessionCookie := &http.Cookie{
		Name:  "tower_session",
		Value: auth.IssueSession(secret, "user1", "admin", 0, nowUnix(), 3600),
	}

	call := func(method, header string) int {
		reached = false
		r := httptest.NewRequest(method, "/api/admin/nodes", nil)
		r.AddCookie(sessionCookie)
		if header != "" {
			r.Header.Set("X-Requested-With", header)
		}
		rec := httptest.NewRecorder()
		h(rec, r)
		return rec.Code
	}

	// GET without header → passes (read-only, not a CSRF risk).
	if code := call("GET", ""); code != http.StatusOK {
		t.Fatalf("GET without header: code=%d, want 200", code)
	}

	// HEAD without header → passes.
	if code := call("HEAD", ""); code != http.StatusOK {
		t.Fatalf("HEAD without header: code=%d, want 200", code)
	}

	// POST without header → 403 (CSRF guard rejects forged cross-site form/fetch).
	if code := call("POST", ""); code != http.StatusForbidden {
		t.Fatalf("POST without header: code=%d, want 403", code)
	}
	if reached {
		t.Fatal("handler must not be reached on CSRF-rejected request")
	}

	// POST with wrong header value → 403.
	if code := call("POST", "XMLHttpRequest"); code != http.StatusForbidden {
		t.Fatalf("POST with wrong header: code=%d, want 403", code)
	}

	// POST with correct header → passes.
	if code := call("POST", "tower"); code != http.StatusOK {
		t.Fatalf("POST with X-Requested-With: tower: code=%d, want 200", code)
	}
	if !reached {
		t.Fatal("handler should be reached when CSRF header is correct")
	}

	// PUT without header → 403.
	if code := call("PUT", ""); code != http.StatusForbidden {
		t.Fatalf("PUT without header: code=%d, want 403", code)
	}

	// PATCH with correct header → passes.
	if code := call("PATCH", "tower"); code != http.StatusOK {
		t.Fatalf("PATCH with X-Requested-With: tower: code=%d, want 200", code)
	}

	// DELETE with correct header → passes.
	if code := call("DELETE", "tower"); code != http.StatusOK {
		t.Fatalf("DELETE with X-Requested-With: tower: code=%d, want 200", code)
	}
}
