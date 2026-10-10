package main

import (
	"context"
	"sync"

	core "github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/store"
)

type fakeManager struct {
	mu    sync.Mutex
	calls map[string]int

	bootstrapFn   func(context.Context) (core.Overview, error)
	interfacesFn  func(context.Context, core.HostInterfaceRequest) (core.HostInterfacesResult, error)
	scanFn        func(context.Context, core.DiscoveryRequest) (core.ScanResult, error)
	addFn         func(context.Context, core.ManualDeviceRequest) (core.DeviceDTO, error)
	authFn        func(context.Context, core.AuthenticateRequest) (core.AuthenticateResult, error)
	forgetFn      func(context.Context, string) (core.Overview, error)
	selectFn      func(context.Context, string) (core.Overview, error)
	overviewFn    func(context.Context) (core.Overview, error)
	profilesFn    func(context.Context, string) (core.MediaProfilesResult, error)
	resolveFn     func(context.Context, core.StreamSelection) (string, error)
	preferencesFn func(context.Context, core.PreferencesRequest) (core.Overview, error)
	closeFn       func() error
}

func newFakeManager() *fakeManager {
	return &fakeManager{calls: make(map[string]int)}
}

func (f *fakeManager) called(name string) {
	f.mu.Lock()
	f.calls[name]++
	f.mu.Unlock()
}

func (f *fakeManager) callCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

func (f *fakeManager) totalCallsExceptClose() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	total := 0
	for name, count := range f.calls {
		if name != "close" {
			total += count
		}
	}
	return total
}

func (f *fakeManager) Bootstrap(ctx context.Context) (core.Overview, error) {
	f.called("bootstrap")
	if f.bootstrapFn != nil {
		return f.bootstrapFn(ctx)
	}
	return core.Overview{}, nil
}

func (f *fakeManager) ListHostInterfaces(ctx context.Context, request core.HostInterfaceRequest) (core.HostInterfacesResult, error) {
	f.called("interfaces")
	if f.interfacesFn != nil {
		return f.interfacesFn(ctx, request)
	}
	return core.HostInterfacesResult{}, nil
}

func (f *fakeManager) Scan(ctx context.Context, request core.DiscoveryRequest) (core.ScanResult, error) {
	f.called("scan")
	if f.scanFn != nil {
		return f.scanFn(ctx, request)
	}
	return core.ScanResult{}, nil
}

func (f *fakeManager) AddManualDevice(ctx context.Context, request core.ManualDeviceRequest) (core.DeviceDTO, error) {
	f.called("add")
	if f.addFn != nil {
		return f.addFn(ctx, request)
	}
	return core.DeviceDTO{}, nil
}

func (f *fakeManager) AuthenticateDevice(ctx context.Context, request core.AuthenticateRequest) (core.AuthenticateResult, error) {
	f.called("authenticate")
	if f.authFn != nil {
		return f.authFn(ctx, request)
	}
	return core.AuthenticateResult{}, nil
}

func (f *fakeManager) ForgetDevice(ctx context.Context, deviceID string) (core.Overview, error) {
	f.called("forget")
	if f.forgetFn != nil {
		return f.forgetFn(ctx, deviceID)
	}
	return core.Overview{}, nil
}

func (f *fakeManager) SelectDevice(ctx context.Context, deviceID string) (core.Overview, error) {
	f.called("select")
	if f.selectFn != nil {
		return f.selectFn(ctx, deviceID)
	}
	return core.Overview{}, nil
}

func (f *fakeManager) Overview(ctx context.Context) (core.Overview, error) {
	f.called("overview")
	if f.overviewFn != nil {
		return f.overviewFn(ctx)
	}
	return core.Overview{}, nil
}

func (f *fakeManager) Profiles(ctx context.Context, deviceID string) (core.MediaProfilesResult, error) {
	f.called("profiles")
	if f.profilesFn != nil {
		return f.profilesFn(ctx, deviceID)
	}
	return core.MediaProfilesResult{}, nil
}

func (f *fakeManager) ResolveStreamURI(ctx context.Context, selection core.StreamSelection) (string, error) {
	f.called("resolve")
	if f.resolveFn != nil {
		return f.resolveFn(ctx, selection)
	}
	return "", nil
}

func (f *fakeManager) UpdatePreferences(ctx context.Context, request core.PreferencesRequest) (core.Overview, error) {
	f.called("preferences")
	if f.preferencesFn != nil {
		return f.preferencesFn(ctx, request)
	}
	return core.Overview{}, nil
}

func (f *fakeManager) Close() error {
	f.called("close")
	if f.closeFn != nil {
		return f.closeFn()
	}
	return nil
}

type fakeLifecycleCloser struct {
	mu      sync.Mutex
	calls   int
	closeFn func(context.Context) error
}

func (f *fakeLifecycleCloser) Close(ctx context.Context) error {
	f.mu.Lock()
	f.calls++
	fn := f.closeFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	return nil
}

func (f *fakeLifecycleCloser) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

type fakeVLCLauncher struct {
	mu        sync.Mutex
	available bool
	calls     int
	transport core.StreamTransport
	uri       string
	launchErr error
}

func (f *fakeVLCLauncher) Available() bool { return f != nil && f.available }

func (f *fakeVLCLauncher) Launch(_ context.Context, transport core.StreamTransport, uri string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.transport = transport
	f.uri = uri
	return f.launchErr
}

func (f *fakeVLCLauncher) snapshot() (int, core.StreamTransport, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.transport, f.uri
}

type recordingProcessStarter struct {
	mu         sync.Mutex
	executable string
	arguments  []string
	err        error
}

func (r *recordingProcessStarter) Start(_ context.Context, executable string, arguments []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.executable = executable
	r.arguments = append([]string(nil), arguments...)
	return r.err
}

type countingSecretProvider struct {
	mu      sync.Mutex
	gets    int
	puts    int
	deletes int
}

func (p *countingSecretProvider) Put(_ context.Context, _ string, _ store.Secret) error {
	p.mu.Lock()
	p.puts++
	p.mu.Unlock()
	return nil
}

func (p *countingSecretProvider) Get(_ context.Context, _ string) (store.Secret, error) {
	p.mu.Lock()
	p.gets++
	p.mu.Unlock()
	return store.Secret{}, store.ErrSecretNotFound
}

func (p *countingSecretProvider) Delete(_ context.Context, _ string) error {
	p.mu.Lock()
	p.deletes++
	p.mu.Unlock()
	return store.ErrSecretNotFound
}

func (p *countingSecretProvider) counts() (int, int, int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gets, p.puts, p.deletes
}

var _ Manager = (*fakeManager)(nil)
var _ LifecycleCloser = (*fakeLifecycleCloser)(nil)
var _ VLCLauncher = (*fakeVLCLauncher)(nil)
var _ store.SecretProvider = (*countingSecretProvider)(nil)
