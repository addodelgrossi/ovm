package main

import (
	"context"
	"errors"
	"sync"
	"time"

	core "github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/buildinfo"
	"github.com/addodelgrossi/ovm/pkg/stream"
)

const (
	defaultBindingCallTimeout = 65 * time.Second
	defaultShutdownTimeout    = 10 * time.Second
)

// Manager is the exact backend surface consumed by the Wails host. Production
// composition supplies *app.Manager; tests inject deterministic fakes.
type Manager interface {
	Bootstrap(context.Context) (core.Overview, error)
	ListHostInterfaces(context.Context, core.HostInterfaceRequest) (core.HostInterfacesResult, error)
	Scan(context.Context, core.DiscoveryRequest) (core.ScanResult, error)
	AddManualDevice(context.Context, core.ManualDeviceRequest) (core.DeviceDTO, error)
	AuthenticateDevice(context.Context, core.AuthenticateRequest) (core.AuthenticateResult, error)
	ForgetDevice(context.Context, string) (core.Overview, error)
	SelectDevice(context.Context, string) (core.Overview, error)
	Overview(context.Context) (core.Overview, error)
	Profiles(context.Context, string) (core.MediaProfilesResult, error)
	ResolveStreamURI(context.Context, core.StreamSelection) (string, error)
	OpenLiveStream(context.Context, core.StreamSelection) (stream.LiveSource, error)
	OpenSnapshotFeed(context.Context, core.StreamSelection) (core.SnapshotFeed, error)
	ProbeStream(context.Context, core.StreamSelection, time.Duration) (core.ProbeResult, error)
	UpdatePreferences(context.Context, core.PreferencesRequest) (core.Overview, error)
	Close() error
}

// LifecycleCloser owns resources in addition to the Manager. The demo
// environment uses it to close its loopback HTTP server after manager calls
// have drained.
type LifecycleCloser interface {
	Close(context.Context) error
}

// AppDependencies is the explicit Wails-host dependency bundle.
type AppDependencies struct {
	Manager         Manager
	DemoCloser      LifecycleCloser
	VLC             VLCLauncher
	PreviewSources  PreviewSourceFactory
	Demo            bool
	CallTimeout     time.Duration
	ShutdownTimeout time.Duration
}

// App exposes desktop-safe application services to the frontend. It owns an
// independent root context, tracks every exported call, and coordinates scan
// cancellation and bounded shutdown without retaining Wails request contexts.
type App struct {
	mu sync.Mutex

	rootCtx context.Context
	cancel  context.CancelFunc

	manager    Manager
	demoCloser LifecycleCloser
	vlc        VLCLauncher
	previews   *previewController
	// files starts the platform file manager for RevealClip.
	files processStarter
	caps  CapabilityFlags

	callTimeout     time.Duration
	shutdownTimeout time.Duration

	closed    bool
	closeDone chan struct{}
	closeErr  error

	inflight int
	idle     chan struct{}
	scan     *scanOperation
}

type scanOperation struct {
	cancel context.CancelFunc
}

// NewApp creates the desktop application host from explicit dependencies.
func NewApp(deps AppDependencies) (*App, error) {
	if deps.Manager == nil {
		return nil, errors.New("the desktop manager is not configured")
	}
	callTimeout := deps.CallTimeout
	if callTimeout <= 0 {
		callTimeout = defaultBindingCallTimeout
	}
	shutdownTimeout := deps.ShutdownTimeout
	if shutdownTimeout <= 0 {
		shutdownTimeout = defaultShutdownTimeout
	}

	rootCtx, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})
	close(idle)
	vlcAvailable := deps.VLC != nil && deps.VLC.Available() && !deps.Demo
	previews := newPreviewController(deps.PreviewSources, []string{wailsPageOrigin})
	return &App{
		rootCtx:    rootCtx,
		cancel:     cancel,
		manager:    deps.Manager,
		demoCloser: deps.DemoCloser,
		vlc:        deps.VLC,
		previews:   previews,
		files:      commandProcessStarter{},
		caps: CapabilityFlags{
			Demo:                  deps.Demo,
			RTSPPlaybackAvailable: vlcAvailable,
			VLCAvailable:          vlcAvailable,
			LivePreviewAvailable:  previews.available(),
		},
		callTimeout:     callTimeout,
		shutdownTimeout: shutdownTimeout,
		closeDone:       make(chan struct{}),
		idle:            idle,
	}, nil
}

