package config

import (
	"log/slog"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

const testDBURL = "postgres://semar@localhost:5432/semar"

func TestLoadAPIRequiresDatabaseURL(t *testing.T) {
	if _, err := LoadAPI(env(nil)); err == nil {
		t.Fatal("LoadAPI without SEMAR_DATABASE_URL succeeded")
	}
}

func TestLoadAPIDefaults(t *testing.T) {
	cfg, err := LoadAPI(env(map[string]string{"SEMAR_DATABASE_URL": testDBURL}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.LogLevel != slog.LevelInfo || cfg.ShutdownTimeout != 15*time.Second ||
		cfg.DatabaseURL != testDBURL || !cfg.MigrateOnStart || cfg.SessionTTL != 12*time.Hour || !cfg.CookieSecure ||
		cfg.BootstrapAdminPassword != "" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadAPIOverrides(t *testing.T) {
	cfg, err := LoadAPI(env(map[string]string{
		"SEMAR_HTTP_ADDR":        "127.0.0.1:9000",
		"SEMAR_LOG_LEVEL":        "debug",
		"SEMAR_SHUTDOWN_TIMEOUT": "3s",
		"SEMAR_DATABASE_URL":     testDBURL,
		"SEMAR_MIGRATE_ON_START": "false",
		"SEMAR_SESSION_TTL":      "30m",
		"SEMAR_COOKIE_SECURE":    "false",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9000" || cfg.LogLevel != slog.LevelDebug || cfg.ShutdownTimeout != 3*time.Second || cfg.MigrateOnStart ||
		cfg.SessionTTL != 30*time.Minute || cfg.CookieSecure {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadAPIRejectsInvalid(t *testing.T) {
	for _, m := range []map[string]string{
		{"SEMAR_LOG_LEVEL": "loud"},
		{"SEMAR_SHUTDOWN_TIMEOUT": "soon"},
		{"SEMAR_SHUTDOWN_TIMEOUT": "-1s"},
		{"SEMAR_MIGRATE_ON_START": "maybe"},
		{"SEMAR_SESSION_TTL": "10s"},
		{"SEMAR_SESSION_TTL": "720h"},
		{"SEMAR_COOKIE_SECURE": "sometimes"},
	} {
		m["SEMAR_DATABASE_URL"] = testDBURL
		if _, err := LoadAPI(env(m)); err == nil {
			t.Errorf("LoadAPI(%v) succeeded, want error", m)
		}
	}
}
