package config

import (
	"errors"
	"flag"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func smppEnv() map[string]string {
	return map[string]string{
		"SMPP_ADDR":      "smpp.example.com:2775",
		"SMPP_SYSTEM_ID": "user",
		"SMPP_PASSWORD":  "pass",
		"SMS_SENDER":     "MYAPP",
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := load(nil, env(smppEnv()))
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Port != "8080" || cfg.LogLevel != slog.LevelInfo || cfg.Protocol != ProtocolSMPP || cfg.SMPP.TLS {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	s := cfg.SMPP
	if s.ConnectTimeout != 10*time.Second || s.SubmitTimeout != 10*time.Second || s.EnquireLink != 30*time.Second {
		t.Fatalf("unexpected SMPP defaults: %+v", s)
	}
	if s.SourceAddr != "MYAPP" {
		t.Fatalf("SourceAddr = %q, want MYAPP", s.SourceAddr)
	}
	if got := cfg.SendTimeout(); got != 20*time.Second {
		t.Fatalf("SendTimeout = %v, want 20s", got)
	}
}

func TestLoadSMPPOverrides(t *testing.T) {
	e := smppEnv()
	for k, v := range map[string]string{
		"PORT":                 "9000",
		"LOG_LEVEL":            "debug",
		"SMS_PROVIDER":         "My-SMSC",
		"SMPP_TLS":             "true",
		"SMPP_SYSTEM_TYPE":     "cmt",
		"SMS_SENDER":           "+33 7 00 00 00 00",
		"SMPP_CONNECT_TIMEOUT": "3s",
		"SMPP_SUBMIT_TIMEOUT":  "5s",
		"SMPP_ENQUIRE_LINK":    "1m",
	} {
		e[k] = v
	}
	cfg, err := load(nil, env(e))
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	s := cfg.SMPP
	if cfg.Port != "9000" || cfg.LogLevel != slog.LevelDebug || cfg.Provider != "my-smsc" || !s.TLS ||
		s.SystemType != "cmt" || s.SourceAddr != "+33700000000" || s.ConnectTimeout != 3*time.Second ||
		s.SubmitTimeout != 5*time.Second || s.EnquireLink != time.Minute {
		t.Fatalf("overrides not applied: %+v", cfg)
	}
}

func TestLoadSMPPErrors(t *testing.T) {
	_, err := load(nil, env(map[string]string{
		"SMPP_ADDR":           "no-port",
		"SMS_SENDER":          "WAY-TOO-LONG-NAME",
		"SMPP_TLS":            "maybe",
		"SMPP_SUBMIT_TIMEOUT": "-1s",
		"LOG_LEVEL":           "loud",
	}))
	if err == nil {
		t.Fatal("load succeeded, want error")
	}
	for _, want := range []string{
		"SMPP_ADDR", "SMPP_SYSTEM_ID", "SMPP_PASSWORD", "SMS_SENDER",
		"SMPP_TLS", "SMPP_SUBMIT_TIMEOUT", "LOG_LEVEL",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %s: %v", want, err)
		}
	}
}

func TestLoadHTTPProviders(t *testing.T) {
	cfg, err := load(nil, env(map[string]string{
		"SMS_PROTOCOL":       "HTTP",
		"SMS_PROVIDER":       "Twilio",
		"SMS_API_TIMEOUT":    "4s",
		"TWILIO_ACCOUNT_SID": "AC123",
		"TWILIO_AUTH_TOKEN":  "token",
		"SMS_SENDER":         "+33 7 00 00 00 00",
	}))
	if err != nil {
		t.Fatalf("twilio: load error: %v", err)
	}
	if tw := cfg.Twilio; cfg.Protocol != ProtocolHTTP || cfg.Provider != ProviderTwilio ||
		tw.AccountSID != "AC123" || tw.AuthToken != "token" || tw.Sender != "+33700000000" ||
		tw.Timeout != 4*time.Second || cfg.SendTimeout() != 4*time.Second {
		t.Fatalf("twilio: unexpected config: %+v", cfg)
	}

	// A messaging service replaces the sender.
	if _, err := load(nil, env(map[string]string{
		"SMS_PROTOCOL":                 "http",
		"SMS_PROVIDER":                 "twilio",
		"TWILIO_ACCOUNT_SID":           "AC123",
		"TWILIO_AUTH_TOKEN":            "token",
		"TWILIO_MESSAGING_SERVICE_SID": "MG123",
	})); err != nil {
		t.Fatalf("twilio messaging service: load error: %v", err)
	}

	cfg, err = load(nil, env(map[string]string{
		"SMS_PROTOCOL":     "http",
		"SMS_PROVIDER":     "ovh",
		"OVH_SMS_ACCOUNT":  "sms-xx11111-1",
		"OVH_SMS_LOGIN":    "user",
		"OVH_SMS_PASSWORD": "pass",
		"OVH_SMS_NO_STOP":  "true",
		"SMS_SENDER":       "MYAPP",
	}))
	if err != nil {
		t.Fatalf("ovh: load error: %v", err)
	}
	if o := cfg.OVH; o.Account != "sms-xx11111-1" || o.Login != "user" || o.Password != "pass" ||
		!o.NoStop || o.Sender != "MYAPP" || o.Timeout != 10*time.Second {
		t.Fatalf("ovh: unexpected config: %+v", o)
	}

	// Without a sender, ClickSend uses a shared number.
	cfg, err = load(nil, env(map[string]string{
		"SMS_PROTOCOL":       "http",
		"SMS_PROVIDER":       "clicksend",
		"CLICKSEND_USERNAME": "user",
		"CLICKSEND_API_KEY":  "key",
	}))
	if err != nil {
		t.Fatalf("clicksend: load error: %v", err)
	}
	if c := cfg.ClickSend; c.Username != "user" || c.APIKey != "key" || c.Sender != "" {
		t.Fatalf("clicksend: unexpected config: %+v", c)
	}
}

func TestLoadHTTPErrors(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want []string
	}{
		{"no provider", map[string]string{"SMS_PROTOCOL": "http"}, []string{"SMS_PROVIDER is required"}},
		{"unknown provider", map[string]string{"SMS_PROTOCOL": "http", "SMS_PROVIDER": "acme"}, []string{`unknown provider "acme"`}},
		{"unknown protocol", map[string]string{"SMS_PROTOCOL": "smtp"}, []string{`unknown protocol "smtp"`}},
		{"twilio", map[string]string{"SMS_PROTOCOL": "http", "SMS_PROVIDER": "twilio", "SMS_API_TIMEOUT": "soon"},
			[]string{"TWILIO_ACCOUNT_SID", "TWILIO_AUTH_TOKEN", "TWILIO_MESSAGING_SERVICE_SID", "SMS_API_TIMEOUT"}},
		{"ovh", map[string]string{"SMS_PROTOCOL": "http", "SMS_PROVIDER": "ovh", "OVH_SMS_NO_STOP": "maybe"},
			[]string{"OVH_SMS_ACCOUNT", "OVH_SMS_LOGIN", "OVH_SMS_PASSWORD", "SMS_SENDER", "OVH_SMS_NO_STOP"}},
		{"clicksend", map[string]string{"SMS_PROTOCOL": "http", "SMS_PROVIDER": "clicksend"},
			[]string{"CLICKSEND_USERNAME", "CLICKSEND_API_KEY"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := load(nil, env(tt.env))
			if err == nil {
				t.Fatal("load succeeded, want error")
			}
			for _, want := range tt.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not mention %s: %v", want, err)
				}
			}
			if strings.Contains(err.Error(), "SMPP_") {
				t.Errorf("error mentions SMPP settings: %v", err)
			}
		})
	}
}

func TestLoadFlags(t *testing.T) {
	e := map[string]string{
		"SMS_PROTOCOL":       "smpp",
		"SMS_PROVIDER":       "ovh",
		"CLICKSEND_USERNAME": "user",
		"CLICKSEND_API_KEY":  "key",
	}
	cfg, err := load([]string{"-protocol", "http", "-provider=clicksend"}, env(e))
	if err != nil {
		t.Fatalf("load error: %v", err)
	}
	if cfg.Protocol != ProtocolHTTP || cfg.Provider != ProviderClickSend {
		t.Fatalf("flags not applied: protocol %q, provider %q", cfg.Protocol, cfg.Provider)
	}

	if _, err := load([]string{"extra"}, env(e)); err == nil || !strings.Contains(err.Error(), "unexpected arguments") {
		t.Fatalf("load with extra argument: error = %v", err)
	}
}

func TestLoadHelp(t *testing.T) {
	if _, err := load([]string{"-h"}, env(nil)); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("load -h: error = %v, want flag.ErrHelp", err)
	}
}
