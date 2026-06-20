package nodeclient

import (
	"bytes"
	"context"
	"encoding/json"
)

// GetFeatures fetches all adapters' SDK feature toggles.
func (c *Client) GetFeatures(ctx context.Context) (map[string]map[string]any, error) {
	var f map[string]map[string]any
	err := c.doJSON(ctx, "GET", "/settings/api/features", nil, &f)
	return f, err
}

// PatchFeatures updates one adapter's feature toggles.
func (c *Client) PatchFeatures(ctx context.Context, adapter string, patch map[string]any) error {
	raw, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	return c.doJSON(ctx, "PATCH", "/settings/api/features/"+adapter, bytes.NewReader(raw), nil)
}
