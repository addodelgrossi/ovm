package soap

import (
	"net/http"
	"strings"
	"testing"
)

func TestRedactXMLSecrets(t *testing.T) {
	t.Parallel()

	input := []byte(`<s:Envelope><wsse:UsernameToken><wsse:Username>operator</wsse:Username><wsse:Password Type="digest">digest-secret</wsse:Password><wsse:PasswordDigest>another-secret</wsse:PasswordDigest><wsse:Nonce EncodingType="base64">nonce-secret</wsse:Nonce><wsu:Created>2026-10-08T12:00:00Z</wsu:Created></wsse:UsernameToken></s:Envelope>`)
	redacted := string(RedactXML(input))
	for _, secret := range []string{"operator", "digest-secret", "another-secret", "nonce-secret"} {
		if strings.Contains(redacted, secret) {
			t.Errorf("RedactXML() retained %q in %s", secret, redacted)
		}
	}
	if !strings.Contains(redacted, RedactedValue) || !strings.Contains(redacted, "2026-10-08T12:00:00Z") {
		t.Errorf("RedactXML() = %s, want redactions while retaining non-secret Created", redacted)
	}
}

func TestRedactMalformedXMLAndTextCredentials(t *testing.T) {
	t.Parallel()

	//nolint:gosec // Synthetic credential-shaped text verifies mandatory redaction.
	input := "POST https://operator:camera-secret@example.invalid/onvif\nAuthorization: Digest response=credential\nProxy-Authorization: Basic credential\n<Password>xml-secret"
	redacted := RedactText(input)
	for _, secret := range []string{"operator", "camera-secret", "credential", "xml-secret"} {
		if strings.Contains(redacted, secret) {
			t.Errorf("RedactText() retained %q in %s", secret, redacted)
		}
	}
}

func TestRedactHeadersReturnsIndependentCopy(t *testing.T) {
	t.Parallel()

	headers := http.Header{
		"Authorization":       []string{"Digest secret"},
		"Proxy-Authorization": []string{"Basic secret"},
		"X-Synthetic":         []string{"visible"},
	}
	redacted := RedactHeaders(headers)
	if got := redacted.Get("Authorization"); got != RedactedValue {
		t.Errorf("Authorization = %q, want redacted", got)
	}
	if got := redacted.Get("Proxy-Authorization"); got != RedactedValue {
		t.Errorf("Proxy-Authorization = %q, want redacted", got)
	}
	if got := redacted.Get("X-Synthetic"); got != "visible" {
		t.Errorf("X-Synthetic = %q, want visible", got)
	}
	if got := headers.Get("Authorization"); got != "Digest secret" {
		t.Errorf("source headers were mutated: %q", got)
	}
}

func TestRedactURLUserInfo(t *testing.T) {
	t.Parallel()

	redacted := RedactURL("https://operator:camera-secret@example.invalid/onvif?mode=test")
	if strings.Contains(redacted, "operator") || strings.Contains(redacted, "camera-secret") {
		t.Fatalf("RedactURL() leaked userinfo: %s", redacted)
	}
	if !strings.Contains(redacted, RedactedValue) || !strings.Contains(redacted, "example.invalid") {
		t.Errorf("RedactURL() = %s", redacted)
	}
}
