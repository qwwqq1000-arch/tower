package policy

import "testing"

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
