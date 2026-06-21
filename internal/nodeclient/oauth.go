package nodeclient

import (
	"bytes"
	"context"
	"encoding/json"
)

// LoginURL is the node's PKCE authorize info.
type LoginURL struct {
	AuthorizeURL string `json:"authorizeUrl"`
	CodeVerifier string `json:"codeVerifier"`
	State        string `json:"state"`
}

// LoginURL starts the node's OAuth flow (POST /auth/login-url).
func (c *Client) LoginURL(ctx context.Context) (LoginURL, error) {
	var lu LoginURL
	err := c.doJSON(ctx, "POST", "/auth/login-url", bytes.NewReader([]byte(`{}`)), &lu)
	return lu, err
}

// Exchange completes the node's OAuth flow (POST /auth/exchange).
func (c *Client) Exchange(ctx context.Context, codeVerifier, state, code string) error {
	body, _ := json.Marshal(map[string]string{"codeVerifier": codeVerifier, "state": state, "code": code})
	return c.doJSON(ctx, "POST", "/auth/exchange", bytes.NewReader(body), nil)
}

// Profile is one account profile on a node.
type Profile struct {
	ID               string `json:"id"`
	Type             string `json:"type"`
	Email            string `json:"email"`
	SubscriptionType string `json:"subscriptionType"`
	LoggedIn         bool   `json:"loggedIn"`
}

// ProfilesList lists the node's configured profiles (GET /profiles/list).
func (c *Client) ProfilesList(ctx context.Context) ([]Profile, error) {
	var resp struct {
		Profiles []Profile `json:"profiles"`
	}
	if err := c.doJSON(ctx, "GET", "/profiles/list", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Profiles, nil
}
