package discovery

import (
	"context"
	"net"
	"sort"
)

// InterfaceReason is a stable, machine-readable classification of why a host
// interface is or is not usable for WS-Discovery. The codes are a closed set
// and are safe to surface across the application boundary: they never carry a
// raw operating-system error string.
type InterfaceReason string

const (
	// ReasonUsable reports that the interface satisfies every usability
	// criterion and carries at least one usable unicast IPv4 address.
	ReasonUsable InterfaceReason = "usable"
	// ReasonDown reports that the interface is not up and therefore cannot
	// carry traffic.
	ReasonDown InterfaceReason = "down"
	// ReasonLoopbackExcluded reports that the interface is a loopback interface
	// and the active filter does not opt loopback in.
	ReasonLoopbackExcluded InterfaceReason = "loopback_excluded"
	// ReasonPointToPoint reports that the interface is a point-to-point link
	// (PPP/VPN/tunnel), which is excluded unconditionally.
	ReasonPointToPoint InterfaceReason = "point_to_point"
	// ReasonMulticastUnsupported reports that the interface does not advertise
	// multicast capability, which WS-Discovery requires on every interface.
	ReasonMulticastUnsupported InterfaceReason = "multicast_unsupported"
	// ReasonNoUsableIPv4 reports that the interface advertises no usable
	// unicast IPv4 address.
	ReasonNoUsableIPv4 InterfaceReason = "no_usable_ipv4"
	// ReasonAddressUnavailable reports that the interface's addresses could not
	// be read. The underlying operating-system error is never exposed here.
	ReasonAddressUnavailable InterfaceReason = "address_unavailable"
)

// HostInterface is a stable, boundary-safe assessment of a single host network
// interface for WS-Discovery. It is a projection of the standard library's
// net.Interface that never leaks live handles or raw OS error strings.
type HostInterface struct {
	// Index is the interface index as reported by the source.
	Index int
	// Name is the interface name (for example "en0").
	Name string
	// Loopback reports whether the interface is a loopback interface.
	Loopback bool
	// Usable reports whether the interface satisfies every usability criterion
	// and is therefore a valid WS-Discovery egress. It is true exactly when
	// Reason is ReasonUsable.
	Usable bool
	// IPv4 holds the usable, sorted, cloned, dotted-decimal IPv4 unicast
	// addresses on the interface. It is populated only for a usable interface
	// and is empty otherwise. The strings are independent copies, so a caller
	// may retain them without aliasing any internal state.
	IPv4 []string
	// Reason is the stable classification of this interface's usability.
	Reason InterfaceReason
}

// HostInterfaceOptions configures a host-interface assessment. The zero value
// is the production configuration: the system interface source and the
// production filter (loopback excluded).
type HostInterfaceOptions struct {
	// InterfaceSource overrides the source of host interfaces. Nil selects the
	// system source, so production callers may leave it unset.
	InterfaceSource InterfaceSource
	// IncludeLoopback admits loopback interfaces to the assessment. It mirrors
	// Options.IncludeLoopback and, exactly as in active discovery, opting a
	// loopback in never relaxes any other usability check: an opted-in loopback
	// must still advertise multicast and carry a usable IPv4 address.
	IncludeLoopback bool
}

// ListHostInterfaces assesses every host interface under the supplied options
// and returns a deterministic, boundary-safe report. It is the production entry
// point used by the application layer: with a zero HostInterfaceOptions it
// reads the system interfaces and excludes loopback exactly as active discovery
// does by default.
//
// It reuses the exact WS-Discovery usability policy (up, non-loopback by
// default, non-point-to-point, multicast-capable, with at least one usable
// unicast IPv4 address). Link-local IPv4 remains allowed. See
// AssessHostInterfaces for the per-interface error handling: a flaky
// interface's address-read failure becomes ReasonAddressUnavailable rather than
// failing the whole call or exposing the operating-system error.
func ListHostInterfaces(ctx context.Context, opts HostInterfaceOptions) ([]HostInterface, error) {
	source := opts.InterfaceSource
	if source == nil {
		source = SystemInterfaceSource{}
	}
	return AssessHostInterfaces(ctx, source, interfaceFilter{includeLoopback: opts.IncludeLoopback})
}

// AssessHostInterfaces enumerates every host interface and returns a stable,
// boundary-safe assessment of each one, in deterministic order (by index, then
// by name). Unlike usableInterfaces, it never filters interfaces out: an
// unusable interface is returned with Usable set to false and a stable Reason.
//
// The assessment reuses the exact usability policy applied by projectInterface
// (up, non-loopback by default, non-point-to-point, multicast-capable, with at
// least one usable unicast IPv4 address). Link-local IPv4 remains allowed;
// loopback is excluded by production default and an explicit opt-in via filter
// never relaxes any other check. A per-interface address-read failure is folded
// into the ReasonAddressUnavailable code rather than aborting the whole
// enumeration or exposing the operating-system error, so a single flaky
// interface cannot deny a report of the others.
//
// Enumerating the interface set is a whole-host operation: if the source cannot
// list interfaces at all, that error is returned (wrapped) because there is
// nothing to assess. The context is honored before the enumeration so a
// cancelled caller does no work.
func AssessHostInterfaces(ctx context.Context, source InterfaceSource, filter interfaceFilter) ([]HostInterface, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, err := source.Interfaces()
	if err != nil {
		return nil, enumerateError(err)
	}
	assessed := make([]HostInterface, 0, len(raw))
	for _, iface := range raw {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ipv4, reason := assessInterface(source, iface, filter)
		host := HostInterface{
			Index:    iface.Index,
			Name:     iface.Name,
			Loopback: iface.Flags&net.FlagLoopback != 0,
			Usable:   reason == ReasonUsable,
			Reason:   reason,
		}
		if host.Usable {
			host.IPv4 = cloneIPv4Strings(ipv4)
		}
		assessed = append(assessed, host)
	}
	sort.Slice(assessed, func(i, j int) bool {
		if assessed[i].Index != assessed[j].Index {
			return assessed[i].Index < assessed[j].Index
		}
		return assessed[i].Name < assessed[j].Name
	})
	return assessed, nil
}

// cloneIPv4Strings renders a slice of IPv4 addresses as independent
// dotted-decimal strings. The returned slice shares no backing array or bytes
// with the input, so a caller may retain it freely.
func cloneIPv4Strings(ips []net.IP) []string {
	if len(ips) == 0 {
		return nil
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}
