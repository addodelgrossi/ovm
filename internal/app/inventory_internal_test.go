package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// stubSeam is a deterministic deviceSeam double. Each field, when non-nil,
// overrides the default success for its operation; a nil field returns a zero
// value with no error.
type stubSeam struct {
	systemErr  error
	infoErr    error
	capsErr    error
	servErr    error
	scopeErr   error
	hostErr    error
	netErr     error
	infoResult device.DeviceInformation

	// started records the sequence in which calls begin, and inflight tracks
	// the peak observed concurrency, so tests can assert bounded concurrency.
	mu       sync.Mutex
	started  []OperationName
	inflight int32
	peak     int32
	block    chan struct{}
}

func (s *stubSeam) enter(op OperationName) {
	s.mu.Lock()
	s.started = append(s.started, op)
	s.mu.Unlock()
	current := atomic.AddInt32(&s.inflight, 1)
	for {
		peak := atomic.LoadInt32(&s.peak)
		if current <= peak || atomic.CompareAndSwapInt32(&s.peak, peak, current) {
			break
		}
	}
	if s.block != nil {
		<-s.block
	}
	atomic.AddInt32(&s.inflight, -1)
}

func (s *stubSeam) SystemDateAndTime(ctx context.Context) (device.SystemDateAndTime, error) {
	s.enter(OpSystemDateAndTime)
	if s.systemErr != nil {
		return device.SystemDateAndTime{}, s.systemErr
	}
	return device.SystemDateAndTime{DateTimeType: "Manual", UTC: time.Unix(0, 0).UTC(), UTCValid: true}, nil
}

func (s *stubSeam) DeviceInformation(ctx context.Context) (device.DeviceInformation, error) {
	s.enter(OpDeviceInformation)
	if s.infoErr != nil {
		return device.DeviceInformation{}, s.infoErr
	}
	return s.infoResult, nil
}

func (s *stubSeam) Capabilities(ctx context.Context) (device.Capabilities, error) {
	s.enter(OpCapabilities)
	return device.Capabilities{}, s.capsErr
}

func (s *stubSeam) Services(ctx context.Context) ([]device.Service, error) {
	s.enter(OpServices)
	return nil, s.servErr
}

func (s *stubSeam) Scopes(ctx context.Context) ([]device.Scope, error) {
	s.enter(OpScopes)
	return nil, s.scopeErr
}

func (s *stubSeam) Hostname(ctx context.Context) (device.Hostname, error) {
	s.enter(OpHostname)
	return device.Hostname{}, s.hostErr
}

func (s *stubSeam) NetworkInterfaces(ctx context.Context) ([]device.NetworkInterface, error) {
	s.enter(OpNetworkInterfaces)
	return nil, s.netErr
}

func stateOf(inv Inventory, op OperationName) CapabilityState {
	for _, status := range inv.Operations {
		if status.Operation == op {
			return status.State
		}
	}
	return ""
}

func TestRunInventoryStateMapping(t *testing.T) {
	seam := &stubSeam{
		systemErr: soap.ErrActionNotSupported,
		infoErr:   soap.ErrNotAuthorized,
		capsErr:   device.ErrMalformedXML,
		servErr:   context.DeadlineExceeded,
		scopeErr:  context.Canceled,
		hostErr:   fmt.Errorf("wrap: %w", &soap.HTTPError{StatusCode: 500}),
		// netErr nil -> supported
	}
	inv, err := runInventory(context.Background(), "http://device.invalid/onvif/device_service", seam, 4)
	if err != nil {
		t.Fatalf("runInventory error = %v", err)
	}

	cases := map[OperationName]CapabilityState{
		OpSystemDateAndTime: CapabilityUnsupported,
		OpDeviceInformation: CapabilityUnknown,
		OpCapabilities:      CapabilityUnknown,
		OpServices:          CapabilityTemporarilyUnavailable,
		OpScopes:            CapabilityTemporarilyUnavailable,
		OpHostname:          CapabilityUnknown, // HTTP 500 is protocol ambiguity
		OpNetworkInterfaces: CapabilitySupported,
	}
	for op, want := range cases {
		if got := stateOf(inv, op); got != want {
			t.Errorf("operation %s state = %s, want %s", op, got, want)
		}
	}
}

