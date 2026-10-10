package app

import (
	"context"

	"github.com/addodelgrossi/ovm/internal/store"
)

// ManagerRepository is the narrow persistence seam the device Manager depends
// on. It is deliberately an interface so the Manager never instantiates a
// production disk repository or an operating-system keyring in this package:
// composition roots inject either the production adapter (see
// NewProductionManagerRepository) or a deterministic in-memory fake in tests.
//
// The contract is intentionally compatible with the concrete store types:
//   - Snapshot, Device, Preferences, Upsert, Remove, Select, and
//     SetPreferences mirror *store.Repository.
//   - PutSecret, GetSecret, and DeleteSecret mirror store.SecretProvider.
//   - NewDeviceID and NewSecretReference let the repository own crypto-random
//     identifier generation, which is why the Manager never calls crypto/rand
//     directly.
//
// Every method returns copies or opaque values; the Manager never shares a live
// handle across its lock boundaries, and it never holds its own mutex while
// calling into this interface.
type ManagerRepository interface {
	// Snapshot returns a deep copy of the current metadata state.
	Snapshot() store.Snapshot
	// Device returns a copy of a single device by ID.
	Device(id string) (store.Device, error)
	// Preferences returns a copy of the durable preferences.
	Preferences() store.Preferences
	// Upsert validates, canonicalizes, and atomically persists metadata,
	// generating a stable ID when the device has none.
	Upsert(device store.Device) (store.Device, error)
	// Remove atomically removes metadata and clears a matching selection.
	Remove(id string) (store.Device, error)
	// Select updates the selected device; an empty ID clears the selection.
	Select(id string) error
	// SetPreferences atomically replaces durable preferences.
	SetPreferences(preferences store.Preferences) error

	// PutSecret stores an opaque secret payload under an opaque reference.
	PutSecret(ctx context.Context, reference string, secret store.Secret) error
	// GetSecret retrieves an opaque secret payload by reference.
	GetSecret(ctx context.Context, reference string) (store.Secret, error)
	// DeleteSecret removes a secret payload by reference. A missing reference
	// yields store.ErrSecretNotFound so the caller can treat deletion as
	// idempotent.
	DeleteSecret(ctx context.Context, reference string) error

	// NewDeviceID returns a fresh crypto-random stable device identifier.
	NewDeviceID() (string, error)
	// NewSecretReference returns a fresh crypto-random opaque secret reference,
	// independent of endpoint and username values.
	NewSecretReference() (string, error)
}

// productionManagerRepository adapts the concrete store types to
// ManagerRepository without adding behavior. It composes an existing
// *store.Repository and store.SecretProvider that the caller already built and
// owns, so this package never constructs production disk or keyring resources.
type productionManagerRepository struct {
	repository *store.Repository
	secrets    store.SecretProvider
}

// NewProductionManagerRepository wraps an existing repository and secret
// provider as a ManagerRepository. Both arguments are required; the caller owns
// their lifecycle (opening the disk repository and selecting the platform
// keyring happen in the composition root, not here).
func NewProductionManagerRepository(repository *store.Repository, secrets store.SecretProvider) (ManagerRepository, error) {
	if repository == nil || secrets == nil {
		return nil, newManagerError(CategoryInternal, "the device repository is not configured", nil)
	}
	return &productionManagerRepository{repository: repository, secrets: secrets}, nil
}

func (r *productionManagerRepository) Snapshot() store.Snapshot { return r.repository.Snapshot() }

func (r *productionManagerRepository) Device(id string) (store.Device, error) {
	return r.repository.Device(id)
}

func (r *productionManagerRepository) Preferences() store.Preferences {
	return r.repository.Preferences()
}

func (r *productionManagerRepository) Upsert(device store.Device) (store.Device, error) {
	return r.repository.Upsert(device)
}

func (r *productionManagerRepository) Remove(id string) (store.Device, error) {
	return r.repository.Remove(id)
}

func (r *productionManagerRepository) Select(id string) error { return r.repository.Select(id) }

func (r *productionManagerRepository) SetPreferences(preferences store.Preferences) error {
	return r.repository.SetPreferences(preferences)
}

func (r *productionManagerRepository) PutSecret(ctx context.Context, reference string, secret store.Secret) error {
	return r.secrets.Put(ctx, reference, secret)
}

func (r *productionManagerRepository) GetSecret(ctx context.Context, reference string) (store.Secret, error) {
	return r.secrets.Get(ctx, reference)
}

func (r *productionManagerRepository) DeleteSecret(ctx context.Context, reference string) error {
	return r.secrets.Delete(ctx, reference)
}

func (r *productionManagerRepository) NewDeviceID() (string, error) { return store.NewDeviceID() }

func (r *productionManagerRepository) NewSecretReference() (string, error) {
	return store.NewSecretReference()
}
