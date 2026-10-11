package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha1" // #nosec G505 -- WS-Security UsernameToken PasswordDigest requires SHA-1 by specification.
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	defaultNonceSize = 20
	wsseNamespace    = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
	wsuNamespace     = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"
	//nolint:gosec // This is a public WS-Security type URI, not a credential.
	passwordDigestURI = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest"
	base64BinaryURI   = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary"

	// redactedValue is the single, value-free placeholder emitted by every
	// formatting and serialization path so credential material never reaches a
	// log line or a JSON document by accident.
	redactedValue = "auth.UsernameToken{REDACTED}"
)

// UsernameToken is outbound WS-Security credential material. It deliberately
// does not retain the plaintext password or raw nonce, and its fields are
// unexported so the username, password digest, nonce, and created timestamp
// cannot be printed or serialized by accident. The only sanctioned way to emit
// the material is HeaderXML, which renders the WS-Security Security header.
//
// The redaction contract is enforced across every default sink:
//   - fmt verbs %v, %+v, %#v, %s, and %q route through String/GoString and
//     render only a value-free placeholder (see String and GoString).
//   - encoding/json refuses to serialize the token (see MarshalJSON), so a
//     token embedded in a logged or persisted structure fails loudly rather
//     than leaking the digest tuple.
type UsernameToken struct {
	username       string
	passwordDigest string
	nonce          string
	created        string
}

// UsernameTokenGenerator provides injectable entropy and time for tests. Zero
// fields use crypto/rand.Reader, time.Now, and a 20-byte nonce.
type UsernameTokenGenerator struct {
	Random    io.Reader
	Now       func() time.Time
	NonceSize int
}

// Generate creates a PasswordDigest UsernameToken with an offset-adjusted,
// whole-second UTC RFC3339 timestamp.
func (g UsernameTokenGenerator) Generate(username, password string, clockOffset time.Duration) (UsernameToken, error) {
	random := g.Random
	if random == nil {
		random = rand.Reader
	}
	nonceSize := g.NonceSize
	if nonceSize == 0 {
		nonceSize = defaultNonceSize
	}
	if nonceSize < 16 || nonceSize > 64 {
		return UsernameToken{}, errors.New("UsernameToken nonce size must be between 16 and 64 bytes")
	}
	rawNonce := make([]byte, nonceSize)
	if _, err := io.ReadFull(random, rawNonce); err != nil {
		return UsernameToken{}, fmt.Errorf("generate UsernameToken nonce: %w", err)
	}
	now := g.Now
	if now == nil {
		now = time.Now
	}
	created := now().Add(clockOffset).UTC().Truncate(time.Second).Format(time.RFC3339)

	digestInput := make([]byte, 0, len(rawNonce)+len(created)+len(password))
	digestInput = append(digestInput, rawNonce...)
	digestInput = append(digestInput, created...)
	digestInput = append(digestInput, password...)
	digest := sha1.Sum(digestInput) // #nosec G401 -- mandated by the WS-Security PasswordDigest formula.
	digestText := base64.StdEncoding.EncodeToString(digest[:])
	nonceText := base64.StdEncoding.EncodeToString(rawNonce)
	clear(digestInput)
	clear(rawNonce)

	return UsernameToken{
		username:       username,
		passwordDigest: digestText,
		nonce:          nonceText,
		created:        created,
	}, nil
}

// HeaderXML marshals the token as a WS-Security Security header fragment. This
// is the only sanctioned way to emit the token's material.
func (t UsernameToken) HeaderXML() ([]byte, error) {
	if t.username == "" || t.passwordDigest == "" || t.nonce == "" || t.created == "" {
		return nil, errors.New("UsernameToken is incomplete")
	}
	var output bytes.Buffer
	output.WriteString(`<wsse:Security xmlns:wsse="` + wsseNamespace + `" xmlns:wsu="` + wsuNamespace + `"><wsse:UsernameToken><wsse:Username>`)
	if err := xml.EscapeText(&output, []byte(t.username)); err != nil {
		return nil, fmt.Errorf("marshal UsernameToken username: %w", err)
	}
	output.WriteString(`</wsse:Username><wsse:Password Type="` + passwordDigestURI + `">`)
	if err := xml.EscapeText(&output, []byte(t.passwordDigest)); err != nil {
		return nil, fmt.Errorf("marshal UsernameToken digest: %w", err)
	}
	output.WriteString(`</wsse:Password><wsse:Nonce EncodingType="` + base64BinaryURI + `">`)
	if err := xml.EscapeText(&output, []byte(t.nonce)); err != nil {
		return nil, fmt.Errorf("marshal UsernameToken nonce: %w", err)
	}
	output.WriteString(`</wsse:Nonce><wsu:Created>`)
	if err := xml.EscapeText(&output, []byte(t.created)); err != nil {
		return nil, fmt.Errorf("marshal UsernameToken timestamp: %w", err)
	}
	output.WriteString(`</wsu:Created></wsse:UsernameToken></wsse:Security>`)
	return output.Bytes(), nil
}

// IsZero reports whether the token holds no material. It lets callers check for
// an unset token without reading any credential field.
func (t UsernameToken) IsZero() bool {
	return t == UsernameToken{}
}

// String implements fmt.Stringer and renders only a value-free placeholder, so
// %v, %s, and %q never expose the username, digest, nonce, or timestamp.
func (t UsernameToken) String() string {
	return redactedValue
}

// GoString implements fmt.GoStringer so the %#v verb also renders the
// placeholder rather than the struct's unexported credential fields.
func (t UsernameToken) GoString() string {
	return redactedValue
}

// MarshalJSON refuses to serialize the token. Credential material must never be
// persisted or logged as JSON, so encoding/json fails loudly instead of
// emitting the digest tuple. The error carries no field values.
func (t UsernameToken) MarshalJSON() ([]byte, error) {
	return nil, errors.New("auth.UsernameToken is not JSON-serializable: credential material must not be persisted")
}
