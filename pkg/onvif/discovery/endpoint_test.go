package discovery

import (
	"errors"
	"testing"
)

func TestCanonicalizeEndpointSuccess(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "bare host defaults to http and device service", input: "192.168.1.10", want: "http://192.168.1.10/onvif/device_service"},
		{name: "host with explicit port", input: "192.168.1.10:8080", want: "http://192.168.1.10:8080/onvif/device_service"},
		{name: "https scheme preserved", input: "https://cam.local", want: "https://cam.local/onvif/device_service"},
		{name: "explicit service path preserved", input: "http://cam.local/onvif/device_service", want: "http://cam.local/onvif/device_service"},
		{name: "alternate explicit path preserved", input: "http://cam.local/custom/service", want: "http://cam.local/custom/service"},
		{name: "root path maps to device service", input: "http://cam.local/", want: "http://cam.local/onvif/device_service"},
		{name: "trailing slash trimmed deterministically", input: "http://cam.local/custom/service/", want: "http://cam.local/custom/service"},
		{name: "uppercase scheme normalised", input: "HTTP://cam.local", want: "http://cam.local/onvif/device_service"},
		{name: "ipv6 host", input: "http://[2001:db8::1]:8000", want: "http://[2001:db8::1]:8000/onvif/device_service"},
		{name: "surrounding whitespace trimmed", input: "  192.168.1.10  ", want: "http://192.168.1.10/onvif/device_service"},
		// Host canonicalisation.
		{name: "mixed case host lowercased", input: "http://CAM.Local/onvif", want: "http://cam.local/onvif"},
		{name: "default http port dropped", input: "http://cam.local:80/onvif", want: "http://cam.local/onvif"},
		{name: "default https port dropped", input: "https://cam.local:443/onvif", want: "https://cam.local/onvif"},
		{name: "non default port kept", input: "http://cam.local:8443/onvif", want: "http://cam.local:8443/onvif"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalizeEndpoint(tc.input)
			if err != nil {
				t.Fatalf("CanonicalizeEndpoint(%q): %v", tc.input, err)
			}
			if got.String() != tc.want {
				t.Fatalf("canonical = %q, want %q", got.String(), tc.want)
			}
		})
	}
}

// TestCanonicalizeEndpointPreservesEncodedPaths verifies the escaped-path fix:
// an explicit percent-escape in a valid service path is preserved verbatim and
// is NOT encoded a second time (the previous bug turned "%20" into "%2520").
// The canonical String must round-trip to the same encoded path.
func TestCanonicalizeEndpointPreservesEncodedPaths(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "space escape preserved", input: "http://cam.local/custom%20service", want: "http://cam.local/custom%20service"},
		{name: "encoded segment preserved", input: "http://cam.local/a%2Fb/service", want: "http://cam.local/a%2Fb/service"},
		{name: "encoded plus preserved", input: "http://cam.local/on%2Bvif", want: "http://cam.local/on%2Bvif"},
		{name: "encoded path with port", input: "http://cam.local:8080/custom%20service", want: "http://cam.local:8080/custom%20service"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalizeEndpoint(tc.input)
			if err != nil {
				t.Fatalf("CanonicalizeEndpoint(%q): %v", tc.input, err)
			}
			if got.String() != tc.want {
				t.Fatalf("canonical = %q, want %q (no double-encoding)", got.String(), tc.want)
			}
			// Re-canonicalising the output must be a fixed point.
			again, err := CanonicalizeEndpoint(got.String())
			if err != nil {
				t.Fatalf("re-canonicalise: %v", err)
			}
			if again.String() != tc.want {
				t.Fatalf("not idempotent: %q then %q", got.String(), again.String())
			}
		})
	}
}

func TestCanonicalizeEndpointDeterministic(t *testing.T) {
	first, err := CanonicalizeEndpoint("CAM.local")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := CanonicalizeEndpoint("CAM.local")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.String() != second.String() {
		t.Fatalf("non-deterministic: %q vs %q", first.String(), second.String())
	}
}

func TestCanonicalizeEndpointRejectsUnsafeInputs(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "whitespace only", input: "   "},
		{name: "ftp scheme", input: "ftp://cam.local"},
		{name: "file scheme", input: "file:///etc/passwd"},
		// gosec G101 flags a password-in-URL literal; this is deliberate test
		// data proving userinfo is rejected, so the finding is suppressed.
		{name: "userinfo", input: "http://user:pass@cam.local"}, //nolint:gosec // test literal for userinfo rejection
		{name: "userinfo without password", input: "http://admin@cam.local"},
		{name: "fragment", input: "http://cam.local/onvif#frag"},
		{name: "query string", input: "http://cam.local/onvif?x=1"},
		{name: "path traversal", input: "http://cam.local/../secret"},
		// Traversal and control characters hidden behind percent-escapes must
		// be caught after decoding, not passed through as opaque encoded text.
		{name: "encoded traversal", input: "http://cam.local/%2e%2e/secret"},
		{name: "encoded nul", input: "http://cam.local/on%00vif"},
		{name: "encoded newline", input: "http://cam.local/on%0avif"},
		{name: "invalid port", input: "http://cam.local:0/onvif"},
		{name: "non numeric port", input: "http://cam.local:http/onvif"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CanonicalizeEndpoint(tc.input)
			if !errors.Is(err, ErrInvalidEndpoint) {
				t.Fatalf("err = %v, want ErrInvalidEndpoint", err)
			}
		})
	}
}

func TestCanonicalizeEndpointErrorsDoNotLeakCredentials(t *testing.T) {
	_, err := CanonicalizeEndpoint("http://admin:secret@cam.local") //nolint:gosec // test literal verifying credentials never leak
	if err == nil {
		t.Fatal("expected error")
	}
	if msg := err.Error(); contains(msg, "secret") || contains(msg, "admin") {
		t.Fatalf("error leaked credentials: %q", msg)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) > 0 && len(haystack) >= len(needle) && (indexOf(haystack, needle) >= 0)
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
