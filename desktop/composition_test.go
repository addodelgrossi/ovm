package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	core "github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/store"
)

func TestParseDesktopOptions(t *testing.T) {
	for _, test := range []struct {
		name      string
		arguments []string
		wantDemo  bool
		wantError bool
	}{
		{name: "normal"},
		{name: "demo", arguments: []string{"--demo"}, wantDemo: true},
		{name: "explicit demo", arguments: []string{"--demo=true"}, wantDemo: true},
		{name: "unknown flag", arguments: []string{"--unknown"}, wantError: true},
		{name: "positional argument", arguments: []string{"unexpected"}, wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			options, err := parseDesktopOptions(test.arguments)
			if (err != nil) != test.wantError {
				t.Fatalf("parseDesktopOptions(%q) error = %v", test.arguments, err)
			}
			if err == nil && options.Demo != test.wantDemo {
				t.Fatalf("Demo = %v, want %v", options.Demo, test.wantDemo)
			}
		})
	}
}

func TestProductionCompositionDoesNotTouchSecretProviderAtStartup(t *testing.T) {
	repository, err := store.NewRepository(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	secrets := &countingSecretProvider{}
	factories := compositionFactories{
		newRepository: func() (*store.Repository, error) { return repository, nil },
		newSecrets:    func() (store.SecretProvider, error) { return secrets, nil },
		newManager: func(dependencies core.ManagerDependencies) (Manager, error) {
			return core.NewManager(dependencies)
		},
		newVLC: func() VLCLauncher { return &fakeVLCLauncher{} },
	}
	host, err := composeDesktop(context.Background(), desktopOptions{}, factories)
	if err != nil {
		t.Fatalf("composeDesktop() error = %v", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), time.Second)
		defer cleanupCancel()
		_ = host.close(cleanupCtx)
	})

	response := host.Bootstrap()
	if response.Error != nil || response.Result == nil {
		t.Fatalf("Bootstrap() response = %+v", response)
	}
	gets, puts, deletes := secrets.counts()
	if gets != 0 || puts != 0 || deletes != 0 {
		t.Fatalf("startup keyring calls = get:%d put:%d delete:%d, want zero", gets, puts, deletes)
	}
}

func TestDemoCompositionSkipsProductionStorageAndClosesOwner(t *testing.T) {
	manager := newFakeManager()
	closer := &fakeLifecycleCloser{}
	productionRepositoryCalls := 0
	productionSecretCalls := 0
	factories := compositionFactories{
		newRepository: func() (*store.Repository, error) {
			productionRepositoryCalls++
			return nil, nil
		},
		newSecrets: func() (store.SecretProvider, error) {
			productionSecretCalls++
			return nil, nil
		},
		newDemo: func(context.Context, *core.SOAPConsole) (Manager, LifecycleCloser, error) {
			return manager, closer, nil
		},
		newVLC: func() VLCLauncher { return &fakeVLCLauncher{available: true} },
	}
	host, err := composeDesktop(context.Background(), desktopOptions{Demo: true}, factories)
	if err != nil {
		t.Fatalf("composeDesktop(demo) error = %v", err)
	}
	bootstrap := host.Bootstrap()
	if bootstrap.Error != nil || bootstrap.Result == nil || !bootstrap.Result.Capabilities.Demo {
		t.Fatalf("demo Bootstrap() response = %+v", bootstrap)
	}
	if bootstrap.Result.Capabilities.VLCAvailable || bootstrap.Result.Capabilities.RTSPPlaybackAvailable {
		t.Fatalf("demo capabilities claim playback: %+v", bootstrap.Result.Capabilities)
	}
	if productionRepositoryCalls != 0 || productionSecretCalls != 0 {
		t.Fatalf("demo touched production composition: repository=%d secrets=%d", productionRepositoryCalls, productionSecretCalls)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := host.close(ctx); err != nil {
		t.Fatalf("close() error = %v", err)
	}
	if manager.callCount("close") != 1 || closer.callCount() != 1 {
		t.Fatalf("demo manager/owner close calls = %d/%d", manager.callCount("close"), closer.callCount())
	}
}

func TestProductionDemoCompositionBootstrapsAndCloses(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	factories := productionCompositionFactories()
	factories.newVLC = func() VLCLauncher { return &fakeVLCLauncher{available: true} }
	host, err := composeDesktop(context.Background(), desktopOptions{Demo: true}, factories)
	if err != nil {
		t.Fatalf("composeDesktop(production demo) error = %v", err)
	}
	response := host.Bootstrap()
	if response.Error != nil || response.Result == nil || len(response.Result.Overview.Devices) != 1 {
		t.Fatalf("demo Bootstrap() response = %+v", response)
	}
	if response.Result.Overview.Devices[0].DisplayName != "OVM Demo Camera" {
		t.Fatalf("demo device = %+v", response.Result.Overview.Devices[0])
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := host.close(ctx); err != nil {
		t.Fatalf("close() error = %v", err)
	}
	closed := host.Bootstrap()
	if closed.Error == nil || closed.Error.Code != "app_closed" {
		t.Fatalf("post-close Bootstrap() response = %+v", closed)
	}
}
