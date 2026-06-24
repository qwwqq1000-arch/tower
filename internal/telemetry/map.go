// Package telemetry polls dumb nodes and projects their reported quota/health
// onto the authoritative state engine.
package telemetry

import (
	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
)

// OfflineFromHealth reports whether a node/account should be treated as offline.
// Deprecated: prefer OfflineForProfile when per-profile quota data is available.
func OfflineFromHealth(h nodeclient.Health, healthErr error) bool {
	if healthErr != nil {
		return true
	}
	if !h.Auth.LoggedIn {
		return true
	}
	return h.Status == "unhealthy"
}

// OfflineForProfile derives per-account offline status from the individual
// profile's IsActive field when the node is reachable. This prevents a
// logged-out "current" profile from marking all sibling profiles offline.
//
// Rules:
//   - If healthErr != nil (network/node down) → all profiles are offline.
//   - If h.Status == "unhealthy" → all profiles are offline.
//   - If !foundInQuota (profile absent from QuotaAll response) → offline.
//   - Otherwise offline == !pq.IsActive (per-profile auth/session state).
func OfflineForProfile(h nodeclient.Health, healthErr error, pq nodeclient.ProfileQuota, foundInQuota bool) bool {
	if healthErr != nil {
		return true
	}
	if h.Status == "unhealthy" {
		return true
	}
	if !foundInQuota {
		return true
	}
	return !pq.IsActive
}
