package app

import (
	"context"

	"github.com/addodelgrossi/ovm/internal/store"
)

// deviceSession resolves a stored device and its credentials for a
// configuration call. release must be called when the call ends.
func (m *Manager) deviceSession(ctx context.Context, deviceID string) (string, *Credentials, credentialRelease, error) {
	noop := credentialRelease(func() {})
	if err := m.ensureOpen(); err != nil {
		return "", nil, noop, err
	}
	device, resolveErr := m.resolveDevice(deviceID)
	if resolveErr != nil {
		return "", nil, noop, resolveErr
	}
	if device.CredentialReference == "" {
		return device.Endpoint, nil, noop, nil
	}
	credentials, release, credErr := m.loadCredentials(ctx, device)
	if credErr != nil {
		return "", nil, noop, credErr
	}
	return device.Endpoint, credentials, release, nil
}

// withDevice runs a configuration use case against a stored device with its
// credentials, mapping application errors to manager errors. A partial result
// accompanies an error.
func withDevice[T any](ctx context.Context, m *Manager, deviceID string, configured bool, area string, call func(host string, credentials *Credentials) (T, error)) (T, error) {
	var zero T
	if !configured {
		return zero, newManagerError(CategoryInternal, area+" configuration is not available", nil)
	}
	host, credentials, release, err := m.deviceSession(ctx, deviceID)
	if err != nil {
		return zero, err
	}
	defer release()
	result, err := call(host, credentials)
	if err != nil {
		return result, managerErrorFromApp(err)
	}
	return result, nil
}

// ReadEncoder returns a profile's encoder settings and options.
func (m *Manager) ReadEncoder(ctx context.Context, deviceID, profileToken string) (EncoderView, error) {
	return withDevice(ctx, m, deviceID, m.encoder != nil, "encoder", func(host string, credentials *Credentials) (EncoderView, error) {
		return m.encoder.ReadEncoder(ctx, EncoderRequest{Host: host, Credentials: credentials, ProfileToken: profileToken})
	})
}

// ApplyEncoder changes a profile's encoder and reports the verified result.
func (m *Manager) ApplyEncoder(ctx context.Context, deviceID, profileToken string, change EncoderChange) (EncoderApplyResult, error) {
	return withDevice(ctx, m, deviceID, m.encoder != nil, "encoder", func(host string, credentials *Credentials) (EncoderApplyResult, error) {
		return m.encoder.ApplyEncoder(ctx, EncoderRequest{Host: host, Credentials: credentials, ProfileToken: profileToken}, change)
	})
}

// ReadImaging returns the image settings of a profile's video source.
func (m *Manager) ReadImaging(ctx context.Context, deviceID, profileToken string) (ImagingView, error) {
	return withDevice(ctx, m, deviceID, m.imaging != nil, "image", func(host string, credentials *Credentials) (ImagingView, error) {
		return m.imaging.ReadImaging(ctx, EncoderRequest{Host: host, Credentials: credentials, ProfileToken: profileToken})
	})
}

// ApplyImaging changes image settings and reports the verified result.
func (m *Manager) ApplyImaging(ctx context.Context, deviceID, profileToken string, change ImagingSettingsDTO) (ImagingApplyResult, error) {
	return withDevice(ctx, m, deviceID, m.imaging != nil, "image", func(host string, credentials *Credentials) (ImagingApplyResult, error) {
		return m.imaging.ApplyImaging(ctx, EncoderRequest{Host: host, Credentials: credentials, ProfileToken: profileToken}, change)
	})
}

func newDeviceRequest(host string, credentials *Credentials) DeviceRequest {
	return DeviceRequest{Host: host, Credentials: credentials}
}

// ReadTime returns the camera clock configuration and measured offset.
func (m *Manager) ReadTime(ctx context.Context, deviceID string) (TimeView, error) {
	return withDevice(ctx, m, deviceID, m.timeConfig != nil, "time", func(host string, credentials *Credentials) (TimeView, error) {
		return m.timeConfig.ReadTime(ctx, newDeviceRequest(host, credentials))
	})
}

// ApplyTime changes the clock configuration and reports the verified result.
func (m *Manager) ApplyTime(ctx context.Context, deviceID string, change TimeChange) (TimeApplyResult, error) {
	return withDevice(ctx, m, deviceID, m.timeConfig != nil, "time", func(host string, credentials *Credentials) (TimeApplyResult, error) {
		return m.timeConfig.ApplyTime(ctx, newDeviceRequest(host, credentials), change)
	})
}

// ReadUsers lists accounts and flags a stored factory-default credential.
func (m *Manager) ReadUsers(ctx context.Context, deviceID string) (UsersView, error) {
	return withDevice(ctx, m, deviceID, m.users != nil, "account", func(host string, credentials *Credentials) (UsersView, error) {
		return m.users.ReadUsers(ctx, newDeviceRequest(host, credentials))
	})
}

