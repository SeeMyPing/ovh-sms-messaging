package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(env(map[string]string{
		"OVH_SMS_ACCOUNT":  "sms-xx11111-1",
		"OVH_SMS_LOGIN":    "user",
		"OVH_SMS_PASSWORD": "pass",
		"OVH_SMS_SENDER":   "MYAPP",
	}))
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Port != "8080" || cfg.LogLevel != slog.LevelInfo || cfg.OVH.Timeout != 10*time.Second || cfg.OVH.NoStop {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := load(env(map[string]string{
		"PORT":             "9000",
		"LOG_LEVEL":        "debug",
		"OVH_SMS_ACCOUNT":  "sms-xx11111-1",
		"OVH_SMS_LOGIN":    "user",
		"OVH_SMS_PASSWORD": "pass",
		"OVH_SMS_SENDER":   "MYAPP",
		"OVH_SMS_NO_STOP":  "true",
		"OVH_TIMEOUT":      "5s",
	}))
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Port != "9000" || cfg.LogLevel != slog.LevelDebug || cfg.OVH.Timeout != 5*time.Second || !cfg.OVH.NoStop {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadErrors(t *testing.T) {
	_, err := load(env(map[string]string{
		"OVH_SMS_NO_STOP": "maybe",
		"OVH_TIMEOUT":     "-1s",
		"LOG_LEVEL":       "loud",
	}))
	if err == nil {
		t.Fatal("load succeeded, want error")
	}
	for _, want := range []string{
		"OVH_SMS_ACCOUNT", "OVH_SMS_LOGIN", "OVH_SMS_PASSWORD", "OVH_SMS_SENDER",
		"OVH_SMS_NO_STOP", "OVH_TIMEOUT", "LOG_LEVEL",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}
