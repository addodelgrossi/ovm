package discovery

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// defaultServicePath is the ONVIF device service path assumed when a manual
// endpoint names only a host (and optional port).
const defaultServicePath = "/onvif/device_service"

// schemePrefix matches an explicit URL scheme with an authority at the start of
// an endpoint, for example "http://" or "ftp://". It requires the "//" so that
// a bare "host:port" is not mistaken for a "scheme:opaque" value.
var schemePrefix = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://`)

// Endpoint is a canonicalized manual ONVIF endpoint suitable for use by
// commands such as "ovm info <host>". The canonical form is deterministic:
// identical inputs always produce an identical String.
type Endpoint struct {
	// Scheme is "http" or "https".
	Scheme string
	// Host is the host or IP, including an explicit port when supplied. Its
	// case is canonicalised (registered names lower-cased) and a redundant
	// default port is dropped.
	Host string
	// Path is the DECODED service path, matching url.URL.Path semantics. For a
	// path with percent-escapes, RawPath carries the exact encoded form.
	Path string
	// RawPath is the encoded service path when it is a valid encoding of Path
	// that differs from the default escaping, matching url.URL.RawPath
	// semantics. It is empty when the default escaping of Path already
	// round-trips, so String never double-encodes an explicit "%" escape. This
	// field is additive and backward compatible: callers that only read Path
	// are unaffected.
	RawPath string
}

// String returns the deterministic canonical endpoint URL. It relies on
// url.URL's Path/RawPath contract so a validated encoded path is emitted
// verbatim rather than being escaped a second time.
func (e Endpoint) String() string {
	u := url.URL{Scheme: e.Scheme, Host: e.Host, Path: e.Path, RawPath: e.RawPath}
	return u.String()
}

// CanonicalizeEndpoint normalises a manual endpoint for the device service.
//
// Rules:
//   - Only http and https are accepted; a bare host defaults to http.
//   - User information, query strings, and fragments are rejected as unsafe.
//   - The scheme is lower-cased, a registered host name is lower-cased, and a
//     redundant default port (80 for http, 443 for https) is dropped so the
//     canonical form is stable.
//   - A host or IP with no path (optionally with an explicit port) maps to the
//     ONVIF device service path.
//   - An explicit, valid service path is preserved verbatim, including any
//     percent-escapes, without being re-encoded.
//   - A decoded path containing traversal or control characters is rejected.
//   - The result is a deterministic canonical string.
//
// Errors are safe: they describe the problem without echoing credentials.
func CanonicalizeEndpoint(input string) (Endpoint, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return Endpoint{}, fmt.Errorf("%w: empty endpoint", ErrInvalidEndpoint)
	}

	// A value with no scheme is treated as a host; url.Parse would otherwise
	// interpret "host:port" as scheme:opaque. A value that carries any other
	// "scheme://" authority is left intact so the http/https check below
	// rejects it.
	if !schemePrefix.MatchString(trimmed) {
		trimmed = "http://" + trimmed
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return Endpoint{}, fmt.Errorf("%w: endpoint is not a valid URL", ErrInvalidEndpoint)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return Endpoint{}, fmt.Errorf("%w: scheme must be http or https", ErrInvalidEndpoint)
	}
	if parsed.User != nil {
		return Endpoint{}, fmt.Errorf("%w: user information is not allowed", ErrInvalidEndpoint)
	}
	if parsed.Fragment != "" || strings.Contains(trimmed, "#") {
		return Endpoint{}, fmt.Errorf("%w: fragments are not allowed", ErrInvalidEndpoint)
	}
	if parsed.RawQuery != "" || strings.Contains(trimmed, "?") {
		return Endpoint{}, fmt.Errorf("%w: query strings are not allowed", ErrInvalidEndpoint)
	}

	host, err := canonicalHost(parsed, scheme)
	if err != nil {
		return Endpoint{}, err
	}

	// Use the DECODED path (url.URL.Path) as the canonical value and validate
	// it against traversal and control characters. The encoded form is carried
	// separately so String never escapes an explicit "%" a second time.
	decodedPath, rawPath := canonicalPath(parsed)
	if err := validatePath(decodedPath); err != nil {
		return Endpoint{}, err
	}

	return Endpoint{Scheme: scheme, Host: host, Path: decodedPath, RawPath: rawPath}, nil
}

