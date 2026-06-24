package dispatch

import (
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/policy"
)

func TestResolveOrderAccountWins(t *testing.T) {
	base := policy.Defaults()
	g := policy.Patch{MaxConcurrent: ptrInt(5)}  // 全局
	o := policy.Patch{MaxConcurrent: ptrInt(7)}  // 租户
	a := policy.Patch{MaxConcurrent: ptrInt(9)}  // 账户
	got := policy.Resolve(base, g, o, a) // 顺序:全局→租户→账户,后者赢
	if got.MaxConcurrent != 9 {
		t.Fatalf("账户层应赢,得 %d", got.MaxConcurrent)
	}
	got2 := policy.Resolve(base, g, o) // 无账户层 → 租户值
	if got2.MaxConcurrent != 7 {
		t.Fatalf("无账户层应=租户7,得 %d", got2.MaxConcurrent)
	}
}

func ptrInt(i int) *int { return &i }
