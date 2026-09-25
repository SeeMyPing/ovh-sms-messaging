package smpp

import (
	"bytes"
	"strings"
	"testing"

	"github.com/linxGnu/gosmpp/data"
)

func TestEncode(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantCode  byte
		wantSizes []int
	}{
		{name: "short GSM", text: "hello", wantCode: data.GSM7BITCoding, wantSizes: []int{5}},
		{name: "GSM single limit", text: strings.Repeat("a", 160), wantCode: data.GSM7BITCoding, wantSizes: []int{160}},
		{name: "GSM concatenated", text: strings.Repeat("a", 161), wantCode: data.GSM7BITCoding, wantSizes: []int{153, 8}},
		// "€" is an extension character: two septets, never split.
		{name: "GSM escape at boundary", text: strings.Repeat("a", 152) + "€" + strings.Repeat("b", 10), wantCode: data.GSM7BITCoding, wantSizes: []int{152, 12}},
		{name: "UCS-2 single limit", text: strings.Repeat("ç", 70), wantCode: data.UCS2Coding, wantSizes: []int{140}},
		{name: "UCS-2 concatenated", text: strings.Repeat("ç", 71), wantCode: data.UCS2Coding, wantSizes: []int{134, 8}},
		// Emoji take 4 bytes (surrogate pair): 40 of them do not fit in one SMS.
		{name: "surrogate pairs", text: strings.Repeat("😀", 40), wantCode: data.UCS2Coding, wantSizes: []int{132, 28}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc, segments, err := encode(tt.text)
			if err != nil {
				t.Fatalf("encode error: %v", err)
			}
			if enc.DataCoding() != tt.wantCode {
				t.Errorf("data coding = %d, want %d", enc.DataCoding(), tt.wantCode)
			}
			var sizes []int
			for _, s := range segments {
				sizes = append(sizes, len(s))
			}
			if !equalInts(sizes, tt.wantSizes) {
				t.Errorf("segment sizes = %v, want %v", sizes, tt.wantSizes)
			}

			// Segments must decode back to the original text.
			decoded, err := enc.Decode(bytes.Join(segments, nil))
			if err != nil || decoded != tt.text {
				t.Errorf("round trip = %q, %v", decoded, err)
			}
		})
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
