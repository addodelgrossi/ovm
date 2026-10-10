// Package demo composes the production desktop/device Manager against an
// in-process, loopback-only fake ONVIF camera so the application can be
// exercised end-to-end without any real hardware, disk state, or OS keyring.
//
// The composition is deliberately production-faithful: it drives the real
// internal/app Inventory and MediaProfiles adapters through the completed
// internal/app Manager, pointed at a same-origin internal/fakedevice served
// over an IPv4 loopback listener. Only the persistence seam is substituted —
// a private, concurrency-safe in-memory repository and a
// store.MemorySecretProvider — so nothing ever touches disk or the platform
// keyring.
//
// The demo advertises no streaming features. It explicitly reports RTSP,
// playback, and VLC as unavailable and never implements or claims RTSP,
// despite the synthetic profile stream URIs that the media adapter surfaces.
//
// Dependency direction is internal/demo -> internal/fakedevice, internal/app,
// internal/store. Nothing outside this package depends on it.
package demo

import (
	"context"
	"sort"
	"sync"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/store"
)

// memoryRepository is a private, concurrency-safe implementation of
// app.ManagerRepository backed entirely by process memory. It mirrors the
// observable contract of the production *store.Repository seam closely enough
// for the Manager (deterministic ID ordering, atomic upsert/remove, selection
// repair) while never persisting to disk.
//
// Secrets are delegated to an injected store.SecretProvider, which in the demo
// is always a store.MemorySecretProvider. Metadata and secrets are kept in
// separate maps exactly as the production repository keeps metadata and keyring
// separate.
type memoryRepository struct {
	secrets store.SecretProvider

	mu          sync.RWMutex
	devices     map[string]store.Device
	preferences store.Preferences
}

// newMemoryRepository builds an empty in-memory repository bound to the given
// secret provider. The provider must be non-nil; the demo always supplies a
// store.MemorySecretProvider.
func newMemoryRepository(secrets store.SecretProvider) *memoryRepository {
	return &memoryRepository{
		secrets:     secrets,
		devices:     make(map[string]store.Device),
		preferences: store.DefaultPreferences(),
	}
}

// snapshotLocked assembles a deterministic snapshot. The caller holds the read
// or write lock.
func (r *memoryRepository) snapshotLocked() store.Snapshot {
	ids := make([]string, 0, len(r.devices))
	for id := range r.devices {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	devices := make([]store.Device, 0, len(ids))
	for _, id := range ids {
		devices = append(devices, r.devices[id])
	}
	return store.Snapshot{
		SchemaVersion: store.SchemaVersion,
		Devices:       devices,
		Preferences:   r.preferences,
	}
}

// Snapshot returns a deterministic, deep-enough copy of the current state.
func (r *memoryRepository) Snapshot() store.Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.snapshotLocked()
}

// Device returns a copy of a single device by ID.
func (r *memoryRepository) Device(id string) (store.Device, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	device, exists := r.devices[id]
	if !exists {
		return store.Device{}, store.ErrDeviceNotFound
	}
	return device, nil
}

// Preferences returns a copy of the durable preferences.
func (r *memoryRepository) Preferences() store.Preferences {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.preferences
}

// Upsert validates minimally, assigns a fresh ID when absent, and stores the
// device. It mirrors the production atomicity: either the full device is stored
// or an error is returned and state is unchanged.
func (r *memoryRepository) Upsert(device store.Device) (store.Device, error) {
	if device.Endpoint == "" {
		return store.Device{}, store.ErrInvalidData
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if device.ID == "" {
		id, err := store.NewDeviceID()
		if err != nil {
			return store.Device{}, err
		}
		device.ID = id
	}
	r.devices[device.ID] = device
	return device, nil
}

// Remove deletes a device and clears a matching selection, returning the
// removed device.
func (r *memoryRepository) Remove(id string) (store.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	device, exists := r.devices[id]
	if !exists {
		return store.Device{}, store.ErrDeviceNotFound
	}
	delete(r.devices, id)
	if r.preferences.SelectedDeviceID == id {
		r.preferences.SelectedDeviceID = ""
	}
	return device, nil
}

// Select updates the selected device; an empty ID clears the selection. A
// non-empty, unknown ID is rejected.
func (r *memoryRepository) Select(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id == "" {
		r.preferences.SelectedDeviceID = ""
		return nil
	}
	if _, exists := r.devices[id]; !exists {
		return store.ErrDeviceNotFound
	}
	r.preferences.SelectedDeviceID = id
	return nil
}

// SetPreferences atomically replaces durable preferences, rejecting a dangling
// selection to match the production repository's referential guarantee.
func (r *memoryRepository) SetPreferences(preferences store.Preferences) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if preferences.SelectedDeviceID != "" {
		if _, exists := r.devices[preferences.SelectedDeviceID]; !exists {
			return store.ErrDeviceNotFound
		}
	}
	r.preferences = preferences
	return nil
}

// PutSecret stores an opaque secret payload under an opaque reference.
func (r *memoryRepository) PutSecret(ctx context.Context, reference string, secret store.Secret) error {
	return r.secrets.Put(ctx, reference, secret)
}

// GetSecret retrieves an opaque secret payload by reference.
func (r *memoryRepository) GetSecret(ctx context.Context, reference string) (store.Secret, error) {
	return r.secrets.Get(ctx, reference)
}

// DeleteSecret removes a secret payload by reference. A missing reference
// yields store.ErrSecretNotFound so callers may treat deletion as idempotent.
func (r *memoryRepository) DeleteSecret(ctx context.Context, reference string) error {
	return r.secrets.Delete(ctx, reference)
}

// NewDeviceID returns a fresh crypto-random stable device identifier.
func (r *memoryRepository) NewDeviceID() (string, error) {
	return store.NewDeviceID()
}

// NewSecretReference returns a fresh crypto-random opaque secret reference.
func (r *memoryRepository) NewSecretReference() (string, error) {
	return store.NewSecretReference()
}

// Compile-time assertion that the in-memory repository satisfies the Manager's
// narrow persistence seam.
var _ app.ManagerRepository = (*memoryRepository)(nil)
