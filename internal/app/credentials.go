package app

import "errors"

// Policy carries explicit, opt-in authentication downgrade decisions for one
// use case. Every field defaults to the secure posture. The decisions remain
// separate from secret material so they can be inspected without touching the
// password.
type Policy struct {
	// AllowBasic permits HTTP Basic authentication. HTTPS is still required
	// unless AllowBasicOverHTTP is independently enabled.
	AllowBasic bool
	// AllowBasicOverHTTP permits Basic over plaintext HTTP. It is meaningful
	// only with AllowBasic and represents a separate, higher-risk decision.
	AllowBasicOverHTTP bool
	// AllowLegacyDigest permits legacy no-qop (RFC 2069) Digest. Legacy no-qop
	// is refused by default because it lacks qop=auth replay protections.
	AllowLegacyDigest bool
}

// Credentials carries ephemeral authentication material for one inventory use
// case. It is deliberately non-serializable: it has no exported secret fields,
// and its String and GoString methods always redact the contents.
type Credentials struct {
	username string
	password string
	policy   Policy
}

// NewCredentials constructs credentials with Basic optionally enabled for
// HTTPS. Plaintext HTTP and legacy Digest remain disabled.
func NewCredentials(username, password string, allowBasic bool) (*Credentials, error) {
	return NewCredentialsWithPolicy(username, password, Policy{AllowBasic: allowBasic})
}

// NewCredentialsWithPolicy constructs ephemeral credentials with explicit
// downgrade policy. A username is required, and plaintext Basic cannot be
// enabled unless Basic itself is enabled.
func NewCredentialsWithPolicy(username, password string, policy Policy) (*Credentials, error) {
	if username == "" {
		return nil, errors.New("credentials username is required")
	}
	if policy.AllowBasicOverHTTP && !policy.AllowBasic {
		return nil, errors.New("plaintext HTTP Basic requires Basic authentication to be enabled")
	}
	return &Credentials{username: username, password: password, policy: policy}, nil
}

// Username returns the credential username. It is not secret on its own, but
// callers must still avoid logging it alongside identifying information.
func (c *Credentials) Username() string {
	if c == nil {
		return ""
	}
	return c.username
}

// AllowBasic reports whether HTTP Basic is explicitly permitted.
func (c *Credentials) AllowBasic() bool {
	return c != nil && c.policy.AllowBasic
}

// AllowBasicOverHTTP reports whether the separate plaintext Basic downgrade is
// explicitly permitted.
func (c *Credentials) AllowBasicOverHTTP() bool {
	return c != nil && c.policy.AllowBasicOverHTTP
}

// AllowLegacyDigest reports whether legacy no-qop (RFC 2069) Digest is
// explicitly permitted.
func (c *Credentials) AllowLegacyDigest() bool {
	return c != nil && c.policy.AllowLegacyDigest
}

// String returns a credential-safe representation.
func (Credentials) String() string { return "app.Credentials{REDACTED}" }

// GoString returns a credential-safe Go-syntax representation.
func (Credentials) GoString() string { return "app.Credentials{REDACTED}" }

// MarshalJSON refuses to serialize ephemeral credentials.
func (Credentials) MarshalJSON() ([]byte, error) {
	return nil, errors.New("app.Credentials cannot be serialized")
}
