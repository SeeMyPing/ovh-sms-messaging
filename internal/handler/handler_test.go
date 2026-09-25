package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/message"
)

type fakeSender struct {
	err  error
	sent []message.SMS
}

func (f *fakeSender) Send(_ context.Context, sms message.SMS) ([]string, error) {
	f.sent = append(f.sent, sms)
	return []string{"1"}, f.err
}

type statusError struct{ permanent bool }

func (e statusError) Error() string   { return "rejected" }
func (e statusError) Permanent() bool { return e.permanent }

const validBody = `{"to":"+33612345678","message":"hello"}`

func TestHandleMessage(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		sendErr  error
		wantCode int
		wantSent int
	}{
		{name: "sent", body: validBody, wantCode: http.StatusOK, wantSent: 1},
		{name: "invalid JSON", body: `nope`, wantCode: http.StatusOK},
		{name: "invalid number", body: `{"to":"0612345678","message":"hello"}`, wantCode: http.StatusOK},
		{name: "body too large", body: `{"message":"` + strings.Repeat("a", maxBodySize) + `"}`, wantCode: http.StatusOK},
		{name: "permanent rejection", body: validBody, sendErr: fmt.Errorf("wrapped: %w", statusError{permanent: true}), wantCode: http.StatusOK, wantSent: 1},
		{name: "transient rejection", body: validBody, sendErr: statusError{permanent: false}, wantCode: http.StatusServiceUnavailable, wantSent: 1},
		{name: "network error", body: validBody, sendErr: errors.New("timeout"), wantCode: http.StatusServiceUnavailable, wantSent: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sender := &fakeSender{err: tt.sendErr}
			h := New(sender, slog.New(slog.NewTextHandler(io.Discard, nil)))

			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body)))

			if rec.Code != tt.wantCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if len(sender.sent) != tt.wantSent {
				t.Errorf("sent %d SMS, want %d", len(sender.sent), tt.wantSent)
			}
		})
	}
}

func TestRoutes(t *testing.T) {
	h := New(&fakeSender{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	tests := []struct {
		method, path string
		want         int
	}{
		{http.MethodGet, "/healthz", http.StatusOK},
		{http.MethodGet, "/", http.StatusMethodNotAllowed},
		{http.MethodPost, "/any/path", http.StatusOK},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, strings.NewReader(validBody)))
		if rec.Code != tt.want {
			t.Errorf("%s %s = %d, want %d", tt.method, tt.path, rec.Code, tt.want)
		}
	}
}

func TestSafeHeaders(t *testing.T) {
	got := safeHeaders(http.Header{
		"Authorization": {"Bearer x"},
		"X-Auth-Token":  {"y"},
		"Content-Type":  {"application/json"},
		"X-Message-Id":  {"1"},
	})
	if got["Authorization"] != "[redacted]" || got["X-Auth-Token"] != "[redacted]" {
		t.Errorf("credentials not redacted: %v", got)
	}
	if got["Content-Type"] != "application/json" || got["X-Message-Id"] != "1" {
		t.Errorf("headers altered: %v", got)
	}
}