// SetUser changes an account. When it is the account ovm signs in with and
// the camera accepted the new password, the stored secret is rotated so the
// next connection keeps working.
func (m *Manager) SetUser(ctx context.Context, deviceID string, change UserChange) (UsersApplyResult, error) {
	var signedInUser string
	result, err := withDevice(ctx, m, deviceID, m.users != nil, "account", func(host string, credentials *Credentials) (UsersApplyResult, error) {
		if credentials != nil {
			signedInUser = credentials.username
		}
		return m.users.SetUser(ctx, newDeviceRequest(host, credentials), change)
	})
	if err != nil || !result.SignInVerified || signedInUser != change.Username {
		return result, err
	}
	device, resolveErr := m.resolveDevice(deviceID)
	if resolveErr != nil {
		return result, resolveErr
	}
	secret, secretErr := store.NewSecret(change.Username, change.Password, device.AuthPolicy)
	if secretErr != nil {
		return result, newManagerError(CategoryInternal, "the new password could not be stored", secretErr)
	}
	if _, persistErr := m.persistCredentials(ctx, device, secret); persistErr != nil {
		return result, newManagerError(persistErr.Category, "the camera accepted the new password but it could not be stored; authenticate again with the new password", persistErr)
	}
	return result, nil
}

// CreateUser adds an account.
func (m *Manager) CreateUser(ctx context.Context, deviceID string, change UserChange) (UsersApplyResult, error) {
	return withDevice(ctx, m, deviceID, m.users != nil, "account", func(host string, credentials *Credentials) (UsersApplyResult, error) {
		return m.users.CreateUser(ctx, newDeviceRequest(host, credentials), change)
	})
}

// DeleteUser removes an account.
func (m *Manager) DeleteUser(ctx context.Context, deviceID, username string) (UsersApplyResult, error) {
	return withDevice(ctx, m, deviceID, m.users != nil, "account", func(host string, credentials *Credentials) (UsersApplyResult, error) {
		return m.users.DeleteUser(ctx, newDeviceRequest(host, credentials), username)
	})
}

// ReadNetwork returns the primary interface configuration.
func (m *Manager) ReadNetwork(ctx context.Context, deviceID string) (NetworkView, error) {
	return withDevice(ctx, m, deviceID, m.network != nil, "network", func(host string, credentials *Credentials) (NetworkView, error) {
		return m.network.ReadNetwork(ctx, newDeviceRequest(host, credentials))
	})
}

// ApplyNetwork changes the network configuration. When the camera was found
// again at a new address, the stored endpoint follows it.
func (m *Manager) ApplyNetwork(ctx context.Context, deviceID string, change NetworkChange) (NetworkApplyResult, error) {
	result, err := withDevice(ctx, m, deviceID, m.network != nil, "network", func(host string, credentials *Credentials) (NetworkApplyResult, error) {
		return m.network.ApplyNetwork(ctx, newDeviceRequest(host, credentials), change)
	})
	if err != nil || result.NewEndpoint == "" || result.Report.Outcome == OutcomeDisconnected {
		return result, err
	}
	device, resolveErr := m.resolveDevice(deviceID)
	if resolveErr != nil {
		return result, resolveErr
	}
	device.Endpoint = result.NewEndpoint
	if _, upsertErr := m.repository.Upsert(device); upsertErr != nil {
		return result, newManagerError(CategoryInternal, "the camera moved but its new address could not be saved", upsertErr)
	}
	return result, nil
}

// Reboot restarts the camera and waits for it.
func (m *Manager) Reboot(ctx context.Context, deviceID string) (MaintenanceResult, error) {
	return withDevice(ctx, m, deviceID, m.maintenance != nil, "maintenance", func(host string, credentials *Credentials) (MaintenanceResult, error) {
		return m.maintenance.Reboot(ctx, newDeviceRequest(host, credentials))
	})
}

// SoftFactoryReset restores factory settings except network and accounts.
func (m *Manager) SoftFactoryReset(ctx context.Context, deviceID string) (MaintenanceResult, error) {
	return withDevice(ctx, m, deviceID, m.maintenance != nil, "maintenance", func(host string, credentials *Credentials) (MaintenanceResult, error) {
		return m.maintenance.SoftFactoryReset(ctx, newDeviceRequest(host, credentials))
	})
}

// SystemLog returns a bounded camera log.
func (m *Manager) SystemLog(ctx context.Context, deviceID, logType string) (SystemLogResult, error) {
	return withDevice(ctx, m, deviceID, m.maintenance != nil, "maintenance", func(host string, credentials *Credentials) (SystemLogResult, error) {
		return m.maintenance.SystemLog(ctx, newDeviceRequest(host, credentials), logType)
	})
}
