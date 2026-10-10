package app

import (
	"context"
	"sync"
	"testing"
	"time"
)

func timeoutChan() <-chan time.Time {
	return time.After(2 * time.Second)
}

func waitForMediaCall(t *testing.T, m *fakeMedia) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		calls := m.calls
		m.mu.Unlock()
		if calls > 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("media call did not start")
}

func TestNewerScanCancelsPriorScan(t *testing.T) {
	h := newHarness(t)
	block := make(chan struct{})
	started := make(chan struct{})
	h.discovery.block = block
	h.discovery.started = started
	h.discovery.result = DiscoveryResult{}

	priorErr := make(chan error, 1)
	go func() {
		_, err := h.manager.Scan(context.Background(), DiscoveryRequest{})
		priorErr <- err
	}()
	<-started // first scan is inside Discover

	// Reset started for the second scan and unblock all.
	h.discovery.mu.Lock()
	h.discovery.started = nil
	h.discovery.block = nil
	h.discovery.mu.Unlock()

	// A newer scan cancels the prior one's context.
	secondDone := make(chan struct{})
	go func() {
		_, _ = h.manager.Scan(context.Background(), DiscoveryRequest{})
		close(secondDone)
	}()

	select {
	case err := <-priorErr:
		managed := mustManagerError(t, err)
		if managed.Category != CategoryCancelled {
			t.Fatalf("expected prior scan cancelled, got %s", managed.Category)
		}
	case <-timeoutChan():
		t.Fatalf("prior scan was not cancelled by newer scan")
	}
	close(block)
	<-secondDone
}

func TestNewerDeviceOpCancelsPrior(t *testing.T) {
	h := newHarness(t)
	device := seedDevice(t, h, "192.168.20.10")
	h.inventory.proof = func(_ *Credentials) (Inventory, error) { return Inventory{}, nil }
	if _, err := h.manager.AuthenticateDevice(context.Background(), AuthenticateRequest{DeviceID: device.ID, Username: "admin", Password: "pw"}); err != nil {
		t.Fatalf("auth: %v", err)
	}

	block := make(chan struct{})
	h.media.block = block
	firstErr := make(chan error, 1)
	go func() {
		_, err := h.manager.Profiles(context.Background(), device.ID)
		firstErr <- err
	}()
	waitForMediaCall(t, h.media)

	// Second op for the same device cancels the first.
	h.media.mu.Lock()
	h.media.block = nil
	h.media.mu.Unlock()
	secondDone := make(chan struct{})
	go func() {
		_, _ = h.manager.Profiles(context.Background(), device.ID)
		close(secondDone)
	}()

	select {
	case err := <-firstErr:
		managed := mustManagerError(t, err)
		if managed.Category != CategoryCancelled {
			t.Fatalf("expected first op cancelled, got %s", managed.Category)
		}
	case <-timeoutChan():
		t.Fatalf("first device op not cancelled by newer op")
	}
	close(block)
	<-secondDone
}

func TestCallerCancellationHonored(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := h.manager.Overview(ctx)
	managed := mustManagerError(t, err)
	if managed.Category != CategoryCancelled {
		t.Fatalf("expected cancelled, got %s", managed.Category)
	}
}

func TestConcurrentOperationsAreRaceSafe(t *testing.T) {
	h := newHarness(t)
	h.inventory.proof = func(_ *Credentials) (Inventory, error) { return Inventory{}, nil }
	h.discovery.result = DiscoveryResult{Devices: []DiscoveredDevice{
		withEPR("urn:uuid:c1", "http://192.168.30.1/onvif/device_service"),
		withEPR("urn:uuid:c2", "http://192.168.30.2/onvif/device_service"),
	}}
	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			_, _ = h.manager.Scan(context.Background(), DiscoveryRequest{})
			_, _ = h.manager.Overview(context.Background())
			_, _ = h.manager.Bootstrap(context.Background())
			done <- struct{}{}
		}()
	}
	for i := 0; i < 8; i++ {
		select {
		case <-done:
		case <-timeoutChan():
			t.Fatalf("concurrent operations did not complete")
		}
	}
	// Close concurrently with operations to exercise race-safe teardown.
	_ = h.manager.Close()
}

func TestSimultaneousScanRegistrationLeavesExactlyOneActive(t *testing.T) {
	h := newHarness(t)
	const callers = 32
	contexts := make([]context.Context, callers)
	finishes := make([]func(), callers)
	start := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(callers)
	done.Add(callers)
	for index := 0; index < callers; index++ {
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-start
			ctx, finish, err := h.manager.beginScan(context.Background())
			if err != nil {
				t.Errorf("beginScan() error = %v", err)
				return
			}
			contexts[index] = ctx
			finishes[index] = finish
		}(index)
	}
	ready.Wait()
	close(start)
	done.Wait()

	active := 0
	for _, ctx := range contexts {
		if ctx != nil && ctx.Err() == nil {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("active scan contexts = %d, want exactly 1", active)
	}
	for _, finish := range finishes {
		if finish != nil {
			finish()
		}
	}
}

func TestSimultaneousDeviceOperationRegistrationLeavesExactlyOneActive(t *testing.T) {
	h := newHarness(t)
	const callers = 32
	contexts := make([]context.Context, callers)
	finishes := make([]func(), callers)
	start := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(callers)
	done.Add(callers)
	for index := 0; index < callers; index++ {
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-start
			ctx, finish, err := h.manager.beginDeviceOp(context.Background(), "dev_same")
			if err != nil {
				t.Errorf("beginDeviceOp() error = %v", err)
				return
			}
			contexts[index] = ctx
			finishes[index] = finish
		}(index)
	}
	ready.Wait()
	close(start)
	done.Wait()

	active := 0
	for _, ctx := range contexts {
		if ctx != nil && ctx.Err() == nil {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("active device operation contexts = %d, want exactly 1", active)
	}
	for _, finish := range finishes {
		if finish != nil {
			finish()
		}
	}
}
