package cli

import (
	"strings"
	"testing"
)

func TestSanitizeLineRemovesControlAndBidiControls(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"crlf", "line1\r\nline2"},
		{"ansi_escape", "text\x1b[31mred\x1b[0m"},
		{"osc_title", "x\x1b]0;pwned\x07"},
		{"nul", "a\x00b"},
		{"tab", "a\tb"},
		{"c1_control", "a\u0085b"},
		{"bidi_override", "file\u202Egnp.exe"},
		{"bidi_isolate", "a\u2066b\u2069c"},
		{"line_separator", "a\u2028b"},
		{"zero_width", "a\u200Bb"},
		{"del", "a\x7fb"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeLine(tc.in)
			if strings.ContainsAny(got, "\r\n\t\x00\x1b\x07\x7f") {
				t.Fatalf("output retained a raw control: %q", got)
			}
			for _, r := range []rune{'\u0085', '\u202E', '\u2066', '\u2069', '\u2028', '\u200B'} {
				if strings.ContainsRune(got, r) {
					t.Fatalf("output retained a formatting/bidi control %U: %q", r, got)
				}
			}
			// The result is always a single line.
			if strings.Contains(got, "\n") {
				t.Fatalf("output spans multiple lines: %q", got)
			}
		})
	}
}

func TestSanitizeLinePreservesReasonableUnicode(t *testing.T) {
	cases := []string{
		"ACME Networks",
		"camera-01.local",
		"http://10.0.0.2/onvif/device_service",
		"Café Ômega カメラ 中文 Ω",
		"a b c  spaced",
	}
	for _, in := range cases {
		if got := sanitizeLine(in); got != in {
			t.Fatalf("sanitizeLine(%q) = %q, want unchanged", in, got)
		}
	}
}

func TestSanitizeLineEmptyIsEmpty(t *testing.T) {
	if got := sanitizeLine(""); got != "" {
		t.Fatalf("empty string must stay empty, got %q", got)
	}
}

func TestSanitizeLineReplacesInvalidUTF8(t *testing.T) {
	// A lone continuation byte is invalid UTF-8 and must be replaced, not
	// dropped silently in a way that could merge adjacent fields.
	got := sanitizeLine("a\xffb")
	if strings.Contains(got, "\xff") {
		t.Fatalf("invalid UTF-8 byte survived: %q", got)
	}
	if !strings.HasPrefix(got, "a") || !strings.HasSuffix(got, "b") {
		t.Fatalf("surrounding runes were lost: %q", got)
	}
}
