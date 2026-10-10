package device

import (
	"errors"
	"strings"
	"testing"
)

// TestSafeOperationalXAddr exercises the operational XAddr validator directly,
// including inputs (control characters, empty) that the SOAP XML parser would
// reject before they ever reached decodeServices. It proves the validator is a
// standalone, fail-closed gate and that it preserves a safe path+query.
func TestSafeOperationalXAddr(t *testing.T) {
	t.Parallel()

	t.Run("preserves safe path and query", func(t *testing.T) {
		t.Parallel()
		const raw = "http://device.invalid/onvif/media?token=opaque-synthetic"
		got, err := safeOperationalXAddr(raw)
		if err != nil {
			t.Fatalf("safeOperationalXAddr(%q) error = %v", raw, err)
		}
		if got != raw {
			t.Fatalf("operational = %q, want %q", got, raw)
		}
	})

	rejections := []struct {
		name string
		in   string
		want error
	}{
		{"empty", "", ErrMissingField},
		{"control char", "http://device.invalid/onvif\x01/media", ErrInvalidField},
		{"null byte", "http://device.invalid/\x00", ErrInvalidField},
		{"userinfo", "http://user:pass@device.invalid/onvif", ErrInvalidField},
		{"fragment", "http://device.invalid/onvif#frag", ErrInvalidField},
		{"ftp scheme", "ftp://device.invalid/onvif", ErrInvalidField},
		{"relative", "/onvif/media", ErrInvalidField},
		{"no host", "http:///onvif", ErrInvalidField},
		// Percent-encoded controls in the path are decoded by url.Parse into
		// URL.Path and must be rejected after decoding.
		{"encoded null in path", "http://device.invalid/onvif/%00media", ErrInvalidField},
		{"encoded newline in path", "http://device.invalid/onvif/%0Amedia", ErrInvalidField},
		{"encoded cr in path", "http://device.invalid/onvif/%0Dmedia", ErrInvalidField},
		{"encoded del in path", "http://device.invalid/onvif/%7Fmedia", ErrInvalidField},
		// Percent-encoded controls in the retained query must also be rejected.
		{"encoded newline in query", "http://device.invalid/onvif?x=%0A", ErrInvalidField},
		{"encoded null in query", "http://device.invalid/onvif?x=%00", ErrInvalidField},
	}
	for _, tc := range rejections {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := safeOperationalXAddr(tc.in)
			if !errors.Is(err, tc.want) {
				t.Fatalf("safeOperationalXAddr(%q) error = %v, want %v", tc.in, err, tc.want)
			}
			if strings.Contains(err.Error(), "device.invalid") || strings.Contains(err.Error(), "user:pass") {
				t.Fatalf("error leaked the offending value: %v", err)
			}
		})
	}
}
