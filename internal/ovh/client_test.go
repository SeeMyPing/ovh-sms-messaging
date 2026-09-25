package ovh

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/SeeMyPing/ovh-sms-messaging/internal/message"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient(Config{
		Endpoint: srv.URL,
		Account:  "sms-xx11111-1",
		Login:    "user",
		Password: "s3cret",
		Sender:   "DEFAULT",
		NoStop:   true,
		Timeout:  time.Second,
	})
}

func TestSendSuccess(t *testing.T) {
	var got url.Values
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		w.Write([]byte(`{"status":100,"creditLeft":"1987","SmsIds":["10867690",42]}`))
	})

	res, err := c.Send(context.Background(), message.SMS{
		To:      []string{"+33612345678", "+33700000000"},
		Message: "hello world",
		Tag:     "otp",
	})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if want := (Result{CreditLeft: "1987", SMSIDs: []string{"10867690", "42"}}); !reflect.DeepEqual(res, want) {
		t.Fatalf("Send = %+v, want %+v", res, want)
	}

	want := url.Values{
		"account":     {"sms-xx11111-1"},
		"login":       {"user"},
		"password":    {"s3cret"},
		"from":        {"DEFAULT"},
		"to":          {"+33612345678,+33700000000"},
		"message":     {"hello world"},
		"contentType": {"application/json"},
		"tag":         {"otp"},
		"noStop":      {"1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("query = %v, want %v", got, want)
	}
}

func TestSendMessageSender(t *testing.T) {
	var from string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		from = r.URL.Query().Get("from")
		w.Write([]byte(`{"status":101}`))
	})
	if _, err := c.Send(context.Background(), message.SMS{To: []string{"+33612345678"}, Message: "hi", Sender: "MYAPP"}); err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if from != "MYAPP" {
		t.Fatalf("from = %q, want MYAPP", from)
	}
}

func TestSendErrors(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body          string
		wantAPIStatus int
		wantPermanent bool
	}{
		{name: "missing parameter", status: 200, body: `{"status":201,"message":"Missing message"}`, wantAPIStatus: 201, wantPermanent: true},
		{name: "invalid parameter", status: 200, body: `{"status":202,"message":"Invalid tag"}`, wantAPIStatus: 202, wantPermanent: true},
		{name: "unauthorized IP", status: 200, body: `{"status":401,"message":"No authorized IP"}`, wantAPIStatus: 401},
		{name: "HTTP 503", status: 503, body: `oops`},
		{name: "not JSON", status: 200, body: `OK`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			})
			_, err := c.Send(context.Background(), message.SMS{To: []string{"+33612345678"}, Message: "hi"})
			if err == nil {
				t.Fatal("Send succeeded, want error")
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				if tt.wantAPIStatus != 0 {
					t.Fatalf("error = %v, want APIError %d", err, tt.wantAPIStatus)
				}
				return
			}
			if apiErr.Status != tt.wantAPIStatus || apiErr.Permanent() != tt.wantPermanent {
				t.Fatalf("APIError = %+v (permanent %v), want status %d permanent %v",
					apiErr, apiErr.Permanent(), tt.wantAPIStatus, tt.wantPermanent)
			}
		})
	}
}

func TestSendNetworkErrorHidesPassword(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	})
	c.http.Timeout = 50 * time.Millisecond

	_, err := c.Send(context.Background(), message.SMS{To: []string{"+33612345678"}, Message: "hi"})
	if err == nil {
		t.Fatal("Send succeeded, want timeout")
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("error leaks the password: %v", err)
	}
}
