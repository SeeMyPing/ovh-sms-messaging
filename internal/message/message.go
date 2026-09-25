// Package message decodes and validates the SMS requests read from the queue.
package message

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// ErrInvalid marks a message that can never be sent, whatever the number of
// retries: malformed JSON, missing fields or invalid phone numbers.
var ErrInvalid = errors.New("invalid message")

// MaxLength is the maximum message length, in characters. Longer texts are
// sent as several concatenated SMS: 1600 characters is about ten of them.
const MaxLength = 1600

// maxAlphanumericSender is the length limit of an alphanumeric sender.
const maxAlphanumericSender = 11

// SMS is the payload expected in the queue message body.
type SMS struct {
	To      string `json:"to"`
	Message string `json:"message"`
	Sender  string `json:"sender,omitempty"`
}

// Parse decodes a queue message body, validates it and normalizes the
// recipient and sender.
func Parse(body []byte) (SMS, error) {
	var sms SMS
	if err := json.Unmarshal(body, &sms); err != nil {
		return SMS{}, fmt.Errorf("%w: decode JSON: %v", ErrInvalid, err)
	}
	if strings.TrimSpace(sms.Message) == "" {
		return SMS{}, fmt.Errorf("%w: empty message", ErrInvalid)
	}
	if n := utf8.RuneCountInString(sms.Message); n > MaxLength {
		return SMS{}, fmt.Errorf("%w: message is %d characters long, limit is %d", ErrInvalid, n, MaxLength)
	}
	if sms.To == "" {
		return SMS{}, fmt.Errorf("%w: no recipient", ErrInvalid)
	}

	var err error
	if sms.To, err = NormalizeNumber(sms.To); err != nil {
		return SMS{}, fmt.Errorf("%w: recipient: %v", ErrInvalid, err)
	}
	if sms.Sender != "" {
		if sms.Sender, err = NormalizeSender(sms.Sender); err != nil {
			return SMS{}, fmt.Errorf("%w: sender: %v", ErrInvalid, err)
		}
	}
	return sms, nil
}

// NormalizeNumber converts an international phone number (+33 6 12 34 56 78,
// 0033612345678, +33-6.12.34.56.78) to its E.164 form (+33612345678).
// National numbers (0612345678) are rejected: the country cannot be guessed.
func NormalizeNumber(raw string) (string, error) {
	n := stripSeparators(raw)

	var digits string
	switch {
	case strings.HasPrefix(n, "+"):
		digits = n[1:]
	case strings.HasPrefix(n, "00"):
		digits = n[2:]
	default:
		return "", fmt.Errorf("%q is not in international format (+33… or 0033…)", raw)
	}

	if !isDigits(digits) {
		return "", fmt.Errorf("%q contains invalid characters", raw)
	}
	// E.164 numbers have at most 15 digits, country code included.
	if len(digits) < 8 || len(digits) > 15 || digits[0] == '0' {
		return "", fmt.Errorf("%q is not a valid international number", raw)
	}
	return "+" + digits, nil
}

// NormalizeSender validates a sender, which is either:
//   - an international number (+33612345678), normalized like NormalizeNumber;
//   - a short code or other numeric sender (36180), up to 15 digits;
//   - an alphanumeric name (MYAPP), up to 11 printable ASCII characters.
func NormalizeSender(raw string) (string, error) {
	if n := stripSeparators(raw); strings.HasPrefix(n, "+") {
		return NormalizeNumber(raw)
	} else if isDigits(n) {
		if len(n) > 15 {
			return "", fmt.Errorf("numeric sender %q is longer than 15 digits", raw)
		}
		return n, nil
	}

	if len(raw) > maxAlphanumericSender {
		return "", fmt.Errorf("alphanumeric sender %q is longer than %d characters", raw, maxAlphanumericSender)
	}
	for _, r := range raw {
		if r < ' ' || r > '~' {
			return "", fmt.Errorf("alphanumeric sender %q contains non-ASCII characters", raw)
		}
	}
	return raw, nil
}

// Mask hides all but the last two digits of a phone number, for logging.
func Mask(number string) string {
	if len(number) <= 6 {
		return strings.Repeat("*", len(number))
	}
	return number[:3] + strings.Repeat("*", len(number)-5) + number[len(number)-2:]
}

func stripSeparators(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case ' ', '.', '-', '(', ')':
			return -1
		}
		return r
	}, s)
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
