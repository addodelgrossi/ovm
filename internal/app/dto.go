package app

// CapabilityState is the normalized support state of a single Device
// operation. The four values are exhaustive and stable; adapters must map every
// outcome to exactly one of them.
type CapabilityState string

const (
	// CapabilitySupported means the operation completed successfully.
	CapabilitySupported CapabilityState = "supported"
	// CapabilityUnsupported means the device reported ActionNotSupported.
	CapabilityUnsupported CapabilityState = "unsupported"
	// CapabilityUnknown means the result was ambiguous: an authentication,
	// protocol, or decode problem that does not prove support either way.
	CapabilityUnknown CapabilityState = "unknown"
	// CapabilityTemporarilyUnavailable means a transient network or timeout
	// condition prevented a conclusive result.
	CapabilityTemporarilyUnavailable CapabilityState = "temporarily_unavailable"
)

// OperationName is the stable, machine-readable name of a modeled Device
// operation. The seven operations are reported in a deterministic order.
type OperationName string

const (
	// OpSystemDateAndTime is GetSystemDateAndTime.
	OpSystemDateAndTime OperationName = "system_date_and_time"
	// OpDeviceInformation is GetDeviceInformation.
	OpDeviceInformation OperationName = "device_information"
	// OpCapabilities is GetCapabilities.
	OpCapabilities OperationName = "capabilities"
	// OpServices is GetServices.
	OpServices OperationName = "services"
	// OpScopes is GetScopes.
	OpScopes OperationName = "scopes"
	// OpHostname is GetHostname.
	OpHostname OperationName = "hostname"
	// OpNetworkInterfaces is GetNetworkInterfaces.
	OpNetworkInterfaces OperationName = "network_interfaces"
)

// orderedOperations is the deterministic order in which inventory operations
// are reported and (for independent calls) scheduled.
var orderedOperations = []OperationName{
	OpSystemDateAndTime,
	OpDeviceInformation,
	OpCapabilities,
	OpServices,
	OpScopes,
	OpHostname,
	OpNetworkInterfaces,
}

// OperationStatus reports the capability state of one Device operation and a
// safe, English, credential-free detail describing the outcome. Detail is
// present for non-supported states and is empty for a clean success.
type OperationStatus struct {
	// Operation is the stable operation name.
	Operation OperationName `json:"operation"`
	// State is the normalized capability state.
	State CapabilityState `json:"state"`
	// Detail is a safe, human-readable explanation for a non-supported state.
	Detail string `json:"detail,omitempty"`
}

// DiscoveredDevice is a normalized WS-Discovery result. It is credential-free
// and carries no raw datagram bytes.
type DiscoveredDevice struct {
	// EndpointReference is the stable WS-Addressing identity of the device.
	EndpointReference string `json:"endpoint_reference"`
	// Addresses holds the merged, deduplicated, sorted transport addresses.
	Addresses []string `json:"addresses"`
	// Scopes holds the merged, deduplicated, sorted scope URIs.
	Scopes []string `json:"scopes,omitempty"`
	// Types holds the merged, deduplicated, sorted advertised types.
	Types []string `json:"types,omitempty"`
	// MetadataVersion is the highest metadata version observed, as reported.
	MetadataVersion string `json:"metadata_version,omitempty"`
}

// DiscoveryDiagnostic is a stable, machine-readable explanation for an empty or
// failed discovery round. It never implies a permission problem for a plain
// timeout.
type DiscoveryDiagnostic struct {
	// Category is the stable diagnostic category.
	Category string `json:"category"`
	// Detail is a safe, human-readable explanation.
	Detail string `json:"detail,omitempty"`
}

// DiscoveryResult is the normalized outcome of a discovery round.
type DiscoveryResult struct {
	// Devices holds the discovered devices in deterministic order.
	Devices []DiscoveredDevice `json:"devices"`
	// Diagnostic explains an empty round. It is omitted when devices were
	// found.
	Diagnostic *DiscoveryDiagnostic `json:"diagnostic,omitempty"`
}

// SystemDateAndTimeDTO is the normalized GetSystemDateAndTime payload.
type SystemDateAndTimeDTO struct {
	// DateTimeType is the device time mode, for example "Manual" or "NTP".
	DateTimeType string `json:"date_time_type,omitempty"`
	// DaylightSavings is the device daylight-savings flag.
	DaylightSavings bool `json:"daylight_savings"`
	// TimeZone is the POSIX TZ string, when reported.
	TimeZone string `json:"time_zone,omitempty"`
	// UTC is the device UTC time in RFC3339, present only when the device
	// supplied a complete, valid value.
	UTC string `json:"utc,omitempty"`
}

