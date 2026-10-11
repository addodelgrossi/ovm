package media2

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
	if utf8.RuneCountInString(token) > maxReferenceTokenLen {
		return fmt.Errorf("%w: %s exceeds %d characters", ErrInvalidInput, field, maxReferenceTokenLen)
	}
	for _, r := range token {
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
	_ = xml.EscapeText(&buffer, []byte(value))
	return buffer.String()
}

// validateStreamProtocol checks that a caller-supplied Media2 stream protocol
// is one of the tr2:TransportProtocol enumeration values before it is
// serialized.
func validateStreamProtocol(protocol StreamProtocol) error {
	switch protocol {
	case StreamRtspUnicast, StreamRtspMulticast, StreamRtspsUnicast,
		StreamRtspsMulticast, StreamRTSP, StreamRtspOverHTTP:
		return nil
	default:
		return fmt.Errorf("%w: Protocol is not a valid tr2:TransportProtocol", ErrInvalidInput)
	}
}

func validateConfigurationTypes(values []ConfigurationType) error {
	if len(values) > 11 {
		return fmt.Errorf("%w: too many Type filters", ErrInvalidInput)
	}
	seen := make(map[ConfigurationType]struct{}, len(values))
	for _, value := range values {
		if err := validateConfigurationType(value); err != nil {
			return err
		}
		if _, duplicate := seen[value]; duplicate {
			return fmt.Errorf("%w: duplicate Type filter", ErrInvalidInput)
		}
		seen[value] = struct{}{}
	}
	if _, all := seen[ConfigurationAll]; all && len(seen) != 1 {
		return fmt.Errorf("%w: All must be the only Type filter", ErrInvalidInput)
	}
	return nil
}

// validateConfigurationType checks that a GetProfiles type filter is one of the
// tr2:ConfigurationEnumeration values.
func validateConfigurationType(value ConfigurationType) error {
	switch value {
	case ConfigurationAll, ConfigurationVideoSource, ConfigurationVideoEncoder,
		ConfigurationAudioSource, ConfigurationAudioEncoder, ConfigurationAudioOutput,
		ConfigurationAudioDecoder, ConfigurationMetadata, ConfigurationAnalytics,
		ConfigurationPTZ, ConfigurationReceiver:
		return nil
	default:
		return fmt.Errorf("%w: Type is not a valid tr2:ConfigurationEnumeration", ErrInvalidInput)
	}
}
