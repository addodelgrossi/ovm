package app

import (
	"strings"
	"testing"
)

// TestRedactBoundaryURIRequiresAbsoluteScheme proves the general URI boundary
// validator rejects anything that is not a well-formed absolute URI with a
// non-empty, syntactically valid scheme. Relative references, scheme-less
// values, malformed schemes, and control characters are all replaced wholesale
// with the invalid marker rather than being partially cleaned by a bare
// url.Parse.
func TestRedactBoundaryURIRequiresAbsoluteScheme(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		raw   string
		valid bool
	}{
		{"absolute_http", "http://host.invalid/path", true},
		{"absolute_https", "https://host.invalid/path", true},
		{"absolute_urn", "urn:uuid:1234", true},
		{"absolute_onvif_scope", "onvif://www.onvif.org/type/video_encoder", true},
		{"custom_scheme_with_digits", "a1+-.://host/x", true},

		{"relative_path", "/onvif/device_service", false},
		{"relative_bare", "device_service", false},
		{"scheme_relative", "//host.invalid/x", false},
		{"empty_scheme_colon", "://host/x", false},
		{"scheme_starts_with_digit", "1http://host/x", false},
		{"scheme_with_space", "ht tp://host/x", false},
		{"control_char", "http://host.invalid/\x01path", false},
		{"newline_injection", "http://host.invalid/x\nSet-Cookie: y", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := redactBoundaryURI(c.raw)
			if c.valid && got == boundaryInvalidMarker {
				t.Fatalf("valid absolute URI %q was marked invalid", c.raw)
			}
			if !c.valid && got != boundaryInvalidMarker {
				t.Fatalf("non-absolute/invalid URI %q not marked invalid, got %q", c.raw, got)
			}
		})
	}
}

// TestRedactBoundaryURIEmptyPassesThrough confirms an empty optional field is
// returned unchanged rather than being marked invalid.
func TestRedactBoundaryURIEmptyPassesThrough(t *testing.T) {
	t.Parallel()
	if got := redactBoundaryURI(""); got != "" {
		t.Fatalf("empty input should pass through, got %q", got)
	}
}

// TestRedactBoundaryURIRelativeWithSecretNeverCrossesRaw proves a relative
// value that embeds a secret-looking marker is dropped entirely, so the raw
// string can never cross the boundary even partially.
func TestRedactBoundaryURIRelativeWithSecretNeverCrossesRaw(t *testing.T) {
	t.Parallel()
	secret := uniqueSecret("relsecret")
	raw := "/path/" + secret + "?token=" + secret
	got := redactBoundaryURI(raw)
	if got != boundaryInvalidMarker {
		t.Fatalf("relative URI with secret should be fully dropped, got %q", got)
	}
	if strings.Contains(got, secret) {
		t.Fatalf("relative URI leaked secret: %q", got)
	}
}

// TestRedactBoundaryURIPreservesPathSegment documents that path segments are
// retained verbatim and are explicitly NOT claimed to be credential-redacted;
// only userinfo, query, and fragment are redacted.
func TestRedactBoundaryURIPreservesPathSegment(t *testing.T) {
	t.Parallel()
	raw := "http://host.invalid/onvif/device_service"
	got := redactBoundaryURI(raw)
	if got != raw {
		t.Fatalf("path segment must be preserved verbatim, got %q", got)
	}
}

// TestRedactBoundaryExpandedNameRejectsRelativeNamespace proves the expanded
// name validator now rejects a relative namespace (which a bare url.Parse would
// have accepted), replacing the whole value with the invalid marker.
func TestRedactBoundaryExpandedNameRejectsRelativeNamespace(t *testing.T) {
	t.Parallel()
	if got := redactBoundaryExpandedName("{/relative/ns}Local"); got != boundaryInvalidMarker {
		t.Fatalf("relative namespace should be marked invalid, got %q", got)
	}
	if got := redactBoundaryExpandedName("{not-a-uri}Local"); got != boundaryInvalidMarker {
		t.Fatalf("scheme-less namespace should be marked invalid, got %q", got)
	}
}
