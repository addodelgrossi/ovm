package cli

import (
	"context"
	"errors"

	"github.com/addodelgrossi/ovm/internal/app"
)

// Exit codes are a stable, documented mapping used by cmd/ovm/main.go. The
// values follow common CLI conventions and never infer a permission problem
// from a timeout.
const (
	// ExitOK indicates success.
	ExitOK = 0
	// ExitGeneric indicates a generic or internal failure.
	ExitGeneric = 1
	// ExitUsage indicates invalid input, flags, or argument combinations.
	ExitUsage = 2
	// ExitNetwork indicates a network, discovery, or timeout failure.
	ExitNetwork = 3
	// ExitAuthentication indicates an authentication failure.
	ExitAuthentication = 4
	// ExitProtocol indicates a malformed or unexpected protocol response.
	ExitProtocol = 5
	// ExitCancelled indicates cancellation (for example an interrupt signal).
	ExitCancelled = 130
)

// usageErrorMarker wraps an error that must map to the usage exit code even
// when it is not an *app.Error, for example a Cobra flag-parsing failure.
type usageErrorMarker struct{ err error }

func (e *usageErrorMarker) Error() string { return e.err.Error() }
func (e *usageErrorMarker) Unwrap() error { return e.err }

// markUsageError tags err so ExitCodeForError maps it to ExitUsage.
func markUsageError(err error) error {
	if err == nil {
		return nil
	}
	return &usageErrorMarker{err: err}
}

// presentedError wraps an error whose user-facing explanation has already been
// written to stderr by the command itself (for example a discovery diagnostic).
// The process entry point detects it through IsPresented and suppresses the
// duplicate top-level error print while still mapping the wrapped error to its
// exit code. This keeps "print the safe error exactly once" true even when a
// diagnostic was emitted before the error was known to be fatal.
type presentedError struct{ err error }

func (e *presentedError) Error() string { return e.err.Error() }
func (e *presentedError) Unwrap() error { return e.err }

// markPresented tags err as already presented to the user. A nil error is
// returned unchanged.
func markPresented(err error) error {
	if err == nil {
		return nil
	}
	return &presentedError{err: err}
}

// IsPresented reports whether err (or anything it wraps) was already presented
// to the user by the command, so the process entry point must not print it
// again. It is exported for cmd/ovm/main.go.
func IsPresented(err error) bool {
	var presented *presentedError
	return errors.As(err, &presented)
}

// ExitCodeForError maps an error returned by command execution to a stable exit
// code. The mapping is total: a nil error is ExitOK and any unclassified error
// is ExitGeneric. Cancellation is detected both through the typed application
// category and through context sentinels so an interrupted run exits 130.
//
// Precedence is deliberate and ordered:
//
//  1. An explicit usage marker wins unconditionally: bad input is a usage
//     failure even if the context was later cancelled.
//  2. A wrapped or joined context.Canceled then wins over the typed-category
//     switch. Cancellation is an authoritative signal, and ordering it ahead of
//     the app.Error switch guarantees an interrupt maps to 130 even when a
//     lower layer attached a competing, incorrectly categorized category (for
//     example a cancellation surfaced as a network error). A plain
//     context.DeadlineExceeded is intentionally *not* promoted here: a deadline
//     is a timeout/network condition and must fall through to the typed switch
//     or to the network default.
//  3. The typed application category.
//  4. Context sentinels as a final fallback for un-typed errors.
func ExitCodeForError(err error) int {
	if err == nil {
		return ExitOK
	}

	// A tagged usage error (for example a Cobra flag-parsing failure) takes
	// precedence so bad input always maps to the usage code.
	var usage *usageErrorMarker
	if errors.As(err, &usage) {
		return ExitUsage
	}

	// A wrapped or joined context.Canceled is authoritative and is checked
	// before the typed-category switch so cancellation cannot be masked by a
	// competing, incorrectly categorized app.Error further down the chain.
	// Only genuine cancellation is promoted here; a bare deadline is left to
	// the typed switch and network default below.
	if errors.Is(err, context.Canceled) {
		return ExitCancelled
	}

	// Typed application errors carry an explicit, stable category.
	var appErr *app.Error
	if errors.As(err, &appErr) {
		switch appErr.Category {
		case app.CategoryUsage:
			return ExitUsage
		case app.CategoryNetwork:
			return ExitNetwork
		case app.CategoryAuthentication:
			return ExitAuthentication
		case app.CategoryProtocol:
			return ExitProtocol
		case app.CategoryCancelled:
			return ExitCancelled
		case app.CategoryInternal:
			return ExitGeneric
		}
	}

	// An un-typed deadline still maps to a stable code. A deadline is a
	// network/timeout condition and never a permission problem.
	if errors.Is(err, context.DeadlineExceeded) {
		return ExitNetwork
	}
	return ExitGeneric
}
