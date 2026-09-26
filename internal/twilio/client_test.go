package twilio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/SeeMyPing/sqs-to-smpp-gateway/internal/message"
)

func newTestClient(t *testing.T, cfg Config, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	cfg.Endpoint = srv.URL
	cfg.AccountSID = "AC123"
	cfg.AuthToken = "s3cret"
	cfg.Timeout = time.Second
	return NewClient(cfg)
}

func TestSendSuccess(t *testing.T) {
	var (
		path, user, pass string
		form             url.Values
	)
	c := newTestClient(t, Config{Sender: "DEFAULT"}, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		user, pass, _ = r.BasicAuth()
		r.ParseForm()
		form = r.PostForm
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"sid":"SM42","status":"queued"}`))
	})

	ids, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hello"})
	if err != nil {
		t.Fatalf("Send error: %v", err)
	}
	if !reflect.DeepEqual(ids, []string{"SM42"}) {
		t.Fatalf("ids = %v, want [SM42]", ids)
	}
	if path != "/2010-04-01/Accounts/AC123/Messages.json" || user != "AC123" || pass != "s3cret" {
		t.Errorf("path %q, auth %q:%q", path, user, pass)
	}
	want := url.Values{"To": {"+33612345678"}, "Body": {"hello"}, "From": {"DEFAULT"}}
	if !reflect.DeepEqual(form, want) {
		t.Errorf("form = %v, want %v", form, want)
	}
}

func TestSendSenders(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		sender   string
		wantFrom string
		wantMG   string
	}{
		{name: "message sender", cfg: Config{Sender: "DEFAULT"}, sender: "MYAPP", wantFrom: "MYAPP"},
		{name: "messaging service", cfg: Config{MessagingServiceSID: "MG1"}, wantMG: "MG1"},
		{name: "both", cfg: Config{MessagingServiceSID: "MG1"}, sender: "MYAPP", wantFrom: "MYAPP", wantMG: "MG1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var form url.Values
			c := newTestClient(t, tt.cfg, func(w http.ResponseWriter, r *http.Request) {
				r.ParseForm()
				form = r.PostForm
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte(`{"sid":"SM1"}`))
			})
			if _, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hi", Sender: tt.sender}); err != nil {
				t.Fatalf("Send error: %v", err)
			}
			if form.Get("From") != tt.wantFrom || form.Get("MessagingServiceSid") != tt.wantMG {
				t.Errorf("From %q, MessagingServiceSid %q", form.Get("From"), form.Get("MessagingServiceSid"))
			}
		})
	}
}

func TestSendErrors(t *testing.T) {
	tests := []struct {
		name          string
		status        int
		body          string
		wantCode      int
		wantPermanent bool
	}{
		{name: "invalid number", status: 400, body: `{"code":21211,"message":"Invalid 'To' Phone Number","status":400}`, wantCode: 21211, wantPermanent: true},
		{name: "unsubscribed", status: 400, body: `{"code":21610,"message":"Attempt to send to unsubscribed recipient"}`, wantCode: 21610, wantPermanent: true},
		{name: "region not enabled", status: 400, body: `{"code":21408,"message":"Permission to send an SMS has not been enabled"}`, wantCode: 21408},
		{name: "bad credentials", status: 401, body: `{"code":20003,"message":"Authenticate"}`, wantCode: 20003},
		{name: "throttled", status: 429, body: `{"code":20429,"message":"Too Many Requests"}`, wantCode: 20429},
		{name: "HTTP 503 not JSON", status: 503, body: `oops`},
		{name: "no sid", status: 201, body: `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestClient(t, Config{Sender: "DEFAULT"}, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
				w.Write([]byte(tt.body))
			})
			_, err := c.Send(context.Background(), message.SMS{To: "+33612345678", Message: "hi"})
			if err == nil {
				t.Fatal("Send succeeded, want error")
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				if tt.wantCode != 0 {
					t.Fatalf("error = %v, want APIError %d", err, tt.wantCode)
				}
				return
			}
			if apiErr.Code != tt.wantCode || apiErr.HTTPStatus != tt.status || apiErr.Permanent() != tt.wantPermanent {
				t.Fatalf("APIError = %+v (permanent %v), want code %d permanent %v",
					apiErr, apiErr.Permanent(), tt.wantCode, tt.wantPermanent)
			}
		})
	}
}
