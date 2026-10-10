package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	core "github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/demo"
	"github.com/addodelgrossi/ovm/internal/store"
	"github.com/addodelgrossi/ovm/internal/synthmedia"
	"github.com/addodelgrossi/ovm/pkg/stream"
)

type desktopOptions struct {
	Demo bool
}

func parseDesktopOptions(arguments []string) (desktopOptions, error) {
	set := flag.NewFlagSet("ovm", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	var options desktopOptions
	set.BoolVar(&options.Demo, "demo", false, "run the isolated in-memory demo environment")
	if err := set.Parse(arguments); err != nil || set.NArg() != 0 {
		return desktopOptions{}, errors.New("the desktop arguments are not valid")
	}
	return options, nil
}

type compositionFactories struct {
	newRepository func() (*store.Repository, error)
	newSecrets    func() (store.SecretProvider, error)
	newManager    func(core.ManagerDependencies) (Manager, error)
	newDemo       func(context.Context) (Manager, LifecycleCloser, error)
	newVLC        func() VLCLauncher
}

func productionCompositionFactories() compositionFactories {
	return compositionFactories{
		newRepository: func() (*store.Repository, error) {
			return store.NewDefaultRepository()
		},
		newSecrets: store.NewPlatformSecretProvider,
		newManager: func(dependencies core.ManagerDependencies) (Manager, error) {
			return core.NewManager(dependencies)
		},
		newDemo: func(ctx context.Context) (Manager, LifecycleCloser, error) {
			environment, err := demo.NewEnvironment(ctx)
			if err != nil {
				return nil, nil, err
			}
			return environment.Manager(), environment, nil
		},
		newVLC: newProductionVLCLauncher,
	}
}

// demoPreviewSources serves the project-owned synthetic H.264 stream in real
// time for every demo profile; no network or camera is involved.
func demoPreviewSources(_ context.Context, _ Manager, _ core.StreamSelection) (stream.H264Source, func(), error) {
	source, err := synthmedia.NewH264Source(synthmedia.H264Config{Width: 640, Height: 352, FPS: 25, GOP: 50})
	if err != nil {
		return nil, nil, err
	}
	return synthmedia.NewPacedH264(source), nil, nil
}

// composeDesktop is the sole production composition root. Normal mode opens
// the JSON metadata repository and constructs the platform secret-provider
// adapter without invoking Get/Put/Delete; keyring access therefore begins only
// when a Manager operation actually needs credentials. Demo mode constructs no
// production repository or keyring and uses the isolated in-memory environment.
func composeDesktop(ctx context.Context, options desktopOptions, factories compositionFactories) (*App, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if factories.newVLC == nil {
		return nil, errors.New("the VLC capability factory is not configured")
	}
	launcher := factories.newVLC()

	if options.Demo {
		if factories.newDemo == nil {
			return nil, errors.New("the demo factory is not configured")
		}
		manager, closer, err := factories.newDemo(ctx)
		if err != nil {
			return nil, fmt.Errorf("compose demo environment: %w", err)
		}
		host, err := NewApp(AppDependencies{
			Manager:        manager,
			DemoCloser:     closer,
			VLC:            launcher,
			PreviewSources: demoPreviewSources,
			Demo:           true,
		})
		if err != nil {
			if closer != nil {
				_ = closer.Close(ctx)
			} else if manager != nil {
				_ = manager.Close()
			}
			return nil, err
		}
		return host, nil
	}

	if factories.newRepository == nil || factories.newSecrets == nil || factories.newManager == nil {
		return nil, errors.New("the production composition factories are not configured")
	}
	repository, err := factories.newRepository()
	if err != nil {
		return nil, fmt.Errorf("open device repository: %w", err)
	}
	secrets, err := factories.newSecrets()
	if err != nil {
		return nil, fmt.Errorf("construct platform secret provider: %w", err)
	}
	managerRepository, err := core.NewProductionManagerRepository(repository, secrets)
	if err != nil {
		return nil, fmt.Errorf("compose manager repository: %w", err)
	}
	manager, err := factories.newManager(core.ManagerDependencies{
		Repository:     managerRepository,
		HostInterfaces: core.NewProductionHostInterfaces(),
		Discovery:      core.NewProductionDiscovery(),
		Inventory:      core.NewProductionInventory(),
		Media:          core.NewProductionMediaProfiles(),
	})
	if err != nil {
		return nil, fmt.Errorf("compose device manager: %w", err)
	}
	host, err := NewApp(AppDependencies{Manager: manager, VLC: launcher})
	if err != nil {
		if manager != nil {
			_ = manager.Close()
		}
		return nil, err
	}
	return host, nil
}
