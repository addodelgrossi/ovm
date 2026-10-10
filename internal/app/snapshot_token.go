package app

import (
	"strings"
	"unicode/utf8"
)

// maxSnapshotProfileTokenCodePoints mirrors the ONVIF ReferenceToken schema
// limit. The bound is measured in Unicode code points, not UTF-8 bytes.
const maxSnapshotProfileTokenCodePoints = 64

// ValidateSnapshotProfileToken validates a caller-supplied explicit snapshot
// profile token before any endpoint preparation or network work. The token is
// preserved byte-for-byte: valid XML-special characters are allowed here and
// must be escaped later by the wire serializer. Invalid UTF-8, empty or
// whitespace-only values, C0/C1 controls, the replacement rune, and values over
// the ONVIF 64-code-point limit are rejected with a safe usage error that never
// echoes the token.
func ValidateSnapshotProfileToken(token string) error {
	if token == "" || !utf8.ValidString(token) {
		return usageError("the explicit profile token is not valid")
	}
	if utf8.RuneCountInString(token) > maxSnapshotProfileTokenCodePoints {
		return usageError("the explicit profile token is not valid")
	}
	for _, character := range token {
		if character < 0x20 || (character >= 0x7f && character <= 0x9f) || character == utf8.RuneError {
			return usageError("the explicit profile token is not valid")
		}
	}
	if strings.TrimSpace(token) == "" {
		return usageError("the explicit profile token is not valid")
	}
	return nil
}
