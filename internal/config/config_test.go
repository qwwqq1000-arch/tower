package config

import "testing"

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("TOWER_DATABASE_URL", "postgres://localhost/tower")
	t.Setenv("TOWER_MASTER_KEY", "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	t.Setenv("TOWER_SESSION_SECRET", "s3cret-session-secret-value-32x")
	t.Setenv("TOWER_HTTP_ADDR", "")

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
}

func TestLoad_MissingRequired(t *testing.T) {
	t.Setenv("TOWER_DATABASE_URL", "")
	t.Setenv("TOWER_MASTER_KEY", "")
	t.Setenv("TOWER_SESSION_SECRET", "")

	if _, err := Load(); err == nil {
		t.Fatal("Load() expected error for missing required env, got nil")
	}
}
