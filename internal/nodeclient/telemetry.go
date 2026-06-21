package nodeclient

import (
	"context"
)

type LatPct struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
}

type ModelStat struct {
	Count      int64   `json:"count"`
	AvgTotalMs float64 `json:"avgTotalMs"`
}

type TokenUsage struct {
	TotalInputTokens         int64   `json:"totalInputTokens"`
	TotalOutputTokens        int64   `json:"totalOutputTokens"`
	TotalCacheReadTokens     int64   `json:"totalCacheReadTokens"`
	TotalCacheCreationTokens int64   `json:"totalCacheCreationTokens"`
	AvgCacheHitRate          float64 `json:"avgCacheHitRate"`
}

type TelemetrySummary struct {
	WindowMs          int64                `json:"windowMs"`
	TotalRequests     int64                `json:"totalRequests"`
	ErrorCount        int64                `json:"errorCount"`
	RequestsPerMinute float64              `json:"requestsPerMinute"`
	Ttfb              LatPct               `json:"ttfb"`
	TotalDuration     LatPct               `json:"totalDuration"`
	ByModel           map[string]ModelStat `json:"byModel"`
	TokenUsage        TokenUsage           `json:"tokenUsage"`
}

func (c *Client) TelemetrySummary(ctx context.Context) (TelemetrySummary, error) {
	var out TelemetrySummary
	err := c.doJSON(ctx, "GET", "/telemetry/summary", nil, &out)
	return out, err
}
