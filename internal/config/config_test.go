package config

import (
	"strings"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("TOWER_DATABASE_URL", "postgres://localhost/tower")
	t.Setenv("TOWER_MASTER_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("TOWER_SESSION_SECRET", "s3cret-session-secret-value-32xx")
	t.Setenv("TOWER_HTTP_ADDR", "")
	t.Setenv("TOWER_SECURE_COOKIES", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr default = %q, want :8080", cfg.HTTPAddr)
	}
	if cfg.DatabaseURL != "postgres://localhost/tower" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if cfg.SecureCookies {
		t.Error("SecureCookies should default to false")
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	t.Setenv("TOWER_DATABASE_URL", "")
	t.Setenv("TOWER_MASTER_KEY", "")
	t.Setenv("TOWER_SESSION_SECRET", "")

	_, err := Load()
	if err == nil {
		t.Fatal("Load() expected error for missing required env, got nil")
	}
	for _, varName := range []string{"TOWER_DATABASE_URL", "TOWER_MASTER_KEY", "TOWER_SESSION_SECRET"} {
		if !strings.Contains(err.Error(), varName) {
			t.Errorf("error message missing var name %q: %v", varName, err)
		}
	}
}

func TestLoad_SecureCookiesEnv(t *testing.T) {
	t.Setenv("TOWER_DATABASE_URL", "postgres://localhost/tower")
	t.Setenv("TOWER_MASTER_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("TOWER_SESSION_SECRET", "s3cret-session-secret-value-32xx")
	t.Setenv("TOWER_SECURE_COOKIES", "1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.SecureCookies {
		t.Error("SecureCookies should be true when TOWER_SECURE_COOKIES=1")
	}
}

func TestLoad_ShortSessionSecret(t *testing.T) {
	t.Setenv("TOWER_DATABASE_URL", "postgres://localhost/tower")
	t.Setenv("TOWER_MASTER_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("TOWER_SESSION_SECRET", "tooshort")
	t.Setenv("TOWER_SECURE_COOKIES", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() must not error on short session secret (warn-only): %v", err)
	}
	if cfg.SessionSecret != "tooshort" {
		t.Errorf("SessionSecret = %q, want tooshort", cfg.SessionSecret)
	}
}
