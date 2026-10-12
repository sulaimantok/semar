// Package config loads process configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

// API is the configuration of semar-api.
type API struct {
	HTTPAddr        string
	LogLevel        slog.Level
	ShutdownTimeout time.Duration
	// DatabaseURL is a PostgreSQL connection URL (required).
	DatabaseURL string
	// MigrateOnStart applies pending migrations before serving.
	MigrateOnStart bool
	// BootstrapAdminPassword creates the "admin" user on first start when no
	// user exists. Remove it from the environment afterwards.
	BootstrapAdminPassword string
	// SessionTTL is the lifetime of a login session.
	SessionTTL time.Duration
	// CookieSecure marks the session cookie Secure; disable only for plain
	// HTTP on localhost.
	CookieSecure bool
	// KEKID identifies KEKs[KEKID], the key-encryption key that seals new
	// provider credentials (ADR-0004).
	KEKID string
	// KEKs holds the current KEK and previous ones kept for decryption until
	// `semar-api kek-rewrap` has moved every credential to the current KEK.
	KEKs map[string][]byte
}

// LoadAPI reads SEMAR_* variables via getenv (os.Getenv in production).
func LoadAPI(getenv func(string) string) (API, error) {
	cfg := API{
		HTTPAddr:        ":8080",
		LogLevel:        slog.LevelInfo,
		ShutdownTimeout: 15 * time.Second,
		MigrateOnStart:  true,
		SessionTTL:      12 * time.Hour,
		CookieSecure:    true,
		KEKID:           "k1",
	}
	setString(getenv, "SEMAR_KEK_ID", &cfg.KEKID)
	keks, err := loadKeys(getenv, cfg.KEKID, "SEMAR_KEK", "SEMAR_KEK_PREVIOUS")
	if err != nil {
		return API{}, err
	}
	cfg.KEKs = keks
	cfg.BootstrapAdminPassword = getenv("SEMAR_BOOTSTRAP_ADMIN_PASSWORD")
	if v := getenv("SEMAR_SESSION_TTL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return API{}, fmt.Errorf("SEMAR_SESSION_TTL: %w", err)
		}
		if d < time.Minute || d > 7*24*time.Hour {
			return API{}, fmt.Errorf("SEMAR_SESSION_TTL: must be between 1m and 168h, got %s", d)
		}
		cfg.SessionTTL = d
	}
	if v := getenv("SEMAR_COOKIE_SECURE"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return API{}, fmt.Errorf("SEMAR_COOKIE_SECURE: %w", err)
		}
		cfg.CookieSecure = b
	}
	cfg.DatabaseURL = getenv("SEMAR_DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return API{}, errors.New("SEMAR_DATABASE_URL is required (postgres://user:password@host:5432/semar)")
	}
	if v := getenv("SEMAR_MIGRATE_ON_START"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return API{}, fmt.Errorf("SEMAR_MIGRATE_ON_START: %w", err)
		}
		cfg.MigrateOnStart = b
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
