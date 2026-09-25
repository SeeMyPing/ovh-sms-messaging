package message

import (
	"errors"
	"reflect"
	"testing"
)

func TestNormalizeNumber(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "+33612345678", want: "+33612345678"},
		{in: "0033612345678", want: "+33612345678"},
		{in: "+33 6 12 34 56 78", want: "+33612345678"},
		{in: "+33-6.12.34.56.78", want: "+33612345678"},
		{in: "+1 (415) 555-2671", want: "+14155552671"},
		{in: "0612345678", wantErr: true},
		{in: "", wantErr: true},
		{in: "+", wantErr: true},
		{in: "+33abc", wantErr: true},
		{in: "+0612345678", wantErr: true},
		{in: "+1234567", wantErr: true},
		{in: "+1234567890123456", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := NormalizeNumber(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeNumber(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeNumber(%q) error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("NormalizeNumber(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestParse(t *testing.T) {
	got, err := Parse([]byte(`{"to":["+33612345678","0033700000000"],"message":"hello","sender":"MYAPP","tag":"otp"}`))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	want := SMS{
		To:      []string{"+33612345678", "+33700000000"},
		Message: "hello",
		Sender:  "MYAPP",
		Tag:     "otp",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse = %+v, want %+v", got, want)
	}
}

func TestParseInvalid(t *testing.T) {
	tests := map[string]string{
		"not JSON":       `hello`,
		"to as string":   `{"to":"+33612345678","message":"hello"}`,
		"no recipient":   `{"message":"hello"}`,
		"empty to":       `{"to":[],"message":"hello"}`,
		"empty message":  `{"to":["+33612345678"],"message":"  "}`,
		"national phone": `{"to":["0612345678"],"message":"hello"}`,
		"tag too long":   `{"to":["+33612345678"],"message":"hello","tag":"123456789012345678901"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(body))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("Parse(%s) error = %v, want ErrInvalid", body, err)
			}
		})
	}
}

func TestMask(t *testing.T) {
	if got, want := Mask("+33612345678"), "+33*******78"; got != want {
		t.Fatalf("Mask = %q, want %q", got, want)
	}
	if got, want := Mask("1234"), "****"; got != want {
		t.Fatalf("Mask = %q, want %q", got, want)
	}
}
