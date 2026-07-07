package dispatch

import "testing"

func TestChannelAllowsModel(t *testing.T) {
	tests := []struct {
		name      string
		allowlist string
		model     string
		want      bool
	}{
		// empty allowlist allows everything
		{"empty allows opus", "", "claude-opus-4-8", true},
		{"empty allows haiku", "", "claude-haiku-3-5", true},
		{"empty allows sonnet", "", "claude-sonnet-3-5", true},
		{"empty allows unknown", "", "gpt-4", true},

		// single family
		{"haiku allowlist blocks opus", "haiku", "claude-opus-4-8", false},
		{"haiku allowlist allows haiku", "haiku", "claude-haiku-3-5", true},
		{"haiku allowlist blocks sonnet", "haiku", "claude-sonnet-3-5", false},

		// comma-separated
		{"haiku,sonnet allows sonnet", "haiku,sonnet", "claude-sonnet-3-5", true},
		{"haiku,sonnet allows haiku", "haiku,sonnet", "claude-haiku-3-5", true},
		{"haiku,sonnet blocks opus", "haiku,sonnet", "claude-opus-4-8", false},

		// space-separated
		{"haiku sonnet allows sonnet", "haiku sonnet", "claude-sonnet-3-5", true},
		{"haiku sonnet blocks opus", "haiku sonnet", "claude-opus-4-8", false},

		// opus allowed
		{"opus allowlist allows opus", "opus", "claude-opus-4-8", true},
		{"opus allowlist blocks haiku", "opus", "claude-haiku-3-5", false},

		// whitespace around entries
		{"spaces around entries", " haiku , sonnet ", "claude-sonnet-3-5", true},
		{"spaces around entries blocks opus", " haiku , sonnet ", "claude-opus-4-8", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := channelAllowsModel(tt.allowlist, tt.model)
			if got != tt.want {
				t.Errorf("channelAllowsModel(%q, %q) = %v, want %v", tt.allowlist, tt.model, got, tt.want)
			}
		})
	}
}
