package config

import (
	"bytes"
	"encoding/base64"
	"testing"
	"time"
)

func b64key(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }

func TestLoadWorkerRequiresPayloadKey(t *testing.T) {
	if _, err := LoadWorker(env(nil)); err == nil {
		t.Fatal("LoadWorker without SEMAR_PAYLOAD_KEY succeeded")
	}
}

func TestLoadWorkerDefaults(t *testing.T) {
	cfg, err := LoadWorker(env(map[string]string{"SEMAR_PAYLOAD_KEY": b64key(1)}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TemporalAddress != "localhost:7233" || cfg.TemporalNamespace != "semar" || cfg.SiteID != "default" ||
		cfg.SyncInterval != time.Minute || cfg.PayloadKeyID != "k1" || len(cfg.PayloadKeys["k1"]) != 32 ||
		len(cfg.FakeProviders) != 1 || cfg.FakeProviders[0] != "fake-1" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadWorkerRotationKeys(t *testing.T) {
	cfg, err := LoadWorker(env(map[string]string{
		"SEMAR_PAYLOAD_KEY_ID":        "k2",
		"SEMAR_PAYLOAD_KEY":           b64key(2),
		"SEMAR_PAYLOAD_PREVIOUS_KEYS": "k1=" + b64key(1),
		"SEMAR_FAKE_PROVIDERS":        "a, b,,",
		"SEMAR_SYNC_INTERVAL":         "15s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.PayloadKeys) != 2 || cfg.PayloadKeys["k1"][0] != 1 || cfg.PayloadKeys["k2"][0] != 2 {
		t.Fatalf("payload keys = %v", cfg.PayloadKeys)
	}
	if len(cfg.FakeProviders) != 2 || cfg.SyncInterval != 15*time.Second {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadWorkerRejectsInvalid(t *testing.T) {
	base := map[string]string{"SEMAR_PAYLOAD_KEY": b64key(1)}
	for name, extra := range map[string]map[string]string{
		"short key":         {"SEMAR_PAYLOAD_KEY": base64.StdEncoding.EncodeToString([]byte("short"))},
		"not base64":        {"SEMAR_PAYLOAD_KEY": "%%%"},
		"interval too low":  {"SEMAR_SYNC_INTERVAL": "10ms"},
		"bad previous keys": {"SEMAR_PAYLOAD_PREVIOUS_KEYS": "nokey"},
		"duplicate key id":  {"SEMAR_PAYLOAD_PREVIOUS_KEYS": "k1=" + b64key(3)},
		"bad log level":     {"SEMAR_LOG_LEVEL": "loud"},
	} {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range extra {
			m[k] = v
		}
		if _, err := LoadWorker(env(m)); err == nil {
			t.Errorf("%s: LoadWorker succeeded, want error", name)
		}
	}
}
