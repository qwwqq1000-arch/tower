package nodeclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTelemetrySummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/telemetry/summary" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "k1" {
			t.Errorf("missing api key header")
		}
		_, _ = w.Write([]byte(`{"windowMs":3600000,"totalRequests":13,"errorCount":0,"requestsPerMinute":2.6,` +
			`"ttfb":{"p50":3293,"p95":13314},"totalDuration":{"p50":75517,"p95":194480},` +
			`"byModel":{"claude-opus-4-8":{"count":11,"avgTotalMs":83443}},` +
			`"tokenUsage":{"totalInputTokens":28,"totalOutputTokens":55116,"totalCacheReadTokens":1205355,"totalCacheCreationTokens":281367,"avgCacheHitRate":0.7}}`))
	}))
	defer srv.Close()

	ts, err := New(srv.URL, "k1").TelemetrySummary(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ts.TotalRequests != 13 {
		t.Fatalf("TotalRequests = %d, want 13", ts.TotalRequests)
	}
	if ts.RequestsPerMinute != 2.6 {
		t.Fatalf("RequestsPerMinute = %f, want 2.6", ts.RequestsPerMinute)
	}
	m, ok := ts.ByModel["claude-opus-4-8"]
	if !ok {
		t.Fatal("ByModel missing claude-opus-4-8")
	}
	if m.Count != 11 {
		t.Fatalf("ByModel[claude-opus-4-8].Count = %d, want 11", m.Count)
	}
	if ts.TokenUsage.AvgCacheHitRate != 0.7 {
		t.Fatalf("TokenUsage.AvgCacheHitRate = %f, want 0.7", ts.TokenUsage.AvgCacheHitRate)
	}
}
