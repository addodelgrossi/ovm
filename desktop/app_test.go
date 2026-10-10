package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/store"
)

func newTestApp(t *testing.T, manager Manager, configure func(*AppDependencies)) *App {
	t.Helper()
	dependencies := AppDependencies{
		Manager:         manager,
		VLC:             &fakeVLCLauncher{},
		CallTimeout:     time.Second,
		ShutdownTimeout: time.Second,
	}
	if configure != nil {
		configure(&dependencies)
	}
	host, err := NewApp(dependencies)
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = host.close(ctx)
	})
	return host
}

func TestNewAppRequiresExplicitManager(t *testing.T) {
	if _, err := NewApp(AppDependencies{}); err == nil {
		t.Fatal("NewApp() accepted a missing Manager")
	}
}

func TestBootstrapEnvelopeIncludesCapabilities(t *testing.T) {
	manager := newFakeManager()
	manager.bootstrapFn = func(context.Context) (core.Overview, error) {
		return core.Overview{Locale: "en", Theme: "system", Devices: []core.DeviceDTO{}}, nil
	}
	launcher := &fakeVLCLauncher{available: true}
	host := newTestApp(t, manager, func(dependencies *AppDependencies) {
		dependencies.VLC = launcher
	})

	response := host.Bootstrap()
	if response.Error != nil || response.Result == nil {
		t.Fatalf("Bootstrap() response = %+v", response)
	}
	if response.Result.Capabilities.Demo || !response.Result.Capabilities.VLCAvailable || !response.Result.Capabilities.RTSPPlaybackAvailable {
		t.Fatalf("capabilities = %+v", response.Result.Capabilities)
	}
	if response.Result.Overview.Locale != "en" || manager.callCount("bootstrap") != 1 {
		t.Fatalf("bootstrap result/calls = %+v/%d", response.Result.Overview, manager.callCount("bootstrap"))
	}
}

func TestBindingErrorPreservesSafeManagerClassification(t *testing.T) {
	manager := newFakeManager()
	manager.overviewFn = func(context.Context) (core.Overview, error) {
		return core.Overview{}, &core.ManagerError{
			Category:  core.CategoryInternal,
			Message:   "the stored metadata changed concurrently",
			StoreCode: store.CodeConflict,
		}
	}
	host := newTestApp(t, manager, nil)

	response := host.LoadOverview()
	if response.Result != nil || response.Error == nil {
		t.Fatalf("LoadOverview() response = %+v", response)
	}
	if response.Error.Category != core.CategoryInternal || response.Error.Code != "store_conflict" || response.Error.Message != "the stored metadata changed concurrently" {
		t.Fatalf("binding error = %+v", response.Error)
	}
}

func TestBoundaryValidationRejectsBeforeManagerWork(t *testing.T) {
	manager := newFakeManager()
	host := newTestApp(t, manager, nil)
	tooLong := strings.Repeat("x", maxBindingCredentialBytes+1)
	invalidID := "bad id"
	badLocale := "fr"

	responses := []*BindingError{
		host.Scan(ScanRequest{TimeoutMilliseconds: 1}).Error,
		host.Scan(ScanRequest{InterfaceName: " bad"}).Error,
		host.AddManualDevice(AddManualDeviceRequest{Endpoint: "http://camera/onvif/device_service?token=value"}).Error,
		host.AuthenticateDevice(AuthenticateDeviceRequest{DeviceID: "dev_valid", Username: "operator", Password: tooLong}).Error,
		host.AuthenticateDevice(AuthenticateDeviceRequest{DeviceID: "dev_valid", Username: "operator", AllowBasicOverHTTP: true}).Error,
		host.ForgetDevice(DeviceIDRequest{DeviceID: invalidID}).Error,
		host.SelectDevice(DeviceIDRequest{DeviceID: invalidID}).Error,
		host.LoadProfiles(LoadProfilesRequest{DeviceID: invalidID}).Error,
		host.UpdatePreferences(UpdatePreferencesRequest{Locale: &badLocale}).Error,
		host.OpenInVLC(OpenInVLCRequest{DeviceID: "dev_valid", ProfileToken: "profile", Transport: "sctp"}).Error,
	}
	for index, bindingErr := range responses {
		if bindingErr == nil || bindingErr.Category != core.CategoryUsage || bindingErr.Code == "" || bindingErr.Message == "" {
			t.Fatalf("validation response %d = %+v", index, bindingErr)
		}
	}
	if calls := manager.totalCallsExceptClose(); calls != 0 {
		t.Fatalf("invalid requests reached Manager %d times", calls)
	}
}

