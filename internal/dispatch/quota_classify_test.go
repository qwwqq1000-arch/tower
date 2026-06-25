package dispatch

import (
	"testing"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/cpaclient"
)

func TestParseResetsAt(t *testing.T) {
	t.Run("valid RFC3339", func(t *testing.T) {
		ts := "2026-06-27T12:00:00Z"
		ms := parseResetsAt(ts)
		expected := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC).UnixMilli()
		if ms != expected {
			t.Errorf("got %d, want %d", ms, expected)
		}
	})
	t.Run("empty string", func(t *testing.T) {
		if got := parseResetsAt(""); got != 0 {
			t.Errorf("expected 0, got %d", got)
		}
	})
	t.Run("garbage", func(t *testing.T) {
		if got := parseResetsAt("not-a-date"); got != 0 {
			t.Errorf("expected 0, got %d", got)
		}
	})
	t.Run("RFC3339 with offset", func(t *testing.T) {
		ts := "2026-06-27T20:00:00+08:00"
		ms := parseResetsAt(ts)
		expected := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC).UnixMilli()
		if ms != expected {
			t.Errorf("got %d, want %d", ms, expected)
		}
	})
}

func TestPickQuotaLimit(t *testing.T) {
	now := time.Now().UnixMilli()
	const thresh = 99.0
	resetAt := time.Now().Add(5 * time.Hour).UTC().Format(time.RFC3339)

	t.Run("nil usage returns empty", func(t *testing.T) {
		reason, ms := pickQuotaLimit(nil, thresh, now)
		if reason != "" || ms != 0 {
			t.Errorf("expected ('','0'), got (%q,%d)", reason, ms)
		}
	})
	t.Run("no window exhausted", func(t *testing.T) {
		u := &cpaclient.Usage{
			FiveHour: &cpaclient.UsageWindow{Utilization: 50.0, ResetsAt: resetAt},
			SevenDay: &cpaclient.UsageWindow{Utilization: 30.0, ResetsAt: resetAt},
		}
		reason, _ := pickQuotaLimit(u, thresh, now)
		if reason != "" {
			t.Errorf("expected no limit, got %q", reason)
		}
	})
	t.Run("7d exhausted → reason is 7d (not 5h)", func(t *testing.T) {
		u := &cpaclient.Usage{
			FiveHour: &cpaclient.UsageWindow{Utilization: 99.5, ResetsAt: resetAt},
			SevenDay: &cpaclient.UsageWindow{Utilization: 100.0, ResetsAt: resetAt},
		}
		reason, recov := pickQuotaLimit(u, thresh, now)
		if reason != "7d" {
			t.Errorf("expected '7d', got %q", reason)
		}
		if recov <= 0 {
			t.Errorf("expected positive recoveryMs, got %d", recov)
		}
	})
	t.Run("only 5h exhausted → reason is 5h", func(t *testing.T) {
		u := &cpaclient.Usage{
			FiveHour: &cpaclient.UsageWindow{Utilization: 99.5, ResetsAt: resetAt},
			SevenDay: &cpaclient.UsageWindow{Utilization: 80.0, ResetsAt: resetAt},
		}
		reason, _ := pickQuotaLimit(u, thresh, now)
		if reason != "5h" {
			t.Errorf("expected '5h', got %q", reason)
		}
	})
	t.Run("exactly at threshold is exhausted", func(t *testing.T) {
		u := &cpaclient.Usage{
			FiveHour: &cpaclient.UsageWindow{Utilization: 99.0, ResetsAt: resetAt},
		}
		reason, _ := pickQuotaLimit(u, thresh, now)
		if reason != "5h" {
			t.Errorf("expected '5h' at threshold=99.0, got %q", reason)
		}
	})
	t.Run("below threshold not exhausted", func(t *testing.T) {
		u := &cpaclient.Usage{
			FiveHour: &cpaclient.UsageWindow{Utilization: 98.9, ResetsAt: resetAt},
		}
		reason, _ := pickQuotaLimit(u, thresh, now)
		if reason != "" {
			t.Errorf("expected no limit below threshold, got %q", reason)
		}
	})
	t.Run("empty ResetsAt uses fallback recovery time (7d)", func(t *testing.T) {
		u := &cpaclient.Usage{
			SevenDay: &cpaclient.UsageWindow{Utilization: 100.0, ResetsAt: ""},
		}
		reason, recov := pickQuotaLimit(u, thresh, now)
		if reason != "7d" {
			t.Errorf("expected '7d', got %q", reason)
		}
		expected := now + 7*24*3600*1000
		if recov != expected {
			t.Errorf("expected fallback 7d recovery %d, got %d", expected, recov)
		}
	})
	t.Run("empty ResetsAt uses fallback recovery time (5h)", func(t *testing.T) {
		u := &cpaclient.Usage{
			FiveHour: &cpaclient.UsageWindow{Utilization: 100.0, ResetsAt: ""},
		}
		reason, recov := pickQuotaLimit(u, thresh, now)
		if reason != "5h" {
			t.Errorf("expected '5h', got %q", reason)
		}
		expected := now + 5*3600*1000
		if recov != expected {
			t.Errorf("expected fallback 5h recovery %d, got %d", expected, recov)
		}
	})
	t.Run("nil FiveHour only SevenDay nil returns empty", func(t *testing.T) {
		u := &cpaclient.Usage{}
		reason, _ := pickQuotaLimit(u, thresh, now)
		if reason != "" {
			t.Errorf("expected empty, got %q", reason)
		}
	})
}
