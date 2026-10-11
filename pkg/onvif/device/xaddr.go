package device

import (
	"fmt"
	"net/url"
	"strings"
)

// String renders a Service for human-facing output using only redacted,
// presentation-safe fields. It is defined so that fmt verbs that call Stringer
// (%v, %s) never render the unexported operational endpoint, which may carry a
// credential-bearing query.
func (s Service) String() string {
	return fmt.Sprintf("Service{Namespace:%q XAddr:%q Version:%d.%d}",
		s.Namespace, s.XAddr, s.Version.Major, s.Version.Minor)
}

// GoString renders a Service for the %#v verb using only redacted fields. The
// default %#v reflection would otherwise dump the unexported operational XAddr;
// defining GoString closes that leakage vector so even a debug-formatted
// Service cannot surface the operational query.
func (s Service) GoString() string {
	return fmt.Sprintf("device.Service{Namespace:%q, XAddr:%q, Version:device.ServiceVersion{Major:%d, Minor:%d}, HasVersion:%t}",
		s.Namespace, s.XAddr, s.Version.Major, s.Version.Minor, s.HasVersion)
}

// safeOperationalXAddr validates an advertised service XAddr and returns the
// exact operational value to retain for request construction. The operational
// value preserves the full path and any RawQuery/ForceQuery, because a device
// may legitimately advertise a service endpoint whose query is required to
// reach it. Validation is fail-closed: an XAddr that carries userinfo, a
// fragment, a control character, or a non-HTTP(S) scheme, or that is not an
// absolute URL with a host, is rejected with ErrInvalidField rather than
// retained. The returned error never echoes the offending value.
//
// This hardening sits at the decode boundary so an unsafe advertised endpoint
// can never be stored, surfaced, or used downstream. The redaction of the
// public XAddr field (performed separately by the caller via soap.RedactURL)
// is for presentation; this function is the security gate on the operational
// value.
func safeOperationalXAddr(xaddr string) (string, error) {
	if xaddr == "" {
		return "", fmt.Errorf("%w: XAddr", ErrMissingField)
	}
	if containsControlByte(xaddr) {
		return "", fmt.Errorf("%w: XAddr contains control characters", ErrInvalidField)
	}
	parsed, err := url.Parse(xaddr)
	if err != nil || !parsed.IsAbs() {
		return "", fmt.Errorf("%w: XAddr is not an absolute URL", ErrInvalidField)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("%w: XAddr scheme is not supported", ErrInvalidField)
	}
	if parsed.User != nil {
		return "", fmt.Errorf("%w: XAddr must not contain userinfo", ErrInvalidField)
	}
	if parsed.Fragment != "" || parsed.RawFragment != "" {
		return "", fmt.Errorf("%w: XAddr must not contain a fragment", ErrInvalidField)
	}
	if parsed.Hostname() == "" {
		return "", fmt.Errorf("%w: XAddr has no host", ErrInvalidField)
	}
	// Reject controls that are hidden behind percent-encoding as well as
	// literal controls. url.Parse decodes the path into parsed.Path, so a
	// value such as ".../onvif/%0Amedia" exposes the newline there even though
	// the raw string carried no literal control byte; the retained RawQuery is
	// decoded here before the check. The error never echoes the value.
	if hasDecodedControlInURL(parsed) {
		return "", fmt.Errorf("%w: XAddr contains control characters", ErrInvalidField)
	}
	// Return the exact advertised value (path and query preserved). The parsed
	// String() round-trips the safe components without re-encoding beyond
	// url.URL's canonical escaping.
	return parsed.String(), nil
}

// containsControlByte reports whether s contains an ASCII control character
// (0x00-0x1F or 0x7F). Such bytes have no place in a URL and are a classic
// response-splitting or log-injection vector, so they are rejected outright.
func containsControlByte(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// hasDecodedControlInURL reports whether any retained component of a parsed
// operational XAddr contains an ASCII control character AFTER percent-decoding.
// url.Parse already decodes the path into URL.Path and the fragment into
// URL.Fragment; the query is retained in encoded RawQuery for request
// construction, so it is decoded here before the check. A malformed escape is
// treated as a control so validation fails closed. No value is ever echoed.
func hasDecodedControlInURL(u *url.URL) bool {
	if u == nil {
		return true
	}
	if containsControlByte(u.Path) {
		return true
	}
	if u.RawPath != "" {
		decoded, err := url.PathUnescape(u.RawPath)
		if err != nil || containsControlByte(decoded) {
			return true
		}
	}
	if u.RawQuery != "" {
		decoded, err := url.QueryUnescape(u.RawQuery)
		if err != nil || containsControlByte(decoded) {
			return true
		}
	}
	return false
}
