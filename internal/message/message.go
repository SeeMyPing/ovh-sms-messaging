// Package message decodes and validates the SMS requests read from the queue.
package message

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrInvalid marks a message that can never be sent, whatever the number of
// retries: malformed JSON, missing fields or invalid phone numbers.
var ErrInvalid = errors.New("invalid message")

// SMS is the payload expected in the queue message body.
type SMS struct {
	To      []string `json:"to"`
	Message string   `json:"message"`
	Sender  string   `json:"sender,omitempty"`
	Tag     string   `json:"tag,omitempty"`
}

// maxTagLength is the tag length limit enforced by OVH.
const maxTagLength = 20

// Parse decodes a queue message body, validates it and normalizes the
// recipients to the international format expected by OVH (+33612345678).
func Parse(body []byte) (SMS, error) {
	var sms SMS
	if err := json.Unmarshal(body, &sms); err != nil {
		return SMS{}, fmt.Errorf("%w: decode JSON: %v", ErrInvalid, err)
	}
	if strings.TrimSpace(sms.Message) == "" {
		return SMS{}, fmt.Errorf("%w: empty message", ErrInvalid)
	}
	if len(sms.Tag) > maxTagLength {
		return SMS{}, fmt.Errorf("%w: tag longer than %d characters", ErrInvalid, maxTagLength)
	}
	if len(sms.To) == 0 {
		return SMS{}, fmt.Errorf("%w: no recipient", ErrInvalid)
	}
	for i, to := range sms.To {
		n, err := NormalizeNumber(to)
		if err != nil {
			return SMS{}, fmt.Errorf("%w: recipient %d: %v", ErrInvalid, i, err)
		}
		sms.To[i] = n
	}
	return sms, nil
}

// NormalizeNumber converts an international phone number (+33 6 12 34 56 78,
// 0033612345678, +33-6.12.34.56.78) to the compact form used by OVH
// (+33612345678).
// National numbers (0612345678) are rejected: the country cannot be guessed.
func NormalizeNumber(raw string) (string, error) {
	n := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '.', '-', '(', ')':
			return -1
		}
		return r
	}, raw)

	var digits string
	switch {
	case strings.HasPrefix(n, "+"):
		digits = n[1:]
	case strings.HasPrefix(n, "00"):
		digits = n[2:]
	default:
		return "", fmt.Errorf("%q is not in international format (+33… or 0033…)", raw)
	}

	for _, r := range digits {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("%q contains invalid characters", raw)
		}
	}
	// E.164 numbers have at most 15 digits, country code included.
	if len(digits) < 8 || len(digits) > 15 || digits[0] == '0' {
		return "", fmt.Errorf("%q is not a valid international number", raw)
	}
	return "+" + digits, nil
}

// Mask hides all but the last two digits of a phone number, for logging.
func Mask(number string) string {
	if len(number) <= 6 {
		return strings.Repeat("*", len(number))
	}
	return number[:3] + strings.Repeat("*", len(number)-5) + number[len(number)-2:]
}
