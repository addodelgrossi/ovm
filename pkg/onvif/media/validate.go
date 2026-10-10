package media

import (
	"encoding/xml"
	"fmt"
	"strings"
	"unicode/utf8"
)

// maxReferenceTokenLen bounds a caller-supplied ONVIF reference token. The
// shared schema documents reference tokens as "Length up to 64 characters"
// (tt:ReferenceToken via tt:Name), so a token longer than this cannot be a
// valid device token and is rejected before it reaches the wire.
//
// Source: tt:ReferenceToken / "Length up to 64 characters" in
// https://www.onvif.org/ver10/schema/onvif.xsd
const maxReferenceTokenLen = 64

// validateReferenceToken checks a caller-supplied reference token for the
// invariants that must hold before it is placed in request XML: it must be
// non-empty, valid UTF-8, within the schema length bound, and free of control
// characters. Valid XML special characters (for example &, <, >, ") are
// permitted here and escaped at serialization time by escapeToken; it is the
// combination of validation plus escaping, not stripping, that keeps a token
// both faithful and safe. The returned error never echoes the rejected value,
// only the field name and the structural reason.
func validateReferenceToken(field, token string) error {
	if token == "" {
		return fmt.Errorf("%w: %s is empty", ErrInvalidInput, field)
	}
	if !utf8.ValidString(token) {
		return fmt.Errorf("%w: %s is not valid UTF-8", ErrInvalidInput, field)
	}
	// Count by runes against the schema's character-length bound.
	if utf8.RuneCountInString(token) > maxReferenceTokenLen {
		return fmt.Errorf("%w: %s exceeds %d characters", ErrInvalidInput, field, maxReferenceTokenLen)
	}
	for _, r := range token {
		// Reject C0/C1 control characters and the Unicode replacement
		// character. These are never part of a legitimate reference token and
		// would either be rejected by the XML serializer or silently rewritten,
		// so refusing them early keeps the request faithful and the error
		// specific.
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == utf8.RuneError {
			return fmt.Errorf("%w: %s contains a control character", ErrInvalidInput, field)
		}
	}
	return nil
}

// escapeToken renders a validated dynamic token as XML character data using the
// standard library escaper, so any &, <, >, ", or ' is encoded rather than
// breaking out of its element. Callers must validate the token first; escaping
// is the second half of the escape-after-validate contract.
func escapeToken(value string) string {
	var buffer strings.Builder
	// xml.EscapeText only errors on an underlying io.Writer failure;
	// strings.Builder never fails, so the error is structurally impossible.
	_ = xml.EscapeText(&buffer, []byte(value))
	return buffer.String()
}

// validateStreamSetup checks that a caller-supplied StreamSetup carries values
// from the ONVIF enumerations before it is serialized.
func validateStreamSetup(setup StreamSetup) error {
	switch setup.Stream {
	case StreamRTPUnicast, StreamRTPMulticast:
	default:
		return fmt.Errorf("%w: StreamSetup.Stream is not a valid StreamType", ErrInvalidInput)
	}
	switch setup.Protocol {
	case TransportUDP, TransportTCP, TransportRTSP, TransportHTTP:
	default:
		return fmt.Errorf("%w: StreamSetup.Protocol is not a valid TransportProtocol", ErrInvalidInput)
	}
	return nil
}
