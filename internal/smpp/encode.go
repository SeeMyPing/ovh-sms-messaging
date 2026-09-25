package smpp

import (
	"github.com/linxGnu/gosmpp/data"
)

// Payload limits of a single SMS (3GPP TS 23.038 / 23.040), in encoded units:
// GSM 03.38 septets (one byte each in SMPP, two for extension characters)
// or UCS-2 bytes. Concatenated parts lose 6 bytes (7 septets) to the UDH.
const (
	gsmSingleLimit  = 160
	gsmPartLimit    = 153
	ucs2SingleLimit = 140
	ucs2PartLimit   = 134
)

// encode picks GSM 03.38 when every character supports it, UCS-2 otherwise,
// and splits the text in segments that each fit one SMS. Characters are
// never split across segments (GSM escape sequences, UTF-16 surrogate pairs).
func encode(text string) (data.Encoding, [][]byte, error) {
	enc, single, part := data.Encoding(data.GSM7BIT), gsmSingleLimit, gsmPartLimit
	if _, err := data.GSM7BIT.Encode(text); err != nil {
		enc, single, part = data.UCS2, ucs2SingleLimit, ucs2PartLimit
	}

	all, err := enc.Encode(text)
	if err != nil {
		return nil, nil, err
	}
	if len(all) <= single {
		return enc, [][]byte{all}, nil
	}

	var segments [][]byte
	var current []byte
	for _, r := range text {
		b, err := enc.Encode(string(r))
		if err != nil {
			return nil, nil, err
		}
		if len(current)+len(b) > part {
			segments = append(segments, current)
			current = nil
		}
		current = append(current, b...)
	}
	return enc, append(segments, current), nil
}
