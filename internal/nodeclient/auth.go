package nodeclient

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
)

// ImportCreds is the credential payload pushed to a node's /auth/import.
type ImportCreds struct {
	Access    string `json:"access"`
	Refresh   string `json:"refresh"`
	ExpiresAt int64  `json:"expiresAt"`
	Email     string `json:"email"`
}

// doWithProfile issues a request carrying the x-meridian-profile header.
func (c *Client) doWithProfile(ctx context.Context, method, path, profileID string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	if profileID != "" {
		req.Header.Set("x-meridian-profile", profileID)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errStatus(method, path, resp.StatusCode)
	}
	return nil
}

// AuthImport pushes stored credentials onto a node (for build/re-seed/migrate).
// NOTE: the node-side POST /auth/import endpoint must be added to new-meridian
// (tracked separately); this defines the client contract.
func (c *Client) AuthImport(ctx context.Context, profileID string, ic ImportCreds) error {
	raw, err := json.Marshal(ic)
	if err != nil {
		return err
	}
	return c.doWithProfile(ctx, "POST", "/auth/import", profileID, raw)
}

// AuthRefresh asks the node to refresh a profile's OAuth token.
func (c *Client) AuthRefresh(ctx context.Context, profileID string) error {
	return c.doWithProfile(ctx, "POST", "/auth/refresh", profileID, []byte(`{}`))
}
