// Package config reads the application configuration from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/message"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/smpp"
)

// Config is the application configuration.
type Config struct {
	Port     string
	LogLevel slog.Level
	SMPP     smpp.Config
}

// Load reads the configuration from environment variables and reports every
// missing or invalid one at once.
func Load() (Config, error) {
	return load(os.Getenv)
}

func load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Port: getenv("PORT"),
		SMPP: smpp.Config{
			Addr:           getenv("SMPP_ADDR"),
			SystemID:       getenv("SMPP_SYSTEM_ID"),
			Password:       getenv("SMPP_PASSWORD"),
			SystemType:     getenv("SMPP_SYSTEM_TYPE"),
			SourceAddr:     getenv("SMPP_SOURCE_ADDR"),
			ConnectTimeout: 10 * time.Second,
			SubmitTimeout:  10 * time.Second,
			EnquireLink:    30 * time.Second,
		},
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}

	var errs []error
	for _, v := range []struct{ name, value string }{
		{"SMPP_ADDR", cfg.SMPP.Addr},
		{"SMPP_SYSTEM_ID", cfg.SMPP.SystemID},
		{"SMPP_PASSWORD", cfg.SMPP.Password},
		{"SMPP_SOURCE_ADDR", cfg.SMPP.SourceAddr},
	} {
		if v.value == "" {
			errs = append(errs, fmt.Errorf("%s is required", v.name))
		}
	}

	if cfg.SMPP.Addr != "" {
		if _, _, err := net.SplitHostPort(cfg.SMPP.Addr); err != nil {
			errs = append(errs, fmt.Errorf("SMPP_ADDR: %w", err))
		}
	}
	if cfg.SMPP.SourceAddr != "" {
		src, err := message.NormalizeSender(cfg.SMPP.SourceAddr)
		if err != nil {
			errs = append(errs, fmt.Errorf("SMPP_SOURCE_ADDR: %w", err))
		}
		cfg.SMPP.SourceAddr = src
	}
	if v := getenv("SMPP_TLS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			errs = append(errs, fmt.Errorf("SMPP_TLS: %w", err))
		}
		cfg.SMPP.TLS = b
	}
	for _, d := range []struct {
		name string
		dst  *time.Duration
	}{
		{"SMPP_CONNECT_TIMEOUT", &cfg.SMPP.ConnectTimeout},
		{"SMPP_SUBMIT_TIMEOUT", &cfg.SMPP.SubmitTimeout},
		{"SMPP_ENQUIRE_LINK", &cfg.SMPP.EnquireLink},
	} {
		if v := getenv(d.name); v != "" {
			parsed, err := time.ParseDuration(v)
			if err != nil || parsed <= 0 {
				errs = append(errs, fmt.Errorf("%s: invalid duration %q", d.name, v))
				continue
			}
			*d.dst = parsed
		}
	}
	if v := getenv("LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
		}
	}

	return cfg, errors.Join(errs...)
}
