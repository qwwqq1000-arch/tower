package dispatch

import (
	"strings"

	"github.com/qwwqq1000-arch/tower/internal/policy"
)

// permanentNo1M is the sentinel no_1m_until used when recovery is disabled
// (No1MRecoveryMs <= 0): a far-future timestamp that never elapses in practice.
const permanentNo1M int64 = 1 << 62

// modelSupports1M reports whether the model is in the LongContextSupportedModels
// whitelist (substring match). If the whitelist is empty, all models are assumed
// to support 1M (backwards compat).
func modelSupports1M(model string, cfg policy.Config) bool {
	if len(cfg.LongContextSupportedModels) == 0 {
		return true
	}
	lm := strings.ToLower(model)
	for _, sm := range cfg.LongContextSupportedModels {
		if sm != "" && strings.Contains(lm, strings.ToLower(sm)) {
			return true
		}
	}
	return false
}

// isLongContextRequest reports whether a request should be treated as 1M / long-context.
// A request is long-context when (body exceeds token threshold OR model matches a marker)
// AND the model actually supports 1M (is in SupportedModels). Without the model check,
// a Sonnet request with a big body would wrongly trigger the gate and mark all accounts
// as no_1m even though Sonnet simply doesn't support 1M.
func isLongContextRequest(model string, body []byte, cfg policy.Config) bool {
	triggered := false
	if cfg.LongContextTokenThreshold > 0 && len(body)/4 > cfg.LongContextTokenThreshold {
		triggered = true
	}
	if !triggered {
		lm := strings.ToLower(model)
		for _, mk := range cfg.LongContextModelMarkers {
			if mk != "" && strings.Contains(lm, strings.ToLower(mk)) {
				triggered = true
				break
			}
		}
	}
	if !triggered {
		return false
	}
	return modelSupports1M(model, cfg)
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
