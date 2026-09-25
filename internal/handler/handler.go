// Package handler receives the queue messages pushed by the Scaleway trigger.
//
// The trigger deletes the message when the response is 2xx and retries it
// (up to three times) otherwise. Hence:
//   - SMS sent: 200
//   - transient failure (network, SMSC unavailable or throttling...): 503,
//     to be retried
//   - permanent failure (invalid message, rejected by the SMSC): 200, since
//     a retry would fail the same way; the failure is logged at error level.
package handler

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/message"
)

// maxBodySize bounds the accepted message size. Queue messages are far smaller.
const maxBodySize = 256 << 10

// Sender sends an SMS and returns the IDs assigned by the provider.
//
// An error implementing Permanent() bool, and returning true, means the
// message itself was rejected: it is dropped instead of being retried.
type Sender interface {
	Send(ctx context.Context, sms message.SMS) ([]string, error)
}

type permanent interface {
	Permanent() bool
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

	logger = logger.With("to", message.Mask(sms.To))
	ids, err := h.sender.Send(ctx, sms)
	if err != nil {
		var p permanent
		if errors.As(err, &p) && p.Permanent() {
			logger.ErrorContext(ctx, "message dropped: rejected", "error", err)
			w.WriteHeader(http.StatusOK)
			return
		}
		logger.WarnContext(ctx, "SMS not sent, will be retried", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	logger.InfoContext(ctx, "SMS sent", "message_ids", ids)
	w.WriteHeader(http.StatusOK)
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
