package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// FetchChannelBalance calls the new-api relay's /api/user/self endpoint and
// returns the account balance in USD (quota / 500000).
// baseURL may include a path such as "/v1"; only scheme+host is used.
func FetchChannelBalance(ctx context.Context, baseURL, token, userID string) (float64, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return 0, fmt.Errorf("invalid baseURL %q: %w", baseURL, err)
	}
	origin := u.Scheme + "://" + u.Host

	reqURL := origin + "/api/user/self"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("New-Api-User", userID)

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("balance fetch returned HTTP %d", resp.StatusCode)
	}

	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Quota float64 `json:"quota"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("balance decode: %w", err)
	}
	if !payload.Success {
		return 0, fmt.Errorf("balance API returned success=false")
	}
	return payload.Data.Quota / 500000, nil
}