func TestPasswordNeverAppearsInResponseOrRawError(t *testing.T) {
	const marker = "binding-password-marker-6f817e"
	manager := newFakeManager()
	manager.authFn = func(_ context.Context, request core.AuthenticateRequest) (core.AuthenticateResult, error) {
		if request.Password != marker {
			t.Errorf("Manager received an altered password")
		}
		// Even an incorrectly implemented injected dependency cannot reflect its
		// raw error through the binding mapper.
		return core.AuthenticateResult{}, errors.New(marker)
	}
	host := newTestApp(t, manager, nil)

	response := host.AuthenticateDevice(AuthenticateDeviceRequest{
		DeviceID: "dev_valid",
		Username: "operator",
		Password: marker,
	})
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal(response): %v", err)
	}
	lower := strings.ToLower(string(encoded))
	if strings.Contains(string(encoded), marker) || strings.Contains(lower, "password") || strings.Contains(lower, "secret") {
		t.Fatalf("authentication response reflected sensitive material: %s", encoded)
	}
	if response.Error == nil || response.Error.Code != "internal_error" || response.Error.Message != "an internal error occurred" {
		t.Fatalf("unexpected safe error = %+v", response.Error)
	}
}

func TestCancelScanCancelsActiveBindingCall(t *testing.T) {
	manager := newFakeManager()
	started := make(chan struct{})
	manager.scanFn = func(ctx context.Context, _ core.DiscoveryRequest) (core.ScanResult, error) {
		close(started)
		<-ctx.Done()
		return core.ScanResult{}, core.NewError(core.CategoryCancelled, "discovery was cancelled")
	}
	host := newTestApp(t, manager, nil)

	done := make(chan ScanResponse, 1)
	go func() {
		done <- host.Scan(ScanRequest{TimeoutMilliseconds: 1000})
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("Scan() did not start")
	}
	cancelled := host.CancelScan()
	if cancelled.Error != nil || cancelled.Result == nil || !cancelled.Result.Cancelled {
		t.Fatalf("CancelScan() response = %+v", cancelled)
	}
	select {
	case response := <-done:
		if response.Error == nil || response.Error.Category != core.CategoryCancelled {
			t.Fatalf("Scan() cancellation response = %+v", response)
		}
	case <-time.After(time.Second):
		t.Fatal("Scan() did not stop after CancelScan()")
	}
}

func TestExportedCallTimeoutIsBounded(t *testing.T) {
	manager := newFakeManager()
	manager.overviewFn = func(ctx context.Context) (core.Overview, error) {
		<-ctx.Done()
		return core.Overview{}, ctx.Err()
	}
	host := newTestApp(t, manager, func(dependencies *AppDependencies) {
		dependencies.CallTimeout = 25 * time.Millisecond
	})

	started := time.Now()
	response := host.LoadOverview()
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("LoadOverview() remained blocked for %s", elapsed)
	}
	if response.Error == nil || response.Error.Category != core.CategoryNetwork || response.Error.Code != "operation_timeout" {
		t.Fatalf("timeout response = %+v", response)
	}
}

