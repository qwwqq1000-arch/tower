package policy

import (
	"fmt"
	"sort"
	"testing"
)

func TestLogNormalMedianApproxP50(t *testing.T) {
	const p50, p95 = 2000.0, 5000.0
	var samples []float64
	for i := 0; i < 4000; i++ {
		samples = append(samples, SampleLogNormal(p50, p95, fmt.Sprintf("k%d", i)))
	}
	sort.Float64s(samples)
	med := samples[len(samples)/2]
	if med < p50*0.7 || med > p50*1.4 {
		t.Fatalf("中位数 %v 偏离 p50 %v", med, p50)
	}
	// 应有显著尾部(p95 附近有样本)
	if samples[len(samples)*94/100] < p50 {
		t.Fatalf("无右尾")
	}
}
