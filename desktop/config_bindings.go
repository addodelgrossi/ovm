package main

import (
	"math"
	"strings"
	"unicode/utf8"

	core "github.com/addodelgrossi/ovm/internal/app"
)

type EncoderRequest struct {
	DeviceID     string `json:"device_id"`
	ProfileToken string `json:"profile_token"`
}

type ApplyEncoderRequest struct {
	DeviceID     string             `json:"device_id"`
	ProfileToken string             `json:"profile_token"`
	Change       core.EncoderChange `json:"change"`
}

type EncoderViewResponse struct {
	Result *core.EncoderView `json:"result,omitempty"`
	Error  *BindingError     `json:"error,omitempty"`
}

type EncoderApplyResponse struct {
	Result *core.EncoderApplyResult `json:"result,omitempty"`
	Error  *BindingError            `json:"error,omitempty"`
}

func validateProfileSelection(deviceID, profileToken string) *BindingError {
	if err := validateDeviceID(deviceID, false); err != nil {
		return err
	}
	if err := core.ValidateSnapshotProfileToken(profileToken); err != nil {
		return bindingValidation("invalid_profile_token", "the media profile token is not valid")
	}
	return nil
}

// validateEncoderChange rejects structurally invalid values; the backend
// validates them again against the camera's advertised options.
func validateEncoderChange(change core.EncoderChange) *BindingError {
	encoding := strings.TrimSpace(change.Encoding)
	if encoding == "" || encoding != change.Encoding || utf8.RuneCountInString(encoding) > 16 ||
		change.Width <= 0 || change.Height <= 0 || change.Width > 16384 || change.Height > 16384 ||
		change.FrameRate <= 0 || change.FrameRate > 1000 || math.IsNaN(change.FrameRate) ||
		change.BitrateKbps <= 0 || change.GOPLength < 0 || utf8.RuneCountInString(change.CodecProfile) > 32 {
		return bindingValidation("invalid_encoder_change", "the encoder settings are not valid")
	}
	return nil
}

// ReadEncoder returns a profile's encoder settings and the options the camera
// accepts.
func (a *App) ReadEncoder(request EncoderRequest) EncoderViewResponse {
	if err := validateProfileSelection(request.DeviceID, request.ProfileToken); err != nil {
		return EncoderViewResponse{Error: err}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return EncoderViewResponse{Error: bindingErr}
	}
	defer finish()
	view, err := manager.ReadEncoder(ctx, request.DeviceID, request.ProfileToken)
	if err != nil {
		return EncoderViewResponse{Error: bindingErrorFrom(err)}
	}
	return EncoderViewResponse{Result: &view}
}

// ApplyEncoder changes a profile's encoder and reports what the camera
// actually stored.
func (a *App) ApplyEncoder(request ApplyEncoderRequest) EncoderApplyResponse {
	if err := validateProfileSelection(request.DeviceID, request.ProfileToken); err != nil {
		return EncoderApplyResponse{Error: err}
	}
	if err := validateEncoderChange(request.Change); err != nil {
		return EncoderApplyResponse{Error: err}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return EncoderApplyResponse{Error: bindingErr}
	}
	defer finish()
	result, err := manager.ApplyEncoder(ctx, request.DeviceID, request.ProfileToken, request.Change)
	if err != nil {
		return EncoderApplyResponse{Result: &result, Error: bindingErrorFrom(err)}
	}
	return EncoderApplyResponse{Result: &result}
}

type ApplyImagingRequest struct {
	DeviceID     string                  `json:"device_id"`
	ProfileToken string                  `json:"profile_token"`
	Change       core.ImagingSettingsDTO `json:"change"`
}

type ImagingViewResponse struct {
	Result *core.ImagingView `json:"result,omitempty"`
	Error  *BindingError     `json:"error,omitempty"`
}

type ImagingApplyResponse struct {
	Result *core.ImagingApplyResult `json:"result,omitempty"`
	Error  *BindingError            `json:"error,omitempty"`
}

func validateImagingChange(change core.ImagingSettingsDTO) *BindingError {
	for _, value := range []*float64{change.Brightness, change.ColorSaturation, change.Contrast, change.Sharpness} {
		if value != nil && (math.IsNaN(*value) || math.IsInf(*value, 0)) {
			return bindingValidation("invalid_imaging_change", "the image settings are not valid")
		}
	}
	switch change.IrCutFilter {
	case "", "ON", "OFF", "AUTO":
		return nil
	default:
		return bindingValidation("invalid_imaging_change", "the image settings are not valid")
	}
}

// ReadImaging returns the image settings and options of a profile's source.
func (a *App) ReadImaging(request EncoderRequest) ImagingViewResponse {
	if err := validateProfileSelection(request.DeviceID, request.ProfileToken); err != nil {
		return ImagingViewResponse{Error: err}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return ImagingViewResponse{Error: bindingErr}
	}
	defer finish()
	view, err := manager.ReadImaging(ctx, request.DeviceID, request.ProfileToken)
	if err != nil {
		return ImagingViewResponse{Error: bindingErrorFrom(err)}
	}
	return ImagingViewResponse{Result: &view}
}

// ApplyImaging changes image settings, including IR cut, and reports what the
// camera stored.
func (a *App) ApplyImaging(request ApplyImagingRequest) ImagingApplyResponse {
	if err := validateProfileSelection(request.DeviceID, request.ProfileToken); err != nil {
		return ImagingApplyResponse{Error: err}
	}
	if err := validateImagingChange(request.Change); err != nil {
		return ImagingApplyResponse{Error: err}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return ImagingApplyResponse{Error: bindingErr}
	}
	defer finish()
	result, err := manager.ApplyImaging(ctx, request.DeviceID, request.ProfileToken, request.Change)
	if err != nil {
		return ImagingApplyResponse{Result: &result, Error: bindingErrorFrom(err)}
	}
	return ImagingApplyResponse{Result: &result}
}
