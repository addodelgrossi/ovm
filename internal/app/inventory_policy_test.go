package app

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// stateByOp returns the capability state recorded for op in inv, or the empty
// state if absent.
func stateByOp(inv Inventory, op OperationName) CapabilityState {
	for _, s := range inv.Operations {
		if s.Operation == op {
			return s.State
		}
	}
	return ""
}

// cancelOnSuccessSeam records a successful DeviceInformation immediately, then
// cancels the parent context and blocks every other operation until released.
// This produces a run where at least one non-clock success is recorded before
// an explicit cancellation is observed, so the test can assert cancellation
// precedence over a success.
type cancelOnSuccessSeam struct {
	cancel  context.CancelFunc
	release chan struct{}
	once    sync.Once
}

func (s *cancelOnSuccessSeam) succeedThenCancel() {
	s.once.Do(func() {
		// Trigger cancellation after this success is about to be recorded.
		s.cancel()
	})
}

func (s *cancelOnSuccessSeam) block() { <-s.release }

func (s *cancelOnSuccessSeam) SystemDateAndTime(ctx context.Context) (device.SystemDateAndTime, error) {
	s.block()
	return device.SystemDateAndTime{}, ctx.Err()
}

func (s *cancelOnSuccessSeam) DeviceInformation(ctx context.Context) (device.DeviceInformation, error) {
	// Succeed first, then request cancellation so the success is already in
	// flight when the parent context is cancelled.
	info := device.DeviceInformation{Manufacturer: "ACME"}
	s.succeedThenCancel()
	return info, nil
}

func (s *cancelOnSuccessSeam) Capabilities(ctx context.Context) (device.Capabilities, error) {
	s.block()
	return device.Capabilities{}, ctx.Err()
}

func (s *cancelOnSuccessSeam) Services(ctx context.Context) ([]device.Service, error) {
	s.block()
	return nil, ctx.Err()
}

func (s *cancelOnSuccessSeam) Scopes(ctx context.Context) ([]device.Scope, error) {
	s.block()
	return nil, ctx.Err()
}

func (s *cancelOnSuccessSeam) Hostname(ctx context.Context) (device.Hostname, error) {
	s.block()
	return device.Hostname{}, ctx.Err()
}

func (s *cancelOnSuccessSeam) NetworkInterfaces(ctx context.Context) ([]device.NetworkInterface, error) {
	s.block()
	return nil, ctx.Err()
}

// --- Finding 2: explicit cancellation always wins -------------------------

// TestExplicitCancellationWinsOverSuccesses proves that an explicit parent
// cancellation after work started maps to CategoryCancelled and returns the
// partial inventory, even though a non-clock operation succeeded. The success
// would otherwise yield a nil error.
func TestExplicitCancellationWinsOverSuccesses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	seam := &cancelOnSuccessSeam{cancel: cancel, release: make(chan struct{})}

	// Run with full concurrency so DeviceInformation can succeed while the
	// others are blocked.
	done := make(chan struct{})
	var inv Inventory
	var err error
	go func() {
		inv, err = runInventory(ctx, "http://host/x", seam, 7)
		close(done)
	}()
	// Give the success a moment to record and trigger cancellation, then
	// release the blocked operations so they observe the cancelled context.
	time.Sleep(30 * time.Millisecond)
	close(seam.release)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runInventory did not return")
	}

	if err == nil {
		t.Fatal("expected a cancellation error even though an operation succeeded")
	}
	if CategoryOf(err) != CategoryCancelled {
		t.Fatalf("category = %s, want cancelled", CategoryOf(err))
	}
	if inv.DeviceInformation == nil || inv.DeviceInformation.Manufacturer != "ACME" {
		t.Fatalf("expected the successful device info preserved, got %+v", inv.DeviceInformation)
	}
}

// TestExplicitCancellationWinsOverCompetingAuth proves cancellation beats a
// competing authentication failure at pre-flight.
func TestExplicitCancellationWinsOverCompetingAuth(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	seam := &stubSeam{systemErr: soap.ErrNotAuthorized, infoErr: soap.ErrNotAuthorized}
	_, err := runInventory(ctx, "http://host/x", seam, 2)
	if CategoryOf(err) != CategoryCancelled {
		t.Fatalf("category = %s, want cancelled", CategoryOf(err))
	}
}

// TestContextAwareSemaphoreReleasesOnCancellation proves that queued goroutines
// do not block on a saturated semaphore once the context is cancelled: the run
// returns promptly and records per-operation states.
func TestContextAwareSemaphoreReleasesOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	seam := &stubSeam{block: make(chan struct{})}
	done := make(chan struct{})
	var inv Inventory
	var err error
	go func() {
		inv, err = runInventory(ctx, "http://host/x", seam, 1)
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	close(seam.block)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runInventory did not return promptly after cancellation")
	}
	if CategoryOf(err) != CategoryCancelled {
		t.Fatalf("category = %s, want cancelled", CategoryOf(err))
	}
	if len(inv.Operations) == 0 {
		t.Fatal("expected per-operation states to be recorded")
	}
}

