// Package config reads the application configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/SeeMyPing/ovh-sms-messaging/internal/ovh"
)

// Config is the application configuration.
type Config struct {
	Port     string
	LogLevel slog.Level
	OVH      ovh.Config
}

// Load reads the configuration from environment variables and reports every
// missing or invalid one at once.
func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Port: getenv("PORT"),
		OVH: ovh.Config{
			Endpoint: getenv("OVH_SMS_ENDPOINT"),
			Account:  getenv("OVH_SMS_ACCOUNT"),
			Login:    getenv("OVH_SMS_LOGIN"),
			Password: getenv("OVH_SMS_PASSWORD"),
			Sender:   getenv("OVH_SMS_SENDER"),
			Timeout:  10 * time.Second,
		},
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	var errs []error
	for _, v := range []struct{ name, value string }{
		{"OVH_SMS_ACCOUNT", cfg.OVH.Account},
		{"OVH_SMS_LOGIN", cfg.OVH.Login},
		{"OVH_SMS_PASSWORD", cfg.OVH.Password},
		{"OVH_SMS_SENDER", cfg.OVH.Sender},
	} {
		if v.value == "" {
			errs = append(errs, fmt.Errorf("%s is required", v.name))
		}
	}

	if v := getenv("OVH_SMS_NO_STOP"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("OVH_SMS_NO_STOP: %w", err))
		}
		cfg.OVH.NoStop = b
	}
	if v := getenv("OVH_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			errs = append(errs, fmt.Errorf("OVH_TIMEOUT: invalid duration %q", v))
		}
		cfg.OVH.Timeout = d
	}
	if v := getenv("LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
		}
	}

	return cfg, errors.Join(errs...)
}
