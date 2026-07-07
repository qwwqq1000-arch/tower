package nodeclient

import (
	"bytes"
	"context"
	"encoding/json"
)

// ProxyInfo is the node's current egress proxy (GET /settings/api/proxy).
type ProxyInfo struct {
	Proxy  string `json:"proxy"`  // 明文代理串,空=未设
	Parsed any    `json:"parsed"` // 节点解析结果或 null
}

// GetProxy reads the node's current egress proxy.
func (c *Client) GetProxy(ctx context.Context) (ProxyInfo, error) {
	var pi ProxyInfo
	err := c.doJSON(ctx, "GET", "/settings/api/proxy", nil, &pi)
	return pi, err
}

// TestProxy asks the node to dial raw and report reachability + egress IP.
// It does NOT persist anything.
func (c *Client) TestProxy(ctx context.Context, raw string) (map[string]any, error) {
	body, _ := json.Marshal(map[string]string{"raw": raw})
	var out map[string]any
	err := c.doJSON(ctx, "POST", "/settings/api/proxy/test", bytes.NewReader(body), &out)
	return out, err
}

// SetProxy persists raw (empty string clears it). The node restarts to activate
// redsocks; the response carries {"restarting": true} in that case.
func (c *Client) SetProxy(ctx context.Context, raw string) (map[string]any, error) {
	body, _ := json.Marshal(map[string]string{"raw": raw})
	var out map[string]any
	err := c.doJSON(ctx, "POST", "/settings/api/proxy", bytes.NewReader(body), &out)
	return out, err
}
