package store

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/addodelgrossi/ovm/pkg/onvif/discovery"
)

const (
	maxDevices        = 10_000
	maxIdentifierSize = 128
	maxEndpointSize   = 4_096
	maxEPRSize        = 4_096
	maxDisplayName    = 512
	maxInterfaceName  = 512
	maxSourceSize     = 64
)

var safeIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type repositoryState struct {
	devices     []Device
	preferences Preferences
}

func defaultState() repositoryState {
	return repositoryState{devices: []Device{}, preferences: DefaultPreferences()}
}

func cloneState(value repositoryState) repositoryState {
	devices := make([]Device, len(value.devices))
	copy(devices, value.devices)
	return repositoryState{devices: devices, preferences: value.preferences}
}

func normalizeState(value *repositoryState) {
	if value.devices == nil {
		value.devices = []Device{}
	}
	sort.Slice(value.devices, func(i, j int) bool {
		return value.devices[i].ID < value.devices[j].ID
	})
}

func canonicalizeDeviceForUpsert(device Device) (Device, error) {
	if device.ID != "" && !validIdentifier(device.ID) {
		return Device{}, newError(CodeInvalidData, "the device ID is invalid")
	}
	if len(device.Endpoint) > maxEndpointSize {
		return Device{}, newError(CodeInvalidData, "the device endpoint is invalid")
	}
	endpoint, err := discovery.CanonicalizeEndpoint(device.Endpoint)
	if err != nil {
		return Device{}, newError(CodeInvalidData, "the device endpoint is invalid")
	}
	device.Endpoint = endpoint.String()
	device.EPR = strings.TrimSpace(device.EPR)
	device.DisplayName = strings.TrimSpace(device.DisplayName)
	device.DiscoverySource = DiscoverySource(strings.TrimSpace(string(device.DiscoverySource)))
	if err := validateDeviceMetadata(device); err != nil {
		return Device{}, err
	}
	return device, nil
}

func validateState(value repositoryState) error {
	if len(value.devices) > maxDevices {
		return newError(CodeInvalidData, "the store contains too many devices")
	}

	ids := make(map[string]struct{}, len(value.devices))
	endpoints := make(map[string]struct{}, len(value.devices))
	eprs := make(map[string]struct{}, len(value.devices))
	references := make(map[string]struct{}, len(value.devices))
	for _, device := range value.devices {
		if err := validateDevice(device); err != nil {
			return err
		}
		if _, exists := ids[device.ID]; exists {
			return ErrDuplicate
		}
		ids[device.ID] = struct{}{}
		if _, exists := endpoints[device.Endpoint]; exists {
			return ErrDuplicate
		}
		endpoints[device.Endpoint] = struct{}{}
		if device.EPR != "" {
			if _, exists := eprs[device.EPR]; exists {
				return ErrDuplicate
			}
			eprs[device.EPR] = struct{}{}
		}
		if device.CredentialReference != "" {
			if _, exists := references[device.CredentialReference]; exists {
				return ErrDuplicate
			}
			references[device.CredentialReference] = struct{}{}
		}
	}
	return validatePreferences(value.preferences, ids)
}

func validateDevice(device Device) error {
	if !validIdentifier(device.ID) {
		return newError(CodeInvalidData, "the device ID is invalid")
	}
	if len(device.Endpoint) == 0 || len(device.Endpoint) > maxEndpointSize {
		return newError(CodeInvalidData, "the device endpoint is invalid")
	}
	endpoint, err := discovery.CanonicalizeEndpoint(device.Endpoint)
	if err != nil || endpoint.String() != device.Endpoint {
		return newError(CodeInvalidData, "the device endpoint is not canonical")
	}
	if err := validateDeviceMetadata(device); err != nil {
		return err
	}
	if device.CreatedAt.IsZero() || device.UpdatedAt.IsZero() {
		return newError(CodeInvalidData, "the device timestamps are invalid")
	}
	if device.CreatedAt.Location() != timeUTC() || device.UpdatedAt.Location() != timeUTC() {
		return newError(CodeInvalidData, "the device timestamps must use UTC")
	}
	if device.UpdatedAt.Before(device.CreatedAt) {
		return newError(CodeInvalidData, "the device timestamps are inconsistent")
	}
	return nil
}

// timeUTC is isolated to keep timestamp validation explicit and testable.
func timeUTC() *time.Location {
	return time.UTC
}

func validateDeviceMetadata(device Device) error {
	if device.EPR != "" {
		if err := validateText(device.EPR, maxEPRSize); err != nil {
			return newError(CodeInvalidData, "the device endpoint reference is invalid")
		}
		parsed, err := url.Parse(device.EPR)
		if err != nil || !parsed.IsAbs() || parsed.User != nil {
			return newError(CodeInvalidData, "the device endpoint reference is invalid")
		}
	}
	if err := validateOptionalText(device.DisplayName, maxDisplayName); err != nil {
		return newError(CodeInvalidData, "the device display name is invalid")
	}
	switch device.DiscoverySource {
	case "", DiscoverySourceManual, DiscoverySourceWSDiscovery:
	default:
		return newError(CodeInvalidData, "the discovery source is invalid")
	}
	if len(device.DiscoverySource) > maxSourceSize {
		return newError(CodeInvalidData, "the discovery source is invalid")
	}
	if device.CredentialReference != "" && !validIdentifier(device.CredentialReference) {
		return newError(CodeInvalidData, "the credential reference is invalid")
	}
	if err := validateAuthPolicy(device.AuthPolicy); err != nil {
		return err
	}
	return nil
}

func validateAuthPolicy(policy AuthPolicy) error {
	if policy.AllowBasicOverHTTP && !policy.AllowBasic {
		return newError(CodeInvalidData, "plaintext HTTP Basic requires Basic authentication")
	}
	return nil
}

func validatePreferences(preferences Preferences, ids map[string]struct{}) error {
	if preferences.SelectedDeviceID != "" {
		if !validIdentifier(preferences.SelectedDeviceID) {
			return newError(CodeInvalidData, "the selected device ID is invalid")
		}
		if _, exists := ids[preferences.SelectedDeviceID]; !exists {
			return newError(CodeInvalidData, "the selected device does not exist")
		}
	}
	if err := validateOptionalText(preferences.SelectedHostInterface, maxInterfaceName); err != nil {
		return newError(CodeInvalidData, "the selected host interface is invalid")
	}
	switch preferences.Locale {
	case LocaleEnglish, LocaleBrazilianPortuguese:
	default:
		return newError(CodeInvalidData, "the locale is invalid")
	}
	switch preferences.Theme {
	case ThemeSystem, ThemeLight, ThemeDark:
	default:
		return newError(CodeInvalidData, "the theme is invalid")
	}
	return nil
}

func validIdentifier(value string) bool {
	return len(value) <= maxIdentifierSize && safeIdentifier.MatchString(value)
}

func validateOptionalText(value string, maximum int) error {
	if value == "" {
		return nil
	}
	return validateText(value, maximum)
}

func validateText(value string, maximum int) error {
	if value == "" || len(value) > maximum || !utf8.ValidString(value) {
		return ErrInvalidData
	}
	if strings.TrimSpace(value) != value {
		return ErrInvalidData
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return ErrInvalidData
		}
	}
	return nil
}