// DeviceInformationDTO is the normalized GetDeviceInformation payload.
type DeviceInformationDTO struct {
	Manufacturer    string `json:"manufacturer,omitempty"`
	Model           string `json:"model,omitempty"`
	FirmwareVersion string `json:"firmware_version,omitempty"`
	SerialNumber    string `json:"serial_number,omitempty"`
	HardwareID      string `json:"hardware_id,omitempty"`
}

// CapabilitiesDTO is the normalized GetCapabilities service endpoint set. Raw
// capability XML is deliberately not exposed.
type CapabilitiesDTO struct {
	DeviceXAddr    string `json:"device_xaddr,omitempty"`
	MediaXAddr     string `json:"media_xaddr,omitempty"`
	EventsXAddr    string `json:"events_xaddr,omitempty"`
	ImagingXAddr   string `json:"imaging_xaddr,omitempty"`
	PTZXAddr       string `json:"ptz_xaddr,omitempty"`
	AnalyticsXAddr string `json:"analytics_xaddr,omitempty"`
}

// ServiceDTO is one normalized GetServices entry.
type ServiceDTO struct {
	Namespace    string `json:"namespace,omitempty"`
	XAddr        string `json:"xaddr,omitempty"`
	VersionMajor int    `json:"version_major,omitempty"`
	VersionMinor int    `json:"version_minor,omitempty"`
	HasVersion   bool   `json:"has_version"`
}

// ScopeDTO is one normalized GetScopes entry.
type ScopeDTO struct {
	Definition string `json:"definition,omitempty"`
	Item       string `json:"item,omitempty"`
}

// HostnameDTO is the normalized GetHostname payload.
type HostnameDTO struct {
	FromDHCP bool   `json:"from_dhcp"`
	Name     string `json:"name,omitempty"`
}

// IPAddressDTO is a normalized IPv4 address with its prefix length.
type IPAddressDTO struct {
	Address      string `json:"address,omitempty"`
	PrefixLength int    `json:"prefix_length,omitempty"`
	FromDHCP     bool   `json:"from_dhcp"`
}

// NetworkInterfaceDTO is one normalized GetNetworkInterfaces entry. Raw
// interface XML is deliberately not exposed.
type NetworkInterfaceDTO struct {
	Token         string         `json:"token,omitempty"`
	Enabled       bool           `json:"enabled"`
	HasEnabled    bool           `json:"has_enabled"`
	Name          string         `json:"name,omitempty"`
	HwAddress     string         `json:"hw_address,omitempty"`
	MTU           int            `json:"mtu,omitempty"`
	HasMTU        bool           `json:"has_mtu"`
	IPv4Enabled   bool           `json:"ipv4_enabled"`
	HasIPv4       bool           `json:"has_ipv4"`
	DHCP          bool           `json:"dhcp"`
	HasDHCP       bool           `json:"has_dhcp"`
	IPv4Addresses []IPAddressDTO `json:"ipv4_addresses,omitempty"`
}

// Inventory is the normalized, credential-free result of a device-inventory
// use case. Each payload pointer is non-nil only when its operation succeeded;
// Operations always reports a state for every one of the seven operations in
// deterministic order, enabling useful partial results.
type Inventory struct {
	// Endpoint is the canonical device service endpoint that was queried.
	Endpoint string `json:"endpoint"`
	// Operations reports the capability state of every operation in order.
	Operations []OperationStatus `json:"operations"`

	// SystemDateAndTime is present only when its operation succeeded.
	SystemDateAndTime *SystemDateAndTimeDTO `json:"system_date_and_time,omitempty"`
	// DeviceInformation is present only when its operation succeeded.
	DeviceInformation *DeviceInformationDTO `json:"device_information,omitempty"`
	// Capabilities is present only when its operation succeeded.
	Capabilities *CapabilitiesDTO `json:"capabilities,omitempty"`
	// Services is present only when its operation succeeded (possibly empty).
	Services []ServiceDTO `json:"services,omitempty"`
	// Scopes is present only when its operation succeeded (possibly empty).
	Scopes []ScopeDTO `json:"scopes,omitempty"`
	// Hostname is present only when its operation succeeded.
	Hostname *HostnameDTO `json:"hostname,omitempty"`
	// NetworkInterfaces is present only when its operation succeeded.
	NetworkInterfaces []NetworkInterfaceDTO `json:"network_interfaces,omitempty"`
}
