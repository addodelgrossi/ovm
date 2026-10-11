package device

// Normative ONVIF Device service namespaces and action URIs.
//
// The Device Management service is defined by the ONVIF Device Management WSDL.
// Its target namespace and the SOAP action for each operation are taken
// directly from that WSDL and the ONVIF Core Specification. Action URIs follow
// the ONVIF convention of the service namespace, a slash, and the operation
// name.
//
// Normative sources:
//   - ONVIF Device Management WSDL:
//     https://www.onvif.org/ver10/device/wsdl/devicemgmt.wsdl
//   - ONVIF schema (shared types):
//     https://www.onvif.org/ver10/schema/onvif.xsd
//   - ONVIF Core Specification:
//     https://www.onvif.org/specs/core/ONVIF-Core-Specification.pdf
const (
	// Namespace is the ONVIF Device Management WSDL target namespace. Response
	// elements for every modeled operation live in this namespace.
	Namespace = "http://www.onvif.org/ver10/device/wsdl"
	// SchemaNamespace is the shared ONVIF schema namespace used for common
	// types such as date/time, capability, and network structures.
	SchemaNamespace = "http://www.onvif.org/ver10/schema"
)

// SOAP action URIs for the modeled Device operations. Each is the Device
// Management namespace followed by a slash and the WSDL operation name, as
// required by the ONVIF SOAP binding.
const (
	// ActionGetSystemDateAndTime is the GetSystemDateAndTime action URI.
	ActionGetSystemDateAndTime = Namespace + "/GetSystemDateAndTime"
	// ActionGetDeviceInformation is the GetDeviceInformation action URI.
	ActionGetDeviceInformation = Namespace + "/GetDeviceInformation"
	// ActionGetCapabilities is the GetCapabilities action URI.
	ActionGetCapabilities = Namespace + "/GetCapabilities"
	// ActionGetServices is the GetServices action URI.
	ActionGetServices = Namespace + "/GetServices"
	// ActionGetScopes is the GetScopes action URI.
	ActionGetScopes = Namespace + "/GetScopes"
	// ActionGetHostname is the GetHostname action URI.
	ActionGetHostname = Namespace + "/GetHostname"
	// ActionGetNetworkInterfaces is the GetNetworkInterfaces action URI.
	ActionGetNetworkInterfaces = Namespace + "/GetNetworkInterfaces"
)
