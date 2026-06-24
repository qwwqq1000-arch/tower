package policy

import "hash/fnv"

// RangeF 是一个 [Min,Max] 浮点区间。Resolve 用 accountID+salt 做种子,
// 在区间内取一个稳定值(同号同 salt 跨重启恒定),用于"每个号表现成区间内
// 某个固定的人"。salt 区分不同旋钮,避免同号所有区间取同一分位。
type RangeF struct {
	Min float64 `json:"Min"`
	Max float64 `json:"Max"`
}

func seedFrac(accountID, salt string) float64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(accountID + "|" + salt))
	return float64(h.Sum64()>>11) / float64(uint64(1)<<53) // frac in [0,1), fine resolution
}

func (r RangeF) Resolve(accountID, salt string) float64 {
	if r.Max <= r.Min {
		return r.Min
	}
	return r.Min + seedFrac(accountID, salt)*(r.Max-r.Min)
}

// RangeI 同理,整数(时长 ms / 次数 / RPM 等)。Resolve 返回闭区间 [Min,Max] 内的稳定值。
type RangeI struct {
	Min int64 `json:"Min"`
	Max int64 `json:"Max"`
}

func (r RangeI) Resolve(accountID, salt string) int64 {
	if r.Max <= r.Min {
		return r.Min
	}
	span := r.Max - r.Min
	v := r.Min + int64(seedFrac(accountID, salt)*float64(span+1)) // +1 so Max is reachable
	if v > r.Max {
		v = r.Max
	}
	return v
}
