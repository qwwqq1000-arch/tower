// Package config loads Tower runtime configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config holds all runtime configuration for the Tower control plane.
type Config struct {
	HTTPAddr      string // listen address, e.g. ":8080"
	DatabaseURL   string // postgres connection string
	MasterKeyB64  string // base64-encoded 32-byte AES-256 master key
	SessionSecret string // HMAC secret for session cookies
	AdminUser     string
	AdminPassword string
	// PushToken is an optional fixed bearer token (TOWER_PUSH_TOKEN). When set, the
	// node-push API (POST /api/admin/nodes/push) accepts it via the X-Admin-Token
	// header (or Authorization: Bearer) for headless/automation calls, bypassing the
	// session+CSRF requirement for that one endpoint. Empty = session-only (default).
	PushToken string
	// SecureCookies controls whether the tower_session cookie has the Secure
	// flag set. Defaults to false so plain-HTTP deployments work out of the
	// box. Set TOWER_SECURE_COOKIES=1 (or "true"/"yes") in TLS environments.
	SecureCookies bool
}

// Load reads configuration from the environment. HTTPAddr defaults to ":8080";
// DatabaseURL, MasterKeyB64 and SessionSecret are required.
// SessionSecret must be at least 32 bytes long (matching the master-key
// validation) to ensure adequate HMAC strength.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:      envOr("TOWER_HTTP_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("TOWER_DATABASE_URL"),
		MasterKeyB64:  os.Getenv("TOWER_MASTER_KEY"),
		SessionSecret: os.Getenv("TOWER_SESSION_SECRET"),
		AdminUser:     os.Getenv("TOWER_ADMIN_USER"),
		AdminPassword: os.Getenv("TOWER_ADMIN_PASSWORD"),
		PushToken:     os.Getenv("TOWER_PUSH_TOKEN"),
		SecureCookies: parseBoolEnv("TOWER_SECURE_COOKIES"),
	}
	var errs []string
	if cfg.DatabaseURL == "" {
		errs = append(errs, "TOWER_DATABASE_URL")
	}
	if cfg.MasterKeyB64 == "" {
		errs = append(errs, "TOWER_MASTER_KEY")
	}
	if cfg.SessionSecret == "" {
		errs = append(errs, "TOWER_SESSION_SECRET")
	} else if len(cfg.SessionSecret) < 32 {
		fmt.Fprintf(os.Stderr, "WARNING: TOWER_SESSION_SECRET is %d chars; >=32 strongly recommended\n", len(cfg.SessionSecret))
	}
	if len(errs) > 0 {
		return Config{}, fmt.Errorf("missing required env: %s", strings.Join(errs, ", "))
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// parseBoolEnv returns true when the env var is set to a common truthy string:
// "1", "true", "yes", "on" (case-insensitive). Empty or any other value → false.
func parseBoolEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
