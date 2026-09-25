// Package ovh sends SMS through the OVHcloud http2sms endpoint.
//
// See https://help.ovhcloud.com/csm/en-gb-sms-sending-via-url-http2sms
package ovh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SeeMyPing/ovh-sms-messaging/internal/message"
)

// DefaultEndpoint is the OVHcloud http2sms URL.
const DefaultEndpoint = "https://www.ovh.com/cgi-bin/sms/http2sms.cgi"

// maxResponseSize bounds how much of the OVH response is read.
const maxResponseSize = 1 << 20

// Config holds the SMS account credentials and sending defaults.
type Config struct {
	Endpoint string
	Account  string
	Login    string
	Password string
	// Sender is used when the message does not set its own.
	Sender string
	// NoStop removes the "STOP" mention, for non-advertising messages.
	NoStop  bool
	Timeout time.Duration
}

// Result is the outcome of a successful send.
type Result struct {
	CreditLeft string
	SMSIDs     []string
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

// Send sends sms to all its recipients in a single request.
func (c *Client) Send(ctx context.Context, sms message.SMS) (Result, error) {
	sender := sms.Sender
	if sender == "" {
		sender = c.cfg.Sender
	}

	q := url.Values{}
	q.Set("account", c.cfg.Account)
	q.Set("login", c.cfg.Login)
	q.Set("password", c.cfg.Password)
	q.Set("from", sender)
	q.Set("to", strings.Join(sms.To, ","))
	q.Set("message", sms.Message)
	q.Set("contentType", "application/json")
	if sms.Tag != "" {
		q.Set("tag", sms.Tag)
	}
	if c.cfg.NoStop {
		q.Set("noStop", "1")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.Endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return Result{}, fmt.Errorf("build request: %w", redact(err))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("call http2sms: %w", redact(err))
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return Result{}, fmt.Errorf("read http2sms response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return Result{}, fmt.Errorf("http2sms returned HTTP %d", resp.StatusCode)
	}

	var r struct {
		Status     int          `json:"status"`
		Message    string       `json:"message"`
		CreditLeft flexString   `json:"creditLeft"`
		SMSIDs     []flexString `json:"SmsIds"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Result{}, fmt.Errorf("decode http2sms response: %w", err)
	}
	// Codes from 100 to 199 mean the request was processed.
	if r.Status < 100 || r.Status >= 200 {
		return Result{}, &APIError{Status: r.Status, Message: r.Message}
	}

	res := Result{CreditLeft: string(r.CreditLeft)}
	for _, id := range r.SMSIDs {
		res.SMSIDs = append(res.SMSIDs, string(id))
	}
	return res, nil
}

// redact drops the request URL from net/http errors: it carries the password.
func redact(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("%s: %w", ue.Op, ue.Err)
	}
	return err
}

// flexString decodes a JSON string or number: OVH returns creditLeft and
// SMS ids as strings, but this is not guaranteed.
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
