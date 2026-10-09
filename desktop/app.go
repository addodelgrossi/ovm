package main

import (
	"context"

	"github.com/addodelgrossi/ovm/internal/buildinfo"
)

// App exposes desktop-safe application services to the frontend.
type App struct {
	ctx context.Context
}

// NewApp creates the desktop application host.
func NewApp() *App {
	return &App{}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(_ context.Context) {
	a.ctx = nil
}

// BuildInfo returns the same version metadata exposed by the CLI.
func (a *App) BuildInfo() buildinfo.Info {
	return buildinfo.Current()
}
