// Package cpaclient is a typed client for the CLIProxyAPI (CPA) management API.
// It lets Tower discover the individual accounts (auth files) configured on a CPA
// node so each can be shown in the account pool and dispatched independently
// (via the X-CLIProxy-Account header supported by Tower's CPA fork).
package cpaclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