func TestRunInventoryPartialResultsAndOrder(t *testing.T) {
	seam := &stubSeam{
		infoResult: device.DeviceInformation{Manufacturer: "ACME", Model: "X"},
		capsErr:    soap.ErrNotAuthorized,
	}
	inv, err := runInventory(context.Background(), "http://host/onvif/device_service", seam, 4)
	if err != nil {
		t.Fatalf("runInventory error = %v", err)
	}

	// Partial result: device information populated despite capabilities failing.
	if inv.DeviceInformation == nil || inv.DeviceInformation.Manufacturer != "ACME" {
		t.Fatalf("expected device information to be present, got %+v", inv.DeviceInformation)
	}
	if inv.Capabilities != nil {
		t.Fatalf("capabilities should be nil on failure, got %+v", inv.Capabilities)
	}

	// Deterministic order: Operations always follows orderedOperations.
	if len(inv.Operations) != len(orderedOperations) {
		t.Fatalf("expected %d operations, got %d", len(orderedOperations), len(inv.Operations))
	}
	for i, op := range orderedOperations {
		if inv.Operations[i].Operation != op {
			t.Fatalf("operation %d = %s, want %s", i, inv.Operations[i].Operation, op)
		}
	}
}

func TestRunInventoryBoundedConcurrency(t *testing.T) {
	seam := &stubSeam{block: make(chan struct{})}
	done := make(chan struct{})
	go func() {
		_, _ = runInventory(context.Background(), "http://host/x", seam, 2)
		close(done)
	}()

	// Give the goroutines time to saturate the semaphore, then release them.
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&seam.peak) < 2 {
		select {
		case <-deadline:
			t.Fatalf("concurrency never reached the limit; peak = %d", atomic.LoadInt32(&seam.peak))
		case <-time.After(5 * time.Millisecond):
		}
	}
	close(seam.block)
	<-done

	if peak := atomic.LoadInt32(&seam.peak); peak > 2 {
		t.Fatalf("peak concurrency = %d, want <= 2", peak)
	}
}

func TestRunInventoryCancellationBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	seam := &stubSeam{}
	_, err := runInventory(ctx, "http://host/x", seam, 4)
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if CategoryOf(err) != CategoryCancelled {
		t.Fatalf("category = %s, want %s", CategoryOf(err), CategoryCancelled)
	}
}

func TestClassifyStateTable(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		state CapabilityState
	}{
		{"action_not_supported", soap.ErrActionNotSupported, CapabilityUnsupported},
		{"not_authorized", soap.ErrNotAuthorized, CapabilityUnknown},
		{"deadline", context.DeadlineExceeded, CapabilityTemporarilyUnavailable},
		{"canceled", context.Canceled, CapabilityTemporarilyUnavailable},
		{"malformed", device.ErrMalformedXML, CapabilityUnknown},
		{"unexpected", device.ErrUnexpectedElement, CapabilityUnknown},
		{"version_mismatch", soap.ErrVersionMismatch, CapabilityUnknown},
		{"generic", errors.New("boom"), CapabilityUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state, detail := classifyState(tc.err)
			if state != tc.state {
				t.Fatalf("state = %s, want %s", state, tc.state)
			}
			if detail == "" {
				t.Fatal("detail must be non-empty for a non-supported state")
			}
		})
	}
}

// errorContainsSecret is a redaction guard used by the credential tests.
func errorContainsSecret(err error, secrets ...string) bool {
	if err == nil {
		return false
	}
	text := err.Error()
	for _, secret := range secrets {
		if secret != "" && strings.Contains(text, secret) {
			return true
		}
	}
	return false
}

func TestApplicationErrorRedaction(t *testing.T) {
	const secret = "sup3r-s3cret-pw"
	// A typed error built with a credential-free message must never surface the
	// secret even if a lower-layer cause (hypothetically) contained it.
	cause := fmt.Errorf("transport failed for %s", secret)
	appErr := newError(CategoryNetwork, "a transient network error prevented a result", cause)
	if errorContainsSecret(appErr, secret) {
		t.Fatalf("application error message leaked secret: %q", appErr.Error())
	}
	// The cause remains available through unwrapping for internal diagnostics,
	// but Error() (what users see) stays clean.
	if !errors.Is(appErr, cause) {
		t.Fatal("expected the cause to remain unwrappable")
	}
}
