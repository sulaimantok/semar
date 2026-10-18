package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var siteIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Worker is the configuration of semar-worker.
type Worker struct {
	TemporalAddress   string
	TemporalNamespace string
	// SiteIDs are the sites this worker serves (SEMAR_SITE_ID, comma
	// separated). One worker per site is the norm; several suit Lite and
	// small labs.
	SiteIDs      []string
	LogLevel     slog.Level
	SyncInterval time.Duration
	// PayloadKeyID identifies PayloadKeys[PayloadKeyID], used for encryption.
	PayloadKeyID string
	// PayloadKeys holds the current key and any previous keys still needed to
	// decrypt existing payloads during rotation.
	PayloadKeys map[string][]byte
	// DatabaseURL is the PostgreSQL URL providers are read from and the
	// inventory is written to (required).
	DatabaseURL string
	// OrphanAfter is how many consecutive syncs a resource may be missing
	// before it is marked orphaned.
	OrphanAfter int
	// ReconcileInterval is how often sync schedules are reconciled with the
	// providers in the database.
	ReconcileInterval time.Duration
	// KEKID and KEKs decrypt provider discovery credentials inside sync
	// activities (ADR-0005). Optional: without them only fake providers can
	// be synced.
	KEKID string
	KEKs  map[string][]byte
	// MetricsAddr is the listen address of /metrics and /healthz; empty
	// disables it.
	MetricsAddr string
}

// LoadWorker reads SEMAR_* variables via getenv (os.Getenv in production).
func LoadWorker(getenv func(string) string) (Worker, error) {
	cfg := Worker{
		TemporalAddress:   "localhost:7233",
		TemporalNamespace: "semar",
		SiteIDs:           []string{"default"},
		LogLevel:          slog.LevelInfo,
		SyncInterval:      60 * time.Second,
		PayloadKeyID:      "k1",
		PayloadKeys:       map[string][]byte{},
		OrphanAfter:       3,
		ReconcileInterval: time.Minute,
	}
	cfg.DatabaseURL = getenv("SEMAR_DATABASE_URL")
	if cfg.DatabaseURL == "" {
		return Worker{}, errors.New("SEMAR_DATABASE_URL is required (postgres://user:password@host:5432/semar)")
	}
	if v := getenv("SEMAR_ORPHAN_AFTER"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 100 {
			return Worker{}, fmt.Errorf("SEMAR_ORPHAN_AFTER: want an integer between 1 and 100, got %q", v)
		}
		cfg.OrphanAfter = n
	}
	if v := getenv("SEMAR_SCHEDULE_RECONCILE_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 10*time.Second {
			return Worker{}, fmt.Errorf("SEMAR_SCHEDULE_RECONCILE_INTERVAL: want a duration of at least 10s, got %q", v)
		}
		cfg.ReconcileInterval = d
	}
	setString(getenv, "SEMAR_TEMPORAL_ADDRESS", &cfg.TemporalAddress)
	setString(getenv, "SEMAR_TEMPORAL_NAMESPACE", &cfg.TemporalNamespace)
	if v := getenv("SEMAR_SITE_ID"); v != "" {
		cfg.SiteIDs = nil
		seen := map[string]bool{}
		for _, s := range strings.Split(v, ",") {
			s = strings.TrimSpace(s)
			if s == "" || seen[s] {
				continue
			}
			if !siteIDPattern.MatchString(s) {
				return Worker{}, fmt.Errorf("SEMAR_SITE_ID: %q is not a site ID", s)
			}
			seen[s] = true
			cfg.SiteIDs = append(cfg.SiteIDs, s)
		}
		if len(cfg.SiteIDs) == 0 {
			return Worker{}, errors.New("SEMAR_SITE_ID: no site given")
		}
	}
	setString(getenv, "SEMAR_PAYLOAD_KEY_ID", &cfg.PayloadKeyID)
	setString(getenv, "SEMAR_METRICS_ADDR", &cfg.MetricsAddr)

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

	keys, err := loadKeys(getenv, cfg.PayloadKeyID, "SEMAR_PAYLOAD_KEY", "SEMAR_PAYLOAD_PREVIOUS_KEYS")
	if err != nil {
		return Worker{}, err
	}
	cfg.PayloadKeys = keys

	if getenv("SEMAR_KEK") != "" {
		cfg.KEKID = "k1"
		setString(getenv, "SEMAR_KEK_ID", &cfg.KEKID)
		keks, err := loadKeys(getenv, cfg.KEKID, "SEMAR_KEK", "SEMAR_KEK_PREVIOUS")
		if err != nil {
			return Worker{}, err
		}
		cfg.KEKs = keks
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
