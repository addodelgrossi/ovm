package app

import (
	"errors"

	"github.com/addodelgrossi/ovm/internal/store"
)

// ManagerError is the typed, user-safe error surfaced by the device Manager. It
// preserves the exact application ErrorCategory used everywhere else in this
// package and additionally carries the stable storage and secret-provider codes
// when the failure originated in those layers. It never carries credential
// references, secret material, raw unsafe URLs, or backend diagnostics in its
// Message: the Message is always English, deterministic, and safe to render in
// a JSON/Wails envelope.
//
// The optional StoreCode and SecretKind are the stable, closed-set codes
// defined by the store package. They are empty when the failure did not come
// from the repository or the secret provider respectively, so a caller can key
// on them without ever seeing an internal error string.
type ManagerError struct {
	// Category is the stable application classification used for exit-code
	// mapping and JSON envelopes.
	Category ErrorCategory `json:"category"`
	// Message is a user-safe, English, credential-free description.
	Message string `json:"message"`
	// StoreCode is the stable repository error code when the failure came from
	// the metadata store; empty otherwise.
	StoreCode store.ErrorCode `json:"store_code,omitempty"`
	// SecretKind is the stable secret-provider error kind when the failure came
	// from the secret provider; empty otherwise.
	SecretKind store.SecretErrorKind `json:"secret_kind,omitempty"`
	// cause is the wrapped underlying error, retained for errors.Is/As only. It
	// is never rendered to users by this package.
	cause error
}

// Error implements the error interface with the user-safe message.
func (e *ManagerError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Unwrap exposes the wrapped cause for errors.Is and errors.As.
func (e *ManagerError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// newManagerError builds a typed manager error from an application category and
// a user-safe message, carrying any stable store/secret codes discovered on the
// cause. The cause is retained for unwrapping but never rendered.
func newManagerError(category ErrorCategory, message string, cause error) *ManagerError {
	return &ManagerError{
		Category:   category,
		Message:    message,
		StoreCode:  store.ErrorCodeOf(cause),
		SecretKind: store.SecretErrorKindOf(cause),
		cause:      cause,
	}
}

// managerErrorFromApp lifts a typed application error (for example from a
// delegated use case) into a ManagerError, preserving its exact category and a
// user-safe message. A non-application error is mapped to CategoryInternal with
// a generic message so a backend diagnostic never crosses the boundary.
func managerErrorFromApp(err error) *ManagerError {
	if err == nil {
		return nil
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return newManagerError(appErr.Category, appErr.Message, err)
	}
	// A storage or secret failure that reached here without an application
	// wrapper is mapped by its stable code to a safe category and message.
	if managed := managerErrorFromStore(err); managed != nil {
		return managed
	}
	return newManagerError(CategoryInternal, "an internal error occurred", err)
}

// managerErrorFromStore maps a storage or secret-provider error to a typed
// manager error using only the stable codes, never a raw backend string. It
// returns nil when err is neither a storage nor a secret error so callers can
// fall through to a generic classification.
func managerErrorFromStore(err error) *ManagerError {
	if err == nil {
		return nil
	}
	if code := store.ErrorCodeOf(err); code != "" {
		return newManagerError(storeCodeCategory(code), storeCodeMessage(code), err)
	}
	if kind := store.SecretErrorKindOf(err); kind != "" {
		return newManagerError(secretKindCategory(kind), secretKindMessage(kind), err)
	}
	return nil
}

// storeCodeCategory maps a stable storage code to the application category that
// best describes it for the user. Invalid input is a usage error; everything
// else is an internal/persistence failure that is never the operator's fault to
// correct inline.
func storeCodeCategory(code store.ErrorCode) ErrorCategory {
	switch code {
	case store.CodeInvalidData, store.CodeDuplicate:
		return CategoryUsage
	case store.CodeDeviceNotFound:
		return CategoryUsage
	default:
		return CategoryInternal
	}
}

// storeCodeMessage returns a stable, English, credential-free message for a
// storage code. The messages intentionally never echo persisted values.
func storeCodeMessage(code store.ErrorCode) string {
	switch code {
	case store.CodeInvalidData:
		return "the device metadata is invalid"
	case store.CodeDuplicate:
		return "a device with the same identity already exists"
	case store.CodeDeviceNotFound:
		return "the device was not found"
	case store.CodeConflict:
		return "the stored metadata changed concurrently"
	case store.CodeMalformedStore:
		return "the device store is malformed"
	case store.CodeUnsupportedSchema, store.CodeFutureSchema:
		return "the device store schema is unsupported"
	case store.CodeTransaction:
		return "the device update could not be completed"
	case store.CodeRollbackFailed:
		return "the device update could not be rolled back"
	default:
		return "the device store could not be updated"
	}
}

// secretKindCategory maps a stable secret-provider kind to an application
// category. A missing secret is treated as an authentication requirement; the
// rest are internal/provider failures.
func secretKindCategory(kind store.SecretErrorKind) ErrorCategory {
	switch kind {
	case store.SecretNotFound:
		return CategoryAuthentication
	case store.SecretInvalid:
		return CategoryUsage
	default:
		return CategoryInternal
	}
}

// secretKindMessage returns a stable, English, credential-free message for a
// secret-provider kind. It never carries backend material.
func secretKindMessage(kind store.SecretErrorKind) string {
	switch kind {
	case store.SecretNotFound:
		return "authentication is required for this device"
	case store.SecretInvalid:
		return "the stored credential reference is invalid"
	case store.SecretDenied:
		return "access to secure credential storage was denied"
	case store.SecretUnsupported:
		return "secure credential storage is unsupported on this platform"
	default:
		return "secure credential storage is unavailable"
	}
}
