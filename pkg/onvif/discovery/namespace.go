package discovery

const (
	// nsDiscovery2005 is the WS-Discovery April 2005 namespace.
	nsDiscovery2005 = "http://schemas.xmlsoap.org/ws/2005/04/discovery"
	// nsDiscovery2009 is the WS-Discovery January 2009 (OASIS) namespace.
	nsDiscovery2009 = "http://docs.oasis-open.org/ws-dd/ns/discovery/2009/01"

	// nsAddressing2004 is the WS-Addressing August 2004 namespace. It is the
	// addressing generation that pairs with the 2005/04 WS-Discovery draft.
	nsAddressing2004 = "http://schemas.xmlsoap.org/ws/2004/08/addressing"
	// nsAddressing2005 is the WS-Addressing 2005/08 (W3C) namespace. It is the
	// addressing generation that pairs with the 2009/01 WS-Discovery OASIS
	// standard.
	nsAddressing2005 = "http://www.w3.org/2005/08/addressing"

	// nsSOAPEnvelope12 is the SOAP 1.2 envelope namespace used by WS-Discovery.
	nsSOAPEnvelope12 = "http://www.w3.org/2003/05/soap-envelope"
)

// WS-Discovery ad-hoc discovery target ("To") values. Each discovery family
// has its own target urn; a probe must carry the target that matches the
// family it declares.
const (
	// discoveryTo2005 is the ad-hoc discovery target for the 2005/04 draft.
	discoveryTo2005 = "urn:schemas-xmlsoap-org:ws:2005:04:discovery"
	// discoveryTo2009 is the ad-hoc discovery target for the 2009/01 OASIS
	// standard.
	discoveryTo2009 = "urn:docs-oasis-open-org:ws-dd:ns:discovery:2009:01"
)

// WS-Discovery message local names.
const (
	actionProbe        = "Probe"
	actionProbeMatches = "ProbeMatches"
	actionHello        = "Hello"
	actionBye          = "Bye"
)

// discoveryAction returns the WS-Discovery Action URI for a message local name
// within a given discovery namespace. The Action URI is formed by appending
// "/<local>" to the discovery namespace, which holds for both the 2005/04 and
// 2009/01 families.
func discoveryAction(namespace, local string) string {
	return namespace + "/" + local
}

// Normative ONVIF discovery type namespaces.
//
// The ONVIF Remote Discovery WSDL
// (https://www.onvif.org/ver10/network/wsdl/remotediscovery.wsdl) and the ONVIF
// Core Specification bind the NetworkVideoTransmitter device type to the ONVIF
// network WSDL namespace, conventionally carried on the "dn" prefix.
//
// The Core Specification's "Device" type is NOT defined by the network WSDL.
// It is the device management service type declared by the ONVIF device
// management WSDL (http://www.onvif.org/ver10/device/wsdl), conventionally
// carried on the "tds" prefix. A Probe that advertises "Device" must therefore
// bind it to the device WSDL namespace; emitting "dn:Device" would either leave
// the local name under the network namespace (wrong) or, worse, rely on an
// undeclared prefix. We bind the two advertised types to their respective
// normative namespaces explicitly so that no prefix is ever emitted undeclared.
const (
	// onvifNetworkNamespace is the ONVIF network WSDL namespace. It owns the
	// NetworkVideoTransmitter type and is bound to the "dn" prefix.
	onvifNetworkNamespace = "http://www.onvif.org/ver10/network/wsdl"
	// onvifDeviceNamespace is the ONVIF device management WSDL namespace. It
	// owns the Device type and is bound to the "tds" prefix.
	onvifDeviceNamespace = "http://www.onvif.org/ver10/device/wsdl"

	// onvifNVTPrefix is the conventional prefix for the network WSDL namespace.
	onvifNVTPrefix = "dn"
	// onvifDevicePrefix is the conventional prefix for the device WSDL
	// namespace.
	onvifDevicePrefix = "tds"

	// onvifNVTLocal is the local name of the NetworkVideoTransmitter type.
	onvifNVTLocal = "NetworkVideoTransmitter"
	// onvifDeviceLocal is the local name of the Device type.
	onvifDeviceLocal = "Device"

	// onvifNVTType is the prefixed QName for the ONVIF NetworkVideoTransmitter
	// type, bound to the network WSDL namespace via onvifNVTPrefix.
	onvifNVTType = onvifNVTPrefix + ":" + onvifNVTLocal
	// onvifDeviceType is the prefixed QName for the ONVIF Device type, bound to
	// the device WSDL namespace via onvifDevicePrefix.
	onvifDeviceType = onvifDevicePrefix + ":" + onvifDeviceLocal
)

// qname is a resolved XML qualified name: a namespace URI together with a local
// name. It is independent of any prefix, which is the property that makes a
// Probe's advertised types verifiable rather than merely substring-present.
type qname struct {
	Space string
	Local string
}

// expandedName renders a resolved qualified name as a stable expanded-name
// string of the form "{namespace}local". Two prefix variants that resolve to
// the same namespace and local name produce the same expanded name, so values
// deduplicate on identity rather than on lexical prefix.
func (q qname) expandedName() string {
	return "{" + q.Space + "}" + q.Local
}

// probeTypeQNames returns the resolved qualified names a Probe advertises, in
// emission order. Each entry pairs the normative namespace with the type's
// local name so callers and tests can assert the exact resolved identity.
func probeTypeQNames() []qname {
	return []qname{
		{Space: onvifNetworkNamespace, Local: onvifNVTLocal},
		{Space: onvifDeviceNamespace, Local: onvifDeviceLocal},
	}
}

// addressingForDiscovery returns the one WS-Addressing namespace that is
// coherent with a given WS-Discovery namespace, or "" when the input is not a
// recognised discovery namespace. The pairing is fixed by the specifications
// and is NOT a matter of tolerance:
//
//   - WS-Discovery 2005/04 (draft) pairs ONLY with WS-Addressing 2004/08.
//   - WS-Discovery 2009/01 (OASIS) pairs ONLY with WS-Addressing 2005/08.
//
// A message that mixes a discovery generation with the other generation's
// addressing namespace is incoherent and must be rejected.
func addressingForDiscovery(discoveryNS string) string {
	switch discoveryNS {
	case nsDiscovery2005:
		return nsAddressing2004
	case nsDiscovery2009:
		return nsAddressing2005
	default:
		return ""
	}
}

// isDiscoveryNamespace reports whether the namespace is a recognised
// WS-Discovery namespace.
func isDiscoveryNamespace(namespace string) bool {
	return namespace == nsDiscovery2005 || namespace == nsDiscovery2009
}

// isAddressingNamespace reports whether the namespace is a recognised
// WS-Addressing namespace.
func isAddressingNamespace(namespace string) bool {
	return namespace == nsAddressing2004 || namespace == nsAddressing2005
}

// isSOAPEnvelopeNamespace reports whether the namespace is a SOAP envelope
// namespace accepted for WS-Discovery messages. WS-Discovery mandates SOAP
// 1.2, but tolerating the SOAP 1.1 namespace keeps parsing robust against
// lenient peers without changing semantics.
func isSOAPEnvelopeNamespace(namespace string) bool {
	switch namespace {
	case nsSOAPEnvelope12, "http://schemas.xmlsoap.org/soap/envelope/":
		return true
	default:
		return false
	}
}
