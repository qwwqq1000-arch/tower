package dispatch

import (
	"testing"
	"time"
)

func TestParseLimitReset(t *testing.T) {
	now := time.Date(2026, 6, 23, 10, 0, 0, 0, time.UTC).UnixMilli()
	// The exact new-meridian usage-limit response (captured from a rate-limited account).
	body := `{"type":"error","error":{"type":"api_error","message":"Claude Code returned an error result: You've hit your limit · resets 1:50pm (UTC)"}}`

	limited, reset := parseLimitReset(body, now)
	if !limited {
		t.Fatal("should detect the usage-limit response")
	}
	if want := time.Date(2026, 6, 23, 13, 50, 0, 0, time.UTC).UnixMilli(); reset != want {
		t.Fatalf("reset=%s want %s", time.UnixMilli(reset).UTC(), time.UnixMilli(want).UTC())
	}

	// A normal (non-limit) body is not detected.
	if l, _ := parseLimitReset(`{"type":"message","content":[]}`, now); l {
		t.Fatal("non-limit body must not be detected as limited")
	}

	// When the wall-clock reset already passed today, resolve to the next day.
	lateNow := time.Date(2026, 6, 23, 14, 0, 0, 0, time.UTC).UnixMilli()
	if _, r := parseLimitReset(body, lateNow); r != time.Date(2026, 6, 24, 13, 50, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("passed-time reset=%s want next-day 13:50", time.UnixMilli(r).UTC())
	}

	// 12am resolves to hour 0; 12pm stays hour 12.
	if _, r := parseLimitReset("hit your limit resets 12:00am (UTC)", now); time.UnixMilli(r).UTC().Hour() != 0 {
		t.Fatalf("12am should be hour 0, got %s", time.UnixMilli(r).UTC())
	}

	// Limited but no parseable reset → falls back to ~1h.
	if l, r := parseLimitReset("you've hit your limit, try again later", now); !l || r != now+60*60*1000 {
		t.Fatalf("unparseable reset should default to now+1h, got limited=%v reset=%d", l, r)
	}
}
