package message

import (
	"errors"
	"strings"
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
	got, err := Parse([]byte(`{"to":"0033612345678","message":"hello","sender":"MYAPP"}`))
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	want := SMS{To: "+33612345678", Message: "hello", Sender: "MYAPP"}
	if got != want {
		t.Fatalf("Parse = %+v, want %+v", got, want)
	}
}

func TestParseInvalid(t *testing.T) {
	tests := map[string]string{
		"not JSON":         `hello`,
		"to as array":      `{"to":["+33612345678"],"message":"hello"}`,
		"no recipient":     `{"message":"hello"}`,
		"empty message":    `{"to":"+33612345678","message":"  "}`,
		"message too long": `{"to":"+33612345678","message":"` + strings.Repeat("a", MaxLength+1) + `"}`,
		"national phone":   `{"to":"0612345678","message":"hello"}`,
		"invalid sender":   `{"to":"+33612345678","message":"hello","sender":"TOO-LONG-NAME"}`,
	}
	for name, body := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := Parse([]byte(body))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("Parse(%.60s) error = %v, want ErrInvalid", body, err)
			}
		})
	}
}

func TestParseMaxLength(t *testing.T) {
	body := `{"to":"+33612345678","message":"` + strings.Repeat("é", MaxLength) + `"}`
	if _, err := Parse([]byte(body)); err != nil {
		t.Fatalf("Parse error for %d characters: %v", MaxLength, err)
	}
}

func TestNormalizeSender(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "MYAPP", want: "MYAPP"},
		{in: "My App 2", want: "My App 2"},
		{in: "12345678901", want: "12345678901"},
		{in: "36180", want: "36180"},
		{in: "+33 6 12 34 56 78", want: "+33612345678"},
		{in: "123456789012", wantErr: false, want: "123456789012"},
		{in: "ABCDEFGHIJKL", wantErr: true},
		{in: "Café", wantErr: true},
		{in: "1234567890123456", wantErr: true},
		{in: "+0612345678", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := NormalizeSender(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("NormalizeSender(%q) = %q, want error", tt.in, got)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("NormalizeSender(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
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
