// Package nodeclient is a typed HTTP client for new-meridian dumb nodes.
package nodeclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to one new-meridian node.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// New builds a Client with a default 10s timeout.
func New(baseURL, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// doJSON issues a request and decodes a JSON response into out (nil to skip).
// A non-2xx status is an error.
func (c *Client) doJSON(ctx context.Context, method, path string, body io.Reader, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", c.apiKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("node %s %s: status %d: %s", method, path, resp.StatusCode, truncate(data, 200))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}

func truncate(b []byte, n int) string {
	if len(b) > n {
		return string(b[:n])
	}
	return string(b)
}

func errStatus(method, path string, code int) error {
	return fmt.Errorf("node %s %s: status %d", method, path, code)
}
