package app

import (
	"errors"
	"fmt"
	"unicode/utf8"
)

// Keep broker and compositor chunk bounds in agreement. A chunk is one
// non-interruptible compositor callback; authority is checked between chunks.
const textChunkRunes = 48
const maxBatchTextBytes = 256 << 10

func validateText(text string) error {
	if !utf8.ValidString(text) {
		return errors.New("text must be valid UTF-8")
	}
	for _, r := range text {
		if (r < 32 && r != '\n' && r != '\t') || (r >= 0x7f && r <= 0x9f) {
			return fmt.Errorf("unsupported text control U+%04X; use LF newlines and TAB, and key actions for other controls (CR/CRLF must be converted to LF)", r)
		}
	}
	return nil
}

// Counts are keypresses acknowledged before failure, not app insertion results.
type TextDeliveryError struct {
	Completed int
	Cause     error
}

func (e *TextDeliveryError) Error() string { return e.Cause.Error() }
func (e *TextDeliveryError) Unwrap() error { return e.Cause }
