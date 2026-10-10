package app

import (
	"context"
	"errors"

	"github.com/addodelgrossi/ovm/pkg/onvif/discovery"
)

// HostInterfaceRequest parameterizes a host-interface assessment. The zero
// value is the production configuration: the system interfaces are read and
// loopback is excluded, exactly as active discovery does by default.
type HostInterfaceRequest struct {
	// IncludeLoopback admits loopback interfaces to the assessment. Opting a
	// loopback in never relaxes any other usability check, mirroring active
	// discovery.
	IncludeLoopback bool
}

// HostInterfaceUseCase assesses every host network interface and returns a
// normalized, deterministic report. It returns a typed application error only
// when the whole-host enumeration cannot complete (for example cancellation or
// an operating-system enumeration failure); a per-interface problem is reported
// as that interface's reason code, not as an error.
type HostInterfaceUseCase interface {
	HostInterfaces(ctx context.Context, request HostInterfaceRequest) (HostInterfacesResult, error)
}

// hostInterfaceLister is the narrow seam over pkg/onvif/discovery used by the
// production host-interface use case. The production lister calls
// discovery.ListHostInterfaces; tests inject a deterministic double so public
// CI needs no real network stack.
type hostInterfaceLister interface {
	ListHostInterfaces(ctx context.Context, request HostInterfaceRequest) ([]discovery.HostInterface, error)
}

// productionHostInterfaces is the production HostInterfaceUseCase. It drives the
// discovery package's host-interface assessment and maps the result into the
// normalized DTO.
type productionHostInterfaces struct {
	lister hostInterfaceLister
}

// NewProductionHostInterfaces returns the production HostInterfaceUseCase that
// reads the system's host interfaces through pkg/onvif/discovery. It performs
// no network I/O beyond enumerating interfaces and is safe to call anywhere.
func NewProductionHostInterfaces() HostInterfaceUseCase {
	return &productionHostInterfaces{lister: systemHostInterfaceLister{}}
}

// systemHostInterfaceLister is the production lister. It adapts the application
// request to discovery.HostInterfaceOptions and uses the system interface
// source by leaving the source unset.
type systemHostInterfaceLister struct{}

// ListHostInterfaces implements hostInterfaceLister against the discovery
// package's production entry point.
func (systemHostInterfaceLister) ListHostInterfaces(ctx context.Context, request HostInterfaceRequest) ([]discovery.HostInterface, error) {
	return discovery.ListHostInterfaces(ctx, discovery.HostInterfaceOptions{
		IncludeLoopback: request.IncludeLoopback,
	})
}

// HostInterfaces assesses every host interface and normalizes the result.
//
// A whole-host failure is mapped to a typed application error:
//
//   - context cancellation maps to CategoryCancelled,
//   - a context deadline maps to CategoryNetwork,
//   - any other enumeration failure maps to CategoryNetwork.
//
// The mapping mirrors the discovery use case's cancellation/network split. The
// wrapped cause is retained for errors.Is/As but is never rendered to users:
// the message is credential-free and generic, and the per-interface reason
// codes already describe every interface-local condition without exposing a raw
// operating-system error.
func (h *productionHostInterfaces) HostInterfaces(ctx context.Context, request HostInterfaceRequest) (HostInterfacesResult, error) {
	if err := ctx.Err(); err != nil {
		return HostInterfacesResult{}, mapHostInterfaceError(err)
	}
	hosts, err := h.lister.ListHostInterfaces(ctx, request)
	if err != nil {
		return HostInterfacesResult{}, mapHostInterfaceError(err)
	}
	return normalizeHostInterfaces(hosts), nil
}

// mapHostInterfaceError maps a whole-host assessment failure to a typed,
// user-safe application error. Cancellation and deadline are distinguished;
// everything else is a network/enumeration failure. The message never carries a
// raw operating-system error.
func mapHostInterfaceError(err error) *Error {
	switch {
	case errors.Is(err, context.Canceled):
		return newError(CategoryCancelled, "listing host interfaces was cancelled", err)
	case errors.Is(err, context.DeadlineExceeded):
		return newError(CategoryNetwork, "listing host interfaces timed out", err)
	default:
		return newError(CategoryNetwork, "the host network interfaces could not be read", err)
	}
}

// normalizeHostInterfaces maps the discovery assessment into the normalized
// DTO. The discovery package already returns interfaces in deterministic order
// (index then name) with cloned IPv4 strings and safe reason codes, so this
// mapping is a direct, allocation-isolated projection.
func normalizeHostInterfaces(hosts []discovery.HostInterface) HostInterfacesResult {
	interfaces := make([]HostInterfaceDTO, 0, len(hosts))
	for _, host := range hosts {
		var ipv4 []string
		if len(host.IPv4) > 0 {
			ipv4 = make([]string, len(host.IPv4))
			copy(ipv4, host.IPv4)
		}
		interfaces = append(interfaces, HostInterfaceDTO{
			Index:    host.Index,
			Name:     host.Name,
			Loopback: host.Loopback,
			Usable:   host.Usable,
			IPv4:     ipv4,
			Reason:   HostInterfaceReason(host.Reason),
		})
	}
	return HostInterfacesResult{Interfaces: interfaces}
}
