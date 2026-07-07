package state

import "testing"

func TestSpendWindowAccumulateAndRoll(t *testing.T) {
	a := NewAccount(3)
	const win = int64(18000000) // 5h
	// t=0 累加 30
	a.AddSpend(0, 30, win)
	if got := a.SpendInWindow(0, win); got != 30 {
		t.Fatalf("窗口内应=30,得 %v", got)
	}
	// t=win-1 再加 30 → 窗口内 60(都在窗口)
	a.AddSpend(win-1, 30, win)
	if got := a.SpendInWindow(win-1, win); got != 60 {
		t.Fatalf("应=60,得 %v", got)
	}
	// t=win+1 → 第一笔(t=0)已滚出,只剩第二笔 30
	if got := a.SpendInWindow(win+1, win); got != 30 {
		t.Fatalf("滚动后应=30,得 %v", got)
	}
}