// canonicalHost validates and canonicalises the authority. It lower-cases a
// registered host name (IP literals are already canonical), drops a redundant
// default port for the scheme, and rejects a port that is not a valid numeric
// port. The host must be present and free of whitespace.
func canonicalHost(parsed *url.URL, scheme string) (string, error) {
	host := parsed.Host
	if host == "" {
		return "", fmt.Errorf("%w: host is required", ErrInvalidEndpoint)
	}
	if strings.ContainsAny(host, " \t\r\n") {
		return "", fmt.Errorf("%w: host contains whitespace", ErrInvalidEndpoint)
	}

	hostname := parsed.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("%w: host is required", ErrInvalidEndpoint)
	}
	port := parsed.Port()

	// Lower-case the hostname for a stable canonical form. An IPv6 literal is
	// returned without brackets by url.Hostname(); reconstruct it in bracket
	// form so the result parses back identically.
	lowerHost := strings.ToLower(hostname)

	if port != "" {
		if !validPort(port) {
			return "", fmt.Errorf("%w: port is not valid", ErrInvalidEndpoint)
		}
		if isDefaultPort(scheme, port) {
			port = ""
		}
	}

	rebuilt := lowerHost
	if strings.Contains(lowerHost, ":") {
		// IPv6 literal: brackets are required in authority form.
		rebuilt = "[" + lowerHost + "]"
	}
	if port != "" {
		rebuilt += ":" + port
	}
	return rebuilt, nil
}

// validPort reports whether port is a non-empty string of digits denoting a
// value in the valid TCP/UDP range.
func validPort(port string) bool {
	if port == "" {
		return false
	}
	n := 0
	for _, r := range port {
		if r < '0' || r > '9' {
			return false
		}
		n = n*10 + int(r-'0')
		if n > 65535 {
			return false
		}
	}
	return n >= 1
}

// isDefaultPort reports whether port is the scheme's default port and can be
// dropped from the canonical form.
func isDefaultPort(scheme, port string) bool {
	switch scheme {
	case "http":
		return port == "80"
	case "https":
		return port == "443"
	default:
		return false
	}
}

// canonicalPath maps an empty or root path to the default service path and
// otherwise trims a single trailing slash. It returns the decoded path and the
// encoded path (RawPath) using url.URL semantics: RawPath is non-empty only
// when it is a valid, non-default encoding of the decoded path, so an explicit
// percent-escape survives round-tripping without being doubly encoded.
func canonicalPath(parsed *url.URL) (decoded, raw string) {
	decoded = parsed.Path
	raw = parsed.EscapedPath()
	if decoded == "" || decoded == "/" {
		return defaultServicePath, ""
	}
	if len(decoded) > 1 && strings.HasSuffix(decoded, "/") {
		trimmedDecoded := strings.TrimRight(decoded, "/")
		if trimmedDecoded == "" {
			return defaultServicePath, ""
		}
		// Trim the encoded form by the same trailing slashes so the two stay
		// consistent for String's round-trip.
		trimmedRaw := strings.TrimRight(raw, "/")
		if trimmedRaw == "" {
			trimmedRaw = trimmedDecoded
		}
		decoded, raw = trimmedDecoded, trimmedRaw
	}
	// RawPath is only meaningful when it differs from the default escaping of
	// the decoded path; otherwise leave it empty so url.URL recomputes it.
	if raw == decoded {
		raw = ""
	}
	return decoded, raw
}

// validatePath rejects service paths that are not absolute or that contain
// traversal or control characters. It operates on the DECODED path so that a
// traversal or control character hidden behind a percent-escape (for example
// "%2e%2e", "%00", or "%0a") is caught after decoding rather than slipping
// through as opaque encoded text. A legitimately encoded printable character
// such as a space ("%20") is allowed because, after decoding, it is neither a
// control character nor traversal; only control characters are rejected here.
func validatePath(path string) error {
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("%w: service path must be absolute", ErrInvalidEndpoint)
	}
	if strings.Contains(path, "..") {
		return fmt.Errorf("%w: service path must not contain traversal", ErrInvalidEndpoint)
	}
	for _, r := range path {
		// Reject C0 controls (includes tab, CR, LF) and DEL. A plain space is
		// not a control character and is permitted when it arrived encoded.
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: service path contains control characters", ErrInvalidEndpoint)
		}
	}
	return nil
}
