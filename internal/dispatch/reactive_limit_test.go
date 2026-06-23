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

	limited, reset := parseLimitReset(body, now, kw)
	if !limited {
		t.Fatal("should detect the usage-limit response")
	}
	if want := time.Date(2026, 6, 23, 13, 50, 0, 0, time.UTC).UnixMilli(); reset != want {
		t.Fatalf("reset=%s want %s", time.UnixMilli(reset).UTC(), time.UnixMilli(want).UTC())
	}

	// A normal (non-limit) body is not detected.
	if l, _ := parseLimitReset(`{"type":"message","content":[]}`, now, kw); l {
		t.Fatal("non-limit body must not be detected as limited")
	}

	// CRITICAL: a transient rate_limit_error must NOT limit the account (was a false
	// trigger — error alone is not enough; a quota keyword must match).
	cpaBody := `{"type":"error","error":{"type":"rate_limit_error","message":"Rate limited. Please try again later."}}`
	if l, _ := parseLimitReset(cpaBody, now, kw); l {
		t.Fatal("transient rate_limit_error must NOT be treated as a quota limit")
	}

	// When the wall-clock reset already passed today, resolve to the next day.
	lateNow := time.Date(2026, 6, 23, 14, 0, 0, 0, time.UTC).UnixMilli()
	if _, r := parseLimitReset(body, lateNow, kw); r != time.Date(2026, 6, 24, 13, 50, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("passed-time reset=%s want next-day 13:50", time.UnixMilli(r).UTC())
	}

	// 12am resolves to hour 0.
	if _, r := parseLimitReset("hit your limit resets 12:00am (UTC)", now, kw); time.UnixMilli(r).UTC().Hour() != 0 {
		t.Fatalf("12am should be hour 0, got %s", time.UnixMilli(r).UTC())
	}

	// Keyword matched but no parseable reset → 1h default.
	if l, r := parseLimitReset("you've hit your limit, try again later", now, kw); !l || r != now+60*60*1000 {
		t.Fatalf("keyword-without-reset should default to now+1h, got limited=%v reset=%d", l, r)
	}
}
