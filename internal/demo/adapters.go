package demo

import (
	"context"
	"errors"

	"github.com/addodelgrossi/ovm/internal/app"
)

const demoHostInterfaceName = "demo-loopback"

// demoDiscovery keeps --demo hermetic. A scan deterministically reports the
// already-running loopback camera and never opens a multicast socket or reads a
// production network interface.
type demoDiscovery struct {
	endpoint string
}

func (d demoDiscovery) Discover(ctx context.Context, request app.DiscoveryRequest) (app.DiscoveryResult, error) {
	if err := ctx.Err(); err != nil {
		return app.DiscoveryResult{}, demoContextError(err, "demo discovery was cancelled")
	}
	if request.InterfaceName != "" && request.InterfaceName != demoHostInterfaceName {
		return app.DiscoveryResult{}, app.NewError(app.CategoryUsage, "the demo host interface was not found")
	}
	return app.DiscoveryResult{Devices: []app.DiscoveredDevice{{
		Addresses: []string{d.endpoint},
	}}}, nil
}

// demoHostInterfaces exposes only the synthetic loopback egress used by the
// in-process camera. It does not enumerate or disclose production interfaces.
type demoHostInterfaces struct{}

func (demoHostInterfaces) HostInterfaces(ctx context.Context, _ app.HostInterfaceRequest) (app.HostInterfacesResult, error) {
	if err := ctx.Err(); err != nil {
		return app.HostInterfacesResult{}, demoContextError(err, "listing demo host interfaces was cancelled")
	}
	return app.HostInterfacesResult{Interfaces: []app.HostInterfaceDTO{{
		Index:    0,
		Name:     demoHostInterfaceName,
		Loopback: true,
		Usable:   true,
		IPv4:     []string{"127.0.0.1"},
		Reason:   app.HostInterfaceUsable,
	}}}, nil
}

func demoContextError(err error, cancelledMessage string) error {
	if errors.Is(err, context.Canceled) {
		return app.NewError(app.CategoryCancelled, cancelledMessage)
	}
	return app.NewError(app.CategoryNetwork, "the demo operation timed out")
}

var _ app.DiscoveryUseCase = demoDiscovery{}
var _ app.HostInterfaceUseCase = demoHostInterfaces{}
