package dispatch

import (
	"crypto/sha256"
	"encoding/binary"
)

// deviceShuffle returns a deterministic reordering of keys based on the
// device_id hash. Each device always gets the same ordering, so it
// consistently routes to the same first-choice account. When that account is
// busy/limited, the failover list is also device-specific, spreading devices
// across different failover paths.
func deviceShuffle(keys []string, deviceID string) []string {
	if len(keys) <= 1 {
		return keys
	}
	h := sha256.Sum256([]byte(deviceID))
	seed := binary.LittleEndian.Uint64(h[:8])

	out := make([]string, len(keys))
	copy(out, keys)

	// Fisher-Yates shuffle with deterministic seed.
	for i := len(out) - 1; i > 0; i-- {
		seed = seed*6364136223846793005 + 1442695040888963407 // LCG
		j := int(seed>>33) % (i + 1)
		out[i], out[j] = out[j], out[i]
	}
	return out
}
