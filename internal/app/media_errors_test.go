package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	media1 "github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// secretURLError builds a *url.Error whose embedded URL carries a secret query
// marker, wrapping an inner cause. It models the way net/http surfaces a
// transport failure (the raw url.Error embeds the request URL and query), so a
// test can prove the media classifiers never retain that cause.
func secretURLError(marker string, inner error) error {
	return &url.Error{Op: "Get", URL: "https://cam.local/onvif?access=" + marker, Err: inner}
}

// dialTimeoutError is a net.Error that reports a timeout, modeling a dial/read
// timeout surfaced from the transport.
type dialTimeoutError struct{}

func (dialTimeoutError) Error() string   { return "dial tcp: i/o timeout" }
func (dialTimeoutError) Timeout() bool   { return true }
func (dialTimeoutError) Temporary() bool { return true }

// TestClassifyMediaOpTable exercises the per-operation classifier across the
// full error taxonomy and asserts the normalized capability state. Each case
// uses typed sentinels or concrete error values — never fault reason text.
func TestClassifyMediaOpTable(t *testing.T) {
	t.Parallel()

	const secret = "op-secret-marker"
	cases := []struct {
		name  string
		err   error
		state CapabilityState
	}{
		{"nil is supported", nil, CapabilitySupported},
		{"action not supported", soap.ErrActionNotSupported, CapabilityUnsupported},
		{"cancellation", context.Canceled, CapabilityTemporarilyUnavailable},
		{"deadline", context.DeadlineExceeded, CapabilityTemporarilyUnavailable},
		{"not authorized", soap.ErrNotAuthorized, CapabilityUnknown},
		{"unsupported digest challenge", auth.ErrUnsupportedDigestChallenge, CapabilityUnknown},
		{"auth policy", auth.ErrAuthPolicy, CapabilityUnknown},
		{"insecure digest blocked", ErrInsecureDigestBlocked, CapabilityUnknown},
		{"http 401", &soap.HTTPError{StatusCode: 401}, CapabilityUnknown},
		{"http 403", &soap.HTTPError{StatusCode: 403}, CapabilityUnknown},
		{"http 500 protocol", &soap.HTTPError{StatusCode: 500}, CapabilityUnknown},
		{"invalid arg val", soap.ErrInvalidArgVal, CapabilityUnknown},
		{"dtd not allowed", soap.ErrDTDNotAllowed, CapabilityUnknown},
		{"response too large", &soap.ResponseTooLargeError{Limit: 10, HTTPStatus: 200}, CapabilityUnknown},
		{"media1 malformed", media1.ErrMalformedXML, CapabilityUnknown},
		{"media2 malformed", media2.ErrMalformedXML, CapabilityUnknown},
		{"media2 missing field", media2.ErrMissingField, CapabilityUnknown},
		{"net timeout wrapped in url.Error", secretURLError(secret, dialTimeoutError{}), CapabilityTemporarilyUnavailable},
		{"connection refused", secretURLError(secret, &net.OpError{Op: "dial", Err: errors.New("connection refused")}), CapabilityTemporarilyUnavailable},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := classifyMediaOp(tc.err)
			if got.State != tc.state {
				t.Fatalf("state = %q, want %q", got.State, tc.state)
			}
			// The detail is always a fixed, value-free English string.
			if strings.Contains(got.Detail, secret) {
				t.Fatalf("detail leaked the secret marker: %q", got.Detail)
			}
		})
	}
}

// TestMapMediaErrorTable exercises the fatal classifier and asserts both the
// error category and that no raw URL/query ever survives in the returned
// error's recursive unwrap chain or any fmt rendering.
func TestMapMediaErrorTable(t *testing.T) {
	t.Parallel()

	const secret = "fatal-secret-marker"
	cases := []struct {
		name     string
		err      error
		category ErrorCategory
	}{
		{"nil", nil, ""},
		{"not authorized", soap.ErrNotAuthorized, CategoryAuthentication},
		{"unsupported digest challenge", auth.ErrUnsupportedDigestChallenge, CategoryAuthentication},
		{"auth policy", auth.ErrAuthPolicy, CategoryAuthentication},
		{"insecure digest blocked", ErrInsecureDigestBlocked, CategoryAuthentication},
		{"http 401 is auth", &soap.HTTPError{StatusCode: 401}, CategoryAuthentication},
		{"http 403 is auth", &soap.HTTPError{StatusCode: 403}, CategoryAuthentication},
		{"cancellation", context.Canceled, CategoryCancelled},
		{"action not supported", soap.ErrActionNotSupported, CategoryProtocol},
		{"deadline is network", context.DeadlineExceeded, CategoryNetwork},
		{"net timeout wrapped", secretURLError(secret, dialTimeoutError{}), CategoryNetwork},
		{"connection refused wrapped", secretURLError(secret, &net.OpError{Op: "dial", Err: errors.New("connection refused")}), CategoryNetwork},
		{"http 500 non-auth protocol", &soap.HTTPError{StatusCode: 500}, CategoryProtocol},
		{"dtd not allowed", soap.ErrDTDNotAllowed, CategoryProtocol},
		{"response too large", &soap.ResponseTooLargeError{Limit: 10, HTTPStatus: 200}, CategoryProtocol},
		{"media1 malformed", media1.ErrMalformedXML, CategoryProtocol},
		{"media2 malformed", media2.ErrMalformedXML, CategoryProtocol},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mapMediaError(tc.err)
			if tc.err == nil {
				if got != nil {
					t.Fatalf("mapMediaError(nil) = %v, want nil", got)
				}
				return
			}
			if CategoryOf(got) != tc.category {
				t.Fatalf("category = %q, want %q", CategoryOf(got), tc.category)
			}
			assertNoMarkerAnywhere(t, got, secret)
		})
	}
}

// assertNoMarkerAnywhere verifies the marker appears in neither the error
// message, any fmt rendering, nor any node of the recursive unwrap chain.
func assertNoMarkerAnywhere(t *testing.T, err error, marker string) {
	t.Helper()
	if marker == "" || err == nil {
		return
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("Error() leaked marker %q: %v", marker, err)
	}
	if strings.Contains(fmt.Sprintf("%v", err), marker) || strings.Contains(fmt.Sprintf("%+v", err), marker) {
		t.Fatalf("fmt rendering leaked marker %q", marker)
	}
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		if strings.Contains(cur.Error(), marker) {
			t.Fatalf("unwrap chain leaked marker %q at %v", marker, cur)
		}
	}
}
