package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz_OKWithoutDB(t *testing.T) {
	// nil pool → handler must still respond (degraded), never panic.
	h := NewRouter(nil)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (nil pool degraded)", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q", ct)
	}
}
