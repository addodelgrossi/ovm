package auth

import "errors"

// PolicyReason is a stable, non-secret identifier for a refused authentication
// policy decision. Values are safe to log and compare; they never embed
// credentials, nonces, realms, hostnames, or any request-derived data.
type PolicyReason string

const (
	// ReasonBasicDisabled means HTTP Basic was offered but AllowBasic is false.
	ReasonBasicDisabled PolicyReason = "basic_disabled"
	// ReasonBasicOverPlaintext means HTTP Basic was offered over plaintext HTTP
	// while AllowBasicOverHTTP is false.
	ReasonBasicOverPlaintext PolicyReason = "basic_over_plaintext"
	// ReasonRFC2069Disabled means a no-qop (RFC 2069) Digest challenge was
	// offered while AllowRFC2069 is false.
	ReasonRFC2069Disabled PolicyReason = "rfc2069_disabled"
	// ReasonIncoherentBasicPolicy means AllowBasicOverHTTP was requested without
	// AllowBasic, which can never permit any Basic exchange.
	ReasonIncoherentBasicPolicy PolicyReason = "incoherent_basic_policy"
)

// PolicyError reports that an authentication exchange was refused by an
// explicit, non-secret policy. Its message and reason never contain
// credentials or request-derived secrets, so it is safe to surface and log.
type PolicyError struct {
	// Reason is a stable machine-comparable identifier for the refusal.
	Reason PolicyReason
}

// Error implements error with a fixed, non-secret message per reason.
func (e *PolicyError) Error() string {
	switch e.Reason {
	case ReasonBasicDisabled:
		return "HTTP Basic authentication requires explicit policy"
	case ReasonBasicOverPlaintext:
		return "HTTP Basic authentication over plaintext HTTP requires explicit policy"
	case ReasonRFC2069Disabled:
		return "legacy RFC 2069 Digest authentication requires explicit policy"
	case ReasonIncoherentBasicPolicy:
		return "AllowBasicOverHTTP requires AllowBasic"
	default:
		return "authentication policy refused the exchange"
	}
}

// Is lets callers match any PolicyError with errors.Is(err, ErrAuthPolicy) and
// also match a specific reason with errors.Is(err, &PolicyError{Reason: ...}).
func (e *PolicyError) Is(target error) bool {
	if target == ErrAuthPolicy {
		return true
	}
	other, ok := target.(*PolicyError)
	if !ok {
		return false
	}
	return other.Reason == "" || other.Reason == e.Reason
}

// ErrAuthPolicy is a sentinel that matches every PolicyError via errors.Is.
var ErrAuthPolicy = errors.New("authentication policy refused the exchange")

// newPolicyError constructs a typed, non-secret policy error.
func newPolicyError(reason PolicyReason) *PolicyError {
	return &PolicyError{Reason: reason}
}
