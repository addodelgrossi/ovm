package main

import (
	"context"
	"errors"

	core "github.com/addodelgrossi/ovm/internal/app"
)

// BindingError is the only error shape returned by exported desktop methods.
// Its fields are stable, English, and safe to render. Backend causes and input
// values never cross the Wails boundary.
type BindingError struct {
	Category core.ErrorCategory `json:"category"`
	Code     string             `json:"code"`
	Message  string             `json:"message"`
}

// CapabilityFlags describes optional desktop behavior without probing a
// camera or secure storage.
type CapabilityFlags struct {
	Demo                  bool `json:"demo"`
	RTSPPlaybackAvailable bool `json:"rtsp_playback_available"`
	VLCAvailable          bool `json:"vlc_available"`
	LivePreviewAvailable  bool `json:"live_preview_available"`
}

type BootstrapResult struct {
	Overview     core.Overview   `json:"overview"`
	Capabilities CapabilityFlags `json:"capabilities"`
}

type BootstrapResponse struct {
	Result *BootstrapResult `json:"result,omitempty"`
	Error  *BindingError    `json:"error,omitempty"`
}

type ListInterfacesRequest struct {
	IncludeLoopback bool `json:"include_loopback"`
}

type ListInterfacesResponse struct {
	Result *core.HostInterfacesResult `json:"result,omitempty"`
	Error  *BindingError              `json:"error,omitempty"`
}

type ScanRequest struct {
	InterfaceName       string `json:"interface_name"`
	TimeoutMilliseconds int64  `json:"timeout_milliseconds"`
}

type ScanResponse struct {
	Result *core.ScanResult `json:"result,omitempty"`
	Error  *BindingError    `json:"error,omitempty"`
}

type ActionResult struct {
	Completed bool `json:"completed"`
}

type CancelScanResult struct {
	Cancelled bool `json:"cancelled"`
}

type ActionResponse struct {
	Result *ActionResult `json:"result,omitempty"`
	Error  *BindingError `json:"error,omitempty"`
}

type CancelScanResponse struct {
	Result *CancelScanResult `json:"result,omitempty"`
	Error  *BindingError     `json:"error,omitempty"`
}

type AddManualDeviceRequest struct {
	Endpoint    string `json:"endpoint"`
	DisplayName string `json:"display_name"`
}

type DeviceResponse struct {
	Result *core.DeviceDTO `json:"result,omitempty"`
	Error  *BindingError   `json:"error,omitempty"`
}

type AuthenticateDeviceRequest struct {
	DeviceID            string `json:"device_id"`
	Username            string `json:"username"`
	Password            string `json:"password"`
	AllowBasic          bool   `json:"allow_basic"`
	AllowBasicOverHTTP  bool   `json:"allow_basic_over_http"`
	AllowLegacyDigest   bool   `json:"allow_legacy_digest"`
	TimeoutMilliseconds int64  `json:"timeout_milliseconds"`
}

type AuthenticateDeviceResponse struct {
	Result *core.AuthenticateResult `json:"result,omitempty"`
	Error  *BindingError            `json:"error,omitempty"`
}

type DeviceIDRequest struct {
	DeviceID string `json:"device_id"`
}

type OverviewResponse struct {
	Result *core.Overview `json:"result,omitempty"`
	Error  *BindingError  `json:"error,omitempty"`
}

type LoadProfilesRequest struct {
	DeviceID string `json:"device_id"`
}

type LoadProfilesResponse struct {
	Result *core.MediaProfilesResult `json:"result,omitempty"`
	Error  *BindingError             `json:"error,omitempty"`
}

type UpdatePreferencesRequest struct {
	Locale                *string `json:"locale,omitempty"`
	Theme                 *string `json:"theme,omitempty"`
	SelectedHostInterface *string `json:"selected_host_interface,omitempty"`
}

type OpenInVLCRequest struct {
	DeviceID     string `json:"device_id"`
	ProfileToken string `json:"profile_token"`
	Transport    string `json:"transport"`
}

type StartPreviewRequest struct {
	DeviceID     string `json:"device_id"`
	ProfileToken string `json:"profile_token"`
}

// StartPreviewResult tells the WebView where to fetch the fragmented MP4
// stream. The URL is loopback-only and the token is required as a bearer
// credential; neither carries camera credentials.
type StartPreviewResult struct {
	SessionID string `json:"session_id"`
	URL       string `json:"url"`
	Token     string `json:"token"`
}

type StartPreviewResponse struct {
	Result *StartPreviewResult `json:"result,omitempty"`
	Error  *BindingError       `json:"error,omitempty"`
}

