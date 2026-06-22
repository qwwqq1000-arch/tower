package dispatch

import (
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// TestChannelAboveBalanceAlert verifies that a channel is gated when its
// observed balance (balance_usd) falls below the configured alert threshold
// (balance_alert_usd). This is the routing gate for fallback-5.
func TestChannelAboveBalanceAlert(t *testing.T) {
	tests := []struct {
		name           string
		balanceUsd     float64
		balanceAlertUsd float64
		want           bool
	}{
		{
			name:           "no alert configured — always routable",
			balanceUsd:     0,
			balanceAlertUsd: 0,
			want:           true,
		},
		{
			name:           "balance above alert — routable",
			balanceUsd:     10.0,
			balanceAlertUsd: 5.0,
			want:           true,
		},
		{
			name:           "balance equal to alert — routable (boundary)",
			balanceUsd:     5.0,
			balanceAlertUsd: 5.0,
			want:           true,
		},
		{
			name:           "balance below alert — not routable",
			balanceUsd:     3.0,
			balanceAlertUsd: 5.0,
			want:           false,
		},
		{
			name:           "balance zero with alert set — not routable",
			balanceUsd:     0,
			balanceAlertUsd: 1.0,
			want:           false,
		},
		{
			name:           "balance negative with alert set — not routable",
			balanceUsd:     -1.0,
			balanceAlertUsd: 1.0,
			want:           false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := sqlc.FallbackChannel{
				BalanceUsd:      tt.balanceUsd,
				BalanceAlertUsd: tt.balanceAlertUsd,
			}
			got := channelAboveBalanceAlert(ch)
			if got != tt.want {
				t.Errorf("channelAboveBalanceAlert(balanceUsd=%v, alertUsd=%v) = %v, want %v",
					tt.balanceUsd, tt.balanceAlertUsd, got, tt.want)
			}
		})
	}
}
