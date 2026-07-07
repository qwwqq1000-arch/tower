package telemetry

import (
	"errors"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
)

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

// TestOfflineForProfile verifies that per-account offline is derived from the
// profile's IsActive field when the node is reachable, so a logged-out active
// profile does not knock sibling active profiles offline.
func TestOfflineForProfile(t *testing.T) {
	healthyNode := nodeclient.Health{Status: "healthy", Auth: nodeclient.HealthAuth{LoggedIn: false}}

	activeProfile := nodeclient.ProfileQuota{ID: "active", IsActive: true}
	inactiveProfile := nodeclient.ProfileQuota{ID: "inactive", IsActive: false}

	// Node is up (no healthErr, status=healthy) but node-level auth.loggedIn is
	// false (e.g., the "current" profile is logged out). A profile that QuotaAll
	// reports IsActive=true must stay ONLINE.
	if OfflineForProfile(healthyNode, nil, activeProfile, true) {
		t.Fatal("active profile on healthy node → must be online, not offline")
	}

	// A profile that QuotaAll reports IsActive=false must be OFFLINE.
	if !OfflineForProfile(healthyNode, nil, inactiveProfile, true) {
		t.Fatal("inactive profile → must be offline")
	}

	// When a profile is absent from QuotaAll (foundInQuota=false), treat as offline.
	if !OfflineForProfile(healthyNode, nil, nodeclient.ProfileQuota{}, false) {
		t.Fatal("profile absent from QuotaAll → must be offline")
	}

	// When health fetch fails (node down) ALL profiles are offline, even active ones.
	if !OfflineForProfile(nodeclient.Health{}, errors.New("connection refused"), activeProfile, true) {
		t.Fatal("health error (node down) → active profile must still be offline")
	}

	// Node unhealthy → all offline.
	if !OfflineForProfile(nodeclient.Health{Status: "unhealthy"}, nil, activeProfile, true) {
		t.Fatal("unhealthy node → active profile must be offline")
	}
}
