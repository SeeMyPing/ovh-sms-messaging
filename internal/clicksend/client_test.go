package clicksend

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/message"
)

func newTestClient(t *testing.T, sender string, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(Config{
		Endpoint: srv.URL,
		Username: "user",
		APIKey:   "s3cret",
		Sender:   sender,
		Timeout:  time.Second,
	})
}

const successBody = `{"http_code":200,"response_code":"SUCCESS","response_msg":"Messages queued for delivery.",
	"data":{"queued_count":1,"messages":[{"to":"+33612345678","message_id":"BF7AD270","status":"SUCCESS"}]}}`

func TestSendSuccess(t *testing.T) {
	var (
		path, user, pass string
		got              struct{ Messages []smsMessage }
	)
	c := newTestClient(t, "DEFAULT", func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		user, pass, _ = r.BasicAuth()
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(successBody))
	})

	ids, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hello"})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if !reflect.DeepEqual(ids, []string{"BF7AD270"}) {
		t.Fatalf("ids = %v, want [BF7AD270]", ids)
	}
	if path != "/sms/send" || user != "user" || pass != "s3cret" {
		t.Errorf("path %q, auth %q:%q", path, user, pass)
	}
	want := []smsMessage{{Source: source, From: "DEFAULT", To: "+33612345678", Body: "hello"}}
	if !reflect.DeepEqual(got.Messages, want) {
		t.Errorf("messages = %+v, want %+v", got.Messages, want)
	}
}

func TestSendSenders(t *testing.T) {
	tests := []struct {
		name, defaultSender, sender, want string
	}{
		{"message sender", "DEFAULT", "MYAPP", "MYAPP"},
		{"default sender", "DEFAULT", "", "DEFAULT"},
		{"shared number", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got struct {
				Messages []map[string]any
			}
			c := newTestClient(t, tt.defaultSender, func(w http.ResponseWriter, r *http.Request) {
				json.NewDecoder(r.Body).Decode(&got)
				w.Write([]byte(successBody))
			})
			if _, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hi", Sender: tt.sender}); err != nil {
				t.Fatalf("Send error: %v", err)
			}
			from, set := got.Messages[0]["from"]
			if tt.want == "" && set {
				t.Fatalf("from = %v, want unset", from)
			}
			if tt.want != "" && from != tt.want {
				t.Fatalf("from = %v, want %s", from, tt.want)
			}
		})
	}
}

func TestSendErrors(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body          string
		wantCode      string
		wantPermanent bool
	}{
		{name: "invalid recipient", status: 200, body: `{"http_code":200,"response_code":"SUCCESS","data":{"messages":[{"status":"INVALID_RECIPIENT"}]}}`, wantCode: "INVALID_RECIPIENT", wantPermanent: true},
		{name: "no credit", status: 200, body: `{"http_code":200,"response_code":"SUCCESS","data":{"messages":[{"status":"INSUFFICIENT_CREDIT"}]}}`, wantCode: "INSUFFICIENT_CREDIT"},
		{name: "bad credentials", status: 401, body: `{"http_code":401,"response_code":"UNAUTHORIZED","response_msg":"Invalid credentials"}`, wantCode: "UNAUTHORIZED"},
		{name: "throttled", status: 429, body: `{"http_code":429,"response_code":"TOO_MANY_REQUESTS"}`, wantCode: "TOO_MANY_REQUESTS"},
		{name: "HTTP 500 not JSON", status: 500, body: `oops`},
		{name: "no message", status: 200, body: `{"http_code":200,"response_code":"SUCCESS","data":{"messages":[]}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, "", func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			})
			_, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hi"})
			if err == nil {
				t.Fatal("Send succeeded, want error")
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				if tt.wantCode != "" {
					t.Fatalf("error = %v, want APIError %s", err, tt.wantCode)
				}
				return
			}
			if apiErr.Code != tt.wantCode || apiErr.Permanent() != tt.wantPermanent {
				t.Fatalf("APIError = %+v (permanent %v), want code %s permanent %v",
					apiErr, apiErr.Permanent(), tt.wantCode, tt.wantPermanent)
			}
		})
	}
}
