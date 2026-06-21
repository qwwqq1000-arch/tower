package telemetry

import (
	"errors"
	"testing"

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
