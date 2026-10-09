package device

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// preservedDiagnosticXML redacts a complete element before applying the final
// retention bound. The order is security-sensitive: truncating first could cut
// inside a sensitive opening tag and prevent the lexical redactor from seeing
// its closing delimiter.
func preservedDiagnosticXML(element *element, payload []byte) []byte {
	redacted := soap.RedactXML(element.outerXML(payload))
	if len(redacted) > maxPreservedExtensionBytes {
		redacted = redacted[:maxPreservedExtensionBytes]
	}
	return append([]byte(nil), redacted...)
}

// decodeSystemDateAndTime normalizes a GetSystemDateAndTimeResponse against the
// ver10 Device WSDL and shared schema. SystemDateAndTime, DateTimeType, and
// DaylightSavings are required. UTCDateTime remains optional in the shared XSD
// for legacy peers, but when present it must be complete and valid.
func decodeSystemDateAndTime(root *element) (SystemDateAndTime, error) {
	if err := requireRoot(root, "GetSystemDateAndTimeResponse"); err != nil {
		return SystemDateAndTime{}, err
	}
	sdt, err := requiredElement(root, Namespace, "SystemDateAndTime")
	if err != nil {
		return SystemDateAndTime{}, err
	}

	dateTimeType, err := requiredText(sdt, SchemaNamespace, "DateTimeType")
	if err != nil {
		return SystemDateAndTime{}, err
	}
	if dateTimeType != "Manual" && dateTimeType != "NTP" {
		return SystemDateAndTime{}, fmt.Errorf("%w: DateTimeType", ErrInvalidField)
	}
	dstText, err := requiredText(sdt, SchemaNamespace, "DaylightSavings")
	if err != nil {
		return SystemDateAndTime{}, err
	}
	dst, err := parseBool(dstText)
	if err != nil {
		return SystemDateAndTime{}, fmt.Errorf("%w: DaylightSavings", ErrInvalidField)
	}

	result := SystemDateAndTime{DateTimeType: dateTimeType, DaylightSavings: dst}
	if tz, err := optionalElement(sdt, SchemaNamespace, "TimeZone"); err != nil {
		return SystemDateAndTime{}, err
	} else if tz != nil {
		value, textErr := requiredText(tz, SchemaNamespace, "TZ")
		if textErr != nil {
			return SystemDateAndTime{}, textErr
		}
		result.TimeZone = value
	}
	if utc, err := optionalElement(sdt, SchemaNamespace, "UTCDateTime"); err != nil {
		return SystemDateAndTime{}, err
	} else if utc != nil {
		parsed, parseErr := parseONVIFDateTime(utc)
		if parseErr != nil {
			return SystemDateAndTime{}, parseErr
		}
		result.UTC = parsed
		result.UTCValid = true
	}
	// LocalDateTime is intentionally not normalized yet because a time.Time
	// cannot represent the device's local wall clock without a reliable zone.
	// Its singleton cardinality is still validated when it is present.
	if _, err := optionalElement(sdt, SchemaNamespace, "LocalDateTime"); err != nil {
		return SystemDateAndTime{}, err
	}
	return result, nil
}

// parseONVIFDateTime assembles a UTC time from an ONVIF DateTime element. A
// missing component, duplicate singleton, or impossible calendar value is
// rejected so normalization cannot turn invalid input into another instant.
func parseONVIFDateTime(dt *element) (time.Time, error) {
	timeNode, err := requiredElement(dt, SchemaNamespace, "Time")
	if err != nil {
		return time.Time{}, err
	}
	dateNode, err := requiredElement(dt, SchemaNamespace, "Date")
	if err != nil {
		return time.Time{}, err
	}
	hour, err := requireChildInt(timeNode, "Hour")
	if err != nil {
		return time.Time{}, err
	}
	minute, err := requireChildInt(timeNode, "Minute")
	if err != nil {
		return time.Time{}, err
	}
	second, err := requireChildInt(timeNode, "Second")
	if err != nil {
		return time.Time{}, err
	}
	year, err := requireChildInt(dateNode, "Year")
	if err != nil {
		return time.Time{}, err
	}
	month, err := requireChildInt(dateNode, "Month")
	if err != nil {
		return time.Time{}, err
	}
	day, err := requireChildInt(dateNode, "Day")
	if err != nil {
		return time.Time{}, err
	}
	// Go normalizes second 60 to the next minute, so reject it rather than
	// silently changing an instant the model cannot represent faithfully.
	if second < 0 || second > 59 || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return time.Time{}, fmt.Errorf("%w: UTCDateTime time of day", ErrInvalidField)
	}
	if month < 1 || month > 12 || day < 1 || day > 31 {
		return time.Time{}, fmt.Errorf("%w: UTCDateTime calendar date", ErrInvalidField)
	}
	candidate := time.Date(year, time.Month(month), day, hour, minute, second, 0, time.UTC)
	if candidate.Year() != year || int(candidate.Month()) != month || candidate.Day() != day ||
		candidate.Hour() != hour || candidate.Minute() != minute || candidate.Second() != second {
		return time.Time{}, fmt.Errorf("%w: UTCDateTime is not a valid calendar instant", ErrInvalidField)
	}
	return candidate, nil
}

func requireChildInt(parent *element, local string) (int, error) {
	value, err := requiredText(parent, SchemaNamespace, local)
	if err != nil {
		return 0, err
	}
	number, parseErr := strconv.Atoi(strings.TrimSpace(value))
	if parseErr != nil {
		return 0, fmt.Errorf("%w: %s", ErrInvalidField, local)
	}
	return number, nil
}

func decodeDeviceInformation(root *element) (DeviceInformation, error) {
	if err := requireRoot(root, "GetDeviceInformationResponse"); err != nil {
		return DeviceInformation{}, err
	}
	info := DeviceInformation{}
	fields := []struct {
		local string
		dst   *string
	}{
		{"Manufacturer", &info.Manufacturer},
		{"Model", &info.Model},
		{"FirmwareVersion", &info.FirmwareVersion},
		{"SerialNumber", &info.SerialNumber},
		{"HardwareId", &info.HardwareID},
	}
	for _, field := range fields {
		value, err := requiredText(root, Namespace, field.local)
		if err != nil {
			return DeviceInformation{}, err
		}
		*field.dst = value
	}
	return info, nil
}

func decodeCapabilities(root *element, raw []byte) (Capabilities, error) {
	if err := requireRoot(root, "GetCapabilitiesResponse"); err != nil {
		return Capabilities{}, err
	}
	caps, err := requiredElement(root, Namespace, "Capabilities")
	if err != nil {
		return Capabilities{}, err
	}
	result := Capabilities{RawXML: preservedDiagnosticXML(caps, raw)}
	categories := []struct {
		name string
		dst  *string
	}{
		{"Device", &result.DeviceXAddr},
		{"Media", &result.MediaXAddr},
		{"Events", &result.EventsXAddr},
		{"Imaging", &result.ImagingXAddr},
		{"PTZ", &result.PTZXAddr},
		{"Analytics", &result.AnalyticsXAddr},
	}
	for _, category := range categories {
		node, nodeErr := optionalElement(caps, SchemaNamespace, category.name)
		if nodeErr != nil {
			return Capabilities{}, nodeErr
		}
		if node == nil {
			continue
		}
		xaddr, textErr := requiredText(node, SchemaNamespace, "XAddr")
		if textErr != nil {
			return Capabilities{}, textErr
		}
		*category.dst = soap.RedactURL(xaddr)
	}
	return result, nil
}

func decodeServices(root *element, raw []byte) ([]Service, error) {
	if err := requireRoot(root, "GetServicesResponse"); err != nil {
		return nil, err
	}
	entries := root.childrenNamed(Namespace, "Service")
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: Service", ErrMissingField)
	}
	services := make([]Service, 0, len(entries))
	for _, entry := range entries {
		namespace, err := requiredText(entry, Namespace, "Namespace")
		if err != nil {
			return nil, err
		}
		xaddr, err := requiredText(entry, Namespace, "XAddr")
		if err != nil {
			return nil, err
		}
		version, err := requiredElement(entry, Namespace, "Version")
		if err != nil {
			return nil, err
		}
		major, err := requireChildInt(version, "Major")
		if err != nil {
			return nil, err
		}
		minor, err := requireChildInt(version, "Minor")
		if err != nil {
			return nil, err
		}
		service := Service{
			Namespace:  namespace,
			XAddr:      soap.RedactURL(xaddr),
			Version:    ServiceVersion{Major: major, Minor: minor},
			HasVersion: true,
		}
		capabilities, capabilitiesErr := optionalElement(entry, Namespace, "Capabilities")
		if capabilitiesErr != nil {
			return nil, capabilitiesErr
		}
		if capabilities != nil {
			service.CapabilitiesXML = preservedDiagnosticXML(capabilities, raw)
		}
		services = append(services, service)
	}
	return services, nil
}

func decodeScopes(root *element) ([]Scope, error) {
	if err := requireRoot(root, "GetScopesResponse"); err != nil {
		return nil, err
	}
	entries := root.childrenNamed(Namespace, "Scopes")
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: Scopes", ErrMissingField)
	}
	scopes := make([]Scope, 0, len(entries))
	for _, entry := range entries {
		definition, err := requiredText(entry, SchemaNamespace, "ScopeDef")
		if err != nil {
			return nil, err
		}
		if definition != "Fixed" && definition != "Configurable" {
			return nil, fmt.Errorf("%w: ScopeDef", ErrInvalidField)
		}
		item, err := requiredText(entry, SchemaNamespace, "ScopeItem")
		if err != nil {
			return nil, err
		}
		scopes = append(scopes, Scope{Definition: definition, Item: item})
	}
	return scopes, nil
}

func decodeHostname(root *element) (Hostname, error) {
	if err := requireRoot(root, "GetHostnameResponse"); err != nil {
		return Hostname{}, err
	}
	info, err := requiredElement(root, Namespace, "HostnameInformation")
	if err != nil {
		return Hostname{}, err
	}
	fromDHCPText, err := requiredText(info, SchemaNamespace, "FromDHCP")
	if err != nil {
		return Hostname{}, err
	}
	fromDHCP, err := parseBool(fromDHCPText)
	if err != nil {
		return Hostname{}, fmt.Errorf("%w: FromDHCP", ErrInvalidField)
	}
	result := Hostname{FromDHCP: fromDHCP}
	if name, err := optionalText(info, SchemaNamespace, "Name"); err != nil {
		return Hostname{}, err
	} else if name != nil {
		result.Name = *name
	}
	return result, nil
}

func decodeNetworkInterfaces(root *element, raw []byte) ([]NetworkInterface, error) {
	if err := requireRoot(root, "GetNetworkInterfacesResponse"); err != nil {
		return nil, err
	}
	entries := root.childrenNamed(Namespace, "NetworkInterfaces")
	if len(entries) == 0 {
		return nil, fmt.Errorf("%w: NetworkInterfaces", ErrMissingField)
	}
	interfaces := make([]NetworkInterface, 0, len(entries))
	for _, entry := range entries {
		token, ok := entry.unqualifiedAttr("token")
		if !ok {
			return nil, fmt.Errorf("%w: NetworkInterface token attribute", ErrMissingField)
		}
		enabledText, err := requiredText(entry, SchemaNamespace, "Enabled")
		if err != nil {
			return nil, err
		}
		enabled, err := parseBool(enabledText)
		if err != nil {
			return nil, fmt.Errorf("%w: NetworkInterface Enabled", ErrInvalidField)
		}
		iface := NetworkInterface{
			Token:      token,
			Enabled:    enabled,
			HasEnabled: true,
			RawXML:     preservedDiagnosticXML(entry, raw),
		}

		info, err := optionalElement(entry, SchemaNamespace, "Info")
		if err != nil {
			return nil, err
		}
		if info != nil {
			iface.Name, err = requiredText(info, SchemaNamespace, "Name")
			if err != nil {
				return nil, err
			}
			iface.HwAddress, err = requiredText(info, SchemaNamespace, "HwAddress")
			if err != nil {
				return nil, err
			}
			if mtu, textErr := optionalText(info, SchemaNamespace, "MTU"); textErr != nil {
				return nil, textErr
			} else if mtu != nil {
				parsed, parseErr := strconv.Atoi(strings.TrimSpace(*mtu))
				if parseErr != nil {
					return nil, fmt.Errorf("%w: NetworkInterface MTU", ErrInvalidField)
				}
				iface.MTU = parsed
				iface.HasMTU = true
			}
		}

		ipv4, err := optionalElement(entry, SchemaNamespace, "IPv4")
		if err != nil {
			return nil, err
		}
		if ipv4 != nil {
			iface.HasIPv4 = true
			ipv4EnabledText, textErr := requiredText(ipv4, SchemaNamespace, "Enabled")
			if textErr != nil {
				return nil, textErr
			}
			iface.IPv4Enabled, err = parseBool(ipv4EnabledText)
			if err != nil {
				return nil, fmt.Errorf("%w: IPv4 Enabled", ErrInvalidField)
			}
			config, configErr := requiredElement(ipv4, SchemaNamespace, "Config")
			if configErr != nil {
				return nil, configErr
			}
			dhcpText, dhcpErr := requiredText(config, SchemaNamespace, "DHCP")
			if dhcpErr != nil {
				return nil, dhcpErr
			}
			iface.DHCP, err = parseBool(dhcpText)
			if err != nil {
				return nil, fmt.Errorf("%w: IPv4 DHCP", ErrInvalidField)
			}
			iface.HasDHCP = true
			iface.IPv4Addresses, err = decodeIPv4Addresses(config)
			if err != nil {
				return nil, err
			}
		}
		interfaces = append(interfaces, iface)
	}
	return interfaces, nil
}

func decodeIPv4Addresses(config *element) ([]IPAddress, error) {
	var addresses []IPAddress
	appendAddress := func(node *element, fromDHCP bool) error {
		address, err := requiredText(node, SchemaNamespace, "Address")
		if err != nil {
			return err
		}
		prefixText, err := requiredText(node, SchemaNamespace, "PrefixLength")
		if err != nil {
			return err
		}
		parsedAddress, parseErr := netip.ParseAddr(strings.TrimSpace(address))
		if parseErr != nil || !parsedAddress.Is4() {
			return fmt.Errorf("%w: IPv4 Address", ErrInvalidField)
		}
		prefix, parseErr := strconv.Atoi(strings.TrimSpace(prefixText))
		if parseErr != nil || prefix < 0 || prefix > 32 {
			return fmt.Errorf("%w: IPv4 PrefixLength", ErrInvalidField)
		}
		addresses = append(addresses, IPAddress{Address: parsedAddress.String(), PrefixLength: prefix, FromDHCP: fromDHCP})
		return nil
	}
	for _, manual := range config.childrenNamed(SchemaNamespace, "Manual") {
		if err := appendAddress(manual, false); err != nil {
			return nil, err
		}
	}
	linkLocal, err := optionalElement(config, SchemaNamespace, "LinkLocal")
	if err != nil {
		return nil, err
	}
	if linkLocal != nil {
		if err := appendAddress(linkLocal, false); err != nil {
			return nil, err
		}
	}
	fromDHCP, err := optionalElement(config, SchemaNamespace, "FromDHCP")
	if err != nil {
		return nil, err
	}
	if fromDHCP != nil {
		if err := appendAddress(fromDHCP, true); err != nil {
			return nil, err
		}
	}
	return addresses, nil
}

func requiredElement(parent *element, namespace, local string) (*element, error) {
	if parent.hasDuplicateChild(namespace, local) {
		return nil, fmt.Errorf("%w: duplicate %s", ErrInvalidField, local)
	}
	child := parent.child(namespace, local)
	if child == nil {
		return nil, fmt.Errorf("%w: %s", ErrMissingField, local)
	}
	return child, nil
}

func optionalElement(parent *element, namespace, local string) (*element, error) {
	if parent.hasDuplicateChild(namespace, local) {
		return nil, fmt.Errorf("%w: duplicate %s", ErrInvalidField, local)
	}
	return parent.child(namespace, local), nil
}

func requiredText(parent *element, namespace, local string) (string, error) {
	child, err := requiredElement(parent, namespace, local)
	if err != nil {
		return "", err
	}
	return child.trimmedText(), nil
}

func optionalText(parent *element, namespace, local string) (*string, error) {
	child, err := optionalElement(parent, namespace, local)
	if err != nil || child == nil {
		return nil, err
	}
	value := child.trimmedText()
	return &value, nil
}

// parseBool parses the XML schema boolean lexical space (true/false/1/0).
func parseBool(value string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean")
	}
}
