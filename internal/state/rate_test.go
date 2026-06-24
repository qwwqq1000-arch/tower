package state

import "testing"

func TestRateWindowRPM(t *testing.T) {
	a := NewAccount(3)
	for i := 0; i < 8; i++ {
		a.RecordReq(int64(i)) // t=0..7 ms, all within 1-minute window
	}
	if got := a.ReqsInWindow(100, 60000); got != 8 {
		t.Fatalf("窗内应=8,得 %d", got)
	}
	// At t=70000, the window [70000-60000, 70000] = [10000, 70000] — none of 0..7 are in it
	if got := a.ReqsInWindow(70000, 60000); got != 0 {
		t.Fatalf("1min 后应滚出=0,得 %d", got)
	}
}

func TestRateWindowPrune(t *testing.T) {
	a := NewAccount(1)
	const dayMs = int64(86400000)
	// Record a request at t=0, then one at dayMs+1 (prune should drop the first).
	a.RecordReq(0)
	a.RecordReq(dayMs + 1)
	// Window: from now=dayMs+1, windowMs=dayMs: [1, dayMs+1]. t=0 is NOT in [1, dayMs+1].
	if got := a.ReqsInWindow(dayMs+1, dayMs); got != 1 {
		t.Fatalf("1d 后老记录应已剪枝,窗内应=1,得 %d", got)
	}
}

func TestRateWindowRPH(t *testing.T) {
	a := NewAccount(1)
	const hrMs = int64(3600000)
	// Record 5 requests at t=0..4.
	for i := 0; i < 5; i++ {
		a.RecordReq(int64(i))
	}
	// All 5 are in the 1-hour window from t=hrMs.
	if got := a.ReqsInWindow(hrMs, hrMs); got != 5 {
		t.Fatalf("1h 内应=5,得 %d", got)
	}
	// From t=hrMs+5, none of 0..4 are in [5, hrMs+5].
	if got := a.ReqsInWindow(hrMs+5, hrMs); got != 0 {
		t.Fatalf("1h 后应滚出=0,得 %d", got)
	}
}
