package app

import (
	"context"
	"errors"

	"github.com/addodelgrossi/ovm/pkg/onvif/discovery"
)

// discoverer is the narrow seam over pkg/onvif/discovery used by the production
// discovery use case. *discovery.Discoverer satisfies it; tests inject a
// deterministic double so public CI needs no multicast or hardware.
type discoverer interface {
	Discover(ctx context.Context) (discovery.Result, error)
}

// discovererFactory constructs a discoverer for one request. The production
// factory calls discovery.NewDiscoverer with the requested interface and
// timeout; tests supply a fixed double.
type discovererFactory func(request DiscoveryRequest) discoverer

// productionDiscovery is the production DiscoveryUseCase. It constructs a
// discovery.Discoverer per request and maps its Result and Diagnostic into the
// normalized DTOs.
type productionDiscovery struct {
	factory discovererFactory
}

// NewProductionDiscovery returns the production DiscoveryUseCase that drives
// pkg/onvif/discovery. The returned value performs real multicast WS-Discovery
// and is therefore not used in public CI tests, which inject a seam instead.
func NewProductionDiscovery() DiscoveryUseCase {
	return &productionDiscovery{factory: defaultDiscovererFactory}
}

// defaultDiscovererFactory builds a real discoverer honoring the requested
// interface name and timeout. A non-positive timeout defers to the discovery
// package default.
func defaultDiscovererFactory(request DiscoveryRequest) discoverer {
	return discovery.NewDiscoverer(discovery.Options{
		InterfaceName: request.InterfaceName,
		Timeout:       request.Timeout,
	})
}

// Discover performs one discovery round and normalizes the result.
//
// A plain empty round (no_response) is a success: the result carries a
// no_response diagnostic and the error is nil. A concrete socket failure or a
// possible permission restriction is a real network failure: it is promoted to
// a typed CategoryNetwork *Error that still carries the diagnostic through the
// returned DiscoveryResult, so a caller may surface the diagnostic to stderr
// exactly once. A round-start failure (for example no usable interface) is also
// a typed CategoryNetwork (or cancellation) error.
//
// Cancellation never discards devices. The underlying discoverer returns the
// aggregated-so-far Result together with context.Canceled when the caller's
// context ends mid-round. In that case the partial Result is normalized first
// and returned alongside a typed CategoryCancelled error, so callers can render
// the devices discovered before cancellation and still map exit code 130.
func (d *productionDiscovery) Discover(ctx context.Context, request DiscoveryRequest) (DiscoveryResult, error) {
	if err := ctx.Err(); err != nil {
		return DiscoveryResult{}, classifyFatal(err)
	}
	result, err := d.factory(request).Discover(ctx)
	if err != nil {
		// A mid-round cancellation returns whatever devices were aggregated
		// before the context ended. Normalize that partial result first and
		// return it so the caller can present the devices discovered so far,
		// paired with a typed cancellation error that maps to exit code 130.
		if errors.Is(err, context.Canceled) {
			return normalizeDiscoveryResult(result), newError(CategoryCancelled, "discovery was cancelled", err)
		}
		return DiscoveryResult{}, mapDiscoveryStartError(err)
	}

	normalized := normalizeDiscoveryResult(result)

	// A concrete socket or permission diagnostic on an empty round is a real
	// failure that must map to the network exit code, not a successful empty
	// result. The normalized diagnostic is still returned so the caller can
	// present it to stderr before mapping the exit code.
	if len(normalized.Devices) == 0 && normalized.Diagnostic != nil {
		switch result.Diagnostic.Category {
		case discovery.DiagnosticSocketError, discovery.DiagnosticPossiblePermissionRestriction:
			return normalized, newError(CategoryNetwork, "discovery could not complete because of a socket error", nil)
		}
	}
	return normalized, nil
}

// mapDiscoveryStartError maps a round-start failure to a typed application
// error. Cancellation and timeout are distinguished; everything else is a
// network/discovery failure. The message is credential-free and generic because
// discovery errors never carry credentials.
func mapDiscoveryStartError(err error) *Error {
	switch {
	case errors.Is(err, context.Canceled):
		return newError(CategoryCancelled, "discovery was cancelled", err)
	case errors.Is(err, context.DeadlineExceeded):
		return newError(CategoryNetwork, "discovery timed out before it could start", err)
	default:
		return newError(CategoryNetwork, "discovery could not start on the selected interface", err)
	}
}

// normalizeDiscoveryResult maps a discovery.Result into the normalized DTO. The
// discovery package already sorts devices by EndpointReference and deduplicates
// their fields, so ordering is deterministic.
//
// Every URI-bearing field crossing the boundary is validated and redacted
// app-locally rather than relying on soap.RedactURL alone: transport addresses
// (XAddrs) must be absolute HTTP(S) URLs with a host; the EndpointReference,
// scopes, and the namespace inside each expanded type name are validated as
// general URIs. Any value failing validation is replaced wholesale with a safe
// marker so an invalid or credential-bearing raw string can never cross into a
// DTO or output.
func normalizeDiscoveryResult(result discovery.Result) DiscoveryResult {
	devices := make([]DiscoveredDevice, 0, len(result.Devices))
	for _, dev := range result.Devices {
		addresses := make([]string, 0, len(dev.XAddrs))
		for _, address := range dev.XAddrs {
			addresses = append(addresses, redactBoundaryTransportURI(address))
		}
		scopes := make([]string, 0, len(dev.Scopes))
		for _, scope := range dev.Scopes {
			scopes = append(scopes, redactBoundaryURI(scope))
		}
		types := make([]string, 0, len(dev.Types))
		for _, typ := range dev.Types {
			types = append(types, redactBoundaryExpandedName(typ))
		}
		devices = append(devices, DiscoveredDevice{
			EndpointReference: redactBoundaryURI(dev.EndpointReference),
			Addresses:         addresses,
			Scopes:            scopes,
			Types:             types,
			MetadataVersion:   dev.MetadataVersion,
		})
	}
	out := DiscoveryResult{Devices: devices}
	if len(devices) == 0 && result.Diagnostic.Category != discovery.DiagnosticNone {
		out.Diagnostic = &DiscoveryDiagnostic{
			Category: string(result.Diagnostic.Category),
			Detail:   result.Diagnostic.Detail,
		}
	}
	return out
}
