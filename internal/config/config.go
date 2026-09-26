// Package config reads the application configuration from the command line
// and the environment.
package config

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/clicksend"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/message"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/ovh"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/smpp"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/twilio"
)

// Protocols.
const (
	ProtocolSMPP = "smpp"
	ProtocolHTTP = "http"
)

// Providers reachable over HTTP.
const (
	ProviderTwilio    = "twilio"
	ProviderOVH       = "ovh"
	ProviderClickSend = "clicksend"
)

var httpProviders = []string{ProviderTwilio, ProviderOVH, ProviderClickSend}

// Config is the application configuration.
type Config struct {
	Port     string
	LogLevel slog.Level
	// Protocol is ProtocolSMPP or ProtocolHTTP.
	Protocol string
	// Provider selects the HTTP API. With SMPP, any SMSC works: Provider is
	// optional and only shows in the logs.
	Provider string
	// APITimeout bounds each call to an HTTP API.
	APITimeout time.Duration

	// Only the section of the selected protocol and provider is filled.
	SMPP      smpp.Config
	Twilio    twilio.Config
	OVH       ovh.Config
	ClickSend clicksend.Config
}

// SendTimeout bounds a single send, to let in-flight sends finish on
// shutdown.
func (c Config) SendTimeout() time.Duration {
	if c.Protocol == ProtocolSMPP {
		return c.SMPP.ConnectTimeout + c.SMPP.SubmitTimeout
	}
	return c.APITimeout
}

// Load reads the configuration from the command line arguments (without
// the program name) and the environment, and reports every missing or
// invalid setting at once. Flags take precedence over the environment.
// It returns flag.ErrHelp when -h is given.
func Load(args []string) (Config, error) {
	return load(args, os.Getenv)
}

func load(args []string, getenv func(string) string) (Config, error) {
	fs := flag.NewFlagSet("sqs-to-smpp-gateway", flag.ContinueOnError)
	protocol := fs.String("protocol", getenv("SMS_PROTOCOL"),
		"sending protocol: smpp or http (env SMS_PROTOCOL, default smpp)")
	provider := fs.String("provider", getenv("SMS_PROVIDER"),
		"SMS provider: twilio, ovh or clicksend; required with -protocol http (env SMS_PROVIDER)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: sqs-to-smpp-gateway [-protocol smpp|http] [-provider name]")
		fmt.Fprintln(fs.Output(), "\nThe other settings are read from the environment, see the README.")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %q", fs.Args())
	}

	cfg := Config{
		Port:       getenv("PORT"),
		Protocol:   strings.ToLower(strings.TrimSpace(*protocol)),
		Provider:   strings.ToLower(strings.TrimSpace(*provider)),
		APITimeout: 10 * time.Second,
	}
	if cfg.Port == "" {
		cfg.Port = "8080"
	}
	if cfg.Protocol == "" {
		cfg.Protocol = ProtocolSMPP
	}

	var errs []error
	sender := getenv("SMS_SENDER")
	if sender != "" {
		var err error
		if sender, err = message.NormalizeSender(sender); err != nil {
			errs = append(errs, fmt.Errorf("SMS_SENDER: %w", err))
		}
	}
	if v := getenv("SMS_API_TIMEOUT"); v != "" {
		parseDuration("SMS_API_TIMEOUT", v, &cfg.APITimeout, &errs)
	}

	switch cfg.Protocol {
	case ProtocolSMPP:
		cfg.SMPP = loadSMPP(getenv, sender, &errs)
	case ProtocolHTTP:
		switch cfg.Provider {
		case ProviderTwilio:
			cfg.Twilio = twilio.Config{
				AccountSID:          getenv("TWILIO_ACCOUNT_SID"),
				AuthToken:           getenv("TWILIO_AUTH_TOKEN"),
				MessagingServiceSID: getenv("TWILIO_MESSAGING_SERVICE_SID"),
				Sender:              sender,
				Timeout:             cfg.APITimeout,
			}
			requireAll(&errs, "TWILIO_ACCOUNT_SID", cfg.Twilio.AccountSID, "TWILIO_AUTH_TOKEN", cfg.Twilio.AuthToken)
			if sender == "" && cfg.Twilio.MessagingServiceSID == "" {
				errs = append(errs, errors.New("SMS_SENDER or TWILIO_MESSAGING_SERVICE_SID is required"))
			}
		case ProviderOVH:
			cfg.OVH = ovh.Config{
				Account:  getenv("OVH_SMS_ACCOUNT"),
				Login:    getenv("OVH_SMS_LOGIN"),
				Password: getenv("OVH_SMS_PASSWORD"),
				Sender:   sender,
				Timeout:  cfg.APITimeout,
			}
			requireAll(&errs, "OVH_SMS_ACCOUNT", cfg.OVH.Account, "OVH_SMS_LOGIN", cfg.OVH.Login,
				"OVH_SMS_PASSWORD", cfg.OVH.Password, "SMS_SENDER", sender)
			parseBool("OVH_SMS_NO_STOP", getenv("OVH_SMS_NO_STOP"), &cfg.OVH.NoStop, &errs)
		case ProviderClickSend:
			cfg.ClickSend = clicksend.Config{
				Username: getenv("CLICKSEND_USERNAME"),
				APIKey:   getenv("CLICKSEND_API_KEY"),
				Sender:   sender,
				Timeout:  cfg.APITimeout,
			}
			requireAll(&errs, "CLICKSEND_USERNAME", cfg.ClickSend.Username, "CLICKSEND_API_KEY", cfg.ClickSend.APIKey)
		case "":
			errs = append(errs, fmt.Errorf("SMS_PROVIDER is required with protocol http: %s",
				strings.Join(httpProviders, ", ")))
		default:
			errs = append(errs, fmt.Errorf("SMS_PROVIDER: unknown provider %q for protocol http, want one of %s",
				cfg.Provider, strings.Join(httpProviders, ", ")))
		}
	default:
		errs = append(errs, fmt.Errorf("SMS_PROTOCOL: unknown protocol %q, want %s or %s",
			cfg.Protocol, ProtocolSMPP, ProtocolHTTP))
	}

	if v := getenv("LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			errs = append(errs, fmt.Errorf("LOG_LEVEL: %w", err))
		}
	}

	return cfg, errors.Join(errs...)
}

