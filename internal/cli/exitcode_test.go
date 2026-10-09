package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/addodelgrossi/ovm/internal/app"
)

func TestExitCodeForErrorMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, ExitOK},
		{"usage_marker", markUsageError(errors.New("bad flag")), ExitUsage},
		{"app_usage", &app.Error{Category: app.CategoryUsage, Message: "bad input"}, ExitUsage},
		{"app_network", &app.Error{Category: app.CategoryNetwork, Message: "net"}, ExitNetwork},
		{"app_authentication", &app.Error{Category: app.CategoryAuthentication, Message: "auth"}, ExitAuthentication},
		{"app_protocol", &app.Error{Category: app.CategoryProtocol, Message: "proto"}, ExitProtocol},
		{"app_cancelled", &app.Error{Category: app.CategoryCancelled, Message: "cancel"}, ExitCancelled},
		{"app_internal", &app.Error{Category: app.CategoryInternal, Message: "oops"}, ExitGeneric},
		{"context_canceled", context.Canceled, ExitCancelled},
		{"context_deadline", context.DeadlineExceeded, ExitNetwork},
		{"generic", errors.New("boom"), ExitGeneric},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCodeForError(tc.err); got != tc.want {
				t.Fatalf("ExitCodeForError = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestExitCodeWrappedUsageMarker(t *testing.T) {
	wrapped := errors.Join(errors.New("context"), markUsageError(errors.New("bad flag")))
	if got := ExitCodeForError(wrapped); got != ExitUsage {
		t.Fatalf("ExitCodeForError = %d, want %d", got, ExitUsage)
	}
}

func TestIsPresentedDetectsWrappedPresentedError(t *testing.T) {
	base := app.NewError(app.CategoryNetwork, "the device could not be reached")
	presented := markPresented(base)
	if !IsPresented(presented) {
		t.Fatal("expected IsPresented to detect a presented error")
	}
	// A presented error still maps to its wrapped category's exit code.
	if got := ExitCodeForError(presented); got != ExitNetwork {
		t.Fatalf("ExitCodeForError(presented) = %d, want %d", got, ExitNetwork)
	}
	// A plain error is not presented.
	if IsPresented(base) {
		t.Fatal("a non-presented error must not report as presented")
	}
	if IsPresented(nil) {
		t.Fatal("nil must not report as presented")
	}
}

func TestMarkPresentedNilIsNil(t *testing.T) {
	if markPresented(nil) != nil {
		t.Fatal("markPresented(nil) must be nil")
	}
}
