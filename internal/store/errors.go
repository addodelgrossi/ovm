package store

import "errors"

// ErrorCode is a stable, machine-readable repository error classification.
type ErrorCode string

const (
	CodeInvalidData       ErrorCode = "invalid_data"
	CodeMalformedStore    ErrorCode = "malformed_store"
	CodeUnsupportedSchema ErrorCode = "unsupported_schema"
	CodeFutureSchema      ErrorCode = "future_schema"
	CodeDuplicate         ErrorCode = "duplicate"
	CodeDeviceNotFound    ErrorCode = "device_not_found"
	CodeConflict          ErrorCode = "conflict"
	CodePersistence       ErrorCode = "persistence"
	CodeTransaction       ErrorCode = "transaction"
	CodeRollbackFailed    ErrorCode = "rollback_failed"
)

// Error is a typed, user-safe storage error. Messages never contain persisted
// values, paths, credentials, keyring payloads, or backend diagnostics.
type Error struct {
	Code    ErrorCode
	message string
	cause   error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.message
}

// Unwrap exposes only causes that are already sanitized typed errors.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// Is compares storage errors by stable code.
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && e != nil && e.Code == other.Code
}

var (
	ErrInvalidData       = &Error{Code: CodeInvalidData, message: "stored data is invalid"}
	ErrMalformedStore    = &Error{Code: CodeMalformedStore, message: "the store file is malformed"}
	ErrUnsupportedSchema = &Error{Code: CodeUnsupportedSchema, message: "the store schema version is unsupported"}
	ErrFutureSchema      = &Error{Code: CodeFutureSchema, message: "the store was written by a newer version"}
	ErrDuplicate         = &Error{Code: CodeDuplicate, message: "the store contains duplicate records"}
	ErrDeviceNotFound    = &Error{Code: CodeDeviceNotFound, message: "the device was not found"}
	ErrConflict          = &Error{Code: CodeConflict, message: "the stored metadata changed concurrently"}
	ErrPersistence       = &Error{Code: CodePersistence, message: "the store could not be persisted"}
	ErrTransaction       = &Error{Code: CodeTransaction, message: "the metadata and secret update could not be completed"}
	ErrRollbackFailed    = &Error{Code: CodeRollbackFailed, message: "the metadata and secret update could not be rolled back"}
)

func newError(code ErrorCode, message string) *Error {
	return &Error{Code: code, message: message}
}

func newErrorWithCause(code ErrorCode, message string, cause error) *Error {
	return &Error{Code: code, message: message, cause: cause}
}

// ErrorCodeOf returns the stable storage code, or the empty code for a nil or
// non-storage error.
func ErrorCodeOf(err error) ErrorCode {
	var storeErr *Error
	if errors.As(err, &storeErr) {
		return storeErr.Code
	}
	return ""
}
