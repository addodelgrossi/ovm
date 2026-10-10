package app

import (
	"context"
	"errors"

	"github.com/addodelgrossi/ovm/internal/store"
)

// managerErrorFromContext maps a context error to a typed manager error,
// distinguishing cancellation from a deadline exactly as the rest of the
// package does.
func managerErrorFromContext(err error) *ManagerError {
	switch {
	case errors.Is(err, context.Canceled):
		return newManagerError(CategoryCancelled, "the operation was cancelled", err)
	case errors.Is(err, context.DeadlineExceeded):
		return newManagerError(CategoryNetwork, "the operation timed out", err)
	default:
		return newManagerError(CategoryInternal, "an internal error occurred", err)
	}
}

// statusForCategory maps an application error category to a manager-memory
// runtime status so the catalog view reflects the last operation outcome.
func statusForCategory(category ErrorCategory) DeviceRuntimeStatus {
	switch category {
	case CategoryAuthentication:
		return StatusAuthenticationRequired
	case CategoryNetwork:
		return StatusUnreachable
	case "":
		return StatusReachable
	default:
		return StatusError
	}
}

// credentialRelease clears local references after a scoped credential use. It
// is safe to call more than once.
type credentialRelease func()

// loadCredentials builds ephemeral app credentials for a device from its stored
// secret, scoping the secret Get and the credential construction to the
// shortest possible window. The returned release clears the local credential
// reference; callers must call it as soon as the credential is no longer
// needed and never cache the credential.
//
// A missing or empty credential reference, or a provider "not found", maps to a
// safe auth_required error with CategoryAuthentication, never leaking a backend
// diagnostic. The credential is never logged, serialized, or returned to the
// boundary.
func (m *Manager) loadCredentials(ctx context.Context, device store.Device) (*Credentials, credentialRelease, *ManagerError) {
	noop := credentialRelease(func() {})
	if device.CredentialReference == "" {
		return nil, noop, newManagerError(CategoryAuthentication, "authentication is required for this device", nil)
	}
	credentials, err := m.buildCredentials(ctx, device.CredentialReference)
	if err != nil {
		return nil, noop, err
	}
	release := credentialRelease(func() { credentials = nil })
	return credentials, release, nil
}

// buildCredentials performs the shortest-lived secret Get plus credential
// construction. The retrieved store.Secret is confined to this function: its
// username and password are read into a fresh *Credentials and the secret
// local is cleared before returning, so no plaintext lingers in the manager.
func (m *Manager) buildCredentials(ctx context.Context, reference string) (*Credentials, *ManagerError) {
	secret, err := m.repository.GetSecret(ctx, reference)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, managerErrorFromContext(err)
		}
		if store.SecretErrorKindOf(err) == store.SecretNotFound {
			return nil, newManagerError(CategoryAuthentication, "authentication is required for this device", err)
		}
		if managed := managerErrorFromStore(err); managed != nil {
			return nil, managed
		}
		return nil, newManagerError(CategoryInternal, "secure credential storage is unavailable", err)
	}
	credentials, buildErr := NewCredentialsWithPolicy(secret.Username(), secret.Password(), Policy{
		AllowBasic:         secret.Policy().AllowBasic,
		AllowBasicOverHTTP: secret.Policy().AllowBasicOverHTTP,
		AllowLegacyDigest:  secret.Policy().AllowLegacyDigest,
	})
	// Clear the local secret reference immediately; it is a value type whose
	// String/GoString already redact, but dropping the binding keeps the window
	// minimal.
	secret = store.Secret{}
	if buildErr != nil {
		return nil, newManagerError(CategoryAuthentication, "the stored credential is invalid", buildErr)
	}
	return credentials, nil
}
