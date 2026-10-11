package store

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/quick"
	"time"
)

func TestCoordinatorPutRollsBackNewSecretWhenMetadataFails(t *testing.T) {
	t.Parallel()
	repository, err := NewRepository(filepath.Join(t.TempDir(), "state.json"), WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	repository.writer = atomicJSONWriter{before: func(stage atomicStage) error {
		if stage == stageRename {
			return errors.New("injected metadata failure")
		}
		return nil
	}}
	provider := newScriptedProvider()
	coordinator, err := NewCoordinator(repository, provider)
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	coordinator.entropy = bytes.NewReader(bytes.Repeat([]byte{0x31}, 24))
	secret := mustSecret(t, "operator", "value", AuthPolicy{AllowBasic: true})
	_, err = coordinator.Put(context.Background(), Device{ID: "device-one", Endpoint: "camera.local"}, secret)
	if !errors.Is(err, ErrTransaction) {
		t.Fatalf("Put() error = %v, want ErrTransaction", err)
	}
	if len(provider.snapshot()) != 0 {
		t.Fatal("metadata failure left a new secret behind")
	}
	if len(repository.Snapshot().Devices) != 0 {
		t.Fatal("metadata failure changed repository state")
	}
}

func TestCoordinatorUpdateRestoresMetadataWhenOldSecretDeletionFails(t *testing.T) {
	t.Parallel()
	repository, err := NewRepository(filepath.Join(t.TempDir(), "state.json"), WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	original, err := repository.Upsert(Device{
		ID:                  "device-one",
		Endpoint:            "camera.local",
		DisplayName:         "before",
		CredentialReference: "secret-old",
	})
	if err != nil {
		t.Fatalf("baseline Upsert() error = %v", err)
	}
	provider := newScriptedProvider()
	provider.values["secret-old"] = mustSecret(t, "old-user", "old-value", AuthPolicy{})
	provider.deleteErrors["secret-old"] = ErrSecretDenied
	coordinator, err := NewCoordinator(repository, provider)
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	coordinator.entropy = bytes.NewReader(bytes.Repeat([]byte{0x32}, 24))
	_, err = coordinator.Put(context.Background(), Device{ID: original.ID, Endpoint: original.Endpoint, DisplayName: "after"}, mustSecret(t, "new-user", "new-value", AuthPolicy{AllowLegacyDigest: true}))
	if !errors.Is(err, ErrTransaction) || !errors.Is(err, ErrSecretDenied) {
		t.Fatalf("Put() error = %v, want transaction wrapping denied", err)
	}
	current, err := repository.Device(original.ID)
	if err != nil {
		t.Fatalf("Device() error = %v", err)
	}
	if current.DisplayName != "before" || current.CredentialReference != "secret-old" {
		t.Fatalf("metadata was not restored: %#v", current)
	}
	values := provider.snapshot()
	if len(values) != 1 {
		t.Fatalf("provider value count = %d, want 1", len(values))
	}
	if _, exists := values["secret-old"]; !exists {
		t.Fatal("old secret was not retained")
	}
}

func TestCoordinatorForgetSemantics(t *testing.T) {
	t.Run("missing secret is idempotent success", func(t *testing.T) {
		t.Parallel()
		repository := mustRepositoryWithReferencedDevice(t)
		provider := newScriptedProvider()
		coordinator, err := NewCoordinator(repository, provider)
		if err != nil {
			t.Fatalf("NewCoordinator() error = %v", err)
		}
		if err := coordinator.Forget(context.Background(), "device-one"); err != nil {
			t.Fatalf("Forget() error = %v", err)
		}
		if len(repository.Snapshot().Devices) != 0 {
			t.Fatal("metadata remains after idempotent forget")
		}
	})

	t.Run("denied deletion restores metadata and selection", func(t *testing.T) {
		t.Parallel()
		repository := mustRepositoryWithReferencedDevice(t)
		provider := newScriptedProvider()
		provider.values["secret-old"] = mustSecret(t, "operator", "value", AuthPolicy{})
		provider.deleteErrors["secret-old"] = ErrSecretDenied
		provider.onDelete = func() { _ = repository.Snapshot() }
		coordinator, err := NewCoordinator(repository, provider)
		if err != nil {
			t.Fatalf("NewCoordinator() error = %v", err)
		}
		err = coordinator.Forget(context.Background(), "device-one")
		if !errors.Is(err, ErrTransaction) || !errors.Is(err, ErrSecretDenied) {
			t.Fatalf("Forget() error = %v, want transaction wrapping denied", err)
		}
		snapshot := repository.Snapshot()
		if len(snapshot.Devices) != 1 || snapshot.Preferences.SelectedDeviceID != "device-one" {
			t.Fatalf("rollback snapshot = %#v", snapshot)
		}
	})
}

func TestCoordinatorNeverHoldsRepositoryLockDuringProviderCalls(t *testing.T) {
	t.Parallel()
	repository, err := NewRepository(filepath.Join(t.TempDir(), "state.json"), WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	provider := newScriptedProvider()
	provider.onPut = func() { _ = repository.Snapshot() }
	provider.onDelete = func() { _ = repository.Snapshot() }
	coordinator, err := NewCoordinator(repository, provider)
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	coordinator.entropy = bytes.NewReader(bytes.Repeat([]byte{0x33}, 48))
	secret := mustSecret(t, "operator", "value", AuthPolicy{})
	saved, err := coordinator.Put(context.Background(), Device{ID: "device-one", Endpoint: "camera.local"}, secret)
	if err != nil {
		t.Fatalf("Put() error = %v", err)
	}
	if err := coordinator.Forget(context.Background(), saved.ID); err != nil {
		t.Fatalf("Forget() error = %v", err)
	}
}

func TestPropertyMetadataNeverPersistsSecretMaterial(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	repository, err := NewRepository(path, WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	provider := NewMemorySecretProvider()
	coordinator, err := NewCoordinator(repository, provider)
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}

	property := func(first, second uint64) bool {
		username := fmt.Sprintf("credential-user-marker-%016x", first)
		password := fmt.Sprintf("credential-password-marker-%016x", second)
		secret, secretErr := NewSecret(username, password, AuthPolicy{AllowBasic: true, AllowLegacyDigest: first%2 == 0})
		if secretErr != nil {
			return false
		}
		_, putErr := coordinator.Put(context.Background(), Device{ID: "device-one", Endpoint: "camera.local", DisplayName: "Synthetic camera"}, secret)
		if putErr != nil {
			return false
		}
		// The path is owned by this test's temporary directory.
		encoded, readErr := os.ReadFile(path) //nolint:gosec // test-controlled path
		if readErr != nil {
			return false
		}
		lower := strings.ToLower(string(encoded))
		return !strings.Contains(string(encoded), username) &&
			!strings.Contains(string(encoded), password) &&
			!strings.Contains(lower, `"username"`) &&
			!strings.Contains(lower, `"password"`) &&
			!strings.Contains(lower, "demo_mode") &&
			!strings.Contains(lower, "runtime_status")
	}
	if err := quick.Check(property, &quick.Config{MaxCount: 75}); err != nil {
		t.Fatalf("no-secret property failed: %v", err)
	}
}

func mustRepositoryWithReferencedDevice(t *testing.T) *Repository {
	t.Helper()
	repository, err := NewRepository(filepath.Join(t.TempDir(), "state.json"), WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	if _, err := repository.Upsert(Device{ID: "device-one", Endpoint: "camera.local", CredentialReference: "secret-old"}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if err := repository.Select("device-one"); err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	return repository
}

func mustSecret(t *testing.T, username, password string, policy AuthPolicy) Secret {
	t.Helper()
	secret, err := NewSecret(username, password, policy)
	if err != nil {
		t.Fatalf("NewSecret() error = %v", err)
	}
	return secret
}

type scriptedProvider struct {
	mu           sync.Mutex
	values       map[string]Secret
	putError     error
	deleteErrors map[string]error
	onPut        func()
	onDelete     func()
}

func newScriptedProvider() *scriptedProvider {
	return &scriptedProvider{values: make(map[string]Secret), deleteErrors: make(map[string]error)}
}

func (provider *scriptedProvider) Put(_ context.Context, reference string, secret Secret) error {
	if provider.onPut != nil {
		provider.onPut()
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if provider.putError != nil {
		return provider.putError
	}
	provider.values[reference] = secret
	return nil
}

func (provider *scriptedProvider) Get(_ context.Context, reference string) (Secret, error) {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	secret, exists := provider.values[reference]
	if !exists {
		return Secret{}, ErrSecretNotFound
	}
	return secret, nil
}

func (provider *scriptedProvider) Delete(_ context.Context, reference string) error {
	if provider.onDelete != nil {
		provider.onDelete()
	}
	provider.mu.Lock()
	defer provider.mu.Unlock()
	if err := provider.deleteErrors[reference]; err != nil {
		return err
	}
	if _, exists := provider.values[reference]; !exists {
		return ErrSecretNotFound
	}
	delete(provider.values, reference)
	return nil
}

func (provider *scriptedProvider) snapshot() map[string]Secret {
	provider.mu.Lock()
	defer provider.mu.Unlock()
	copyOfValues := make(map[string]Secret, len(provider.values))
	for reference, secret := range provider.values {
		copyOfValues[reference] = secret
	}
	return copyOfValues
}

func TestCoordinatorCancellationAfterPutUsesIndependentCleanup(t *testing.T) {
	t.Parallel()
	repository, err := NewRepository(filepath.Join(t.TempDir(), "state.json"), WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	provider := newScriptedProvider()
	ctx, cancel := context.WithCancel(context.Background())
	provider.onPut = cancel
	coordinator, err := NewCoordinator(repository, provider)
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	coordinator.entropy = bytes.NewReader(bytes.Repeat([]byte{0x34}, 24))
	_, err = coordinator.Put(ctx, Device{ID: "device-one", Endpoint: "camera.local"}, mustSecret(t, "operator", "value", AuthPolicy{}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Put() error = %v, want context.Canceled", err)
	}
	if len(provider.snapshot()) != 0 {
		t.Fatal("canceled Put() stranded a secret")
	}
	if len(repository.Snapshot().Devices) != 0 {
		t.Fatal("canceled Put() published metadata")
	}
}

func TestCoordinatorReportsRollbackConflictWithoutOverwritingNewerMetadata(t *testing.T) {
	t.Parallel()
	first := mustRepositoryWithReferencedDevice(t)
	second, err := NewRepository(first.Path(), WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("second NewRepository() error = %v", err)
	}
	provider := newScriptedProvider()
	provider.values["secret-old"] = mustSecret(t, "operator", "value", AuthPolicy{})
	provider.deleteErrors["secret-old"] = ErrSecretDenied
	var callbackErr error
	provider.onDelete = func() {
		callbackErr = second.SetPreferences(Preferences{
			SelectedHostInterface: "bridge0",
			Locale:                LocaleEnglish,
			Theme:                 ThemeSystem,
		})
	}
	coordinator, err := NewCoordinator(first, provider)
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	err = coordinator.Forget(context.Background(), "device-one")
	if callbackErr != nil {
		t.Fatalf("concurrent metadata callback error = %v", callbackErr)
	}
	if !errors.Is(err, ErrRollbackFailed) || !errors.Is(err, ErrConflict) || !errors.Is(err, ErrSecretDenied) {
		t.Fatalf("Forget() error = %v, want rollback/conflict/denied", err)
	}
	reopened, err := NewRepository(first.Path())
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	snapshot := reopened.Snapshot()
	if len(snapshot.Devices) != 0 || snapshot.Preferences.SelectedHostInterface != "bridge0" {
		t.Fatalf("newer metadata was overwritten: %#v", snapshot)
	}
}

func TestCoordinatorSanitizesUnknownProviderErrors(t *testing.T) {
	t.Parallel()
	repository, err := NewRepository(filepath.Join(t.TempDir(), "state.json"), WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	provider := newScriptedProvider()
	provider.putError = errors.New("synthetic-secret-backend-marker")
	coordinator, err := NewCoordinator(repository, provider)
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	coordinator.entropy = bytes.NewReader(bytes.Repeat([]byte{0x35}, 24))
	_, err = coordinator.Put(context.Background(), Device{ID: "device-one", Endpoint: "camera.local"}, mustSecret(t, "operator", "value", AuthPolicy{}))
	if !errors.Is(err, ErrSecretUnavailable) {
		t.Fatalf("Put() error = %v, want ErrSecretUnavailable", err)
	}
	for current := err; current != nil; current = errors.Unwrap(current) {
		if strings.Contains(current.Error(), "synthetic-secret-backend-marker") {
			t.Fatalf("error chain leaked backend material: %q", current)
		}
	}
}

func TestCoordinatorCancellationDuringMetadataCommitRollsBackBothResources(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	repository, err := NewRepository(filepath.Join(t.TempDir(), "state.json"), WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	repository.writer = atomicJSONWriter{before: func(stage atomicStage) error {
		if stage == stageParentSync {
			cancel()
		}
		return nil
	}}
	provider := newScriptedProvider()
	coordinator, err := NewCoordinator(repository, provider)
	if err != nil {
		t.Fatalf("NewCoordinator() error = %v", err)
	}
	coordinator.entropy = bytes.NewReader(bytes.Repeat([]byte{0x36}, 24))
	_, err = coordinator.Put(ctx, Device{ID: "device-one", Endpoint: "camera.local"}, mustSecret(t, "operator", "value", AuthPolicy{}))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Put() error = %v, want context.Canceled", err)
	}
	if len(provider.snapshot()) != 0 || len(repository.Snapshot().Devices) != 0 {
		t.Fatal("cancellation during metadata commit was not fully rolled back")
	}
}
