package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/titagaki/peercast-0yp/internal/config"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestLoad_Defaults verifies that omitted fields are filled with built-in defaults.
func TestLoad_Defaults(t *testing.T) {
	cfg, err := config.Load(writeTemp(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PCP.Port != 7144 {
		t.Errorf("PCP.Port = %d, want 7144", cfg.PCP.Port)
	}
	if cfg.PCP.MaxConnections != 100 {
		t.Errorf("PCP.MaxConnections = %d, want 100", cfg.PCP.MaxConnections)
	}
	if cfg.PCP.UpdateInterval != 120 {
		t.Errorf("PCP.UpdateInterval = %d, want 120", cfg.PCP.UpdateInterval)
	}
	if cfg.PCP.HitTimeout != 180 {
		t.Errorf("PCP.HitTimeout = %d, want 180", cfg.PCP.HitTimeout)
	}
	if cfg.PCP.MinClientVersion != 1200 {
		t.Errorf("PCP.MinClientVersion = %d, want 1200", cfg.PCP.MinClientVersion)
	}
	if cfg.HTTP.Port != 80 {
		t.Errorf("HTTP.Port = %d, want 80", cfg.HTTP.Port)
	}
}

// TestLoad_ExplicitValues verifies that explicitly set values are not overridden by defaults.
func TestLoad_ExplicitValues(t *testing.T) {
	toml := `
[PCP]
port = 17144
max_connections = 50
update_interval = 60
hit_timeout = 300
min_client_version = 1218

[HTTP]
port = 8080
cors_origins = ["http://localhost:3000"]
`
	cfg, err := config.Load(writeTemp(t, toml))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PCP.Port != 17144 {
		t.Errorf("PCP.Port = %d, want 17144", cfg.PCP.Port)
	}
	if cfg.PCP.MaxConnections != 50 {
		t.Errorf("PCP.MaxConnections = %d, want 50", cfg.PCP.MaxConnections)
	}
	if cfg.PCP.UpdateInterval != 60 {
		t.Errorf("PCP.UpdateInterval = %d, want 60", cfg.PCP.UpdateInterval)
	}
	if cfg.PCP.HitTimeout != 300 {
		t.Errorf("PCP.HitTimeout = %d, want 300", cfg.PCP.HitTimeout)
	}
	if cfg.PCP.MinClientVersion != 1218 {
		t.Errorf("PCP.MinClientVersion = %d, want 1218", cfg.PCP.MinClientVersion)
	}
	if cfg.HTTP.Port != 8080 {
		t.Errorf("HTTP.Port = %d, want 8080", cfg.HTTP.Port)
	}
	if len(cfg.HTTP.CORSOrigins) != 1 || cfg.HTTP.CORSOrigins[0] != "http://localhost:3000" {
		t.Errorf("CORSOrigins = %v, want [http://localhost:3000]", cfg.HTTP.CORSOrigins)
	}
}

// TestLoad_PartialDefaults verifies that only missing fields get defaults.
func TestLoad_PartialDefaults(t *testing.T) {
	toml := `
[PCP]
port = 17144
`
	cfg, err := config.Load(writeTemp(t, toml))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PCP.Port != 17144 {
		t.Errorf("PCP.Port = %d, want 17144", cfg.PCP.Port)
	}
	if cfg.HTTP.Port != 80 {
		t.Errorf("HTTP.Port = %d, want default 80", cfg.HTTP.Port)
	}
	if cfg.PCP.MaxConnections != 100 {
		t.Errorf("PCP.MaxConnections = %d, want default 100", cfg.PCP.MaxConnections)
	}
}

// TestLoad_DatabaseDSN_FromEnv verifies that DB_* env vars are assembled into a DSN.
func TestLoad_DatabaseDSN_FromEnv(t *testing.T) {
	t.Setenv("DB_USER", "user")
	t.Setenv("DB_PASSWORD", "pass")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_PORT", "5432")
	t.Setenv("DB_NAME", "0yp")

	want := "postgres://user:pass@localhost:5432/0yp?timezone=Asia%2FTokyo"
	cfg, err := config.Load(writeTemp(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.DSN != want {
		t.Errorf("Database.DSN = %q, want %q", cfg.Database.DSN, want)
	}
}

// TestLoad_DatabaseDSN_EmptyWithoutEnv verifies that DSN is empty when DB_* env vars are unset.
func TestLoad_DatabaseDSN_EmptyWithoutEnv(t *testing.T) {
	t.Setenv("DB_USER", "")
	t.Setenv("DB_HOST", "")
	t.Setenv("DB_NAME", "")

	cfg, err := config.Load(writeTemp(t, ""))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.DSN != "" {
		t.Errorf("Database.DSN = %q, want empty", cfg.Database.DSN)
	}
}

// TestLoad_MissingFile verifies that a non-existent path returns an error.
func TestLoad_MissingFile(t *testing.T) {
	_, err := config.Load("/nonexistent/path/to/config.toml")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
