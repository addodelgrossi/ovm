package app

// HostInterfaceReason is a stable, machine-readable classification of a host
// interface's usability for WS-Discovery. It mirrors the discovery package's
// closed set of reason codes and is safe to surface in JSON envelopes: it never
// carries a raw operating-system error string.
type HostInterfaceReason string

const (
	// HostInterfaceUsable reports a fully usable WS-Discovery egress interface.
	HostInterfaceUsable HostInterfaceReason = "usable"
	// HostInterfaceDown reports an interface that is not up.
	HostInterfaceDown HostInterfaceReason = "down"
	// HostInterfaceLoopbackExcluded reports a loopback interface excluded by
	// the active policy.
	HostInterfaceLoopbackExcluded HostInterfaceReason = "loopback_excluded"
	// HostInterfacePointToPoint reports a point-to-point link (PPP/VPN/tunnel)
	// excluded unconditionally.
	HostInterfacePointToPoint HostInterfaceReason = "point_to_point"
	// HostInterfaceMulticastUnsupported reports an interface that does not
	// advertise multicast capability.
	HostInterfaceMulticastUnsupported HostInterfaceReason = "multicast_unsupported"
	// HostInterfaceNoUsableIPv4 reports an interface with no usable unicast
	// IPv4 address.
	HostInterfaceNoUsableIPv4 HostInterfaceReason = "no_usable_ipv4"
	// HostInterfaceAddressUnavailable reports an interface whose addresses
	// could not be read. The underlying operating-system error is never
	// exposed.
	HostInterfaceAddressUnavailable HostInterfaceReason = "address_unavailable"
)

// HostInterfaceDTO is the normalized, boundary-safe view of one host network
// interface. It carries only values that are safe to print or serialize: a
// stable reason code, an explicit usability flag, and cloned IPv4 strings. It
// never carries a live handle or a raw operating-system error.
type HostInterfaceDTO struct {
	// Index is the interface index as reported by the host.
	Index int `json:"index"`
	// Name is the interface name (for example "en0").
	Name string `json:"name"`
	// Loopback reports whether the interface is a loopback interface.
	Loopback bool `json:"loopback"`
	// Usable reports whether the interface is a valid WS-Discovery egress. It
	// is true exactly when Reason is HostInterfaceUsable.
	Usable bool `json:"usable"`
	// IPv4 holds the usable, sorted dotted-decimal IPv4 unicast addresses. It
	// is populated only for a usable interface and omitted otherwise.
	IPv4 []string `json:"ipv4,omitempty"`
	// Reason is the stable usability classification.
	Reason HostInterfaceReason `json:"reason"`
}

// HostInterfacesResult is the normalized outcome of a host-interface
// assessment: every host interface in deterministic order (index then name),
// each with an explicit usability verdict.
type HostInterfacesResult struct {
	// Interfaces holds every assessed host interface in deterministic order.
	Interfaces []HostInterfaceDTO `json:"interfaces"`
}