type StopPreviewRequest struct {
	SessionID string `json:"session_id"`
}

type PreviewStatsRequest struct {
	SessionID string `json:"session_id"`
}

// PreviewStats are live measurements of a preview session. Durations are in
// milliseconds for the frontend.
type PreviewStats struct {
	Width                  int     `json:"width"`
	Height                 int     `json:"height"`
	FramesPerSecond        float64 `json:"frames_per_second"`
	BitrateKbps            float64 `json:"bitrate_kbps"`
	GOPFrames              int     `json:"gop_frames"`
	GOPSeconds             float64 `json:"gop_seconds"`
	TimeToFirstFrameMillis int64   `json:"time_to_first_frame_ms"`
	Frames                 uint64  `json:"frames"`
	Keyframes              uint64  `json:"keyframes"`
	LostPackets            uint64  `json:"lost_packets"`
	ElapsedMillis          int64   `json:"elapsed_ms"`
	FirstFrameReceived     bool    `json:"first_frame_received"`
}

type PreviewStatsResponse struct {
	Result *PreviewStats `json:"result,omitempty"`
	Error  *BindingError `json:"error,omitempty"`
}

// Bootstrap loads the persisted catalog and returns immutable desktop
// capabilities. Constructing and calling it does not access secure storage.
func (a *App) Bootstrap() BootstrapResponse {
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return BootstrapResponse{Error: bindingErr}
	}
	defer finish()

	overview, err := manager.Bootstrap(ctx)
	if err != nil {
		return BootstrapResponse{Error: bindingErrorFrom(err)}
	}
	return BootstrapResponse{Result: &BootstrapResult{Overview: overview, Capabilities: a.caps}}
}

// ListInterfaces returns the backend's deterministic host-interface assessment.
func (a *App) ListInterfaces(request ListInterfacesRequest) ListInterfacesResponse {
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return ListInterfacesResponse{Error: bindingErr}
	}
	defer finish()

	result, err := manager.ListHostInterfaces(ctx, core.HostInterfaceRequest{IncludeLoopback: request.IncludeLoopback})
	if err != nil {
		return ListInterfacesResponse{Error: bindingErrorFrom(err)}
	}
	return ListInterfacesResponse{Result: &result}
}

// Scan validates its boundary request, installs an independently cancellable
// scan child, and delegates discovery and reconciliation to the Manager.
func (a *App) Scan(request ScanRequest) ScanResponse {
	timeout, validationErr := validateScanRequest(request)
	if validationErr != nil {
		return ScanResponse{Error: validationErr}
	}
	manager, callCtx, finishCall, bindingErr := a.beginCall()
	if bindingErr != nil {
		return ScanResponse{Error: bindingErr}
	}
	defer finishCall()

	scanCtx, _, finishScan := a.installScan(callCtx)
	defer finishScan()
	result, err := manager.Scan(scanCtx, core.DiscoveryRequest{
		InterfaceName: request.InterfaceName,
		Timeout:       timeout,
	})
	if err != nil {
		return ScanResponse{Result: &result, Error: bindingErrorFrom(err)}
	}
	return ScanResponse{Result: &result}
}

// CancelScan idempotently cancels the active scan, if one exists.
func (a *App) CancelScan() CancelScanResponse {
	_, _, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return CancelScanResponse{Error: bindingErr}
	}
	defer finish()

	a.mu.Lock()
	scan := a.scan
	a.mu.Unlock()
	if scan != nil {
		scan.cancel()
	}
	return CancelScanResponse{Result: &CancelScanResult{Cancelled: scan != nil}}
}

func (a *App) AddManualDevice(request AddManualDeviceRequest) DeviceResponse {
	if validationErr := validateManualDeviceRequest(request); validationErr != nil {
		return DeviceResponse{Error: validationErr}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return DeviceResponse{Error: bindingErr}
	}
	defer finish()

	result, err := manager.AddManualDevice(ctx, core.ManualDeviceRequest{
		Endpoint:    request.Endpoint,
		DisplayName: request.DisplayName,
	})
	if err != nil {
		return DeviceResponse{Error: bindingErrorFrom(err)}
	}
	return DeviceResponse{Result: &result}
}

func (a *App) AuthenticateDevice(request AuthenticateDeviceRequest) AuthenticateDeviceResponse {
	// Clear this binding-local copy on every return path. The Manager receives a
	// short-lived value and neither layer stores it after this synchronous call.
	defer func() { request.Password = "" }()
	if validationErr := validateAuthenticateDeviceRequest(request); validationErr != nil {
		return AuthenticateDeviceResponse{Error: validationErr}
	}
	timeout, _ := validatedTimeout(request.TimeoutMilliseconds)
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return AuthenticateDeviceResponse{Error: bindingErr}
	}
	defer finish()

	managerRequest := core.AuthenticateRequest{
		DeviceID:           request.DeviceID,
		Username:           request.Username,
		Password:           request.Password,
		AllowBasic:         request.AllowBasic,
		AllowBasicOverHTTP: request.AllowBasicOverHTTP,
		AllowLegacyDigest:  request.AllowLegacyDigest,
		Timeout:            int64(timeout),
	}
	result, err := manager.AuthenticateDevice(ctx, managerRequest)
	managerRequest.Password = ""
	request.Password = ""
	if err != nil {
		return AuthenticateDeviceResponse{Result: &result, Error: bindingErrorFrom(err)}
	}
	return AuthenticateDeviceResponse{Result: &result}
}

func (a *App) ForgetDevice(request DeviceIDRequest) OverviewResponse {
	if validationErr := validateDeviceID(request.DeviceID, false); validationErr != nil {
		return OverviewResponse{Error: validationErr}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return OverviewResponse{Error: bindingErr}
	}
	defer finish()

	result, err := manager.ForgetDevice(ctx, request.DeviceID)
	if err != nil {
		return OverviewResponse{Error: bindingErrorFrom(err)}
	}
	return OverviewResponse{Result: &result}
}

func (a *App) SelectDevice(request DeviceIDRequest) OverviewResponse {
	if validationErr := validateDeviceID(request.DeviceID, true); validationErr != nil {
		return OverviewResponse{Error: validationErr}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return OverviewResponse{Error: bindingErr}
	}
	defer finish()

	result, err := manager.SelectDevice(ctx, request.DeviceID)
	if err != nil {
		return OverviewResponse{Error: bindingErrorFrom(err)}
	}
	return OverviewResponse{Result: &result}
}

func (a *App) LoadOverview() OverviewResponse {
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return OverviewResponse{Error: bindingErr}
	}
	defer finish()

	result, err := manager.Overview(ctx)
	if err != nil {
		return OverviewResponse{Error: bindingErrorFrom(err)}
	}
	return OverviewResponse{Result: &result}
}

func (a *App) LoadProfiles(request LoadProfilesRequest) LoadProfilesResponse {
	if validationErr := validateDeviceID(request.DeviceID, true); validationErr != nil {
		return LoadProfilesResponse{Error: validationErr}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return LoadProfilesResponse{Error: bindingErr}
	}
	defer finish()

	result, err := manager.Profiles(ctx, request.DeviceID)
	if err != nil {
		return LoadProfilesResponse{Result: &result, Error: bindingErrorFrom(err)}
	}
	return LoadProfilesResponse{Result: &result}
}

func (a *App) UpdatePreferences(request UpdatePreferencesRequest) OverviewResponse {
	if validationErr := validateUpdatePreferencesRequest(request); validationErr != nil {
		return OverviewResponse{Error: validationErr}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return OverviewResponse{Error: bindingErr}
	}
	defer finish()

	result, err := manager.UpdatePreferences(ctx, core.PreferencesRequest{
		Locale:                request.Locale,
		Theme:                 request.Theme,
		SelectedHostInterface: request.SelectedHostInterface,
	})
	if err != nil {
		return OverviewResponse{Error: bindingErrorFrom(err)}
	}
	return OverviewResponse{Result: &result}
}

// OpenInVLC accepts only backend identities. The Manager re-resolves the URI;
// no URL supplied by the frontend can reach process execution.
func (a *App) OpenInVLC(request OpenInVLCRequest) ActionResponse {
	selection, validationErr := validateOpenInVLCRequest(request)
	if validationErr != nil {
		return ActionResponse{Error: validationErr}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return ActionResponse{Error: bindingErr}
	}
	defer finish()

	a.mu.Lock()
	launcher := a.vlc
	available := a.caps.VLCAvailable
	a.mu.Unlock()
	if !available || launcher == nil {
		return ActionResponse{Error: bindingUnavailable("vlc_unavailable", "VLC is unavailable")}
	}

	uri, err := manager.ResolveStreamURI(ctx, selection)
	if err != nil {
		return ActionResponse{Error: bindingErrorFrom(err)}
	}
	if err := launcher.Launch(ctx, selection.Transport, uri); err != nil {
		return ActionResponse{Error: bindingErrorFrom(err)}
	}
	return ActionResponse{Result: &ActionResult{Completed: true}}
}

