// Package twilio sends SMS through the Twilio Programmable Messaging API.
//
// See https://www.twilio.com/docs/messaging/api/message-resource#create-a-message-resource
package twilio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/httpapi"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/message"
)

// DefaultEndpoint is the Twilio REST API base URL.
const DefaultEndpoint = "https://api.twilio.com"

// Config holds the Twilio account credentials and sending defaults.
type Config struct {
	Endpoint   string
	AccountSID string
	AuthToken  string
	// MessagingServiceSID, if set, lets Twilio pick the sender from the
	// service's pool when neither the message nor Sender sets one.
	MessagingServiceSID string
	// Sender is used when the message does not set its own.
	Sender  string
	Timeout time.Duration
}

// APIError is a failure reported by Twilio.
type APIError struct {
	HTTPStatus int
	Code       int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("twilio HTTP %d, error %d: %s", e.HTTPStatus, e.Code, e.Message)
}

// Permanent reports whether the message itself is at fault. Other errors
// (credentials, sender, region permissions, throttling...) depend on the
// account or may clear up: those messages are retried, then kept in the
// dead letter queue.
//
// See https://www.twilio.com/docs/api/errors
func (e *APIError) Permanent() bool {
	switch e.Code {
	case 21211, // invalid 'To' phone number
		21602, // message body is required
		21610, // recipient unsubscribed (replied STOP)
		21614, // 'To' is not a valid mobile number
		21617: // body exceeds the 1600 characters limit
		return true
	}
	return false
}

// Client calls the Twilio Messages API.
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

// Send creates a message and returns its SID. Twilio splits long texts
// itself.
func (c *Client) Send(ctx context.Context, sms message.SMS) ([]string, error) {
	form := url.Values{}
	form.Set("To", sms.To)
	form.Set("Body", sms.Message)
	if sender := sms.Sender; sender != "" {
		form.Set("From", sender)
	} else if c.cfg.Sender != "" {
		form.Set("From", c.cfg.Sender)
	}
	if c.cfg.MessagingServiceSID != "" {
		form.Set("MessagingServiceSid", c.cfg.MessagingServiceSID)
	}

	endpoint := fmt.Sprintf("%s/2010-04-01/Accounts/%s/Messages.json",
		strings.TrimSuffix(c.cfg.Endpoint, "/"), url.PathEscape(c.cfg.AccountSID))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(c.cfg.AccountSID, c.cfg.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	status, body, err := httpapi.Do(c.http, req)
	if err != nil {
		return nil, fmt.Errorf("call twilio: %w", err)
	}

	var r struct {
		SID     string `json:"sid"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	jsonErr := json.Unmarshal(body, &r)
	if status < 200 || status >= 300 {
		if jsonErr != nil {
			return nil, fmt.Errorf("twilio returned HTTP %d", status)
		}
		return nil, &APIError{HTTPStatus: status, Code: r.Code, Message: r.Message}
	}
	if jsonErr != nil {
		return nil, fmt.Errorf("decode twilio response: %w", jsonErr)
	}
	if r.SID == "" {
		return nil, fmt.Errorf("twilio response has no message sid")
	}
	return []string{r.SID}, nil
}

// Close does nothing: the client keeps no session.
func (c *Client) Close() error { return nil }
