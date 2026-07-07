package policy

import (
	"fmt"
	"testing"
)

// TestWindowSaltChangesCapPerWindow verifies that window-based salts produce
// different cap values across windows while remaining stable within a window.
func TestWindowSaltChangesCapPerWindow(t *testing.T) {
	r := RangeF{Min: 100, Max: 150}

	// Same window index → same cap (stable within a window).
	w1a := r.Resolve("k", fmt.Sprintf("spend5h:%d", 1))
	w1b := r.Resolve("k", fmt.Sprintf("spend5h:%d", 1))
	if w1a != w1b {
		t.Fatalf("within-window instability: %v != %v", w1a, w1b)
	}
	if w1a < 100 || w1a > 150 {
		t.Fatalf("window 1 cap out of range: %v", w1a)
	}

	// Different window index → different cap for at least some keys.
	w2 := r.Resolve("k", fmt.Sprintf("spend5h:%d", 2))
	if w2 < 100 || w2 > 150 {
		t.Fatalf("window 2 cap out of range: %v", w2)
	}

	// Over a set of keys, at least one pair should differ across windows.
	anyDiffer := false
	for i := 0; i < 20; i++ {
		key := fmt.Sprintf("key_%d", i)
		va := r.Resolve(key, fmt.Sprintf("spend5h:%d", 1))
		vb := r.Resolve(key, fmt.Sprintf("spend5h:%d", 2))
		if va != vb {
			anyDiffer = true
		}
	}
	if !anyDiffer {
		t.Fatal("window-index salt change should produce at least one different cap across 20 keys")
	}
}

func TestRangeResolveDeterministicAndInBounds(t *testing.T) {
	r := RangeF{Min: 100, Max: 200}
	a := r.Resolve("acc_123", "spend5h")
	b := r.Resolve("acc_123", "spend5h")
	if a != b {
		t.Fatalf("不稳定:%v != %v", a, b)
	}
	if a < 100 || a > 200 {
		t.Fatalf("越界:%v", a)
	}
	// 不同 salt 应通常给不同值(避免同号所有区间同分位)
	if r.Resolve("acc_123", "rpm") == a {
		t.Logf("提示:salt 区分弱,但非致命")
	}
	// 不同账号通常不同
	if r.Resolve("acc_999", "spend5h") == a {
		t.Logf("提示:账号区分弱")
	}
}

func TestRangeResolveDegenerate(t *testing.T) {
	if got := (RangeF{Min: 5, Max: 5}).Resolve("x", "y"); got != 5 {
		t.Fatalf("相等区间应=Min,得 %v", got)
	}
	if got := (RangeF{Min: 9, Max: 1}).Resolve("x", "y"); got != 9 {
		t.Fatalf("Max<Min 应=Min,得 %v", got)
	}
	if got := (RangeI{Min: 3, Max: 10}).Resolve("acc", "burst"); got < 3 || got > 10 {
		t.Fatalf("RangeI 越界:%v", got)
	}
}

func TestRangeIReachesMax(t *testing.T) {
	r := RangeI{Min: 0, Max: 5}
	minReached := false
	maxReached := false

	for i := 0; i < 1000; i++ {
		accountID := fmt.Sprintf("acc_%d", i)
		result := r.Resolve(accountID, "test_salt")

		if result < r.Min || result > r.Max {
			t.Fatalf("结果越界:%d 不在 [%d,%d]", result, r.Min, r.Max)
		}

		if result == r.Min {
			minReached = true
		}
		if result == r.Max {
			maxReached = true
		}
	}

	if !minReached {
		t.Errorf("Min(%d) 从未被达到", r.Min)
	}
	if !maxReached {
		t.Errorf("Max(%d) 从未被达到", r.Max)
	}
}
