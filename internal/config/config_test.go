package config

import (
	"log/slog"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadAPIDefaults(t *testing.T) {
	cfg, err := LoadAPI(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != ":8080" || cfg.LogLevel != slog.LevelInfo || cfg.ShutdownTimeout != 15*time.Second {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadAPIOverrides(t *testing.T) {
	cfg, err := LoadAPI(env(map[string]string{
		"SEMAR_HTTP_ADDR":        "127.0.0.1:9000",
		"SEMAR_LOG_LEVEL":        "debug",
		"SEMAR_SHUTDOWN_TIMEOUT": "3s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTPAddr != "127.0.0.1:9000" || cfg.LogLevel != slog.LevelDebug || cfg.ShutdownTimeout != 3*time.Second {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadAPIRejectsInvalid(t *testing.T) {
	for _, m := range []map[string]string{
		{"SEMAR_LOG_LEVEL": "loud"},
		{"SEMAR_SHUTDOWN_TIMEOUT": "soon"},
		{"SEMAR_SHUTDOWN_TIMEOUT": "-1s"},
	} {
		if _, err := LoadAPI(env(m)); err == nil {
			t.Errorf("LoadAPI(%v) succeeded, want error", m)
		}
	}
}
