package auth

import (
	"bytes"
	"encoding/base64"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestUsernameTokenKnownVector(t *testing.T) {
	t.Parallel()

	rawNonce, err := base64.StdEncoding.DecodeString("WScqanjCEAC4mQoBE07sAQ==")
	if err != nil {
		t.Fatalf("decode nonce: %v", err)
	}
	now := time.Date(2003, time.July, 16, 1, 24, 32, 999_000_000, time.UTC)
	generator := UsernameTokenGenerator{
		Random:    bytes.NewReader(rawNonce),
		Now:       func() time.Time { return now },
		NonceSize: len(rawNonce),
	}

	token, err := generator.Generate("NNK", "verySecret", 0)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if token.created != "2003-07-16T01:24:32Z" {
		t.Errorf("Created = %q, want whole-second RFC3339", token.created)
	}
	if token.nonce != "WScqanjCEAC4mQoBE07sAQ==" {
		t.Errorf("Nonce = %q", token.nonce)
	}
	if token.passwordDigest != "wA/wIG0oKnlI6rjkbqV+JPygnPg=" {
		t.Errorf("PasswordDigest = %q, want independently verified fixed vector", token.passwordDigest)
	}
}

func TestUsernameTokenAppliesClockOffsetAndMarshalsSafeHeader(t *testing.T) {
	t.Parallel()

	generator := UsernameTokenGenerator{
		Random: bytes.NewReader(bytes.Repeat([]byte{0x42}, 20)),
		Now: func() time.Time {
			return time.Date(2026, 10, 8, 12, 0, 0, 800_000_000, time.FixedZone("test", -4*60*60))
		},
		NonceSize: 20,
	}
	token, err := generator.Generate("operator<&", "camera-secret", 2*time.Second)
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if token.created != "2026-10-08T16:00:02Z" {
		t.Errorf("Created = %q, want offset-adjusted UTC whole seconds", token.created)
	}
	header, err := token.HeaderXML()
	if err != nil {
		t.Fatalf("HeaderXML() error = %v", err)
	}
	text := string(header)
	if strings.Contains(text, "camera-secret") {
		t.Fatalf("HeaderXML() leaked plaintext password: %s", text)
	}
	for _, want := range []string{"UsernameToken", "PasswordDigest", "Nonce", "Created", "operator&lt;&amp;"} {
		if !strings.Contains(text, want) {
			t.Errorf("HeaderXML() = %s, want %q", text, want)
		}
	}
}

func TestUsernameTokenUsesCryptographicRandomnessByDefault(t *testing.T) {
	t.Parallel()

	generator := UsernameTokenGenerator{Now: func() time.Time { return time.Unix(0, 0).UTC() }}
	first, err := generator.Generate("operator", "secret", 0)
	if err != nil {
		t.Fatalf("first Generate() error = %v", err)
	}
	second, err := generator.Generate("operator", "secret", 0)
	if err != nil {
		t.Fatalf("second Generate() error = %v", err)
	}
	if first.nonce == second.nonce || first.passwordDigest == second.passwordDigest {
		t.Fatal("default generator reused cryptographic nonce material")
	}
}

func TestUsernameTokenReportsRandomSourceFailureWithoutCredentials(t *testing.T) {
	t.Parallel()

	generator := UsernameTokenGenerator{
		Random: errorReader{},
		Now:    time.Now,
	}
	_, err := generator.Generate("sensitive-user", "sensitive-password", 0)
	if err == nil {
		t.Fatal("Generate() error = nil, want random source failure")
	}
	if strings.Contains(err.Error(), "sensitive-user") || strings.Contains(err.Error(), "sensitive-password") {
		t.Fatalf("Generate() error leaked credentials: %v", err)
	}
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Generate() error = %v, want wrapped io.ErrUnexpectedEOF", err)
	}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }
