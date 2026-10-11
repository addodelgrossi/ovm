package main

import (
	"strings"
	"unicode/utf8"

	core "github.com/addodelgrossi/ovm/internal/app"
)

type DeviceIDOnlyRequest struct {
	DeviceID string `json:"device_id"`
}

type TimeViewResponse struct {
	Result *core.TimeView `json:"result,omitempty"`
	Error  *BindingError  `json:"error,omitempty"`
}

type ApplyTimeRequest struct {
	DeviceID string          `json:"device_id"`
	Change   core.TimeChange `json:"change"`
}

type TimeApplyResponse struct {
	Result *core.TimeApplyResult `json:"result,omitempty"`
	Error  *BindingError         `json:"error,omitempty"`
}

type UsersViewResponse struct {
	Result *core.UsersView `json:"result,omitempty"`
	Error  *BindingError   `json:"error,omitempty"`
}

// UserChangeRequest carries a password only inbound; it is never returned.
type UserChangeRequest struct {
	DeviceID string `json:"device_id"`
	Username string `json:"username"`
	Password string `json:"password"`
	Level    string `json:"level"`
}

type DeleteUserRequest struct {
	DeviceID string `json:"device_id"`
	Username string `json:"username"`
}

type UsersApplyResponse struct {
	Result *core.UsersApplyResult `json:"result,omitempty"`
	Error  *BindingError          `json:"error,omitempty"`
}

type NetworkViewResponse struct {
	Result *core.NetworkView `json:"result,omitempty"`
	Error  *BindingError     `json:"error,omitempty"`
}

// ApplyNetworkRequest requires a second confirmation: the technician retypes
// the new address, or "DHCP".
type ApplyNetworkRequest struct {
	DeviceID     string             `json:"device_id"`
	Change       core.NetworkChange `json:"change"`
	Confirmation string             `json:"confirmation"`
}

type NetworkApplyResponse struct {
	Result *core.NetworkApplyResult `json:"result,omitempty"`
	Error  *BindingError            `json:"error,omitempty"`
}

// FactoryResetRequest requires typing RESET.
type FactoryResetRequest struct {
	DeviceID     string `json:"device_id"`
	Confirmation string `json:"confirmation"`
}

type MaintenanceResponse struct {
	Result *core.MaintenanceResult `json:"result,omitempty"`
	Error  *BindingError           `json:"error,omitempty"`
}

type SystemLogRequest struct {
	DeviceID string `json:"device_id"`
	Type     string `json:"type"`
}

type SystemLogResponse struct {
	Result *core.SystemLogResult `json:"result,omitempty"`
	Error  *BindingError         `json:"error,omitempty"`
}

// ReadTime returns the camera clock configuration.
func (a *App) ReadTime(request DeviceIDOnlyRequest) TimeViewResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return TimeViewResponse{Error: err}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return TimeViewResponse{Error: bindingErr}
	}
	defer finish()
	view, err := manager.ReadTime(ctx, request.DeviceID)
	if err != nil {
		return TimeViewResponse{Error: bindingErrorFrom(err)}
	}
	return TimeViewResponse{Result: &view}
}

// ApplyTime sets NTP or the manual clock.
func (a *App) ApplyTime(request ApplyTimeRequest) TimeApplyResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return TimeApplyResponse{Error: err}
	}
	if len(request.Change.NTPServers) > 4 || len(request.Change.TimeZone) > 64 {
		return TimeApplyResponse{Error: bindingValidation("invalid_time_change", "the time settings are not valid")}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return TimeApplyResponse{Error: bindingErr}
	}
	defer finish()
	result, err := manager.ApplyTime(ctx, request.DeviceID, request.Change)
	if err != nil {
		return TimeApplyResponse{Result: &result, Error: bindingErrorFrom(err)}
	}
	return TimeApplyResponse{Result: &result}
}

// ReadUsers lists camera accounts.
func (a *App) ReadUsers(request DeviceIDOnlyRequest) UsersViewResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return UsersViewResponse{Error: err}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return UsersViewResponse{Error: bindingErr}
	}
	defer finish()
	view, err := manager.ReadUsers(ctx, request.DeviceID)
	if err != nil {
		return UsersViewResponse{Error: bindingErrorFrom(err)}
	}
	return UsersViewResponse{Result: &view}
}

func validUserText(username, password string) bool {
	return validBoundaryText(username, 32, true) && utf8.ValidString(password) && utf8.RuneCountInString(password) <= 64
}

func (a *App) userCall(request UserChangeRequest, create bool) UsersApplyResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return UsersApplyResponse{Error: err}
	}
	if !validUserText(request.Username, request.Password) {
		return UsersApplyResponse{Error: bindingValidation("invalid_user", "the account details are not valid")}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return UsersApplyResponse{Error: bindingErr}
	}
	defer finish()
	change := core.UserChange{Username: request.Username, Password: request.Password, Level: request.Level}
	var (
		result core.UsersApplyResult
		err    error
	)
	if create {
		result, err = manager.CreateUser(ctx, request.DeviceID, change)
	} else {
		result, err = manager.SetUser(ctx, request.DeviceID, change)
	}
	if err != nil {
		return UsersApplyResponse{Result: &result, Error: bindingErrorFrom(err)}
	}
	return UsersApplyResponse{Result: &result}
}

// SetUser changes an account password and level; when it is the account ovm
// signs in with, the stored credential is rotated after the camera accepts it.
func (a *App) SetUser(request UserChangeRequest) UsersApplyResponse {
	return a.userCall(request, false)
}

// CreateUser adds an account.
func (a *App) CreateUser(request UserChangeRequest) UsersApplyResponse {
	return a.userCall(request, true)
}

// DeleteUser removes an account other than the one ovm signs in with.
func (a *App) DeleteUser(request DeleteUserRequest) UsersApplyResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return UsersApplyResponse{Error: err}
	}
	if !validBoundaryText(request.Username, 32, true) {
		return UsersApplyResponse{Error: bindingValidation("invalid_user", "the account details are not valid")}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return UsersApplyResponse{Error: bindingErr}
	}
	defer finish()
	result, err := manager.DeleteUser(ctx, request.DeviceID, request.Username)
	if err != nil {
		return UsersApplyResponse{Result: &result, Error: bindingErrorFrom(err)}
	}
	return UsersApplyResponse{Result: &result}
}

// ReadNetwork returns the camera network configuration.
func (a *App) ReadNetwork(request DeviceIDOnlyRequest) NetworkViewResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return NetworkViewResponse{Error: err}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return NetworkViewResponse{Error: bindingErr}
	}
	defer finish()
	view, err := manager.ReadNetwork(ctx, request.DeviceID)
	if err != nil {
		return NetworkViewResponse{Error: bindingErrorFrom(err)}
	}
	return NetworkViewResponse{Result: &view}
}

// networkConfirmation is what the technician must retype.
func networkConfirmation(change core.NetworkChange) string {
	if change.DHCP {
		return "DHCP"
	}
	return change.Address
}

// ApplyNetwork changes the camera network configuration after the second
// confirmation.
func (a *App) ApplyNetwork(request ApplyNetworkRequest) NetworkApplyResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return NetworkApplyResponse{Error: err}
	}
	if strings.TrimSpace(request.Confirmation) != networkConfirmation(request.Change) {
		return NetworkApplyResponse{Error: bindingValidation("confirmation_mismatch", "the confirmation does not match the new address")}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return NetworkApplyResponse{Error: bindingErr}
	}
	defer finish()
	result, err := manager.ApplyNetwork(ctx, request.DeviceID, request.Change)
	if err != nil {
		return NetworkApplyResponse{Result: &result, Error: bindingErrorFrom(err)}
	}
	return NetworkApplyResponse{Result: &result}
}

// Reboot restarts the camera and waits for it to return.
func (a *App) Reboot(request DeviceIDOnlyRequest) MaintenanceResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return MaintenanceResponse{Error: err}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return MaintenanceResponse{Error: bindingErr}
	}
	defer finish()
	result, err := manager.Reboot(ctx, request.DeviceID)
	if err != nil {
		return MaintenanceResponse{Error: bindingErrorFrom(err)}
	}
	return MaintenanceResponse{Result: &result}
}

// SoftFactoryReset restores factory settings, keeping network and accounts,
// after the RESET confirmation.
func (a *App) SoftFactoryReset(request FactoryResetRequest) MaintenanceResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return MaintenanceResponse{Error: err}
	}
	if request.Confirmation != "RESET" {
		return MaintenanceResponse{Error: bindingValidation("confirmation_mismatch", "type RESET to confirm the factory reset")}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return MaintenanceResponse{Error: bindingErr}
	}
	defer finish()
	result, err := manager.SoftFactoryReset(ctx, request.DeviceID)
	if err != nil {
		return MaintenanceResponse{Error: bindingErrorFrom(err)}
	}
	return MaintenanceResponse{Result: &result}
}

// SystemLog returns a bounded camera log.
func (a *App) SystemLog(request SystemLogRequest) SystemLogResponse {
	if err := validateDeviceID(request.DeviceID, false); err != nil {
		return SystemLogResponse{Error: err}
	}
	if request.Type != "System" && request.Type != "Access" {
		return SystemLogResponse{Error: bindingValidation("invalid_log_type", "the log type must be System or Access")}
	}
	manager, ctx, finish, bindingErr := a.beginCall()
	if bindingErr != nil {
		return SystemLogResponse{Error: bindingErr}
	}
	defer finish()
	result, err := manager.SystemLog(ctx, request.DeviceID, request.Type)
	if err != nil {
		return SystemLogResponse{Error: bindingErrorFrom(err)}
	}
	return SystemLogResponse{Result: &result}
}
