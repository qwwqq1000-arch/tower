package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/dispatch"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

func TestDispatchHandler_NoKey401(t *testing.T) {
	store := state.NewStore(func() int64 { return 0 }, func(a, b int64) int64 { return a })
	svc := dispatch.NewService(nil, store, policy.Defaults(), func() int64 { return 0 }, nil)
	h := NewRouter(nil, "test-secret-padding-to-32-chars!", svc, nil, false, nil, "")
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"opus"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("no dk_ key → status=%d, want 401", rec.Code)
	}
}
