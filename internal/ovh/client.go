// Package ovh sends SMS through the OVHcloud http2sms endpoint.
//
// See https://docs.ovhcloud.com/en/guides/web-cloud/messaging/sms/send-sms-http2sms
package ovh

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/linxGnu/gosmpp/data"

	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/httpapi"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/message"
)

// DefaultEndpoint is the OVHcloud http2sms URL.
const DefaultEndpoint = "https://www.ovh.com/cgi-bin/sms/http2sms.cgi"

// Config holds the SMS account credentials and sending defaults.
type Config struct {
	Endpoint string
	// Account is the SMS account, e.g. sms-xx11111-1.
	Account string
	// Login and Password identify an SMS user of the account.
	Login    string
	Password string
	// Sender is used when the message does not set its own. It must be
	// declared on the account.
	Sender string
	// NoStop removes the "STOP" mention, for non-advertising messages.
	NoStop  bool
	Timeout time.Duration
}

// APIError is a failure reported by OVH in the response body.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("ovh http2sms status %d: %s", e.Status, e.Message)
}

// Permanent reports whether retrying the same request is pointless:
// 201 (missing parameter) and 202 (invalid parameter).
// Other errors, such as 401 (IP not authorized), depend on the account
// configuration and may succeed once it is fixed.
func (e *APIError) Permanent() bool {
	return e.Status == 201 || e.Status == 202
}

// Client calls the http2sms endpoint.
type Client struct {
	cfg  Config
	http *http.Client
}

// NewClient returns a Client using cfg.
func NewClient(cfg Config) *Client {
	if cfg.Endpoint == "" {
		cfg.Endpoint = DefaultEndpoint
	}
	return &Client{cfg: cfg, http: &http.Client{Timeout: cfg.Timeout}}
}

// Send sends sms and returns the IDs assigned by OVH. OVH splits long texts
// itself.
func (c *Client) Send(ctx context.Context, sms message.SMS) ([]string, error) {
	sender := sms.Sender
	if sender == "" {
		sender = c.cfg.Sender
	}

	q := url.Values{}
	q.Set("account", c.cfg.Account)
	q.Set("login", c.cfg.Login)
	q.Set("password", c.cfg.Password)
	q.Set("from", sender)
	q.Set("to", sms.To)
	q.Set("message", sms.Message)
	q.Set("contentType", "application/json")
	// 1: GSM 7-bit, 2: Unicode.
	if _, err := data.GSM7BIT.Encode(sms.Message); err != nil {
		q.Set("smsCoding", "2")
	} else {
		q.Set("smsCoding", "1")
	}
	if c.cfg.NoStop {
		q.Set("noStop", "1")
	}

	// The URL carries the password: httpapi.Do keeps it out of errors.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.Endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("build http2sms request: invalid endpoint")
	}
	status, body, err := httpapi.Do(c.http, req)
	if err != nil {
		return nil, fmt.Errorf("call http2sms: %w", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("http2sms returned HTTP %d", status)
	}

	var r struct {
		Status  int          `json:"status"`
		Message string       `json:"message"`
		SMSIDs  []flexString `json:"SmsIds"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decode http2sms response: %w", err)
	}
	// Codes from 100 to 199 mean the request was processed.
	if r.Status < 100 || r.Status >= 200 {
		return nil, &APIError{Status: r.Status, Message: r.Message}
	}

	ids := make([]string, 0, len(r.SMSIDs))
	for _, id := range r.SMSIDs {
		ids = append(ids, string(id))
	}
	return ids, nil
}

// Close does nothing: the client keeps no session.
func (c *Client) Close() error { return nil }

// flexString decodes a JSON string or number: OVH returns SMS ids as
// strings, but this is not guaranteed.
type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		*f = flexString(s)
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	*f = flexString(n)
	return nil
}