// startup intentionally does not adopt the Wails context. Exported calls are
// children of the App-owned root context so shutdown remains deterministic even
// if the framework context has already been cancelled.
func (a *App) startup(_ context.Context) {}

func (a *App) shutdown(_ context.Context) {
	ctx, cancel := context.WithTimeout(context.Background(), a.shutdownTimeout)
	defer cancel()
	_ = a.close(ctx)
}

// close cancels new and active work, closes the Manager, waits for exported
// calls to drain, then closes demo-owned resources. It is race-safe,
// idempotent, and bounded by ctx.
func (a *App) close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	a.mu.Lock()
	if a.closed {
		done := a.closeDone
		a.mu.Unlock()
		select {
		case <-done:
			a.mu.Lock()
			err := a.closeErr
			a.mu.Unlock()
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	a.closed = true
	manager := a.manager
	demoCloser := a.demoCloser
	rootCancel := a.cancel
	scan := a.scan
	a.scan = nil
	idle := a.idle
	previews := a.previews
	a.mu.Unlock()

	if rootCancel != nil {
		rootCancel()
	}
	if scan != nil {
		scan.cancel()
	}

	var closeErrors []error
	if err := previews.close(ctx); err != nil {
		closeErrors = append(closeErrors, err)
	}
	if manager != nil {
		if err := manager.Close(); err != nil {
			closeErrors = append(closeErrors, err)
		}
	}

	select {
	case <-idle:
	case <-ctx.Done():
		closeErrors = append(closeErrors, ctx.Err())
	}

	if demoCloser != nil {
		done := make(chan error, 1)
		go func() { done <- demoCloser.Close(ctx) }()
		select {
		case err := <-done:
			if err != nil {
				closeErrors = append(closeErrors, err)
			}
		case <-ctx.Done():
			closeErrors = append(closeErrors, ctx.Err())
		}
	}

	finalErr := errors.Join(closeErrors...)
	a.mu.Lock()
	// Drop all references that can reach the repository, secret provider, demo
	// environment, process launcher, or cancellation tree.
	a.manager = nil
	a.demoCloser = nil
	a.vlc = nil
	a.previews = nil
	a.rootCtx = nil
	a.cancel = nil
	a.closeErr = finalErr
	close(a.closeDone)
	a.mu.Unlock()
	return finalErr
}

func (a *App) beginCall() (Manager, context.Context, func(), *BindingError) {
	a.mu.Lock()
	if a.closed || a.manager == nil || a.rootCtx == nil {
		a.mu.Unlock()
		return nil, nil, nil, bindingUnavailable("app_closed", "the desktop application is closed")
	}
	if a.inflight == 0 {
		a.idle = make(chan struct{})
	}
	a.inflight++
	manager := a.manager
	rootCtx := a.rootCtx
	timeout := a.callTimeout
	a.mu.Unlock()

	ctx, cancel := context.WithTimeout(rootCtx, timeout)
	var once sync.Once
	finish := func() {
		once.Do(func() {
			cancel()
			a.mu.Lock()
			a.inflight--
			if a.inflight == 0 {
				close(a.idle)
			}
			a.mu.Unlock()
		})
	}
	return manager, ctx, finish, nil
}

func (a *App) installScan(parent context.Context) (context.Context, *scanOperation, func()) {
	ctx, cancel := context.WithCancel(parent)
	current := &scanOperation{cancel: cancel}

	a.mu.Lock()
	prior := a.scan
	a.scan = current
	a.mu.Unlock()
	if prior != nil {
		prior.cancel()
	}

	finish := func() {
		a.mu.Lock()
		if a.scan == current {
			a.scan = nil
		}
		a.mu.Unlock()
		cancel()
	}
	return ctx, current, finish
}

// BuildInfo returns the same non-sensitive version metadata exposed by the CLI.
func (a *App) BuildInfo() buildinfo.Info {
	return buildinfo.Current()
}
