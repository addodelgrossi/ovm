package cli

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// sanitizeLine renders an untrusted, possibly camera- or network-controlled
// string as a single safe, printable line suitable for a terminal. It is used
// for every human-mode field that originates outside this process so a crafted
// device cannot inject terminal escape sequences, overwrite the display with
// carriage returns, split a report across lines, or reorder it with bidi
// controls.
//
// The transformation:
//   - replaces every C0 control (including CR, LF, TAB, and NUL), the DEL
//     character, and every C1 control with U+FFFD so a single logical field
//     always renders on one line;
//   - replaces the ANSI escape byte (U+001B) and other controls so no escape
//     sequence can reach the terminal;
//   - replaces Unicode bidirectional formatting controls (LRE/RLE/PDF/LRO/RLO,
//     LRI/RLI/FSI/PDI, and the LRM/RLM/ALM marks) so display order cannot be
//     spoofed;
//   - replaces any remaining Unicode format characters (category Cf) and
//     invalid UTF-8 bytes;
//   - preserves ordinary printable Unicode, including non-Latin scripts and
//     spaces, unchanged.
//
// JSON output never passes through this function: encoding/json escapes control
// characters safely while retaining the original byte values, which machine
// consumers require.
func sanitizeLine(s string) string {
	if s == "" {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			// Invalid UTF-8 byte: replace and advance one byte.
			b.WriteRune('\uFFFD')
			i++
			continue
		}
		i += size
		if isUnsafeRune(r) {
			b.WriteRune('\uFFFD')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// isUnsafeRune reports whether r must be replaced before being written to a
// terminal. It rejects all control characters, the DEL character, bidi and
// other Unicode format controls, line/paragraph separators, and non-characters.
func isUnsafeRune(r rune) bool {
	switch {
	case r == 0x7F: // DEL
		return true
	case r < 0x20: // C0 controls, including CR, LF, TAB, NUL, ESC-adjacent
		return true
	case r >= 0x80 && r <= 0x9F: // C1 controls
		return true
	case r == '\u2028' || r == '\u2029': // line / paragraph separators
		return true
	case isBidiControl(r):
		return true
	case unicode.Is(unicode.Cf, r): // other format controls (zero-width, etc.)
		return true
	default:
		return false
	}
}

// isBidiControl reports whether r is a Unicode bidirectional formatting control
// that could reorder displayed text. These are also category Cf, but they are
// listed explicitly so the policy is self-documenting and robust to table
// changes.
func isBidiControl(r rune) bool {
	switch r {
	case '\u200E', // LEFT-TO-RIGHT MARK
		'\u200F', // RIGHT-TO-LEFT MARK
		'\u061C', // ARABIC LETTER MARK
		'\u202A', // LEFT-TO-RIGHT EMBEDDING
		'\u202B', // RIGHT-TO-LEFT EMBEDDING
		'\u202C', // POP DIRECTIONAL FORMATTING
		'\u202D', // LEFT-TO-RIGHT OVERRIDE
		'\u202E', // RIGHT-TO-LEFT OVERRIDE
		'\u2066', // LEFT-TO-RIGHT ISOLATE
		'\u2067', // RIGHT-TO-LEFT ISOLATE
		'\u2068', // FIRST STRONG ISOLATE
		'\u2069': // POP DIRECTIONAL ISOLATE
		return true
	default:
		return false
	}
}
