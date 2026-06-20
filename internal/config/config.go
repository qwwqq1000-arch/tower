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
}

// Load reads configuration from the environment. HTTPAddr defaults to ":8080";
// DatabaseURL, MasterKeyB64 and SessionSecret are required.
func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:      envOr("TOWER_HTTP_ADDR", ":8080"),
		DatabaseURL:   os.Getenv("TOWER_DATABASE_URL"),
		MasterKeyB64:  os.Getenv("TOWER_MASTER_KEY"),
		SessionSecret: os.Getenv("TOWER_SESSION_SECRET"),
	}
	var missing []string
	if cfg.DatabaseURL == "" {
		missing = append(missing, "TOWER_DATABASE_URL")
	}
	if cfg.MasterKeyB64 == "" {
		missing = append(missing, "TOWER_MASTER_KEY")
	}
	if cfg.SessionSecret == "" {
		missing = append(missing, "TOWER_SESSION_SECRET")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf("missing required env: %s", strings.Join(missing, ", "))
	}
	return cfg, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
