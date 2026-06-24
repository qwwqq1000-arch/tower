package dispatch

import (
	"testing"
	"time"
)

// toMs converts a Beijing local time string "HH:MM" on a fixed date to Unix milliseconds.
func beijingMs(t *testing.T, hhmm string) int64 {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	// Use a fixed date so the test is deterministic.
	ts, err := time.ParseInLocation("2006-01-02 15:04", "2026-06-22 "+hhmm, loc)
	if err != nil {
		t.Fatalf("parse time %q: %v", hhmm, err)
	}
	return ts.UnixMilli()
}

func TestSlotActiveNow(t *testing.T) {
	tests := []struct {
		name     string
		start    int // minute-of-day
		end      int
		time     string // HH:MM Beijing
		wantActive bool
	}{
		// Always-active cases (start == end or [0,1440))
		{"always_active_zero_zero", 0, 0, "12:00", true},
		{"always_active_full_day", 0, 1440, "23:59", true},

		// Normal window (start < end): 09:00 – 18:00
		{"normal_inside", 9 * 60, 18 * 60, "14:30", true},
		{"normal_at_start", 9 * 60, 18 * 60, "09:00", true},
		{"normal_at_end_exclusive", 9 * 60, 18 * 60, "18:00", false},
		{"normal_before", 9 * 60, 18 * 60, "08:59", false},
		{"normal_after", 9 * 60, 18 * 60, "20:00", false},

		// Overnight window (start > end): 22:00 – 06:00
		{"overnight_after_start", 22 * 60, 6 * 60, "23:30", true},
		{"overnight_midnight", 22 * 60, 6 * 60, "00:00", true},
		{"overnight_before_end", 22 * 60, 6 * 60, "05:59", true},
		{"overnight_at_start", 22 * 60, 6 * 60, "22:00", true},
		{"overnight_at_end_exclusive", 22 * 60, 6 * 60, "06:00", false},
		{"overnight_midday_inactive", 22 * 60, 6 * 60, "12:00", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nowMs := beijingMs(t, tt.time)
			got := slotActiveNow(tt.start, tt.end, nowMs, "Asia/Shanghai")
			if got != tt.wantActive {
				t.Errorf("slotActiveNow(%d, %d, %q) = %v, want %v",
					tt.start, tt.end, tt.time, got, tt.wantActive)
			}
		})
	}
}
