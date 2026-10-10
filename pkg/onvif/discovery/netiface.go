package discovery

import (
	"fmt"
	"net"
	"sort"
)

// Interface describes a network interface together with the usable IPv4
// addresses discovered on it. It is a stable, comparable projection of the
// standard library's net.Interface that avoids leaking live handles into the
// rest of the package.
type Interface struct {
	// Index is the interface index as reported by the source.
	Index int
	// Name is the interface name (for example "en0").
	Name string
	// Loopback reports whether the interface is a loopback interface.
	Loopback bool
	// IPv4 holds the usable, sorted, unicast IPv4 addresses on the interface.
	IPv4 []net.IP
}

// InterfaceSource enumerates the host network interfaces and their addresses.
// Production code uses SystemInterfaceSource; tests inject a deterministic
// implementation so that interface filtering and selection require no real
// network stack.
type InterfaceSource interface {
	// Interfaces returns the available interfaces.
	Interfaces() ([]net.Interface, error)
	// Addrs returns the addresses assigned to the interface.
	Addrs(iface net.Interface) ([]net.Addr, error)
}

// SystemInterfaceSource reads interfaces from the operating system.
type SystemInterfaceSource struct{}

// Interfaces implements InterfaceSource using net.Interfaces.
func (SystemInterfaceSource) Interfaces() ([]net.Interface, error) {
	return net.Interfaces()
}

// Addrs implements InterfaceSource using the interface's own Addrs method.
func (SystemInterfaceSource) Addrs(iface net.Interface) ([]net.Addr, error) {
	return iface.Addrs()
}

// interfaceFilter controls which interfaces are considered usable.
type interfaceFilter struct {
	// includeLoopback admits loopback interfaces, which is required for
	// deterministic tests that bind multicast to the loopback device. Opting a
	// loopback in does NOT relax any other usability criterion: an opted-in
	// loopback must still advertise net.FlagMulticast like every other usable
	// interface. Test fakes mark their opted-in loopback multicast-capable to
	// satisfy this.
	includeLoopback bool
}

// enumerateError wraps a whole-host interface-enumeration failure with a stable
// prefix. It is the single place that frames the "enumerate interfaces" error
// so the active-discovery path and the host-interface assessment path agree.
func enumerateError(err error) error {
	return fmt.Errorf("enumerate interfaces: %w", err)
}

// usableInterfaces enumerates up, IPv4-capable, multicast-capable interfaces
// from the source and returns them sorted by index for deterministic ordering.
// Loopback interfaces are excluded unless the filter opts in; point-to-point
// and non-multicast interfaces are excluded by default (see projectInterface).
func usableInterfaces(source InterfaceSource, filter interfaceFilter) ([]Interface, error) {
	raw, err := source.Interfaces()
	if err != nil {
		return nil, enumerateError(err)
	}
	var usable []Interface
	for _, iface := range raw {
		projected, ok, err := projectInterface(source, iface, filter)
		if err != nil {
			return nil, err
		}
		if ok {
			usable = append(usable, projected)
		}
	}
	sort.Slice(usable, func(i, j int) bool {
		if usable[i].Index != usable[j].Index {
			return usable[i].Index < usable[j].Index
		}
		return usable[i].Name < usable[j].Name
	})
	return usable, nil
}

// interfaceFlagReason applies the flag-only portion of the WS-Discovery
// usability policy and reports a stable reason. It is the single source of
// truth for the flag checks, shared by projectInterface (which collapses every
// non-usable reason into a silent skip) and by assessInterface (which reports
// the reason). An address-independent verdict of ReasonUsable means the flags
// pass and the caller must still confirm at least one usable IPv4 address.
//
// Usability policy for WS-Discovery (interface-scoped IPv4 multicast):
//
//   - The interface must be up. A down interface cannot carry traffic.
//   - Loopback is excluded unless the filter opts in.
//   - Point-to-point interfaces are excluded unconditionally. PPP/VPN/tunnel
//     links are point-to-point: there is no shared L2 segment for ad-hoc
//     multicast discovery, and probing them tends to leak discovery traffic
//     across a VPN or simply never reach a camera. The exclusion is absolute:
//     a named point-to-point interface is still rejected, even when it is up,
//     multicast-flagged, and carries a usable IPv4 address.
//   - The interface must advertise net.FlagMulticast. WS-Discovery is driven by
//     IPv4 multicast to 239.255.255.250:3702; an interface that cannot do
//     multicast can neither emit a probe to the group nor join it to observe
//     Hello/Bye. This requirement applies to EVERY usable interface, including
//     an explicitly opted-in loopback: there is no multicast exception for
//     loopback.
//
// The order of the checks is significant and preserved: down, then loopback,
// then point-to-point, then multicast. It mirrors the historical ordering of
// projectInterface so the two paths classify identically.
func interfaceFlagReason(iface net.Interface, filter interfaceFilter) InterfaceReason {
	if iface.Flags&net.FlagUp == 0 {
		return ReasonDown
	}
	if iface.Flags&net.FlagLoopback != 0 && !filter.includeLoopback {
		return ReasonLoopbackExcluded
	}
	// Point-to-point links (PPP, VPN, tunnels) have no shared segment for
	// ad-hoc multicast discovery; exclude them even when named.
	if iface.Flags&net.FlagPointToPoint != 0 {
		return ReasonPointToPoint
	}
	// Require multicast capability on EVERY usable interface, including an
	// opted-in loopback. There is no loopback relaxation: a device that cannot
	// multicast cannot carry WS-Discovery.
	if iface.Flags&net.FlagMulticast == 0 {
		return ReasonMulticastUnsupported
	}
	return ReasonUsable
}

