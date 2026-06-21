// Package reconcile enforces desired node SDK settings (self-healing drift).
package reconcile

import "fmt"

// Diff returns, per adapter, the desired fields whose actual value is missing or
// different — i.e. exactly what must be PATCHed onto the node.
func Diff(desired, actual map[string]map[string]any) map[string]map[string]any {
	out := map[string]map[string]any{}
	for adapter, want := range desired {
		have := actual[adapter]
		patch := map[string]any{}
		for k, wv := range want {
			hv, ok := have[k]
			if !ok || fmt.Sprintf("%v", hv) != fmt.Sprintf("%v", wv) {
				patch[k] = wv
			}
		}
		if len(patch) > 0 {
			out[adapter] = patch
		}
	}
	return out
}
