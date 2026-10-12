// Package config loads process configuration from the environment.
package config

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// API is the configuration of semar-api.
type API struct {
	HTTPAddr        string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
}

// LoadAPI reads SEMAR_* variables via getenv (os.Getenv in production).
func LoadAPI(getenv func(string) string) (API, error) {
	cfg := API{
		HTTPAddr:        ":8080",
		LogLevel:        slog.LevelInfo,
		ShutdownTimeout: 15 * time.Second,
	}
	if v := getenv("SEMAR_HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}
	if v := getenv("SEMAR_LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(strings.ToUpper(v))); err != nil {
			return API{}, fmt.Errorf("SEMAR_LOG_LEVEL: %w", err)
		}
	}
	if v := getenv("SEMAR_SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return API{}, fmt.Errorf("SEMAR_SHUTDOWN_TIMEOUT: %w", err)
		}
		if d <= 0 {
			return API{}, fmt.Errorf("SEMAR_SHUTDOWN_TIMEOUT: must be positive, got %s", d)
		}
		cfg.ShutdownTimeout = d
	}
	return cfg, nil
}