// StartPreview opens a live preview for a device profile. Like OpenInVLC it
// accepts only identities; the backend resolves the stream itself.
func (a *App) StartPreview(request StartPreviewRequest) StartPreviewResponse {
	selection, validationErr := validateStartPreviewRequest(request)
	if validationErr != nil {
		return StartPreviewResponse{Error: validationErr}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return StartPreviewResponse{Error: bindingErr}
	}
	defer finish()

	a.mu.Lock()
	previews := a.previews
	root := a.rootCtx
	a.mu.Unlock()
	if !previews.available() || root == nil {
		return StartPreviewResponse{Error: bindingUnavailable("preview_unavailable", "live preview is unavailable")}
	}
	result, err := previews.start(ctx, root, manager, selection)
	if err != nil {
		return StartPreviewResponse{Error: bindingErrorFrom(err)}
	}
	return StartPreviewResponse{Result: result}
}

// StopPreview ends a preview session. Stopping an unknown or finished session
// is not an error.
func (a *App) StopPreview(request StopPreviewRequest) ActionResponse {
	if !validPreviewSessionID(request.SessionID) {
		return ActionResponse{Error: bindingValidation("invalid_session_id", "the preview session identifier is not valid")}
	}
	a.mu.Lock()
	previews := a.previews
	a.mu.Unlock()
	if previews == nil {
		return ActionResponse{Error: bindingUnavailable("app_closed", "the desktop application is closed")}
	}
	previews.stop(request.SessionID)
	return ActionResponse{Result: &ActionResult{Completed: true}}
}

// PreviewStats returns live measurements for an active preview.
func (a *App) PreviewStats(request PreviewStatsRequest) PreviewStatsResponse {
	if !validPreviewSessionID(request.SessionID) {
		return PreviewStatsResponse{Error: bindingValidation("invalid_session_id", "the preview session identifier is not valid")}
	}
	a.mu.Lock()
	previews := a.previews
	a.mu.Unlock()
	if previews == nil {
		return PreviewStatsResponse{Error: bindingUnavailable("app_closed", "the desktop application is closed")}
	}
	stats, ok := previews.stats(request.SessionID)
	if !ok {
		return PreviewStatsResponse{Error: bindingValidation("preview_not_found", "the preview session is not active")}
	}
	return PreviewStatsResponse{Result: &PreviewStats{
		Width:                  stats.Width,
		Height:                 stats.Height,
		FramesPerSecond:        stats.FramesPerSecond,
		BitrateKbps:            stats.BitrateKbps,
		GOPFrames:              stats.GOPFrames,
		GOPSeconds:             stats.GOPSeconds,
		TimeToFirstFrameMillis: stats.TimeToFirstFrame.Milliseconds(),
		Frames:                 stats.Frames,
		Keyframes:              stats.Keyframes,
		LostPackets:            stats.LostPackets,
		ElapsedMillis:          stats.Elapsed.Milliseconds(),
		FirstFrameReceived:     stats.Keyframes > 0,
	}}
}

func bindingErrorFrom(err error) *BindingError {
	if err == nil {
		return nil
	}
	if errors.Is(err, errPreviewLimit) {
		return &BindingError{Category: core.CategoryUsage, Code: "preview_limit", Message: "at most four previews can run at once"}
	}
	var managerErr *core.ManagerError
	if errors.As(err, &managerErr) {
		code := "operation_" + string(managerErr.Category)
		if managerErr.StoreCode != "" {
			code = "store_" + string(managerErr.StoreCode)
		} else if managerErr.SecretKind != "" {
			code = "secret_" + string(managerErr.SecretKind)
		}
		return &BindingError{Category: managerErr.Category, Code: code, Message: managerErr.Message}
	}
	var appErr *core.Error
	if errors.As(err, &appErr) {
		return &BindingError{Category: appErr.Category, Code: "operation_" + string(appErr.Category), Message: appErr.Message}
	}
	var vlcErr *VLCError
	if errors.As(err, &vlcErr) {
		return &BindingError{Category: vlcErr.Category, Code: vlcErr.Code, Message: vlcErr.Message}
	}
	if errors.Is(err, context.Canceled) {
		return &BindingError{Category: core.CategoryCancelled, Code: "operation_cancelled", Message: "the operation was cancelled"}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &BindingError{Category: core.CategoryNetwork, Code: "operation_timeout", Message: "the operation timed out"}
	}
	return &BindingError{Category: core.CategoryInternal, Code: "internal_error", Message: "an internal error occurred"}
}

func bindingValidation(code, message string) *BindingError {
	return &BindingError{Category: core.CategoryUsage, Code: code, Message: message}
}

func bindingUnavailable(code, message string) *BindingError {
	return &BindingError{Category: core.CategoryInternal, Code: code, Message: message}
}
