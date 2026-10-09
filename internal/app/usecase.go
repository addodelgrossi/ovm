package app

import (
	"context"
	"time"
)

// DiscoveryRequest parameterizes a discovery use case. The zero value is
// usable: an empty interface name probes every usable interface and a
// non-positive timeout uses the adapter default.
type DiscoveryRequest struct {
	// InterfaceName selects a single interface by name; empty probes all.
	InterfaceName string
	// Timeout bounds the discovery round; non-positive uses the default.
	Timeout time.Duration
}

// DiscoveryUseCase performs a single, context-aware discovery round and returns
// a normalized result. It returns a typed application error only when the round
// cannot start; an empty round reports a diagnostic in the result.
type DiscoveryUseCase interface {
	Discover(ctx context.Context, request DiscoveryRequest) (DiscoveryResult, error)
}

// InventoryRequest parameterizes a device-inventory use case. Host is a manual
// endpoint (host, host:port, or full URL) that the adapter canonicalizes.
// Credentials, when non-nil, are applied ephemerally and never stored or
// returned.
type InventoryRequest struct {
	// Host is the manual endpoint to canonicalize and query.
	Host string
	// Credentials, when non-nil, authenticate the session. A nil value targets
	// an unauthenticated camera.
	Credentials *Credentials
	// Timeout bounds the whole use case; non-positive uses the default.
	Timeout time.Duration
	// MaxConcurrency bounds parallel independent inventory calls; non-positive
	// uses the adapter default.
	MaxConcurrency int
}

// InventoryUseCase retrieves a normalized, credential-free device inventory,
// reporting a capability state for every operation and supporting useful
// partial results.
type InventoryUseCase interface {
	Inventory(ctx context.Context, request InventoryRequest) (Inventory, error)
}
