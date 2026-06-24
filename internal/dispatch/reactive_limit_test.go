package dispatch

import (
	"testing"
	"time"
)

func TestParseLimitReset(t *testing.T) {
	now := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC).UnixMilli()
	kw := []string{"hit your limit", "usage limit"}
	// The exact new-meridian usage-limit response (captured from a rate-limited account).
	body := `{"type":"error","error":{"type":"api_error","message":"Claude Code returned an error result: You've hit your limit · resets 1:50pm (UTC)"}}`

	const defaultResetMs = int64(300000)

	limited, reset := parseLimitReset(500, body, now, kw, nil, defaultResetMs)
	if !limited {
		t.Fatal("should detect the usage-limit response")
	}
	if want := time.Date(2026, 6, 23, 13, 50, 0, 0, time.UTC).UnixMilli(); reset != want {
		t.Fatalf("reset=%s want %s", time.UnixMilli(reset).UTC(), time.UnixMilli(want).UTC())
	}

	// A normal (non-limit) body is not detected.
	if l, _ := parseLimitReset(500, `{"type":"message","content":[]}`, now, kw, nil, defaultResetMs); l {
		t.Fatal("non-limit body must not be detected as limited")
	}

	// CRITICAL: a transient rate_limit_error must NOT limit the account (was a false
	// trigger — error alone is not enough; a quota keyword must match).
	cpaBody := `{"type":"error","error":{"type":"rate_limit_error","message":"Rate limited. Please try again later."}}`
	if l, _ := parseLimitReset(500, cpaBody, now, kw, nil, defaultResetMs); l {
		t.Fatal("transient rate_limit_error must NOT be treated as a quota limit")
	}

	// When the wall-clock reset already passed today, resolve to the next day.
	lateNow := time.Date(2026, 6, 23, 14, 0, 0, 0, time.UTC).UnixMilli()
	if _, r := parseLimitReset(500, body, lateNow, kw, nil, defaultResetMs); r != time.Date(2026, 6, 24, 13, 50, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("passed-time reset=%s want next-day 13:50", time.UnixMilli(r).UTC())
	}

	// 12am resolves to hour 0.
	if _, r := parseLimitReset(500, "hit your limit resets 12:00am (UTC)", now, kw, nil, defaultResetMs); time.UnixMilli(r).UTC().Hour() != 0 {
		t.Fatalf("12am should be hour 0, got %s", time.UnixMilli(r).UTC())
	}

	// Keyword matched but no parseable reset → defaultResetMs (5min = 300000ms).
	if l, r := parseLimitReset(500, "you've hit your limit, try again later", now, kw, nil, defaultResetMs); !l || r != now+defaultResetMs {
		t.Fatalf("keyword-without-reset should default to now+defaultResetMs, got limited=%v reset=%d", l, r)
	}

	// CPA's "cooling down" wording IS a limit when that keyword is configured (no reset
	// → defaultResetMs). CPA returns it differently from the subscription "hit your limit".
	kwCpa := []string{"hit your limit", "usage limit", "cooling down"}
	cpaCooling := `{"type":"error","error":{"type":"rate_limit_error","message":"All credentials for model claude-opus-4-7 are cooling down via provider claude"}}`
	if l, r := parseLimitReset(429, cpaCooling, now, kwCpa, nil, defaultResetMs); !l || r != now+defaultResetMs {
		t.Fatalf("CPA cooling-down should limit with defaultResetMs, got limited=%v reset=%d", l, r)
	}
	// ...but NOT when "cooling down" isn't in the keyword set (it's still a bare rate_limit_error).
	if l, _ := parseLimitReset(429, cpaCooling, now, kw, nil, defaultResetMs); l {
		t.Fatal("cooling-down must not limit when the keyword isn't configured")
	}

	// 7-day quota: the reset carries a DATE ("resets Jun 28 12:50pm (UTC)") — parse to
	// the absolute deadline (days away), not today's 12:50pm (limit-7d).
	weekly := `{"error":{"message":"You've hit your limit · resets Jun 28 12:50pm (UTC)"}}`
	if l, r := parseLimitReset(500, weekly, now, kw, nil, defaultResetMs); !l || r != time.Date(2026, 6, 28, 12, 50, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("7d date reset, got limited=%v reset=%s", l, time.UnixMilli(r).UTC())
	}

	// Status-code gate: a status not in the configured codes is NOT scanned, even when
	// the body would match a keyword (perf — avoids scanning every response).
	if l, _ := parseLimitReset(200, body, now, kw, []int{429, 500}, defaultResetMs); l {
		t.Fatal("status not in QuotaLimitStatusCodes must skip the scan")
	}
	if l, _ := parseLimitReset(429, cpaCooling, now, kwCpa, []int{429, 500}, defaultResetMs); !l {
		t.Fatal("status in codes should still scan + detect")
	}
}
