package app

import (
	"context"
	"errors"
	"time"

	"github.com/addodelgrossi/ovm/internal/store"
)

// AuthenticateDevice proves credentials against a device and, only on proof,
// persists them. The three policy booleans in the request are independent and
// are never inferred.
//
// Ordering and transaction:
//  1. Build ephemeral app credentials from the request (never cached).
//  2. Run Inventory first. A nil Inventory error is the ONLY proof that the
//     credentials are usable (the inventory use case's existing useful-inventory
//     policy decides what "useful" means). When proof fails, the partial
//     inventory and the typed error category are preserved and NOTHING is
//     persisted.
//  3. On proof, create a store.Secret, write it under a FRESH random reference,
//     then atomically upsert the full device metadata pointing at that
//     reference.
//  4. If the metadata upsert fails or the caller's context is cancelled after
//     the secret write, best-effort delete the just-written secret so it is not
//     stranded (compensation uses a detached, bounded context).
//  5. On a successful rotation (the device already referenced a different
//     secret), best-effort delete the previous reference.
//
// The secret Get and the credential construction are scoped to the shortest
// helper call and the local credential is cleared immediately after the proof
// and persist steps; credentials are never cached on the manager.
func (m *Manager) AuthenticateDevice(ctx context.Context, request AuthenticateRequest) (AuthenticateResult, error) {
	if err := m.ensureOpen(); err != nil {
		return AuthenticateResult{}, err
	}
	device, resolveErr := m.resolveDevice(request.DeviceID)
	if resolveErr != nil {
		return AuthenticateResult{}, resolveErr
	}

	credentials, credErr := NewCredentialsWithPolicy(request.Username, request.Password, Policy{
		AllowBasic:         request.AllowBasic,
		AllowBasicOverHTTP: request.AllowBasicOverHTTP,
		AllowLegacyDigest:  request.AllowLegacyDigest,
	})
	if credErr != nil {
		return AuthenticateResult{}, newManagerError(CategoryUsage, "the credentials are invalid", credErr)
	}

	opCtx, finish, beginErr := m.beginDeviceOp(ctx, device.ID)
	if beginErr != nil {
		return AuthenticateResult{}, beginErr
	}
	defer finish()

	timeout := time.Duration(request.Timeout)
	if timeout <= 0 {
		timeout = defaultManagerTimeout
	}

	// Step 2: prove credentials via Inventory. Only a nil error proves them.
	inventory, invErr := m.inventory.Inventory(opCtx, InventoryRequest{
		Host:        device.Endpoint,
		Credentials: credentials,
		Timeout:     timeout,
	})
	if invErr != nil {
		managed := managerErrorFromApp(invErr)
		m.setStatus(device.ID, statusForCategory(managed.Category))
		// Preserve the partial inventory alongside the typed error.
		return AuthenticateResult{Inventory: inventory}, managed
	}

	// Step 3: build and store the secret under a fresh reference, then persist
	// metadata. Credentials are consumed into a store.Secret and the local
	// *Credentials reference is dropped before the persist transaction.
	secret, secretErr := store.NewSecret(credentials.Username(), request.Password, store.AuthPolicy{
		AllowBasic:         request.AllowBasic,
		AllowBasicOverHTTP: request.AllowBasicOverHTTP,
		AllowLegacyDigest:  request.AllowLegacyDigest,
	})
	credentials = nil
	if secretErr != nil {
		return AuthenticateResult{Inventory: inventory}, newManagerError(CategoryUsage, "the credentials are invalid", secretErr)
	}

	saved, persistErr := m.persistCredentials(opCtx, device, secret)
	if persistErr != nil {
		m.setStatus(device.ID, statusForCategory(persistErr.Category))
		return AuthenticateResult{Inventory: inventory}, persistErr
	}

	m.setStatus(device.ID, StatusReachable)
	selected := m.repository.Preferences().SelectedDeviceID
	return AuthenticateResult{
		Device:    deviceToDTO(saved, saved.ID == selected, StatusReachable),
		Inventory: inventory,
	}, nil
}

// persistCredentials writes a fresh secret and atomically upserts the full
// device metadata pointing at the new reference. On metadata failure or caller
// cancellation after the write, it best-effort deletes the just-written secret.
// On a successful rotation it best-effort deletes the previous reference. It
// never holds the manager mutex across a provider or repository call.
func (m *Manager) persistCredentials(ctx context.Context, device store.Device, secret store.Secret) (store.Device, *ManagerError) {
	reference, refErr := m.repository.NewSecretReference()
	if refErr != nil {
		return store.Device{}, newManagerError(CategoryInternal, "secure credential storage is unavailable", refErr)
	}

	if err := m.repository.PutSecret(ctx, reference, secret); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// A provider may complete the write while cancelling the request.
			m.cleanupSecret(ctx, reference)
			return store.Device{}, managerErrorFromContext(err)
		}
		if managed := managerErrorFromStore(err); managed != nil {
			return store.Device{}, managed
		}
		return store.Device{}, newManagerError(CategoryInternal, "secure credential storage is unavailable", err)
	}

	// If the caller cancelled during the write, do not publish metadata and
	// clean up the new secret.
	if err := ctx.Err(); err != nil {
		m.cleanupSecret(ctx, reference)
		return store.Device{}, managerErrorFromContext(err)
	}

	previousReference := device.CredentialReference
	device.CredentialReference = reference
	device.AuthPolicy = secret.Policy()

	saved, upsertErr := m.repository.Upsert(device)
	if upsertErr != nil {
		// Metadata failed: best-effort delete the stranded new secret.
		m.cleanupSecret(ctx, reference)
		if managed := managerErrorFromStore(upsertErr); managed != nil {
			return store.Device{}, managed
		}
		return store.Device{}, newManagerError(CategoryInternal, "the device could not be updated", upsertErr)
	}

	// Rotation cleanup: delete the prior reference, best effort. A failure here
	// does not invalidate the committed metadata, so it is not surfaced as an
	// error; the new reference is authoritative.
	if previousReference != "" && previousReference != reference {
		m.cleanupSecret(ctx, previousReference)
	}
	return saved, nil
}

// cleanupSecret best-effort deletes a secret reference using a detached,
// bounded context so compensation runs even when the caller's context is
// already cancelled. A missing secret is an idempotent success and any other
// failure is swallowed because this is a compensating action.
func (m *Manager) cleanupSecret(ctx context.Context, reference string) {
	if reference == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), compensationTimeout)
	defer cancel()
	_ = m.deleteSecretIdempotent(cleanupCtx, reference)
}

// deleteSecretIdempotent deletes a secret reference, treating a missing
// reference as success.
func (m *Manager) deleteSecretIdempotent(ctx context.Context, reference string) error {
	err := m.repository.DeleteSecret(ctx, reference)
	if err == nil || store.SecretErrorKindOf(err) == store.SecretNotFound {
		return nil
	}
	return err
}

// compensationTimeout bounds a detached compensation (secret cleanup) so a hung
// provider cannot stall the manager.
const compensationTimeout = 5 * time.Second

// ForgetDevice removes a device and its stored credential. Ordering:
//  1. Cancel the device's active network operation first.
//  2. Delete the referenced secret BEFORE removing metadata (a missing secret
//     is an idempotent success). This differs from store.Coordinator.Forget,
//     whose order removes metadata first, so the Coordinator is intentionally
//     not used here.
//  3. Remove the metadata, which also clears a matching selection.
//  4. Clean up the device's manager-memory runtime status.
//
// A delete failure other than "not found" is surfaced and the metadata is left
// intact so the record is not orphaned from its secret.
func (m *Manager) ForgetDevice(ctx context.Context, deviceID string) (Overview, error) {
	if err := m.ensureOpen(); err != nil {
		return Overview{}, err
	}
	if err := contextErr(ctx); err != nil {
		return Overview{}, managerErrorFromContext(err)
	}

	// Step 1: cancel any in-flight operation for this device.
	m.cancelDeviceOp(deviceID)

	device, lookupErr := m.repository.Device(deviceID)
	if lookupErr != nil {
		if store.ErrorCodeOf(lookupErr) == store.CodeDeviceNotFound {
			return Overview{}, newManagerError(CategoryUsage, "the device was not found", lookupErr)
		}
		if managed := managerErrorFromStore(lookupErr); managed != nil {
			return Overview{}, managed
		}
		return Overview{}, newManagerError(CategoryInternal, "the device could not be removed", lookupErr)
	}

	// Step 2: delete the secret before metadata, idempotent on missing.
	if device.CredentialReference != "" {
		if err := m.deleteSecretIdempotent(ctx, device.CredentialReference); err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return Overview{}, managerErrorFromContext(err)
			}
			if managed := managerErrorFromStore(err); managed != nil {
				return Overview{}, managed
			}
			return Overview{}, newManagerError(CategoryInternal, "secure credential storage is unavailable", err)
		}
	}

	// Step 3: remove metadata (clears a matching selection in the store).
	if _, err := m.repository.Remove(deviceID); err != nil {
		if store.ErrorCodeOf(err) == store.CodeDeviceNotFound {
			// Already gone after the secret delete; treat as success and fall
			// through to status cleanup and overview.
		} else if managed := managerErrorFromStore(err); managed != nil {
			return Overview{}, managed
		} else {
			return Overview{}, newManagerError(CategoryInternal, "the device could not be removed", err)
		}
	}

	// Step 4: clear manager-memory runtime status for the removed device.
	m.clearStatus(deviceID)

	snapshot := m.repository.Snapshot()
	// Ensure the selection is clear if it somehow still points at the removed
	// device (defensive; Remove already clears a matching selection).
	if snapshot.Preferences.SelectedDeviceID == deviceID {
		if err := m.repository.Select(""); err != nil {
			return Overview{}, managerErrorFromStore(err)
		}
		snapshot = m.repository.Snapshot()
	}
	m.pruneStatuses(snapshot)
	return m.overviewFrom(snapshot), nil
}

// clearStatus removes the manager-memory runtime status for one device.
func (m *Manager) clearStatus(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.statuses, id)
}
