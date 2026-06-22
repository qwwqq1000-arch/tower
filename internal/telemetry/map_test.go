package telemetry

import (
	"errors"
	"testing"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
)

func TestLimitsFromQuota(t *testing.T) {
	p := nodeclient.ProfileQuota{ID: "default", Windows: []nodeclient.QuotaWindow{
		{Type: "five_hour", Status: "allowed", Utilization: 0.5, ResetsAt: 9999},
		{Type: "seven_day_opus", Status: "rejected", Utilization: 1.0, ResetsAt: 5000},
		{Type: "seven_day_sonnet", Status: "allowed", Utilization: 0.97, ResetsAt: 6000}, // >= threshold 0.95
	}}
	lim := LimitsFromQuota(p, 0.95, 1000, 3600000)
	if _, ok := lim["all"]; ok {
		t.Fatal("five_hour allowed+low util → no all-limit")
	}
	if lim["opus"] != 5000 {
		t.Fatalf("opus limit=%d, want 5000 (rejected)", lim["opus"])
	}
	if lim["sonnet"] != 6000 {
		t.Fatalf("sonnet limit=%d, want 6000 (util>=threshold)", lim["sonnet"])
	}
}

func TestLimitsFromQuota_ResetInPastUsesDefault(t *testing.T) {
	p := nodeclient.ProfileQuota{Windows: []nodeclient.QuotaWindow{
		{Type: "five_hour", Status: "rejected", ResetsAt: 0},
	}}
	lim := LimitsFromQuota(p, 0.95, 1000, 1000)
	if lim["all"] != 2000 { // now(1000)+ttl(1000)
		t.Fatalf("all limit=%d, want 2000 (default ttl)", lim["all"])
	}
}

func TestLimitsFromCpaQuota(t *testing.T) {
	now := int64(1_000_000)
	reset5h := time.UnixMilli(now + 7_200_000).UTC().Format(time.RFC3339) // future
	windows := []CpaWindow{
		{Type: "five_hour", Utilization: 95, ResetsAt: reset5h}, // 95% >= threshold 90%
		{Type: "seven_day", Utilization: 10, ResetsAt: ""},      // low → not limited
	}
	lim := LimitsFromCpaQuota(windows, 0.9, now, 3_600_000)
	if len(lim) == 0 {
		t.Fatal("95% util with threshold 0.9 → expected a non-empty limits map")
	}
	got, ok := lim["all"]
	if !ok {
		t.Fatalf("expected 'all' class limited, got map %v", lim)
	}
	if got <= now {
		t.Fatalf("expected a future deadline, got %d (now=%d)", got, now)
	}
	if got != now+7_200_000 {
		t.Fatalf("expected deadline from resets_at (%d), got %d", now+7_200_000, got)
	}
}

func TestLimitsFromCpaQuota_BelowThreshold(t *testing.T) {
	windows := []CpaWindow{
		{Type: "five_hour", Utilization: 50, ResetsAt: ""},
		{Type: "seven_day", Utilization: 89, ResetsAt: ""}, // just under 90%
	}
	lim := LimitsFromCpaQuota(windows, 0.9, 1000, 3600000)
	if len(lim) != 0 {
		t.Fatalf("all windows below threshold → empty map, got %v", lim)
	}
}

func TestLimitsFromCpaQuota_PerClass(t *testing.T) {
	windows := []CpaWindow{
		{Type: "seven_day_opus", Utilization: 100, ResetsAt: ""},   // reset unknown → default ttl
		{Type: "seven_day_sonnet", Utilization: 96, ResetsAt: ""},  // >= threshold
	}
	lim := LimitsFromCpaQuota(windows, 0.95, 1000, 1000)
	if lim["opus"] != 2000 { // now(1000)+ttl(1000)
		t.Fatalf("opus limit=%d, want 2000 (default ttl)", lim["opus"])
	}
	if lim["sonnet"] != 2000 {
		t.Fatalf("sonnet limit=%d, want 2000 (default ttl)", lim["sonnet"])
	}
	if _, ok := lim["all"]; ok {
		t.Fatalf("no all-class window → no all limit, got %v", lim)
	}
}

func TestOfflineFromHealth(t *testing.T) {
	if !OfflineFromHealth(nodeclient.Health{}, errors.New("conn refused")) {
		t.Fatal("health error → offline")
	}
	if !OfflineFromHealth(nodeclient.Health{Status: "healthy", Auth: nodeclient.HealthAuth{LoggedIn: false}}, nil) {
		t.Fatal("not logged in → offline")
	}
	if OfflineFromHealth(nodeclient.Health{Status: "healthy", Auth: nodeclient.HealthAuth{LoggedIn: true}}, nil) {
		t.Fatal("healthy + logged in → online")
	}
}
