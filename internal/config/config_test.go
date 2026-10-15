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

// apiEnv returns the minimal valid API environment plus extra.
func apiEnv(extra map[string]string) func(string) string {
	m := map[string]string{"SEMAR_DATABASE_URL": testDBURL, "SEMAR_KEK": b64key(7)}
	for k, v := range extra {
		m[k] = v
	}
	return env(m)
}

func TestLoadAPIRequiresDatabaseURL(t *testing.T) {
	if _, err := LoadAPI(env(nil)); err == nil {
		t.Fatal("LoadAPI without SEMAR_DATABASE_URL succeeded")
	}
}

func TestLoadAPIDefaults(t *testing.T) {
	cfg, err := LoadAPI(apiEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.LogLevel != slog.LevelInfo || cfg.ShutdownTimeout != 15*time.Second ||
		cfg.DatabaseURL != testDBURL || !cfg.MigrateOnStart || cfg.SessionTTL != 12*time.Hour || !cfg.CookieSecure ||
		cfg.BootstrapAdminPassword != "" || cfg.KEKID != "k1" || len(cfg.KEKs["k1"]) != 32 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadAPIOverrides(t *testing.T) {
	cfg, err := LoadAPI(apiEnv(map[string]string{
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
		{"SEMAR_KEK": "short"},
		{"SEMAR_KEK_PREVIOUS": "k0"},
		{"SEMAR_TRUSTED_ORIGINS": "semar.example.com"},
		{"SEMAR_TRUSTED_ORIGINS": "https://semar.example.com/app"},
		{"SEMAR_TRUSTED_ORIGINS": "ftp://semar.example.com"},
		{"SEMAR_TRUSTED_ORIGINS": " , "},
	} {
		m["SEMAR_DATABASE_URL"] = testDBURL
		if _, ok := m["SEMAR_KEK"]; !ok {
			m["SEMAR_KEK"] = b64key(7)
		}
		if _, err := LoadAPI(env(m)); err == nil {
			t.Errorf("LoadAPI(%v) succeeded, want error", m)
		}
	}
}

func TestLoadAPIRequiresKEK(t *testing.T) {
	if _, err := LoadAPI(env(map[string]string{"SEMAR_DATABASE_URL": testDBURL})); err == nil {
		t.Fatal("LoadAPI without SEMAR_KEK succeeded")
	}
}

func TestLoadAPIKEKRotation(t *testing.T) {
	cfg, err := LoadAPI(apiEnv(map[string]string{
		"SEMAR_KEK_ID":       "k2",
		"SEMAR_KEK":          b64key(2),
		"SEMAR_KEK_PREVIOUS": "k1=" + b64key(1),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.KEKID != "k2" || len(cfg.KEKs) != 2 || cfg.KEKs["k1"][0] != 1 || cfg.KEKs["k2"][0] != 2 {
		t.Fatalf("KEKs = %v (current %s)", cfg.KEKs, cfg.KEKID)
	}
}

func TestLoadAPITrustedOrigins(t *testing.T) {
	cfg, err := LoadAPI(apiEnv(map[string]string{
		"SEMAR_TRUSTED_ORIGINS": "https://SEMAR.example.com/, http://127.0.0.1:5173",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.TrustedOrigins) != 2 || cfg.TrustedOrigins[0] != "https://semar.example.com" || cfg.TrustedOrigins[1] != "http://127.0.0.1:5173" {
		t.Fatalf("TrustedOrigins = %v", cfg.TrustedOrigins)
	}
}
