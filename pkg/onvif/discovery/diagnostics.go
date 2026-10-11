package discovery

import (
	"errors"
	"os"
	"strings"
	"syscall"
)

// DiagnosticCategory names a stable, machine-readable explanation for why a
// discovery attempt produced no devices or failed. Categories are deliberately
// coarse so adapters and tests can switch on them without parsing prose.
type DiagnosticCategory string

const (
	// DiagnosticNone indicates no diagnostic applies (devices were found).
	DiagnosticNone DiagnosticCategory = ""
	// DiagnosticNoResponse indicates the probe completed but no device replied
	// within the timeout. This is the ONLY category used for a timeout or an
	// otherwise empty result; it never implies a permission problem.
	DiagnosticNoResponse DiagnosticCategory = "no_response"
	// DiagnosticSocketError indicates a socket operation failed for a reason
	// that is not specifically a permission restriction.
	DiagnosticSocketError DiagnosticCategory = "socket_error"
	// DiagnosticPossiblePermissionRestriction indicates a concrete,
	// permission-like socket error occurred (for example EACCES or EPERM).
	DiagnosticPossiblePermissionRestriction DiagnosticCategory = "possible_permission_restriction"
)

// Diagnostic carries a stable category together with a safe, human-readable
// detail. Detail never contains raw datagram bytes or credentials.
type Diagnostic struct {
	Category DiagnosticCategory
	Detail   string
}

// String renders the diagnostic as "category: detail" or just the category
// when no detail is present.
func (d Diagnostic) String() string {
	if d.Detail == "" {
		return string(d.Category)
	}
	return string(d.Category) + ": " + d.Detail
}

// classifySocketError maps a socket error to a diagnostic. A permission-like
// error is only reported as possible_permission_restriction when the error
// concretely indicates a permission problem; every other socket error is a
// generic socket_error. Timeouts are never classified here — a timeout with no
// response is strictly no_response and is produced by the caller.
func classifySocketError(err error) Diagnostic {
	if err == nil {
		return Diagnostic{Category: DiagnosticNone}
	}
	if isPermissionError(err) {
		return Diagnostic{
			Category: DiagnosticPossiblePermissionRestriction,
			Detail:   "the operating system denied the socket operation",
		}
	}
	return Diagnostic{
		Category: DiagnosticSocketError,
		Detail:   "a socket operation failed",
	}
}

// isPermissionError reports whether err concretely indicates a permission
// restriction. It matches os.ErrPermission and the EACCES/EPERM errnos; a
// timeout or a generic failure is deliberately excluded.
func isPermissionError(err error) bool {
	if err == nil {
		return false
	}
	// A timeout must never be treated as a permission problem, even if some
	// layer also wraps a permission sentinel.
	var timeout interface{ Timeout() bool }
	if errors.As(err, &timeout) && timeout.Timeout() {
		return false
	}
	if errors.Is(err, os.ErrPermission) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EACCES || errno == syscall.EPERM
	}
	// Fall back to a conservative textual check only for the exact permission
	// phrases, never for timeouts or generic failures.
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "timeout") || strings.Contains(message, "deadline") {
		return false
	}
	return strings.Contains(message, "permission denied") || strings.Contains(message, "operation not permitted")
}
