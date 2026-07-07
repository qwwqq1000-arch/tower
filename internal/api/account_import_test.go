package api

import "testing"

// TestBuildImportAccountParams_PointerOnly documents and locks in the
// pointer-only OAuth import design: Tower does not store raw OAuth tokens
// (those live on the node keyed by profile_id). OauthAccessEnc and
// OauthRefreshEnc MUST remain empty.
func TestBuildImportAccountParams_PointerOnly(t *testing.T) {
	params := buildImportAccountParams("acc_123", "owner_456", "user@example.com", 9999999, 1111111)
	if params.ID != "acc_123" {
		t.Errorf("ID = %q, want acc_123", params.ID)
	}
	if params.OwnerID != "owner_456" {
		t.Errorf("OwnerID = %q, want owner_456", params.OwnerID)
	}
	if params.Email != "user@example.com" {
		t.Errorf("Email = %q, want user@example.com", params.Email)
	}
	if params.OauthAccessEnc != "" {
		t.Errorf("OauthAccessEnc = %q, want empty (pointer-only: no node-held tokens to encrypt)", params.OauthAccessEnc)
	}
	if params.OauthRefreshEnc != "" {
		t.Errorf("OauthRefreshEnc = %q, want empty (pointer-only: no node-held tokens to encrypt)", params.OauthRefreshEnc)
	}
	if params.ExpiresAt != 9999999 {
		t.Errorf("ExpiresAt = %d, want 9999999", params.ExpiresAt)
	}
}