// projectInterface decides whether a single interface is usable and, if so,
// returns its stable projection. It applies the shared flag policy via
// interfaceFlagReason and then requires at least one usable unicast IPv4
// address. A failure to read the interface's addresses is surfaced as an error
// so the active-discovery path treats a flaky interface as a hard failure,
// preserving existing Discoverer and Listener behavior.
func projectInterface(source InterfaceSource, iface net.Interface, filter interfaceFilter) (Interface, bool, error) {
	if interfaceFlagReason(iface, filter) != ReasonUsable {
		return Interface{}, false, nil
	}
	addrs, err := source.Addrs(iface)
	if err != nil {
		return Interface{}, false, fmt.Errorf("read addresses for %q: %w", iface.Name, err)
	}
	ipv4 := usableIPv4(addrs, filter.includeLoopback)
	if len(ipv4) == 0 {
		return Interface{}, false, nil
	}
	return Interface{
		Index:    iface.Index,
		Name:     iface.Name,
		Loopback: iface.Flags&net.FlagLoopback != 0,
		IPv4:     ipv4,
	}, true, nil
}

// assessInterface applies the exact usability policy of projectInterface and
// reports a stable reason instead of silently skipping. It never returns an
// error: a failure to read the interface's addresses is folded into
// ReasonAddressUnavailable so a single flaky interface cannot deny an
// assessment of the others, and so no raw operating-system error leaks to the
// caller. On a usable verdict it returns the usable, deterministically ordered
// IPv4 addresses alongside ReasonUsable.
func assessInterface(source InterfaceSource, iface net.Interface, filter interfaceFilter) ([]net.IP, InterfaceReason) {
	if reason := interfaceFlagReason(iface, filter); reason != ReasonUsable {
		return nil, reason
	}
	addrs, err := source.Addrs(iface)
	if err != nil {
		return nil, ReasonAddressUnavailable
	}
	ipv4 := usableIPv4(addrs, filter.includeLoopback)
	if len(ipv4) == 0 {
		return nil, ReasonNoUsableIPv4
	}
	return ipv4, ReasonUsable
}

// usableIPv4 extracts the usable, deterministically ordered IPv4 unicast
// addresses from a set of interface addresses.
//
// Link-local inclusion: IPv4 link-local unicast addresses (169.254.0.0/16) are
// INCLUDED. The usability policy excludes loopback (unless explicitly allowed),
// but not link-local: ONVIF cameras frequently fall back to a 169.254/16
// autoconfiguration address when no DHCP server is present, and such a camera
// is reachable on a directly connected link. Excluding link-local would make
// those cameras undiscoverable. Unspecified (0.0.0.0), multicast, and all IPv6
// addresses remain rejected because they are never a usable unicast source for
// an interface-scoped probe.
func usableIPv4(addrs []net.Addr, allowLoopback bool) []net.IP {
	seen := make(map[string]struct{})
	var ips []net.IP
	for _, addr := range addrs {
		ip := addrIP(addr)
		if ip == nil {
			continue
		}
		v4 := ip.To4()
		if v4 == nil {
			continue
		}
		if v4.IsUnspecified() || v4.IsMulticast() {
			continue
		}
		if v4.IsLoopback() && !allowLoopback {
			continue
		}
		key := v4.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		ips = append(ips, append(net.IP(nil), v4...))
	}
	sort.Slice(ips, func(i, j int) bool {
		return ips[i].String() < ips[j].String()
	})
	return ips
}

// addrIP extracts the IP from the address shapes the standard library returns.
func addrIP(addr net.Addr) net.IP {
	switch typed := addr.(type) {
	case *net.IPNet:
		return typed.IP
	case *net.IPAddr:
		return typed.IP
	default:
		return nil
	}
}

// selectInterfaces resolves the interface selection against the usable set. An
// empty name selects every usable interface; a non-empty name selects exactly
// one and reports whether it was absent or merely unusable.
func selectInterfaces(usable []Interface, name string) ([]Interface, error) {
	if name == "" {
		if len(usable) == 0 {
			return nil, ErrNoUsableInterfaces
		}
		return usable, nil
	}
	for _, iface := range usable {
		if iface.Name == name {
			return []Interface{iface}, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrInterfaceNotUsable, name)
}

// resolveInterfaces combines usability filtering with selection so callers can
// obtain the exact interfaces a probe or listener should bind to. When a named
// interface is requested, absence and unusability are distinguished.
func resolveInterfaces(source InterfaceSource, filter interfaceFilter, name string) ([]Interface, error) {
	usable, err := usableInterfaces(source, filter)
	if err != nil {
		return nil, err
	}
	if name != "" {
		if _, present, err := interfacePresent(source, name); err != nil {
			return nil, err
		} else if !present {
			return nil, fmt.Errorf("%w: %q", ErrInterfaceNotFound, name)
		}
	}
	return selectInterfaces(usable, name)
}

// interfacePresent reports whether a named interface exists in the source at
// all, independent of usability.
func interfacePresent(source InterfaceSource, name string) (net.Interface, bool, error) {
	raw, err := source.Interfaces()
	if err != nil {
		return net.Interface{}, false, enumerateError(err)
	}
	for _, iface := range raw {
		if iface.Name == name {
			return iface, true, nil
		}
	}
	return net.Interface{}, false, nil
}
