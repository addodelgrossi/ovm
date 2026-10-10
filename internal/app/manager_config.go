package app

import (
	"context"
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

func unavailableConfiguration(area string) error {
	return newManagerError(CategoryInternal, area+" configuration is not available", nil)
}

// ReadEncoder returns a profile's encoder settings and options.
func (m *Manager) ReadEncoder(ctx context.Context, deviceID, profileToken string) (EncoderView, error) {
	if m.encoder == nil {
		return EncoderView{}, unavailableConfiguration("encoder")
	}
	host, credentials, release, err := m.deviceSession(ctx, deviceID)
	if err != nil {
		return EncoderView{}, err
	}
	defer release()
	view, err := m.encoder.ReadEncoder(ctx, EncoderRequest{Host: host, Credentials: credentials, ProfileToken: profileToken})
	if err != nil {
		return view, managerErrorFromApp(err)
	}
	return view, nil
}

// ApplyEncoder changes a profile's encoder and reports the verified result.
func (m *Manager) ApplyEncoder(ctx context.Context, deviceID, profileToken string, change EncoderChange) (EncoderApplyResult, error) {
	if m.encoder == nil {
		return EncoderApplyResult{}, unavailableConfiguration("encoder")
	}
	host, credentials, release, err := m.deviceSession(ctx, deviceID)
	if err != nil {
		return EncoderApplyResult{}, err
	}
	defer release()
	result, err := m.encoder.ApplyEncoder(ctx, EncoderRequest{Host: host, Credentials: credentials, ProfileToken: profileToken}, change)
	if err != nil {
		return result, managerErrorFromApp(err)
	}
	return result, nil
}
