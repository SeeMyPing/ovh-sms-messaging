// Package clicksend sends SMS through the ClickSend REST API v3.
//
// See https://developers.clicksend.com/docs/rest/v3/#send-sms
package clicksend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/httpapi"
	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/message"
)

// DefaultEndpoint is the ClickSend REST API base URL.
const DefaultEndpoint = "https://rest.clicksend.com/v3"

// source identifies this application in the ClickSend dashboard.
const source = "sqs-to-smpp-gateway"

// Config holds the ClickSend credentials and sending defaults.
type Config struct {
	Endpoint string
	Username string
	APIKey   string
	// Sender is used when the message does not set its own. Empty, ClickSend
	// sends from a shared number.
	Sender  string
	Timeout time.Duration
}

// APIError is a failure reported by ClickSend, for the whole request
// (HTTP status and response_code) or for the message (its status).
type APIError struct {
	HTTPStatus int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("clicksend HTTP %d: %s", e.HTTPStatus, e.Code)
	}
	return fmt.Sprintf("clicksend HTTP %d: %s: %s", e.HTTPStatus, e.Code, e.Message)
}

// Permanent reports whether the message itself is at fault. Other errors
// (credentials, credit, sender, throttling...) depend on the account or may
// clear up: those messages are retried, then kept in the dead letter queue.
func (e *APIError) Permanent() bool {
	switch e.Code {
	case "INVALID_RECIPIENT", "EMPTY_MESSAGE":
		return true
	}
	return false
}

// Client calls the ClickSend SMS API.
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

type smsMessage struct {
	Source string `json:"source"`
	From   string `json:"from,omitempty"`
	To     string `json:"to"`
	Body   string `json:"body"`
}

// Send sends sms and returns the message ID assigned by ClickSend.
// ClickSend splits long texts itself.
func (c *Client) Send(ctx context.Context, sms message.SMS) ([]string, error) {
	m := smsMessage{Source: source, From: sms.Sender, To: sms.To, Body: sms.Message}
	if m.From == "" {
		m.From = c.cfg.Sender
	}
	payload, err := json.Marshal(struct {
		Messages []smsMessage `json:"messages"`
	}{[]smsMessage{m}})
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	endpoint := strings.TrimSuffix(c.cfg.Endpoint, "/") + "/sms/send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.SetBasicAuth(c.cfg.Username, c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	status, body, err := httpapi.Do(c.http, req)
	if err != nil {
		return nil, fmt.Errorf("call clicksend: %w", err)
	}

	var r struct {
		ResponseCode string `json:"response_code"`
		ResponseMsg  string `json:"response_msg"`
		Data         struct {
			Messages []struct {
				MessageID string `json:"message_id"`
				Status    string `json:"status"`
			} `json:"messages"`
		} `json:"data"`
	}
	jsonErr := json.Unmarshal(body, &r)
	if status != http.StatusOK || (jsonErr == nil && r.ResponseCode != "SUCCESS") {
		if jsonErr != nil {
			return nil, fmt.Errorf("clicksend returned HTTP %d", status)
		}
		return nil, &APIError{HTTPStatus: status, Code: r.ResponseCode, Message: r.ResponseMsg}
	}
	if jsonErr != nil {
		return nil, fmt.Errorf("decode clicksend response: %w", jsonErr)
	}
	if len(r.Data.Messages) != 1 {
		return nil, fmt.Errorf("clicksend response has %d messages, want 1", len(r.Data.Messages))
	}
	msg := r.Data.Messages[0]
	if msg.Status != "SUCCESS" {
		return nil, &APIError{HTTPStatus: status, Code: msg.Status}
	}
	return []string{msg.MessageID}, nil
}

// Close does nothing: the client keeps no session.
func (c *Client) Close() error { return nil }
