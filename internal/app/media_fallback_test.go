package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// fakeNetError is a minimal net.Error used to prove the predicate disqualifies
// a network timeout through the typed interface, not through fault text.
type fakeNetError struct{ timeout bool }

func (e fakeNetError) Error() string   { return "synthetic network failure" }
func (e fakeNetError) Timeout() bool   { return e.timeout }
func (e fakeNetError) Temporary() bool { return false }

// TestShouldFallBackToMedia1Matrix pins the narrow fallback predicate. It
// returns true ONLY for a direct or wrapped ActionNotSupported with no
// disqualifier, and false for nil and every disqualifying class — including
// each disqualifier errors.Join'ed with ActionNotSupported. Every decision is
// made on typed sentinels/values, never on fault text.
func TestShouldFallBackToMedia1Matrix(t *testing.T) {
	t.Parallel()

	// Eligible: direct and wrapped ActionNotSupported.
	eligible := []error{
		soap.ErrActionNotSupported,
		fmt.Errorf("wrapped: %w", soap.ErrActionNotSupported),
	}
	for _, err := range eligible {
		if !shouldFallBackToMedia1(err) {
			t.Errorf("shouldFallBackToMedia1(%v) = false, want true", err)
		}
	}

	if shouldFallBackToMedia1(nil) {
		t.Error("shouldFallBackToMedia1(nil) = true, want false")
	}

	disqualifiers := []struct {
		name string
		err  error
	}{
		{"not authorized", soap.ErrNotAuthorized},
		{"auth policy", auth.ErrAuthPolicy},
		{"insecure digest blocked", ErrInsecureDigestBlocked},
		{"context canceled", context.Canceled},
		{"context deadline", context.DeadlineExceeded},
		{"net timeout", fakeNetError{timeout: true}},
		{"net failure", fakeNetError{}},
		{"dtd not allowed", soap.ErrDTDNotAllowed},
		{"xml complexity", soap.ErrXMLComplexity},
		{"invalid media type", soap.ErrInvalidMediaType},
		{"envelope mismatch", soap.ErrEnvelopeVersionMismatch},
		{"invalid xml", soap.ErrInvalidXML},
		{"invalid argval", soap.ErrInvalidArgVal},
		{"version mismatch", soap.ErrVersionMismatch},
		{"cross-origin redirect", soap.ErrCrossOriginRedirect},
		{"unsafe redirect", soap.ErrUnsafeRedirect},
		{"invalid endpoint", soap.ErrInvalidEndpoint},
		{"invalid action", soap.ErrInvalidAction},
		{"response too large", &soap.ResponseTooLargeError{Limit: 1}},
		{"request too large", &soap.RequestTooLargeError{Limit: 1, Size: 2}},
		{"http error", &soap.HTTPError{StatusCode: 500}},
		{"parse error", &soap.ParseError{Err: soap.ErrInvalidXML}},
		{"transport error", &soap.TransportError{Err: errors.New("synthetic")}},
		{"media2 malformed", media2.ErrMalformedXML},
		{"media2 unexpected", media2.ErrUnexpectedElement},
		{"media2 missing", media2.ErrMissingField},
		{"media2 invalid", media2.ErrInvalidField},
		{"media2 nil response", media2.ErrNilResponse},
		{"media2 invalid input", media2.ErrInvalidInput},
	}
	for _, tc := range disqualifiers {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// A disqualifier alone must be false.
			if shouldFallBackToMedia1(tc.err) {
				t.Errorf("shouldFallBackToMedia1(%s) = true, want false", tc.name)
			}
			// A disqualifier joined with ActionNotSupported must STILL be false:
			// the disqualifier vetoes the fallback even though ActionNotSupported
			// is present.
			joined := errors.Join(tc.err, soap.ErrActionNotSupported)
			if shouldFallBackToMedia1(joined) {
				t.Errorf("shouldFallBackToMedia1(join(%s, ActionNotSupported)) = true, want false", tc.name)
			}
		})
	}
}