// --- Finding 3: minimum useful inventory ----------------------------------

// TestClockOnlySuccessWithAllAuthRejectedIsAuthentication proves that a run
// where only the clock succeeded and every non-clock operation was
// authentication-rejected returns the partial clock data together with
// CategoryAuthentication.
func TestClockOnlySuccessWithAllAuthRejectedIsAuthentication(t *testing.T) {
	seam := &stubSeam{
		// clock succeeds (systemErr nil)
		infoErr:  soap.ErrNotAuthorized,
		capsErr:  soap.ErrNotAuthorized,
		servErr:  soap.ErrNotAuthorized,
		scopeErr: soap.ErrNotAuthorized,
		hostErr:  soap.ErrNotAuthorized,
		netErr:   soap.ErrNotAuthorized,
	}
	inv, err := runInventory(context.Background(), "http://host/x", seam, 4)
	if CategoryOf(err) != CategoryAuthentication {
		t.Fatalf("category = %s, want authentication", CategoryOf(err))
	}
	if inv.SystemDateAndTime == nil {
		t.Fatal("expected the clock data preserved alongside the authentication verdict")
	}
	if stateByOp(inv, OpSystemDateAndTime) != CapabilitySupported {
		t.Fatalf("clock state = %s, want supported", stateByOp(inv, OpSystemDateAndTime))
	}
}

// TestOneNonClockSuccessIsUsefulInventory proves that one supported non-clock
// operation makes the inventory useful (nil error).
func TestOneNonClockSuccessIsUsefulInventory(t *testing.T) {
	seam := &stubSeam{
		infoResult: device.DeviceInformation{Manufacturer: "ACME"},
		capsErr:    soap.ErrNotAuthorized,
		servErr:    soap.ErrNotAuthorized,
		scopeErr:   soap.ErrNotAuthorized,
		hostErr:    soap.ErrNotAuthorized,
		netErr:     soap.ErrNotAuthorized,
	}
	inv, err := runInventory(context.Background(), "http://host/x", seam, 4)
	if err != nil {
		t.Fatalf("one non-clock success must be a useful inventory, got %v", err)
	}
	if stateByOp(inv, OpDeviceInformation) != CapabilitySupported {
		t.Fatalf("device info state = %s, want supported", stateByOp(inv, OpDeviceInformation))
	}
}

// TestAllNonClockUnsupportedIsUsefulInventory proves that all six non-clock
// operations reporting unsupported is a valid, responsive inventory.
func TestAllNonClockUnsupportedIsUsefulInventory(t *testing.T) {
	seam := &stubSeam{
		infoErr:  soap.ErrActionNotSupported,
		capsErr:  soap.ErrActionNotSupported,
		servErr:  soap.ErrActionNotSupported,
		scopeErr: soap.ErrActionNotSupported,
		hostErr:  soap.ErrActionNotSupported,
		netErr:   soap.ErrActionNotSupported,
	}
	_, err := runInventory(context.Background(), "http://host/x", seam, 4)
	if err != nil {
		t.Fatalf("all-non-clock-unsupported must be a useful inventory, got %v", err)
	}
}

// TestMixedNonClockFailuresNotMisclassifiedAsAuthentication proves that a mix
// of authentication, transient (deadline), and unsupported failures among the
// non-clock operations is NOT classified as authentication. A single auth
// rejection must not mask the competing transient transport faults: because a
// deadline is a network condition and network outranks a partial auth
// rejection in the dominant-failure precedence, the verdict is CategoryNetwork.
// Per-operation states remain accurate (an unsupported op stays unsupported,
// not unknown).
func TestMixedNonClockFailuresNotMisclassifiedAsAuthentication(t *testing.T) {
	seam := &stubSeam{
		infoErr:  soap.ErrNotAuthorized,      // auth (one call)
		capsErr:  context.DeadlineExceeded,   // transient -> network
		servErr:  soap.ErrActionNotSupported, // unsupported -> contributes nothing
		scopeErr: context.DeadlineExceeded,   // transient -> network
		hostErr:  context.DeadlineExceeded,   // transient -> network
		netErr:   context.DeadlineExceeded,   // transient -> network
	}
	inv, err := runInventory(context.Background(), "http://host/x", seam, 4)
	if err == nil {
		t.Fatal("expected a fatal error when no non-clock operation succeeds")
	}
	// The mixture is not authentication: the competing transient (deadline ->
	// network) failures outrank the single auth rejection, so the dominant
	// category is network, not authentication.
	if CategoryOf(err) != CategoryNetwork {
		t.Fatalf("category = %s, want network (mixed auth+deadline+unsupported must not be authentication)", CategoryOf(err))
	}
	// The unsupported op keeps its unsupported state (per-op accuracy).
	if got := stateByOp(inv, OpServices); got != CapabilityUnsupported {
		t.Fatalf("services state = %s, want unsupported", got)
	}
	// A transient op is temporarily_unavailable, not unknown.
	if got := stateByOp(inv, OpCapabilities); got != CapabilityTemporarilyUnavailable {
		t.Fatalf("capabilities state = %s, want temporarily_unavailable", got)
	}
}
