package soap

import (
	"encoding/base64"
	"strings"
	"testing"
)

func FuzzRedactTextNeverRetainsCredentialValues(f *testing.F) {
	for _, seed := range []string{"camera-secret", "nonce-secret", "credential-secret", "operator"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, value string) {
		secret := "SECRET-" + base64.RawURLEncoding.EncodeToString([]byte(value)) + "-VALUE"
		inputs := []string{
			`<Password value="` + secret + `">` + secret + `</Password>`,
			`<π:Nonce xmlns:π="urn:test">` + secret + `</π:Nonce>`,
			`<Username>` + secret,
			"Authorization: Digest " + secret + "\r\n",
			"https://" + secret + ":" + secret + "@example.invalid/onvif",
			"https://example.invalid/action?access_token=" + secret,
		}
		for _, input := range inputs {
			if redacted := RedactText(input); strings.Contains(redacted, secret) {
				t.Fatalf("redacted output retained credential value in %q: %q", input, redacted)
			}
		}
	})
}

func FuzzParseEnvelopeDoesNotPanic(f *testing.F) {
	f.Add([]byte(soap12Response(`<Result/>`)))
	f.Add([]byte(`<!DOCTYPE x><Envelope/>`))
	f.Add([]byte(`<broken`))
	f.Fuzz(func(t *testing.T, input []byte) {
		_, _ = parseEnvelope(input)
	})
}
