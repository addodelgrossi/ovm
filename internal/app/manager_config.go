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
