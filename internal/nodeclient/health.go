package nodeclient

import "context"

// HealthAuth is the auth block of /health.
type HealthAuth struct {
	LoggedIn              bool   `json:"loggedIn"`
	Email                 string `json:"email"`
	SubscriptionType      string `json:"subscriptionType"`
	SubscriptionCreatedAt string `json:"subscriptionCreatedAt"`
	AccountCreatedAt      string `json:"accountCreatedAt"`
}

// Health is the parsed /health response.
type Health struct {
	Status  string     `json:"status"`
	Version string     `json:"version"`
	Mode    string     `json:"mode"`
	Auth    HealthAuth `json:"auth"`
}

// Health fetches GET /health.
func (c *Client) Health(ctx context.Context) (Health, error) {
	var h Health
	err := c.doJSON(ctx, "GET", "/health", nil, &h)
	return h, err
}

// QuotaWindow is one rate-limit bucket.
type QuotaWindow struct {
	Type        string  `json:"type"`
	Status      string  `json:"status"`
	Utilization float64 `json:"utilization"`
	ResetsAt    int64   `json:"resetsAt"`
}

// ProfileQuota is one account's quota windows.
type ProfileQuota struct {
	ID      string        `json:"id"`
	IsActive bool         `json:"isActive"`
	Windows []QuotaWindow `json:"windows"`
}

// QuotaAll is the parsed /v1/usage/quota/all response.
type QuotaAll struct {
	Profiles      []ProfileQuota `json:"profiles"`
	ActiveProfile string         `json:"activeProfile"`
}

// QuotaAll fetches GET /v1/usage/quota/all (all profiles in parallel server-side).
func (c *Client) QuotaAll(ctx context.Context) (QuotaAll, error) {
	var q QuotaAll
	err := c.doJSON(ctx, "GET", "/v1/usage/quota/all", nil, &q)
	return q, err
}
