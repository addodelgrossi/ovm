package discovery

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
)

func TestClassifySocketError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want DiagnosticCategory
	}{
		{name: "nil is none", err: nil, want: DiagnosticNone},
		{name: "generic socket error", err: errors.New("connection reset"), want: DiagnosticSocketError},
		{name: "eacces is permission", err: syscall.EACCES, want: DiagnosticPossiblePermissionRestriction},
		{name: "eperm is permission", err: syscall.EPERM, want: DiagnosticPossiblePermissionRestriction},
		{name: "os.ErrPermission is permission", err: os.ErrPermission, want: DiagnosticPossiblePermissionRestriction},
		{name: "wrapped eacces is permission", err: fmt.Errorf("listen: %w", syscall.EACCES), want: DiagnosticPossiblePermissionRestriction},
		{name: "permission text phrase", err: errors.New("bind: operation not permitted"), want: DiagnosticPossiblePermissionRestriction},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifySocketError(tc.err)
			if got.Category != tc.want {
				t.Fatalf("category = %q, want %q", got.Category, tc.want)
			}
		})
	}
}

// TestTimeoutIsNeverPermission guards the core invariant: a timeout, even if it
// somehow also wraps a permission sentinel, must never be classified as a
// permission restriction.
func TestTimeoutIsNeverPermission(t *testing.T) {
	if isPermissionError(timeoutError{}) {
		t.Fatal("plain timeout classified as permission error")
	}
	wrapped := fmt.Errorf("timeout occurred: %w", os.ErrPermission)
	if isPermissionError(timeoutWrapping{inner: wrapped}) {
		t.Fatal("timeout wrapping permission classified as permission error")
	}
	// A textual timeout must not be a permission error either.
	if isPermissionError(errors.New("i/o timeout")) {
		t.Fatal("textual timeout classified as permission error")
	}
}

// timeoutWrapping reports Timeout() true while also wrapping another error.
type timeoutWrapping struct{ inner error }

func (t timeoutWrapping) Error() string { return t.inner.Error() }
func (t timeoutWrapping) Timeout() bool { return true }
func (t timeoutWrapping) Unwrap() error { return t.inner }

func TestSummariseDiagnostics(t *testing.T) {
	tests := []struct {
		name  string
		diags []Diagnostic
		want  DiagnosticCategory
	}{
		{
			name:  "empty is strictly no_response",
			diags: nil,
			want:  DiagnosticNoResponse,
		},
		{
			name:  "socket error dominates no response",
			diags: []Diagnostic{{Category: DiagnosticSocketError}},
			want:  DiagnosticSocketError,
		},
		{
			name: "permission dominates socket error",
			diags: []Diagnostic{
				{Category: DiagnosticSocketError},
				{Category: DiagnosticPossiblePermissionRestriction},
			},
			want: DiagnosticPossiblePermissionRestriction,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := summariseDiagnostics(tc.diags); got.Category != tc.want {
				t.Fatalf("category = %q, want %q", got.Category, tc.want)
			}
		})
	}
}

func TestDiagnosticString(t *testing.T) {
	if got := (Diagnostic{Category: DiagnosticNoResponse, Detail: "x"}).String(); got != "no_response: x" {
		t.Fatalf("String = %q", got)
	}
	if got := (Diagnostic{Category: DiagnosticSocketError}).String(); got != "socket_error" {
		t.Fatalf("String = %q", got)
	}
}