func TestShutdownWaitsForCallsThenClosesDemoAndClearsReferences(t *testing.T) {
	manager := newFakeManager()
	started := make(chan struct{})
	exited := make(chan struct{})
	manager.profilesFn = func(ctx context.Context, _ string) (core.MediaProfilesResult, error) {
		close(started)
		<-ctx.Done()
		close(exited)
		return core.MediaProfilesResult{}, ctx.Err()
	}
	closer := &fakeLifecycleCloser{}
	closer.closeFn = func(context.Context) error {
		select {
		case <-exited:
			return nil
		default:
			return errors.New("demo closed before the active call exited")
		}
	}
	host := newTestApp(t, manager, func(dependencies *AppDependencies) {
		dependencies.Demo = true
		dependencies.DemoCloser = closer
	})

	callDone := make(chan LoadProfilesResponse, 1)
	go func() { callDone <- host.LoadProfiles(LoadProfilesRequest{DeviceID: "dev_valid"}) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("LoadProfiles() did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := host.close(ctx); err != nil {
		t.Fatalf("close() error = %v", err)
	}
	<-callDone
	if manager.callCount("close") != 1 || closer.callCount() != 1 {
		t.Fatalf("manager/demo close calls = %d/%d", manager.callCount("close"), closer.callCount())
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if host.manager != nil || host.demoCloser != nil || host.vlc != nil || host.rootCtx != nil || host.cancel != nil {
		t.Fatal("shutdown retained manager, demo, launcher, or context references")
	}
}

func TestConcurrentCallsAndShutdownAreRaceSafe(t *testing.T) {
	manager := newFakeManager()
	manager.overviewFn = func(ctx context.Context) (core.Overview, error) {
		select {
		case <-ctx.Done():
			return core.Overview{}, ctx.Err()
		case <-time.After(time.Millisecond):
			return core.Overview{}, nil
		}
	}
	host := newTestApp(t, manager, nil)

	const callers = 48
	var wait sync.WaitGroup
	wait.Add(callers)
	for index := 0; index < callers; index++ {
		go func() {
			defer wait.Done()
			_ = host.LoadOverview()
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	if err := host.close(ctx); err != nil {
		t.Fatalf("close() error = %v", err)
	}
	cancel()
	wait.Wait()

	var closeWait sync.WaitGroup
	closeWait.Add(8)
	for index := 0; index < 8; index++ {
		go func() {
			defer closeWait.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if err := host.close(ctx); err != nil {
				t.Errorf("repeated close() error = %v", err)
			}
		}()
	}
	closeWait.Wait()
	if manager.callCount("close") != 1 {
		t.Fatalf("Manager.Close() calls = %d, want 1", manager.callCount("close"))
	}
}

func TestProbeStreamValidatesAndReturnsPartialResults(t *testing.T) {
	manager := newFakeManager()
	var gotDuration time.Duration
	manager.probeFn = func(_ context.Context, selection core.StreamSelection, duration time.Duration) (core.ProbeResult, error) {
		gotDuration = duration
		return core.ProbeResult{ProfileToken: selection.ProfileToken, Frames: 5}, nil
	}
	app, err := NewApp(AppDependencies{Manager: manager})
	if err != nil {
		t.Fatalf("NewApp() error = %v", err)
	}
	defer app.shutdown(context.Background())
	response := app.ProbeStream(ProbeStreamRequest{DeviceID: "device-one", ProfileToken: "profile_main", Transport: "tcp", DurationSeconds: 30})
	if response.Error != nil || response.Result.Frames != 5 || gotDuration != 30*time.Second {
		t.Fatalf("response = %+v, duration = %v", response, gotDuration)
	}
	for _, request := range []ProbeStreamRequest{
		{DeviceID: "device-one", ProfileToken: "profile_main", Transport: "tcp", DurationSeconds: 0},
		{DeviceID: "device-one", ProfileToken: "profile_main", Transport: "tcp", DurationSeconds: 46},
		{DeviceID: "device-one", ProfileToken: "profile_main", Transport: "sctp", DurationSeconds: 30},
	} {
		if response := app.ProbeStream(request); response.Error == nil || response.Error.Category != core.CategoryUsage {
			t.Errorf("ProbeStream(%+v) = %+v, want usage error", request, response)
		}
	}
}
