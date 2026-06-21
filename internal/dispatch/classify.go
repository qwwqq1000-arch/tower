package dispatch

import "strings"

// ClassifyBanned reports whether a node response is a ban signal: the HTTP
// status is in banSignals, or the body contains any banKeyword (case-insensitive).
func ClassifyBanned(status int, body string, banSignals []int, banKeywords []string) bool {
	for _, s := range banSignals {
		if status == s {
			return true
		}
	}
	lb := strings.ToLower(body)
	for _, k := range banKeywords {
		if k != "" && strings.Contains(lb, strings.ToLower(k)) {
			return true
		}
	}
	return false
}
