package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// newVectorToken builds a deterministic token whose material is known, so the
// redaction tests can assert that none of those exact values ever appears in
// any formatting or serialization output.
func newVectorToken(t *testing.T) (UsernameToken, []string) {
	t.Helper()
	generator := UsernameTokenGenerator{
		Random:    bytes.NewReader(bytes.Repeat([]byte{0x11}, 20)),
		Now:       func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
		NonceSize: 20,
	}
	token, err := generator.Generate("secret-user", "secret-password", 0)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	// The material that must never leak through default formatting or JSON.
	secrets := []string{token.username, token.passwordDigest, token.nonce, token.created, "secret-user"}
	return token, secrets
}

func assertNoSecrets(t *testing.T, label, output string, secrets []string) {
	t.Helper()
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		if strings.Contains(output, secret) {
			t.Errorf("%s leaked credential material %q: %s", label, secret, output)
		}
	}
}

func TestUsernameTokenRedactsAllFormattingVerbs(t *testing.T) {
	t.Parallel()

	token, secrets := newVectorToken(t)

	verbs := map[string]string{
		"%v":  fmt.Sprintf("%v", token),
		"%+v": fmt.Sprintf("%+v", token),
		"%#v": fmt.Sprintf("%#v", token),
		//nolint:staticcheck // Intentionally exercise fmt's %s handling, not direct String().
		"%s":      fmt.Sprintf("%s", token),
		"%q":      fmt.Sprintf("%q", token),
		"ptr %v":  fmt.Sprintf("%v", &token),
		"ptr %#v": fmt.Sprintf("%#v", &token),
	}
	for name, output := range verbs {
		assertNoSecrets(t, name, output, secrets)
		if !strings.Contains(output, "REDACTED") {
			t.Errorf("%s output = %q, want a REDACTED placeholder", name, output)
		}
	}

	if got := token.String(); got != redactedValue {
		t.Errorf("String() = %q, want %q", got, redactedValue)
	}
	if got := token.GoString(); got != redactedValue {
		t.Errorf("GoString() = %q, want %q", got, redactedValue)
	}
}

func TestUsernameTokenRejectsJSONSerialization(t *testing.T) {
	t.Parallel()

	token, secrets := newVectorToken(t)

	if _, err := json.Marshal(token); err == nil {
		t.Fatal("json.Marshal(token) error = nil, want refusal")
	}

	// A token embedded in a larger structure must also fail to serialize rather
	// than silently emitting the digest tuple.
	wrapper := struct {
		Token UsernameToken `json:"token"`
	}{Token: token}
	data, err := json.Marshal(wrapper)
	if err == nil {
		t.Fatalf("json.Marshal(wrapper) error = nil, output = %s", data)
	}
	assertNoSecrets(t, "json error", err.Error(), secrets)
}

// TestUsernameTokenHeaderXMLUnchanged pins the exact header bytes for a known
// vector so the opacity refactor cannot silently alter the wire output.
func TestUsernameTokenHeaderXMLUnchanged(t *testing.T) {
	t.Parallel()

	token, _ := newVectorToken(t)
	header, err := token.HeaderXML()
	if err != nil {
		t.Fatalf("HeaderXML() error = %v", err)
	}
	got := string(header)

	// The nonce and digest are the deterministic outputs of the fixed vector.
	wantNonce := token.nonce
	wantDigest := token.passwordDigest
	wantFragments := []string{
		`<wsse:Security xmlns:wsse="` + wsseNamespace + `" xmlns:wsu="` + wsuNamespace + `">`,
		`<wsse:UsernameToken><wsse:Username>secret-user</wsse:Username>`,
		`<wsse:Password Type="` + passwordDigestURI + `">` + wantDigest + `</wsse:Password>`,
		`<wsse:Nonce EncodingType="` + base64BinaryURI + `">` + wantNonce + `</wsse:Nonce>`,
		`<wsu:Created>2026-01-02T03:04:05Z</wsu:Created>`,
		`</wsse:UsernameToken></wsse:Security>`,
	}
	for _, fragment := range wantFragments {
		if !strings.Contains(got, fragment) {
			t.Errorf("HeaderXML() = %s, missing fragment %q", got, fragment)
		}
	}
}

func TestUsernameTokenHeaderXMLRejectsIncompleteToken(t *testing.T) {
	t.Parallel()

	if _, err := (UsernameToken{}).HeaderXML(); err == nil {
		t.Fatal("HeaderXML() on zero token error = nil, want incomplete error")
	}

	token, _ := newVectorToken(t)
	if token.IsZero() {
		t.Fatal("IsZero() = true for populated token")
	}
	if !(UsernameToken{}).IsZero() {
		t.Fatal("IsZero() = false for zero token")
	}
}
