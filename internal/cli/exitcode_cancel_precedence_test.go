package cli

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/addodelgrossi/ovm/internal/app"
)

// TestExitCodeCancelledBeatsMiscategorizedAppError proves finding #2's harden:
// after the explicit usage-marker precedence, a wrapped or joined
// context.Canceled maps to 130 even when the chain also carries a typed
// app.Error whose category is wrong (here, a cancellation incorrectly surfaced
// as a network error). Cancellation is authoritative and must not be masked.
func TestExitCodeCancelledBeatsMiscategorizedAppError(t *testing.T) {
	miscategorized := app.NewError(app.CategoryNetwork, "the network operation failed")

	tests := []struct {
		name string
		err  error
	}{
		{
			name: "wrapped",
			err:  fmt.Errorf("inventory failed: %w", errors.Join(miscategorized, context.Canceled)),
		},
		{
			name: "joined",
			err:  errors.Join(miscategorized, context.Canceled),
		},
		{
			name: "wrapped_canceled_outer",
			err:  fmt.Errorf("%w: %w", context.Canceled, miscategorized),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCodeForError(tc.err); got != ExitCancelled {
				t.Fatalf("ExitCodeForError = %d, want %d (cancellation must beat a miscategorized app error)", got, ExitCancelled)
			}
		})
	}
}

// TestExitCodeUsageStillBeatsCancellation confirms the usage marker keeps its
// unconditional top precedence even when the chain also carries
// context.Canceled: bad input is a usage failure regardless of a later cancel.
func TestExitCodeUsageStillBeatsCancellation(t *testing.T) {
	err := errors.Join(markUsageError(errors.New("bad flag")), context.Canceled)
	if got := ExitCodeForError(err); got != ExitUsage {
		t.Fatalf("ExitCodeForError = %d, want %d (usage must beat cancellation)", got, ExitUsage)
	}
}

// TestExitCodeDeadlineNotPromotedOverAppCategory confirms a bare deadline is
// not promoted ahead of the typed switch: a deadline joined with an
// authentication app error still resolves to the app error's category, since a
// deadline is a timeout/network condition and not an authoritative cancel.
func TestExitCodeDeadlineNotPromotedOverAppCategory(t *testing.T) {
	err := errors.Join(app.NewError(app.CategoryAuthentication, "auth"), context.DeadlineExceeded)
	if got := ExitCodeForError(err); got != ExitAuthentication {
		t.Fatalf("ExitCodeForError = %d, want %d (deadline must not be promoted over the typed category)", got, ExitAuthentication)
	}
}
