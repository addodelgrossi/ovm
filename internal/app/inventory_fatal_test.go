package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// connRefused builds an error shaped like a real "connection refused" from the
// transport: a *net.OpError wrapping ECONNREFUSED, matching what the Go net
// stack returns for a failed TCP dial.
func connRefused() error {
	return &net.OpError{
		Op:   "dial",
		Net:  "tcp",
		Addr: &net.TCPAddr{IP: net.IPv4(10, 0, 0, 2), Port: 80},
		Err:  syscall.ECONNREFUSED,
	}
}

// timeoutErr is a net.Error reporting a timeout, used to exercise the network
// classification path without a real socket.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestNetworkClassificationCoversTimeoutAndRefused(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"timeout", timeoutErr{}},
		{"connection_refused", connRefused()},
		{"dns", &net.DNSError{Err: "no such host", Name: "device.invalid"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !isNetworkError(tc.err) {
				t.Fatalf("%s should be a network error", tc.name)
			}
			if categorizeFatal(tc.err) != CategoryNetwork {
				t.Fatalf("categorizeFatal(%s) = %s, want network", tc.name, categorizeFatal(tc.err))
			}
		})
	}
}

func TestRunInventoryAllNetworkPromotesNetwork(t *testing.T) {
	refused := connRefused()
	seam := &stubSeam{
		systemErr: refused,
		infoErr:   refused,
		capsErr:   refused,
		servErr:   refused,
		scopeErr:  refused,
		hostErr:   refused,
		netErr:    refused,
	}
	inv, err := runInventory(context.Background(), "http://device.invalid/onvif/device_service", seam, 4)
	if err == nil {
		t.Fatal("expected a dominant network error when every operation fails on the network")
	}
	if CategoryOf(err) != CategoryNetwork {
		t.Fatalf("category = %s, want network", CategoryOf(err))
	}
	// The state-only inventory is still returned.
	if len(inv.Operations) != len(orderedOperations) {
		t.Fatalf("expected a populated state-only inventory, got %d operations", len(inv.Operations))
	}
	for _, status := range inv.Operations {
		if status.State != CapabilityTemporarilyUnavailable {
			t.Fatalf("operation %s = %s, want temporarily_unavailable", status.Operation, status.State)
		}
	}
}

func TestConnectionRefusedClassifiesAsNetworkNotUnknown(t *testing.T) {
	// Regression: a bare "connection refused" must be classified as a network
	// condition, never as an unknown/ambiguous outcome.
	refused := connRefused()
	if !isNetworkError(refused) {
		t.Fatal("connection refused should be a network error")
	}
	if categorizeFatal(refused) != CategoryNetwork {
		t.Fatalf("categorizeFatal(connection refused) = %s, want network", categorizeFatal(refused))
	}
	state, _ := classifyState(refused)
	if state != CapabilityTemporarilyUnavailable {
		t.Fatalf("classifyState(connection refused) = %s, want temporarily_unavailable", state)
	}
}

func TestRunInventoryAllMalformedPromotesProtocol(t *testing.T) {
	seam := &stubSeam{
		systemErr: device.ErrMalformedXML,
		infoErr:   device.ErrUnexpectedElement,
		capsErr:   device.ErrMissingField,
		servErr:   device.ErrInvalidField,
		scopeErr:  &soap.ParseError{Err: errors.New("boom")},
		hostErr:   device.ErrMalformedXML,
		netErr:    device.ErrMalformedXML,
	}
	err := mustFail(t, seam)
	if CategoryOf(err) != CategoryProtocol {
		t.Fatalf("category = %s, want protocol", CategoryOf(err))
	}
}

func TestRunInventoryAllAuthenticationPromotesAuthentication(t *testing.T) {
	// A mix of SOAP NotAuthorized and bare HTTP 401/403 all map to the
	// authentication category, which dominates when nothing succeeds.
	seam := &stubSeam{
		systemErr: soap.ErrNotAuthorized,
		infoErr:   fmt.Errorf("http: %w", &soap.HTTPError{StatusCode: 401}),
		capsErr:   fmt.Errorf("http: %w", &soap.HTTPError{StatusCode: 403}),
		servErr:   soap.ErrNotAuthorized,
		scopeErr:  soap.ErrNotAuthorized,
		hostErr:   soap.ErrNotAuthorized,
		netErr:    soap.ErrNotAuthorized,
	}
	err := mustFail(t, seam)
	if CategoryOf(err) != CategoryAuthentication {
		t.Fatalf("category = %s, want authentication", CategoryOf(err))
	}
}

