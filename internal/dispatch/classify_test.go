package dispatch

import "testing"

func TestClassifyBanned(t *testing.T) {
	sigs := []int{401, 403}
	kw := []string{"account_disabled", "suspended"}
	if !ClassifyBanned(401, "", sigs, kw) {
		t.Fatal("401 should be banned")
	}
	if !ClassifyBanned(200, `{"error":"ACCOUNT_DISABLED here"}`, sigs, kw) {
		t.Fatal("keyword (case-insensitive) should be banned")
	}
	if ClassifyBanned(200, "all good", sigs, kw) {
		t.Fatal("clean 200 should not be banned")
	}
	if ClassifyBanned(500, "server error", sigs, kw) {
		t.Fatal("500 not in signals, no keyword → not banned")
	}
}
