package app

import (
	"net/url"
	"strings"
)

// URI boundary redaction.
//
// The application layer treats every URI-bearing field that crosses from the
// device/discovery packages into a user-facing DTO as untrusted. It does not
// rely solely on soap.RedactURL: that helper redacts userinfo and query but
// leaves a fragment intact and still returns a best-effort string for inputs
// that are not valid absolute URIs. At the application boundary we additionally
// require that a raw value which is not a valid *absolute* URI with a non-empty
// scheme (or, for transport endpoints, is not an absolute HTTP(S) URL with a
// host) never crosses into a DTO or output: it is replaced wholesale with a
// safe marker so an invalid, relative, or credential-bearing raw string can
// never leak.
//
// Only the userinfo, query, and fragment components are treated as credential-
// bearing and redacted; path segments are preserved verbatim and are NOT
// claimed to be credential-redacted. A value whose scheme is empty, malformed,
// or contains control characters is rejected entirely rather than partially
// cleaned.
//
// Two markers are used consistently:
//   - boundaryRedactedValue replaces an individual sensitive component
//     (userinfo, query, fragment) within an otherwise valid URI.
//   - boundaryInvalidMarker replaces an entire value that failed validation,
//     so the raw string is omitted from output altogether.
const (
	// boundaryRedactedValue is the marker substituted for a sensitive URI
	// component (userinfo, query, or fragment). It matches soap.RedactedValue
	// so human and JSON output use one consistent token.
	boundaryRedactedValue = "REDACTED"
	// boundaryInvalidMarker replaces a whole value that is not a valid absolute
	// URI (or not a valid transport endpoint). Using a distinct, obviously
	// non-URI marker makes it clear the original was dropped rather than merely
	// partially redacted.
	boundaryInvalidMarker = "[invalid-uri-REDACTED]"
)

// isValidURIScheme reports whether s is a syntactically valid URI scheme per
// RFC 3986: ALPHA *( ALPHA / DIGIT / "+" / "-" / "." ). An empty scheme is
// invalid. This is stricter than url.Parse, which tolerates values that are not
// absolute URIs at all.
func isValidURIScheme(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
			// Always allowed.
		case i > 0 && (c >= '0' && c <= '9' || c == '+' || c == '-' || c == '.'):
			// Allowed only after the first character.
		default:
			return false
		}
	}
	return true
}

// containsControl reports whether s contains any ASCII control character
// (including DEL). Control characters never belong in a URI and are treated as
// a validation failure so they can never reach output.
func containsControl(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] == 0x7f {
			return true
		}
	}
	return false
}

// parseAbsoluteURI parses raw and returns the parsed URL only when it is a
// well-formed absolute URI: it must parse without error, carry no control
// characters, be absolute (IsAbs), and have a syntactically valid, non-empty
// scheme. Anything else returns ok=false so the caller substitutes a safe
// marker. This is deliberately stricter than a bare url.Parse, which accepts
// relative references and some malformed inputs.
func parseAbsoluteURI(raw string) (*url.URL, bool) {
	if containsControl(raw) {
		return nil, false
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return nil, false
	}
	if !parsed.IsAbs() || !isValidURIScheme(parsed.Scheme) {
		return nil, false
	}
	return parsed, true
}

// redactBoundaryURI validates and redacts a general URI-bearing value (for
// example a WS-Discovery EndpointReference, a scope, a service Namespace, or a
// Scope.Item). The value must be a valid *absolute* URI with a non-empty,
// syntactically valid scheme and no control characters; a relative reference,
// a value with an invalid or missing scheme, or an otherwise unparseable raw
// string is replaced entirely with boundaryInvalidMarker so it never crosses
// into a DTO. Userinfo, query, and fragment are always redacted because they
// are the components that carry credentials or opaque secrets; path segments
// are preserved verbatim and are not claimed to be redacted.
//
// An empty input is returned unchanged so optional fields stay empty rather
// than being marked invalid.
func redactBoundaryURI(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, ok := parseAbsoluteURI(raw)
	if !ok {
		return boundaryInvalidMarker
	}
	return redactParsedComponents(parsed)
}

// redactBoundaryTransportURI validates and redacts a transport endpoint URI
// (for example a capabilities XAddr, a service XAddr, or the canonical device
// endpoint). Beyond the general absolute-URI validation it requires an http or
// https scheme and a non-empty host; any other shape is replaced entirely with
// boundaryInvalidMarker so a non-transport, relative, or malformed endpoint
// never crosses into a DTO.
//
// An empty input is returned unchanged so optional fields stay empty.
func redactBoundaryTransportURI(raw string) string {
	if raw == "" {
		return ""
	}
	parsed, ok := parseAbsoluteURI(raw)
	if !ok {
		return boundaryInvalidMarker
	}
	scheme := strings.ToLower(parsed.Scheme)
	if (scheme != "http" && scheme != "https") || parsed.Host == "" || parsed.Hostname() == "" {
		return boundaryInvalidMarker
	}
	return redactParsedComponents(parsed)
}

// redactBoundaryExpandedName validates and redacts a WS-Discovery expanded-name
// value of the form "{namespace}local", where namespace is a URI. The namespace
// URI is validated and redacted with redactBoundaryURI (so credential-bearing
// userinfo/query/fragment inside the namespace cannot leak, and a relative or
// invalid namespace is rejected) and the local part is preserved. A value that
// is not in the expected "{...}..." shape, or whose namespace is not a valid
// absolute URI, is replaced entirely with boundaryInvalidMarker. An empty input
// is returned unchanged.
func redactBoundaryExpandedName(raw string) string {
	if raw == "" {
		return ""
	}
	if !strings.HasPrefix(raw, "{") {
		return boundaryInvalidMarker
	}
	closing := strings.IndexByte(raw, '}')
	if closing < 0 {
		return boundaryInvalidMarker
	}
	namespace := raw[1:closing]
	local := raw[closing+1:]
	if namespace == "" || local == "" {
		return boundaryInvalidMarker
	}
	redactedNamespace := redactBoundaryURI(namespace)
	if redactedNamespace == boundaryInvalidMarker {
		return boundaryInvalidMarker
	}
	return "{" + redactedNamespace + "}" + local
}

// redactParsedComponents redacts the credential-bearing components of a parsed
// URL (userinfo, query, fragment) and returns the reassembled string. Path
// segments are left intact and are not claimed to be redacted. The input
// pointer is not mutated for the caller: a shallow copy is taken first.
func redactParsedComponents(parsed *url.URL) string {
	clone := *parsed
	if clone.User != nil {
		clone.User = url.User(boundaryRedactedValue)
	}
	if clone.RawQuery != "" {
		clone.RawQuery = boundaryRedactedValue
	}
	if clone.ForceQuery {
		// A bare "?" with no query still signals a query boundary; collapse it
		// to the redacted marker for consistency.
		clone.RawQuery = boundaryRedactedValue
		clone.ForceQuery = false
	}
	if clone.Fragment != "" || clone.RawFragment != "" {
		clone.Fragment = boundaryRedactedValue
		clone.RawFragment = ""
	}
	return clone.String()
}
