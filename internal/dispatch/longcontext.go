package dispatch

import (
	"strings"

	"github.com/qwwqq1000-arch/tower/internal/policy"
)

// permanentNo1M is the sentinel no_1m_until used when recovery is disabled
// (No1MRecoveryMs <= 0): a far-future timestamp that never elapses in practice.
const permanentNo1M int64 = 1 << 62

// isLongContextRequest reports whether a request should be treated as 1M / long-context:
// estimated input tokens (len(body)/4) over the threshold, OR the model string contains
// any configured marker. Both inputs are config-driven (not hardcoded).
func isLongContextRequest(model string, body []byte, cfg policy.Config) bool {
	if cfg.LongContextTokenThreshold > 0 && len(body)/4 > cfg.LongContextTokenThreshold {
		return true
	}
	lm := strings.ToLower(model)
	for _, mk := range cfg.LongContextModelMarkers {
		if mk != "" && strings.Contains(lm, strings.ToLower(mk)) {
			return true
		}
	}
	return false
}

// isExtraUsageNo1M reports whether a failed response is the extra-usage 400 that means
// "this account does not support 1M / extra usage". Gated on status code first, then keyword.
func isExtraUsageNo1M(status int, body string, cfg policy.Config) bool {
	okCode := false
	for _, c := range cfg.ExtraUsageStatusCodes {
		if c == status {
			okCode = true
			break
		}
	}
	if !okCode {
		return false
	}
	lb := strings.ToLower(body)
	for _, kw := range cfg.ExtraUsageKeywords {
		if kw != "" && strings.Contains(lb, strings.ToLower(kw)) {
			return true
		}
	}
	return false
}
