// Package telemetry polls dumb nodes and projects their reported quota/health
// onto the authoritative state engine.
package telemetry

import (
	"time"

	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
)

func windowClass(t string) (string, bool) {
	switch t {
	case "five_hour", "seven_day":
		return "all", true
	case "seven_day_opus":
		return "opus", true
	case "seven_day_sonnet":
		return "sonnet", true
	default:
		return "", false
	}
}

// LimitsFromQuota maps a profile's quota windows to model-class rate-limit
// deadlines. A window limits when rejected or utilization >= threshold; the
// deadline is its reset time (or now+defaultTTLMs when reset is past/unknown).
func LimitsFromQuota(p nodeclient.ProfileQuota, threshold float64, now, defaultTTLMs int64) map[string]int64 {
	limits := map[string]int64{}
	for _, w := range p.Windows {
		if w.Status != "rejected" && w.Utilization < threshold {
			continue
		}
		class, ok := windowClass(w.Type)
		if !ok {
			continue
		}
		until := w.ResetsAt
		if until <= now {
			until = now + defaultTTLMs
		}
		// Keep the latest (max) deadline if multiple windows map to one class.
		if cur, exists := limits[class]; !exists || until > cur {
			limits[class] = until
		}
	}
	return limits
}

// CpaWindow is one CPA (Anthropic OAuth) rate-limit window. Unlike the meridian
// quota windows, Utilization is a percentage (0–100) and ResetsAt is an RFC3339
// timestamp string (or empty when unknown).
type CpaWindow struct {
	Type        string
	Utilization float64
	ResetsAt    string
}

// LimitsFromCpaQuota projects a CPA account's usage windows onto model-class
// rate-limit deadlines, mirroring LimitsFromQuota so CPA accounts rotate out of
// dispatch on the same QuotaRotateThreshold as meridian accounts. The CPA
// utilization is a percentage, so it is normalized to a 0–1 fraction before
// comparison; ResetsAt is parsed as RFC3339, falling back to now+defaultTTLMs
// when absent, unparseable, or already in the past.
func LimitsFromCpaQuota(windows []CpaWindow, threshold float64, now, defaultTTLMs int64) map[string]int64 {
	limits := map[string]int64{}
	for _, w := range windows {
		if w.Utilization/100 < threshold {
			continue
		}
		class, ok := windowClass(w.Type)
		if !ok {
			continue
		}
		until := now + defaultTTLMs
		if w.ResetsAt != "" {
			if t, err := time.Parse(time.RFC3339, w.ResetsAt); err == nil {
				if ms := t.UnixMilli(); ms > now {
					until = ms
				}
			}
		}
		// Keep the latest (max) deadline if multiple windows map to one class.
		if cur, exists := limits[class]; !exists || until > cur {
			limits[class] = until
		}
	}
	return limits
}

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