func TestHTTP401And403ClassifyAsAuthentication(t *testing.T) {
	for _, status := range []int{401, 403} {
		httpErr := fmt.Errorf("wrap: %w", &soap.HTTPError{StatusCode: status})
		if !isAuthenticationError(httpErr) {
			t.Fatalf("HTTP %d should be an authentication error", status)
		}
		if categorizeFatal(httpErr) != CategoryAuthentication {
			t.Fatalf("categorizeFatal(HTTP %d) = %s, want authentication", status, categorizeFatal(httpErr))
		}
	}
	// A non-auth HTTP status stays protocol.
	httpErr := fmt.Errorf("wrap: %w", &soap.HTTPError{StatusCode: 500})
	if isAuthenticationError(httpErr) {
		t.Fatal("HTTP 500 must not be an authentication error")
	}
	if categorizeFatal(httpErr) != CategoryProtocol {
		t.Fatalf("categorizeFatal(HTTP 500) = %s, want protocol", categorizeFatal(httpErr))
	}
}

func TestRunInventoryAllCancelledPromotesCancelled(t *testing.T) {
	// Every operation observes context.Canceled after scheduling (not before
	// start), so the per-op state is temporarily_unavailable but the dominant
	// fatal category is cancellation.
	seam := &stubSeam{
		systemErr: context.Canceled,
		infoErr:   context.Canceled,
		capsErr:   context.Canceled,
		servErr:   context.Canceled,
		scopeErr:  context.Canceled,
		hostErr:   context.Canceled,
		netErr:    context.Canceled,
	}
	err := mustFail(t, seam)
	if CategoryOf(err) != CategoryCancelled {
		t.Fatalf("category = %s, want cancelled", CategoryOf(err))
	}
}

func TestRunInventoryAllUnsupportedRemainsValidInventory(t *testing.T) {
	// A device that answers every request with ActionNotSupported is a valid,
	// responsive inventory: no error, every operation marked unsupported.
	seam := &stubSeam{
		systemErr: soap.ErrActionNotSupported,
		infoErr:   soap.ErrActionNotSupported,
		capsErr:   soap.ErrActionNotSupported,
		servErr:   soap.ErrActionNotSupported,
		scopeErr:  soap.ErrActionNotSupported,
		hostErr:   soap.ErrActionNotSupported,
		netErr:    soap.ErrActionNotSupported,
	}
	inv, err := runInventory(context.Background(), "http://host/x", seam, 4)
	if err != nil {
		t.Fatalf("all-unsupported must be a valid inventory, got error = %v", err)
	}
	for _, status := range inv.Operations {
		if status.State != CapabilityUnsupported {
			t.Fatalf("operation %s = %s, want unsupported", status.Operation, status.State)
		}
	}
}

func TestRunInventoryPartialOneSuccessIsNotFatal(t *testing.T) {
	// Six failures and one success: the result is a useful partial inventory
	// with a nil error, so the command exits 0.
	refused := connRefused()
	seam := &stubSeam{
		systemErr:  refused,
		infoResult: device.DeviceInformation{Manufacturer: "ACME"},
		capsErr:    refused,
		servErr:    refused,
		scopeErr:   refused,
		hostErr:    refused,
		netErr:     refused,
	}
	inv, err := runInventory(context.Background(), "http://host/x", seam, 4)
	if err != nil {
		t.Fatalf("one success must yield a nil error, got %v", err)
	}
	if inv.DeviceInformation == nil || inv.DeviceInformation.Manufacturer != "ACME" {
		t.Fatalf("expected the one successful payload to be present, got %+v", inv.DeviceInformation)
	}
	if stateOf(inv, OpDeviceInformation) != CapabilitySupported {
		t.Fatalf("device information should be supported, got %s", stateOf(inv, OpDeviceInformation))
	}
}

