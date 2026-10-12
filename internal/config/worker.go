package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Worker is the configuration of semar-worker.
type Worker struct {
	TemporalAddress   string
	TemporalNamespace string
	SiteID            string
	LogLevel          slog.Level
	SyncInterval      time.Duration
	// PayloadKeyID identifies PayloadKeys[PayloadKeyID], used for encryption.
	PayloadKeyID string
	// PayloadKeys holds the current key and any previous keys still needed to
	// decrypt existing payloads during rotation.
	PayloadKeys map[string][]byte
	// FakeProviders lists Fake Driver provider IDs to sync (development until
	// providers are loaded from the database).
	FakeProviders []string
}

// LoadWorker reads SEMAR_* variables via getenv (os.Getenv in production).
func LoadWorker(getenv func(string) string) (Worker, error) {
	cfg := Worker{
		TemporalAddress:   "localhost:7233",
		TemporalNamespace: "semar",
		SiteID:            "default",
		LogLevel:          slog.LevelInfo,
		SyncInterval:      60 * time.Second,
		PayloadKeyID:      "k1",
		PayloadKeys:       map[string][]byte{},
		FakeProviders:     []string{"fake-1"},
	}
	setString(getenv, "SEMAR_TEMPORAL_ADDRESS", &cfg.TemporalAddress)
	setString(getenv, "SEMAR_TEMPORAL_NAMESPACE", &cfg.TemporalNamespace)
	setString(getenv, "SEMAR_SITE_ID", &cfg.SiteID)
	setString(getenv, "SEMAR_PAYLOAD_KEY_ID", &cfg.PayloadKeyID)

	if v := getenv("SEMAR_LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(strings.ToUpper(v))); err != nil {
			return Worker{}, fmt.Errorf("SEMAR_LOG_LEVEL: %w", err)
		}
	}
	if v := getenv("SEMAR_SYNC_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return Worker{}, fmt.Errorf("SEMAR_SYNC_INTERVAL: %w", err)
		}
		if d < time.Second {
			return Worker{}, fmt.Errorf("SEMAR_SYNC_INTERVAL: must be at least 1s, got %s", d)
		}
		cfg.SyncInterval = d
	}

	key, err := decodeKey(getenv("SEMAR_PAYLOAD_KEY"))
	if err != nil {
		return Worker{}, fmt.Errorf("SEMAR_PAYLOAD_KEY: %w", err)
	}
	cfg.PayloadKeys[cfg.PayloadKeyID] = key

	// SEMAR_PAYLOAD_PREVIOUS_KEYS="k0=<base64>,k-1=<base64>"
	if v := getenv("SEMAR_PAYLOAD_PREVIOUS_KEYS"); v != "" {
		for _, pair := range strings.Split(v, ",") {
			id, b64, ok := strings.Cut(strings.TrimSpace(pair), "=")
			if !ok || id == "" {
				return Worker{}, errors.New("SEMAR_PAYLOAD_PREVIOUS_KEYS: want id=base64[,id=base64]")
			}
			if _, dup := cfg.PayloadKeys[id]; dup {
				return Worker{}, fmt.Errorf("SEMAR_PAYLOAD_PREVIOUS_KEYS: duplicate key id %q", id)
			}
			k, err := decodeKey(b64)
			if err != nil {
				return Worker{}, fmt.Errorf("SEMAR_PAYLOAD_PREVIOUS_KEYS[%s]: %w", id, err)
			}
			cfg.PayloadKeys[id] = k
		}
	}

	if v := getenv("SEMAR_FAKE_PROVIDERS"); v != "" {
		cfg.FakeProviders = nil
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				cfg.FakeProviders = append(cfg.FakeProviders, p)
			}
		}
	}
	return cfg, nil
}

func setString(getenv func(string) string, key string, dst *string) {
	if v := getenv(key); v != "" {
		*dst = v
	}
}

func decodeKey(b64 string) ([]byte, error) {
	if b64 == "" {
		return nil, errors.New("required (32 bytes, base64; e.g. `openssl rand -base64 32`)")
	}
	k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, fmt.Errorf("invalid base64: %w", err)
	}
	if len(k) != 32 {
		return nil, fmt.Errorf("must decode to 32 bytes, got %d", len(k))
	}
	return k, nil
}