func loadSMPP(getenv func(string) string, sender string, errs *[]error) smpp.Config {
	cfg := smpp.Config{
		Addr:           getenv("SMPP_ADDR"),
		SystemID:       getenv("SMPP_SYSTEM_ID"),
		Password:       getenv("SMPP_PASSWORD"),
		SystemType:     getenv("SMPP_SYSTEM_TYPE"),
		SourceAddr:     sender,
		ConnectTimeout: 10 * time.Second,
		SubmitTimeout:  10 * time.Second,
		EnquireLink:    30 * time.Second,
	}
	requireAll(errs, "SMPP_ADDR", cfg.Addr, "SMPP_SYSTEM_ID", cfg.SystemID,
		"SMPP_PASSWORD", cfg.Password, "SMS_SENDER", sender)

	if cfg.Addr != "" {
		if _, _, err := net.SplitHostPort(cfg.Addr); err != nil {
			*errs = append(*errs, fmt.Errorf("SMPP_ADDR: %w", err))
		}
	}
	parseBool("SMPP_TLS", getenv("SMPP_TLS"), &cfg.TLS, errs)
	for _, d := range []struct {
		name string
		dst  *time.Duration
	}{
		{"SMPP_CONNECT_TIMEOUT", &cfg.ConnectTimeout},
		{"SMPP_SUBMIT_TIMEOUT", &cfg.SubmitTimeout},
		{"SMPP_ENQUIRE_LINK", &cfg.EnquireLink},
	} {
		if v := getenv(d.name); v != "" {
			parseDuration(d.name, v, d.dst, errs)
		}
	}
	return cfg
}

// requireAll reports each empty value; pairs are name, value.
func requireAll(errs *[]error, pairs ...string) {
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] == "" {
			*errs = append(*errs, fmt.Errorf("%s is required", pairs[i]))
		}
	}
}

func parseBool(name, value string, dst *bool, errs *[]error) {
	if value == "" {
		return
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("%s: %w", name, err))
		return
	}
	*dst = b
}

// parseDuration sets dst if value is a positive duration.
func parseDuration(name, value string, dst *time.Duration, errs *[]error) {
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 {
		*errs = append(*errs, fmt.Errorf("%s: invalid duration %q", name, value))
		return
	}
	*dst = d
}
