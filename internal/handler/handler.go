// Package handler receives the queue messages pushed by the Scaleway trigger.
//
// The trigger deletes the message when the response is 2xx and retries it
// (up to three times) otherwise. Hence:
//   - SMS sent: 200
//   - transient failure (network, OVH unavailable...): 503, to be retried
//   - permanent failure (invalid message, rejected by OVH): 200, since a
//     retry would fail the same way; the failure is logged at error level.
package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/SeeMyPing/ovh-sms-messaging/internal/message"
	"github.com/SeeMyPing/ovh-sms-messaging/internal/ovh"
)

// maxBodySize bounds the accepted message size. Queue messages are far smaller.
const maxBodySize = 256 << 10

// Sender sends an SMS.
type Sender interface {
	Send(ctx context.Context, sms message.SMS) (ovh.Result, error)
}

// New returns the HTTP handler serving POST / (queue messages) and
// GET /healthz.
func New(sender Sender, logger *slog.Logger) http.Handler {
	h := &handler{sender: sender, logger: logger}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("POST /", h.handleMessage)
	return mux
}

type handler struct {
	sender Sender
	logger *slog.Logger
}

func (h *handler) handleMessage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	logger := h.logger
	if logger.Enabled(ctx, slog.LevelDebug) {
		logger.DebugContext(ctx, "message received", "headers", safeHeaders(r.Header))
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodySize))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			logger.ErrorContext(ctx, "message dropped: body too large", "limit", tooLarge.Limit)
			w.WriteHeader(http.StatusOK)
			return
		}
		logger.WarnContext(ctx, "cannot read message body, will be retried", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	sms, err := message.Parse(body)
	if err != nil {
		logger.ErrorContext(ctx, "message dropped: invalid", "error", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	logger = logger.With("to", maskAll(sms.To), "tag", sms.Tag)
	res, err := h.sender.Send(ctx, sms)
	if err != nil {
		var apiErr *ovh.APIError
		if errors.As(err, &apiErr) && apiErr.Permanent() {
			logger.ErrorContext(ctx, "message dropped: rejected by OVH", "error", err)
			w.WriteHeader(http.StatusOK)
			return
		}
		logger.WarnContext(ctx, "SMS not sent, will be retried", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	logger.InfoContext(ctx, "SMS sent", "sms_ids", res.SMSIDs, "credit_left", res.CreditLeft)
	w.WriteHeader(http.StatusOK)
}

func maskAll(numbers []string) []string {
	masked := make([]string, len(numbers))
	for i, n := range numbers {
		masked[i] = message.Mask(n)
	}
	return masked
}

// safeHeaders returns the request headers without credentials, for debugging
// what the trigger sends.
func safeHeaders(h http.Header) map[string]string {
	out := make(map[string]string, len(h))
	for k, v := range h {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "auth") || strings.Contains(lk, "token") ||
			strings.Contains(lk, "secret") || strings.Contains(lk, "cookie") {
			out[k] = "[redacted]"
			continue
		}
		out[k] = strings.Join(v, ", ")
	}
	return out
}
