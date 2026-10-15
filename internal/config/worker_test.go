package config

import (
	"bytes"
	"encoding/base64"
	"testing"
	"time"
)

func b64key(b byte) string { return base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{b}, 32)) }

// workerEnv returns the minimal valid worker environment plus extra.
func workerEnv(extra map[string]string) func(string) string {
	m := map[string]string{"SEMAR_PAYLOAD_KEY": b64key(1), "SEMAR_DATABASE_URL": testDBURL}
	for k, v := range extra {
		m[k] = v
	}
	return env(m)
}

func TestLoadWorkerRequiresPayloadKeyAndDatabase(t *testing.T) {
	if _, err := LoadWorker(env(map[string]string{"SEMAR_DATABASE_URL": testDBURL})); err == nil {
		t.Fatal("LoadWorker without SEMAR_PAYLOAD_KEY succeeded")
	}
	if _, err := LoadWorker(env(map[string]string{"SEMAR_PAYLOAD_KEY": b64key(1)})); err == nil {
		t.Fatal("LoadWorker without SEMAR_DATABASE_URL succeeded")
	}
}

func TestLoadWorkerDefaults(t *testing.T) {
	cfg, err := LoadWorker(workerEnv(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TemporalAddress != "localhost:7233" || cfg.TemporalNamespace != "semar" || cfg.SiteID != "default" ||
		cfg.SyncInterval != time.Minute || cfg.PayloadKeyID != "k1" || len(cfg.PayloadKeys["k1"]) != 32 ||
		cfg.DatabaseURL != testDBURL || cfg.OrphanAfter != 3 || cfg.ReconcileInterval != time.Minute {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadWorkerOverrides(t *testing.T) {
	cfg, err := LoadWorker(workerEnv(map[string]string{
		"SEMAR_PAYLOAD_KEY_ID":              "k2",
		"SEMAR_PAYLOAD_KEY":                 b64key(2),
		"SEMAR_PAYLOAD_PREVIOUS_KEYS":       "k1=" + b64key(1),
		"SEMAR_SYNC_INTERVAL":               "15s",
		"SEMAR_ORPHAN_AFTER":                "5",
		"SEMAR_SCHEDULE_RECONCILE_INTERVAL": "30s",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.PayloadKeys) != 2 || cfg.PayloadKeys["k1"][0] != 1 || cfg.PayloadKeys["k2"][0] != 2 {
		t.Fatalf("payload keys = %v", cfg.PayloadKeys)
	}
	if cfg.SyncInterval != 15*time.Second || cfg.OrphanAfter != 5 || cfg.ReconcileInterval != 30*time.Second {
		t.Fatalf("cfg = %+v", cfg)
	}
}

func TestLoadWorkerRejectsInvalid(t *testing.T) {
	for name, extra := range map[string]map[string]string{
		"short key":          {"SEMAR_PAYLOAD_KEY": base64.StdEncoding.EncodeToString([]byte("short"))},
		"not base64":         {"SEMAR_PAYLOAD_KEY": "%%%"},
		"interval too low":   {"SEMAR_SYNC_INTERVAL": "10ms"},
		"bad previous keys":  {"SEMAR_PAYLOAD_PREVIOUS_KEYS": "nokey"},
		"duplicate key id":   {"SEMAR_PAYLOAD_PREVIOUS_KEYS": "k1=" + b64key(3)},
		"bad log level":      {"SEMAR_LOG_LEVEL": "loud"},
		"orphan after zero":  {"SEMAR_ORPHAN_AFTER": "0"},
		"orphan after text":  {"SEMAR_ORPHAN_AFTER": "many"},
		"reconcile too fast": {"SEMAR_SCHEDULE_RECONCILE_INTERVAL": "1s"},
	} {
		if _, err := LoadWorker(workerEnv(extra)); err == nil {
			t.Errorf("%s: LoadWorker succeeded, want error", name)
		}
	}
}

func TestLoadWorkerCredentialKEK(t *testing.T) {
	cfg, err := LoadWorker(workerEnv(nil))
	if err != nil || cfg.KEKs != nil {
		t.Fatalf("without SEMAR_KEK: KEKs = %v, %v", cfg.KEKs, err)
	}
	cfg, err = LoadWorker(workerEnv(map[string]string{
		"SEMAR_KEK_ID": "k2", "SEMAR_KEK": b64key(2), "SEMAR_KEK_PREVIOUS": "k1=" + b64key(1),
	}))
	if err != nil || cfg.KEKID != "k2" || len(cfg.KEKs) != 2 || cfg.KEKs["k1"][0] != 1 {
		t.Fatalf("KEKs = %v (current %s), %v", cfg.KEKs, cfg.KEKID, err)
	}
	if _, err := LoadWorker(workerEnv(map[string]string{"SEMAR_KEK": "short"})); err == nil {
		t.Fatal("invalid SEMAR_KEK accepted")
	}
}
