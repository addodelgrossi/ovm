package fakedevice

import "strconv"

// Inventory is the configurable synthetic device-management inventory the fake
// device reports for the typed Device operations. All values are invented and
// imply no real-camera compatibility. A zero value is replaced with defaults by
// withDefaults.
type Inventory struct {
	// Information backs GetDeviceInformation.
	Information DeviceInformationData
	// Services backs GetServices. When empty, defaults are supplied.
	Services []ServiceData
	// Scopes backs GetScopes. When empty, defaults are supplied.
	Scopes []ScopeData
	// Hostname backs GetHostname.
	Hostname HostnameData
	// Capabilities backs GetCapabilities service endpoint addresses.
	Capabilities CapabilitiesData
	// NetworkInterfaces backs GetNetworkInterfaces. When empty, a default
	// interface is supplied.
	NetworkInterfaces []NetworkInterfaceData
}

// DeviceInformationData is synthetic GetDeviceInformation data.
type DeviceInformationData struct {
	Manufacturer    string
	Model           string
	FirmwareVersion string
	SerialNumber    string
	HardwareID      string
}

// ServiceData is one synthetic GetServices entry.
type ServiceData struct {
	Namespace    string
	XAddr        string
	VersionMajor int
	VersionMinor int
}

// ScopeData is one synthetic GetScopes entry.
type ScopeData struct {
	// Definition is "Fixed" or "Configurable".
	Definition string
	Item       string
}

// HostnameData is synthetic GetHostname data.
type HostnameData struct {
	FromDHCP bool
	Name     string
}

// CapabilitiesData holds synthetic service endpoint addresses for
// GetCapabilities. The Device, Media, and Events categories are always
// rendered; the Imaging, PTZ, and Analytics categories are rendered only when
// their address is set, so a caller can exercise optional-category decoding.
type CapabilitiesData struct {
	DeviceXAddr    string
	MediaXAddr     string
	EventsXAddr    string
	ImagingXAddr   string
	PTZXAddr       string
	AnalyticsXAddr string
}

// NetworkInterfaceData is one synthetic GetNetworkInterfaces entry. A zero MTU
// and an empty IPv4Address omit the corresponding optional elements, so a
// caller can exercise optional-field decoding paths. When IPv4FromDHCP is true
// the address is reported as DHCP-derived; otherwise it is reported as manual.
type NetworkInterfaceData struct {
	Token        string
	Enabled      bool
	Name         string
	HwAddress    string
	MTU          int
	IPv4Enabled  bool
	IPv4Address  string
	IPv4Prefix   int
	IPv4FromDHCP bool
}

