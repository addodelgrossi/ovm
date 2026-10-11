package app

import (
	"errors"
	"fmt"
)

// ErrorCategory is a stable, machine-readable classification of a user-safe
// application error. Categories map to CLI exit codes and are safe to surface
// in JSON envelopes. They never encode credential material.
type ErrorCategory string

const (
	// CategoryUsage identifies invalid input, flags, or argument combinations.
	CategoryUsage ErrorCategory = "usage"
	// CategoryNetwork identifies network, discovery, or timeout failures that
	// are not specifically authentication.
	CategoryNetwork ErrorCategory = "network"
	// CategoryAuthentication identifies an authentication/authorization
	// failure, such as an ONVIF NotAuthorized fault.
	CategoryAuthentication ErrorCategory = "authentication"
	// CategoryProtocol identifies a malformed or unexpected protocol response.
	CategoryProtocol ErrorCategory = "protocol"
	// CategoryCancelled identifies a context cancellation (for example an
	// interrupt signal).
	CategoryCancelled ErrorCategory = "cancelled"
	// CategoryInternal identifies a generic or internal failure.
	CategoryInternal ErrorCategory = "internal"
)

// Error is a typed, user-safe application error. Its Message is always safe to
// print: it never contains plaintext passwords, UsernameToken material,
// Authorization header values, or credential-bearing URLs. The wrapped cause is
// retained for errors.Is/errors.As but is only surfaced to callers that
// explicitly unwrap it; it is never rendered to users by this package.
type Error struct {
	// Category is the stable classification used for exit-code mapping and
	// JSON envelopes.
	Category ErrorCategory
	// Message is a user-safe, English, credential-free description.
	Message string
	// cause is the wrapped underlying error, retained for unwrapping only.
	cause error
}

// Error implements the error interface with the user-safe message.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Unwrap exposes the wrapped cause for errors.Is and errors.As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// newError builds a typed application error with a user-safe message and an
// optional wrapped cause. The cause is never rendered by this package; callers
// that need it must unwrap deliberately.
func newError(category ErrorCategory, message string, cause error) *Error {
	return &Error{Category: category, Message: message, cause: cause}
}

// CategoryOf returns the stable category of err, defaulting to
// CategoryInternal when err is not an application error. A nil error yields the
// empty category.
func CategoryOf(err error) ErrorCategory {
	if err == nil {
		return ""
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Category
	}
	return CategoryInternal
}

// usageError builds a CategoryUsage error from a formatted, credential-free
// message. Callers must not interpolate secret material into the format.
func usageError(format string, args ...any) *Error {
	return newError(CategoryUsage, fmt.Sprintf(format, args...), nil)
}

// NewInternalError builds a user-safe CategoryInternal error from a stable,
// credential-free message. It is exported for composition seams (for example a
// command that detects a missing dependency) so they can fail safely with the
// generic exit code instead of panicking. Callers must not interpolate secret
// material into the message.
func NewInternalError(message string) *Error {
	return newError(CategoryInternal, message, nil)
}

// NewError builds a user-safe typed application error from a stable category
// and a credential-free message, with no wrapped cause. It is the general
// constructor for callers outside this package that need to synthesize a typed
// boundary error (for example a composition seam or a test double). Callers
// must not interpolate secret material into the message.
func NewError(category ErrorCategory, message string) *Error {
	return newError(category, message, nil)
}