func TestDominantFailurePrecedenceCancelledWinsOverCompetingCategories(t *testing.T) {
	// Authentication no longer wins by default. When several categories compete
	// and nothing succeeds, the fixed precedence is
	// cancelled > protocol > network > auth > internal, so a per-operation
	// cancellation outranks the competing auth, protocol, network, and internal
	// failures below it.
	seam := &stubSeam{
		systemErr: soap.ErrNotAuthorized,  // authentication
		infoErr:   device.ErrMalformedXML, // protocol
		capsErr:   connRefused(),          // network
		servErr:   context.Canceled,       // cancelled (highest precedence)
		scopeErr:  errors.New("mystery"),  // internal
		hostErr:   device.ErrMalformedXML, // protocol
		netErr:    connRefused(),          // network
	}
	err := mustFail(t, seam)
	if CategoryOf(err) != CategoryCancelled {
		t.Fatalf("category = %s, want cancelled (highest precedence)", CategoryOf(err))
	}
}

func TestDominantFailureProtocolWinsOverNetworkAndAuth(t *testing.T) {
	// With no cancellation present, a competing protocol fault outranks network
	// and a partial authentication rejection. A single auth rejection must not
	// mask a device whose other answers were malformed.
	seam := &stubSeam{
		systemErr: soap.ErrNotAuthorized,  // authentication
		infoErr:   device.ErrMalformedXML, // protocol (highest remaining)
		capsErr:   connRefused(),          // network
		servErr:   connRefused(),          // network
		scopeErr:  connRefused(),          // network
		hostErr:   connRefused(),          // network
		netErr:    connRefused(),          // network
	}
	err := mustFail(t, seam)
	if CategoryOf(err) != CategoryProtocol {
		t.Fatalf("category = %s, want protocol (outranks network and partial auth)", CategoryOf(err))
	}
}

func TestDominantFailureNetworkWinsOverPartialAuth(t *testing.T) {
	// A single authentication rejection competing only with network failures
	// resolves to network: the transport fault is the authoritative, actionable
	// cause and auth is not promoted over it.
	seam := &stubSeam{
		systemErr: soap.ErrNotAuthorized, // authentication (one call)
		infoErr:   connRefused(),         // network
		capsErr:   connRefused(),         // network
		servErr:   connRefused(),         // network
		scopeErr:  connRefused(),         // network
		hostErr:   connRefused(),         // network
		netErr:    connRefused(),         // network
	}
	err := mustFail(t, seam)
	if CategoryOf(err) != CategoryNetwork {
		t.Fatalf("category = %s, want network (outranks a partial auth rejection)", CategoryOf(err))
	}
}

func TestDominantFailureAllNonClockAuthRejectedIsAuthentication(t *testing.T) {
	// When every credential-protected (non-clock) operation is authentication-
	// rejected, the verdict is authentication even if the clock call failed for
	// another reason. This is the "all six protected calls reject auth" case.
	seam := &stubSeam{
		systemErr: connRefused(), // clock call: network, not protected
		infoErr:   soap.ErrNotAuthorized,
		capsErr:   soap.ErrNotAuthorized,
		servErr:   soap.ErrNotAuthorized,
		scopeErr:  soap.ErrNotAuthorized,
		hostErr:   soap.ErrNotAuthorized,
		netErr:    soap.ErrNotAuthorized,
	}
	err := mustFail(t, seam)
	if CategoryOf(err) != CategoryAuthentication {
		t.Fatalf("category = %s, want authentication (all protected calls reject auth)", CategoryOf(err))
	}
}

func TestDominantFailureAuthOnlyWithUnsupportedCoexistingIsAuthentication(t *testing.T) {
	// Authentication is the only unresolved failure category; an unsupported
	// action contributes no category and may coexist without retyping the
	// verdict away from authentication.
	seam := &stubSeam{
		systemErr: soap.ErrNotAuthorized,
		infoErr:   soap.ErrNotAuthorized,
		capsErr:   soap.ErrActionNotSupported, // unsupported: contributes nothing
		servErr:   soap.ErrNotAuthorized,
		scopeErr:  soap.ErrNotAuthorized,
		hostErr:   soap.ErrNotAuthorized,
		netErr:    soap.ErrNotAuthorized,
	}
	err := mustFail(t, seam)
	if CategoryOf(err) != CategoryAuthentication {
		t.Fatalf("category = %s, want authentication (auth-only with unsupported coexisting)", CategoryOf(err))
	}
}

// mustFail runs the inventory and asserts a non-nil error, returning it.
func mustFail(t *testing.T, seam deviceSeam) error {
	t.Helper()
	_, err := runInventory(context.Background(), "http://host/x", seam, 4)
	if err == nil {
		t.Fatal("expected a dominant fatal error when no operation succeeds")
	}
	return err
}