// withDefaults returns a copy of the inventory with empty required fields
// filled by invented, synthetic defaults. The default host and addresses avoid
// private deployment ranges rejected by the fixture provenance checks.
//
// Configurable slices (Services, Scopes, NetworkInterfaces) are deep-copied so
// the fake device owns its inventory independently of the caller's slices. A
// caller that keeps and later mutates the slice it passed to New cannot race
// with or alter the device's rendered responses.
func (inv Inventory) withDefaults() Inventory {
	if inv.Information == (DeviceInformationData{}) {
		inv.Information = DeviceInformationData{
			Manufacturer:    "OVM Synthetic",
			Model:           "FakeCam 1000",
			FirmwareVersion: "1.0.0-synthetic",
			SerialNumber:    "SN-SYNTHETIC-0001",
			HardwareID:      "HW-SYNTHETIC-0001",
		}
	}
	if inv.Capabilities == (CapabilitiesData{}) {
		inv.Capabilities = CapabilitiesData{
			DeviceXAddr:    "http://device.invalid/onvif/device_service",
			MediaXAddr:     "http://device.invalid/onvif/media_service",
			EventsXAddr:    "http://device.invalid/onvif/events_service",
			ImagingXAddr:   "http://device.invalid/onvif/imaging_service",
			PTZXAddr:       "http://device.invalid/onvif/ptz_service",
			AnalyticsXAddr: "http://device.invalid/onvif/analytics_service",
		}
	}
	if len(inv.Services) == 0 {
		inv.Services = []ServiceData{
			{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: "http://device.invalid/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
			{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: "http://device.invalid/onvif/media_service", VersionMajor: 25, VersionMinor: 6},
		}
	} else {
		inv.Services = append([]ServiceData(nil), inv.Services...)
	}
	if len(inv.Scopes) == 0 {
		inv.Scopes = []ScopeData{
			{Definition: "Fixed", Item: "onvif://www.onvif.org/type/video_encoder"},
			{Definition: "Fixed", Item: "onvif://www.onvif.org/Profile/Streaming"},
			{Definition: "Configurable", Item: "onvif://www.onvif.org/name/OVM%20Synthetic"},
		}
	} else {
		inv.Scopes = append([]ScopeData(nil), inv.Scopes...)
	}
	if inv.Hostname == (HostnameData{}) {
		inv.Hostname = HostnameData{FromDHCP: false, Name: "ovm-synthetic"}
	}
	if len(inv.NetworkInterfaces) == 0 {
		inv.NetworkInterfaces = []NetworkInterfaceData{
			{
				Token:        "eth0",
				Enabled:      true,
				Name:         "eth0",
				HwAddress:    "00:00:5E:00:53:01",
				MTU:          1500,
				IPv4Enabled:  true,
				IPv4Address:  "203.0.113.10",
				IPv4Prefix:   24,
				IPv4FromDHCP: false,
			},
		}
	} else {
		inv.NetworkInterfaces = append([]NetworkInterfaceData(nil), inv.NetworkInterfaces...)
	}
	return inv
}

// The following builders emit response payloads using the fixed tt/ter/ovm
// prefixes and the configurable operation prefix. Prefix collisions are
// prevented by config validation.

func buildGetDeviceInformation(config normalizedConfig) string {
	prefix := config.OperationPrefix
	info := config.Inventory.Information
	return `<` + prefix + `:GetDeviceInformationResponse>` +
		`<` + prefix + `:Manufacturer>` + escapeXML(info.Manufacturer) + `</` + prefix + `:Manufacturer>` +
		`<` + prefix + `:Model>` + escapeXML(info.Model) + `</` + prefix + `:Model>` +
		`<` + prefix + `:FirmwareVersion>` + escapeXML(info.FirmwareVersion) + `</` + prefix + `:FirmwareVersion>` +
		`<` + prefix + `:SerialNumber>` + escapeXML(info.SerialNumber) + `</` + prefix + `:SerialNumber>` +
		`<` + prefix + `:HardwareId>` + escapeXML(info.HardwareID) + `</` + prefix + `:HardwareId>` +
		`</` + prefix + `:GetDeviceInformationResponse>`
}

func buildGetCapabilities(config normalizedConfig) string {
	prefix := config.OperationPrefix
	caps := config.Inventory.Capabilities
	category := func(name, xaddr string) string {
		if xaddr == "" {
			return ""
		}
		return `<tt:` + name + `><tt:XAddr>` + escapeXML(xaddr) + `</tt:XAddr></tt:` + name + `>`
	}
	// Categories are emitted in the order the tt:Capabilities complex type
	// declares them in the ONVIF schema (onvif.xsd): Analytics, Device, Events,
	// Imaging, Media, PTZ, then Extension. A conforming decoder relies on this
	// xsd:sequence order, so the fake device must honor it even though only a
	// subset of categories is modeled.
	return `<` + prefix + `:GetCapabilitiesResponse><` + prefix + `:Capabilities>` +
		category("Analytics", caps.AnalyticsXAddr) +
		category("Device", caps.DeviceXAddr) +
		category("Events", caps.EventsXAddr) +
		category("Imaging", caps.ImagingXAddr) +
		category("Media", caps.MediaXAddr) +
		category("PTZ", caps.PTZXAddr) +
		`</` + prefix + `:Capabilities></` + prefix + `:GetCapabilitiesResponse>`
}

func buildGetServices(config normalizedConfig) string {
	prefix := config.OperationPrefix
	var body string
	for _, svc := range config.Inventory.Services {
		body += `<` + prefix + `:Service>` +
			`<` + prefix + `:Namespace>` + escapeXML(svc.Namespace) + `</` + prefix + `:Namespace>` +
			`<` + prefix + `:XAddr>` + escapeXML(svc.XAddr) + `</` + prefix + `:XAddr>` +
			`<` + prefix + `:Version><tt:Major>` + strconv.Itoa(svc.VersionMajor) + `</tt:Major><tt:Minor>` + strconv.Itoa(svc.VersionMinor) + `</tt:Minor></` + prefix + `:Version>` +
			`</` + prefix + `:Service>`
	}
	return `<` + prefix + `:GetServicesResponse>` + body + `</` + prefix + `:GetServicesResponse>`
}

func buildGetScopes(config normalizedConfig) string {
	prefix := config.OperationPrefix
	var body string
	for _, scope := range config.Inventory.Scopes {
		body += `<` + prefix + `:Scopes>` +
			`<tt:ScopeDef>` + escapeXML(scope.Definition) + `</tt:ScopeDef>` +
			`<tt:ScopeItem>` + escapeXML(scope.Item) + `</tt:ScopeItem>` +
			`</` + prefix + `:Scopes>`
	}
	return `<` + prefix + `:GetScopesResponse>` + body + `</` + prefix + `:GetScopesResponse>`
}

func buildGetHostname(config normalizedConfig) string {
	prefix := config.OperationPrefix
	host := config.Inventory.Hostname
	name := ""
	if host.Name != "" {
		name = `<tt:Name>` + escapeXML(host.Name) + `</tt:Name>`
	}
	return `<` + prefix + `:GetHostnameResponse><` + prefix + `:HostnameInformation>` +
		`<tt:FromDHCP>` + strconv.FormatBool(host.FromDHCP) + `</tt:FromDHCP>` +
		name +
		`</` + prefix + `:HostnameInformation></` + prefix + `:GetHostnameResponse>`
}

func buildGetNetworkInterfaces(config normalizedConfig) string {
	prefix := config.OperationPrefix
	var body string
	for _, iface := range config.Inventory.NetworkInterfaces {
		mtu := ""
		if iface.MTU > 0 {
			mtu = `<tt:MTU>` + strconv.Itoa(iface.MTU) + `</tt:MTU>`
		}
		ipv4 := ""
		if iface.IPv4Address != "" {
			// The ONVIF IPv4Configuration reports DHCP-derived addresses under
			// <tt:FromDHCP> and statically configured ones under <tt:Manual>,
			// while the <tt:DHCP> flag records the acquisition mode. Emit the
			// address under the element matching IPv4FromDHCP so a decoder sees
			// honest provenance instead of every address appearing manual.
			addrBody := `<tt:Address>` + escapeXML(iface.IPv4Address) + `</tt:Address><tt:PrefixLength>` + strconv.Itoa(iface.IPv4Prefix) + `</tt:PrefixLength>`
			addressBlock := `<tt:Manual>` + addrBody + `</tt:Manual>`
			if iface.IPv4FromDHCP {
				addressBlock = `<tt:FromDHCP>` + addrBody + `</tt:FromDHCP>`
			}
			ipv4 = `<tt:IPv4>` +
				`<tt:Enabled>` + strconv.FormatBool(iface.IPv4Enabled) + `</tt:Enabled>` +
				`<tt:Config>` +
				addressBlock +
				`<tt:DHCP>` + strconv.FormatBool(iface.IPv4FromDHCP) + `</tt:DHCP>` +
				`</tt:Config>` +
				`</tt:IPv4>`
		}
		body += `<` + prefix + `:NetworkInterfaces token="` + escapeXML(iface.Token) + `">` +
			`<tt:Enabled>` + strconv.FormatBool(iface.Enabled) + `</tt:Enabled>` +
			`<tt:Info>` +
			`<tt:Name>` + escapeXML(iface.Name) + `</tt:Name>` +
			`<tt:HwAddress>` + escapeXML(iface.HwAddress) + `</tt:HwAddress>` +
			mtu +
			`</tt:Info>` +
			ipv4 +
			`</` + prefix + `:NetworkInterfaces>`
	}
	return `<` + prefix + `:GetNetworkInterfacesResponse>` + body + `</` + prefix + `:GetNetworkInterfacesResponse>`
}
