package main

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	core "github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/pkg/onvif/discovery"
)

const (
	maxBindingIdentifierBytes = 128
	maxBindingEndpointBytes   = 4096
	maxBindingDisplayName     = 512
	maxBindingInterfaceName   = 512
	maxBindingCredentialBytes = 1024
	minNetworkTimeoutMillis   = int64(100)
	maxNetworkTimeoutMillis   = int64(60_000)
)

var bindingIdentifier = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

func validateDeviceID(value string, allowEmpty bool) *BindingError {
	if value == "" && allowEmpty {
		return nil
	}
	if len(value) == 0 || len(value) > maxBindingIdentifierBytes || !bindingIdentifier.MatchString(value) {
		return bindingValidation("invalid_device_id", "the device ID is not valid")
	}
	return nil
}

func validateScanRequest(request ScanRequest) (time.Duration, *BindingError) {
	if request.InterfaceName != "" && !validBoundaryText(request.InterfaceName, maxBindingInterfaceName, true) {
		return 0, bindingValidation("invalid_interface", "the host interface name is not valid")
	}
	return validatedTimeout(request.TimeoutMilliseconds)
}

func validatedTimeout(milliseconds int64) (time.Duration, *BindingError) {
	if milliseconds == 0 {
		return 0, nil
	}
	if milliseconds < minNetworkTimeoutMillis || milliseconds > maxNetworkTimeoutMillis {
		return 0, bindingValidation("invalid_timeout", "the timeout must be between 100 and 60000 milliseconds")
	}
	return time.Duration(milliseconds) * time.Millisecond, nil
}

func validateManualDeviceRequest(request AddManualDeviceRequest) *BindingError {
	if len(request.Endpoint) == 0 || len(request.Endpoint) > maxBindingEndpointBytes || !utf8.ValidString(request.Endpoint) {
		return bindingValidation("invalid_endpoint", "the device endpoint is not valid")
	}
	if _, err := discovery.CanonicalizeEndpoint(request.Endpoint); err != nil {
		return bindingValidation("invalid_endpoint", "the device endpoint is not valid")
	}
	if !validBoundaryText(request.DisplayName, maxBindingDisplayName, false) {
		return bindingValidation("invalid_display_name", "the device display name is not valid")
	}
	return nil
}

func validateAuthenticateDeviceRequest(request AuthenticateDeviceRequest) *BindingError {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return err
	}
	if !validBoundaryText(request.Username, maxBindingCredentialBytes, true) {
		return bindingValidation("invalid_credentials", "the credentials are not valid")
	}
	if len(request.Password) > maxBindingCredentialBytes || !utf8.ValidString(request.Password) {
		return bindingValidation("invalid_credentials", "the credentials are not valid")
	}
	if request.AllowBasicOverHTTP && !request.AllowBasic {
		return bindingValidation("invalid_auth_policy", "plaintext HTTP Basic requires Basic authentication")
	}
	_, err := validatedTimeout(request.TimeoutMilliseconds)
	return err
}

func validateUpdatePreferencesRequest(request UpdatePreferencesRequest) *BindingError {
	if request.Locale == nil && request.Theme == nil && request.SelectedHostInterface == nil {
		return bindingValidation("empty_preferences", "at least one preference is required")
	}
	if request.Locale != nil && *request.Locale != "en" && *request.Locale != "pt-BR" {
		return bindingValidation("invalid_locale", "the locale is not supported")
	}
	if request.Theme != nil && *request.Theme != "system" && *request.Theme != "light" && *request.Theme != "dark" {
		return bindingValidation("invalid_theme", "the theme is not supported")
	}
	if request.SelectedHostInterface != nil && !validBoundaryText(*request.SelectedHostInterface, maxBindingInterfaceName, false) {
		return bindingValidation("invalid_interface", "the host interface name is not valid")
	}
	return nil
}

func validateOpenInVLCRequest(request OpenInVLCRequest) (core.StreamSelection, *BindingError) {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return core.StreamSelection{}, err
	}
	if err := core.ValidateSnapshotProfileToken(request.ProfileToken); err != nil {
		return core.StreamSelection{}, bindingValidation("invalid_profile_token", "the media profile token is not valid")
	}
	transport := core.StreamTransport(request.Transport)
	if transport != core.StreamTransportTCP && transport != core.StreamTransportUDP {
		return core.StreamSelection{}, bindingValidation("invalid_transport", "the stream transport is not supported")
	}
	return core.StreamSelection{
		DeviceID:     request.DeviceID,
		ProfileToken: request.ProfileToken,
		Transport:    transport,
	}, nil
}

func validateStartPreviewRequest(request StartPreviewRequest) (core.StreamSelection, *BindingError) {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return core.StreamSelection{}, err
	}
	if err := core.ValidateSnapshotProfileToken(request.ProfileToken); err != nil {
		return core.StreamSelection{}, bindingValidation("invalid_profile_token", "the media profile token is not valid")
	}
	return core.StreamSelection{
		DeviceID:     request.DeviceID,
		ProfileToken: request.ProfileToken,
		Transport:    core.StreamTransportTCP,
	}, nil
}

// validPreviewSessionID accepts the bridge's 32-character lowercase hex IDs.
func validPreviewSessionID(value string) bool {
	if len(value) != 32 {
		return false
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func validBoundaryText(value string, maximum int, required bool) bool {
	if value == "" {
		return !required
	}
	if len(value) > maximum || !utf8.ValidString(value) || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x20 || (character >= 0x7f && character <= 0x9f) || character == utf8.RuneError {
			return false
		}
	}
	return true
}
