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

func required() map[string]string {
	return map[string]string{
		"SMPP_ADDR":        "smpp.example.com:2775",
		"SMPP_SYSTEM_ID":   "user",
		"SMPP_PASSWORD":    "pass",
		"SMPP_SOURCE_ADDR": "MYAPP",
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(env(required()))
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Port != "8080" || cfg.LogLevel != slog.LevelInfo || cfg.SMPP.TLS {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	s := cfg.SMPP
	if s.ConnectTimeout != 10*time.Second || s.SubmitTimeout != 10*time.Second || s.EnquireLink != 30*time.Second {
		t.Fatalf("unexpected SMPP defaults: %+v", s)
	}
}

func TestLoadOverrides(t *testing.T) {
	e := required()
	for k, v := range map[string]string{
		"PORT":                 "9000",
		"LOG_LEVEL":            "debug",
		"SMPP_TLS":             "true",
		"SMPP_SYSTEM_TYPE":     "cmt",
		"SMPP_SOURCE_ADDR":     "+33 7 00 00 00 00",
		"SMPP_CONNECT_TIMEOUT": "3s",
		"SMPP_SUBMIT_TIMEOUT":  "5s",
		"SMPP_ENQUIRE_LINK":    "1m",
	} {
		e[k] = v
	}
	cfg, err := load(env(e))
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	s := cfg.SMPP
	if cfg.Port != "9000" || cfg.LogLevel != slog.LevelDebug || !s.TLS || s.SystemType != "cmt" ||
		s.SourceAddr != "+33700000000" || s.ConnectTimeout != 3*time.Second ||
		s.SubmitTimeout != 5*time.Second || s.EnquireLink != time.Minute {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadErrors(t *testing.T) {
	_, err := load(env(map[string]string{
		"SMPP_ADDR":           "no-port",
		"SMPP_SOURCE_ADDR":    "WAY-TOO-LONG-NAME",
		"SMPP_TLS":            "maybe",
		"SMPP_SUBMIT_TIMEOUT": "-1s",
		"LOG_LEVEL":           "loud",
	}))
	if err == nil {
		t.Fatal("load succeeded, want error")
	}
	for _, want := range []string{
		"SMPP_ADDR", "SMPP_SYSTEM_ID", "SMPP_PASSWORD", "SMPP_SOURCE_ADDR",
		"SMPP_TLS", "SMPP_SUBMIT_TIMEOUT", "LOG_LEVEL",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}
