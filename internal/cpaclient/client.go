// Package cpaclient is a typed client for the CLIProxyAPI (CPA) management API.
// It lets Tower discover the individual accounts (auth files) configured on a CPA
// node so each can be shown in the account pool and dispatched independently
// (via the X-CLIProxy-Account header supported by Tower's CPA fork).
package cpaclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client talks to one CPA node's management API.
type Client struct {
	baseURL string
	mgmtKey string
	http    *http.Client
}

// New builds a CPA management client. baseURL is the node root (e.g.
// http://host:8317); mgmtKey is the plaintext management secret.
func New(baseURL, mgmtKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		mgmtKey: mgmtKey,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Account is one CPA auth file (one upstream account) as reported by the
// management API.
type Account struct {
	ID          string `json:"id"`
	AuthIndex   string `json:"auth_index"`
	Name        string `json:"name"`
	Provider    string `json:"provider"`
	Email       string `json:"email"`
	AccountType string `json:"account_type"`
	Status      string `json:"status"`
	Disabled    bool   `json:"disabled"`
	Unavailable bool   `json:"unavailable"`
	Success     int64  `json:"success"`
	Failed      int64  `json:"failed"`
}

type authFilesResponse struct {
	Files []Account `json:"files"`
}

// ListAccounts returns every account configured on the CPA node.
func (c *Client) ListAccounts(ctx context.Context) ([]Account, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/v0/management/auth-files", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.mgmtKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cpa auth-files: status %d: %s", resp.StatusCode, truncate(data, 200))
	}
	var out authFilesResponse
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("cpa auth-files: decode: %w", err)
	}
	return out.Files, nil
}

// UsageWindow is one rolling rate-limit window from the Anthropic OAuth usage API.
type UsageWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

// Usage is a Claude account's subscription rate-limit state (the data shown in
// the CPA panel: 5h / 7天 / 7天 Sonnet).
type Usage struct {
	FiveHour       *UsageWindow `json:"five_hour"`
	SevenDay       *UsageWindow `json:"seven_day"`
	SevenDayOpus   *UsageWindow `json:"seven_day_opus"`
	SevenDaySonnet *UsageWindow `json:"seven_day_sonnet"`
}

// apiCallRequest is the body sent to POST /v0/management/api-call on CPA v7.2.40+.
type apiCallRequest struct {
	AuthIndex string            `json:"authIndex"`
	Method    string            `json:"method"`
	URL       string            `json:"url"`
	Header    map[string]string `json:"header"`
}

// apiCallResponse is the outer envelope returned by POST /v0/management/api-call.
type apiCallResponse struct {
	StatusCode int    `json:"status_code"`
	Body       string `json:"body"`
}

// Usage fetches one account's rate-limit windows via the CPA management API.
// authIndex is the account's auth_index (used by the new api-call endpoint on
// CPA v7.2.40+). fileID is the legacy account selector (file name / email) used
// by the old GET /v0/management/account-usage endpoint as a fallback for older
// CPA versions that do not expose api-call.
func (c *Client) Usage(ctx context.Context, authIndex, fileID string) (*Usage, error) {
	// --- NEW PATH: POST /v0/management/api-call (CPA v7.2.40+) ---
	body := apiCallRequest{
		AuthIndex: authIndex,
		Method:    "GET",
		URL:       "https://api.anthropic.com/api/oauth/usage",
		Header: map[string]string{
			"Authorization":  "Bearer $TOKEN$",
			"Content-Type":   "application/json",
			"anthropic-beta": "oauth-2025-04-20",
		},
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("cpa api-call usage: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v0/management/api-call", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("cpa api-call usage: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.mgmtKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cpa api-call usage: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		// Old CPA version — fall back to the legacy account-usage endpoint.
		return c.usageLegacy(ctx, fileID)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cpa api-call usage: status %d: %s", resp.StatusCode, truncate(data, 200))
	}

	var outer apiCallResponse
	if err := json.Unmarshal(data, &outer); err != nil {
		return nil, fmt.Errorf("cpa api-call usage: decode outer: %w", err)
	}
	if outer.StatusCode != 200 {
		return nil, fmt.Errorf("cpa api-call usage: upstream status %d: %s", outer.StatusCode, truncate([]byte(outer.Body), 200))
	}
	var out Usage
	if err := json.Unmarshal([]byte(outer.Body), &out); err != nil {
		return nil, fmt.Errorf("cpa api-call usage: decode inner: %w", err)
	}
	return &out, nil
}

// usageLegacy fetches usage via the legacy GET /v0/management/account-usage?id=<selector>
// endpoint (CPA versions prior to v7.2.40).
func (c *Client) usageLegacy(ctx context.Context, selector string) (*Usage, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/v0/management/account-usage?id="+url.QueryEscape(selector), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.mgmtKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("cpa account-usage: status %d: %s", resp.StatusCode, truncate(data, 200))
	}
	var out Usage
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("cpa account-usage: decode: %w", err)
	}
	return &out, nil
}

// DispatchSelector returns the value Tower should send in the X-CLIProxy-Account
// header to pin a request to this account. The id (auth file name) is the most
// stable selector accepted by the fork.
func (a Account) DispatchSelector() string {
	if a.ID != "" {
		return a.ID
	}
	if a.AuthIndex != "" {
		return a.AuthIndex
	}
	return a.Email
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}
