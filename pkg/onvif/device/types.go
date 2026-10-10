package device

import "time"

// SystemDateAndTime is the normalized result of GetSystemDateAndTime.
//
// ONVIF reports device time as a structured UTC date/time and an optional
// local time, together with the time configuration mode and daylight-savings
// flag. The UTC fields are normalized into a single time.Time in UTC when
// present and valid; UTCValid records whether that conversion succeeded.
type SystemDateAndTime struct {
	// DateTimeType is the device time mode, typically "Manual" or "NTP".
	DateTimeType string
	// DaylightSavings is the device daylight-savings flag.
	DaylightSavings bool
	// TimeZone is the POSIX TZ string, when the device reports one.
	TimeZone string
	// UTC is the device UTC time assembled from the structured fields. It is
	// only meaningful when UTCValid is true.
	UTC time.Time
	// UTCValid reports whether the device supplied a complete, valid UTC
	// date and time.
	UTCValid bool
}

// DeviceInformation is the normalized result of GetDeviceInformation.
type DeviceInformation struct {
	Manufacturer    string
	Model           string
	FirmwareVersion string
	SerialNumber    string
	HardwareID      string
}

// Capabilities is the normalized result of GetCapabilities. The ONVIF
// capabilities structure is large and category-optional; only the service
// endpoint addresses that this project consumes are normalized. The remaining
// capability detail is preserved as bounded raw XML for diagnostics and future
// modeling, without being interpreted here.
type Capabilities struct {
	// DeviceXAddr is the Device service endpoint, when advertised.
	DeviceXAddr string
	// MediaXAddr is the Media service endpoint, when advertised.
	MediaXAddr string
	// EventsXAddr is the Events service endpoint, when advertised.
	EventsXAddr string
	// ImagingXAddr is the Imaging service endpoint, when advertised.
	ImagingXAddr string
	// PTZXAddr is the PTZ service endpoint, when advertised.
	PTZXAddr string
	// AnalyticsXAddr is the Analytics service endpoint, when advertised.
	AnalyticsXAddr string
	// RawXML is the bounded, credential-redacted capabilities element,
	// retained because the full capability tree is not modeled here.
	RawXML []byte
}

// Service is one entry from GetServices.
//
// The advertised service address exists in two forms that must never be
// confused. XAddr is the credential- and query-redacted form, safe for
// diagnostics, presentation, logs, JSON, and fmt. The operational form, which
// retains any path and query required to actually reach the service, is held
// in an unexported field and is reachable only through the OperationalXAddr
// accessor at the protocol layer. Keeping the operational value unexported
// guarantees it can never be serialized by encoding/json or rendered by fmt's
// struct formatting, so a credential-bearing query cannot leak through a DTO
// or an error that embeds a Service.
type Service struct {
	// Namespace is the service WSDL namespace identifying the service.
	Namespace string
	// XAddr is the service endpoint address, credential- and query-redacted
	// for safe diagnostics and presentation.
	XAddr string
	// Version is the service version, when present.
	Version ServiceVersion
	// HasVersion reports whether a version element was present.
	HasVersion bool
	// CapabilitiesXML is bounded, credential-redacted raw XML of the
	// per-service Capabilities element when present; it is not interpreted here.
	CapabilitiesXML []byte

	// operationalXAddr is the exact advertised endpoint retaining the full
	// required path and any RawQuery/ForceQuery needed for actual requests. It
	// is unexported so it is never serialized by encoding/json or rendered by
	// fmt; only the OperationalXAddr accessor exposes it, and only to the
	// protocol layer that constructs requests.
	operationalXAddr string
}

// OperationalXAddr returns the exact advertised service endpoint, preserving
// the full path and any query required to reach the service. Unlike XAddr it is
// NOT redacted; callers at the protocol boundary use it to build requests and
// are responsible for never surfacing it in a DTO, an error, or a log line.
func (s Service) OperationalXAddr() string { return s.operationalXAddr }

// ServiceVersion is a major/minor service version.
type ServiceVersion struct {
	Major int
	Minor int
}

// Scope is one ONVIF scope entry from GetScopes.
type Scope struct {
	// Definition is "Fixed" or "Configurable" when the device reports it.
	Definition string
	// Item is the scope URI value.
	Item string
}

// Hostname is the normalized result of GetHostname.
type Hostname struct {
	// FromDHCP reports whether the hostname is obtained from DHCP.
	FromDHCP bool
	// Name is the configured hostname, which may be empty.
	Name string
}

// NetworkInterface is one entry from GetNetworkInterfaces. Only the commonly
// consumed fields are normalized; the full interface element is preserved as
// bounded raw XML.
type NetworkInterface struct {
	// Token is the interface reference token.
	Token string
	// Enabled reports whether the interface is enabled.
	Enabled bool
	// HasEnabled reports whether an Enabled element was present.
	HasEnabled bool
	// Name is the interface name, when reported.
	Name string
	// HwAddress is the MAC address, when reported.
	HwAddress string
	// MTU is the interface MTU, when reported and valid.
	MTU int
	// HasMTU reports whether a valid MTU was present.
	HasMTU bool
	// IPv4Enabled reports whether IPv4 is enabled, when reported.
	IPv4Enabled bool
	// HasIPv4 reports whether IPv4 configuration was present.
	HasIPv4 bool
	// DHCP reports whether IPv4 DHCP acquisition is enabled.
	DHCP bool
	// HasDHCP reports whether the required DHCP field was decoded from an IPv4 configuration.
	HasDHCP bool
	// IPv4Addresses lists normalized IPv4 address/prefix pairs.
	IPv4Addresses []IPAddress
	// RawXML is the bounded, credential-redacted interface element retained
	// for diagnostics.
	RawXML []byte
}

// IPAddress is a normalized IPv4 address with its prefix length.
type IPAddress struct {
	// Address is the dotted-decimal IPv4 address.
	Address string
	// PrefixLength is the network prefix length.
	PrefixLength int
	// FromDHCP reports whether the address came from DHCP, when reported.
	FromDHCP bool
}
