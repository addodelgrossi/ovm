package store

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

var fixedStoreTime = time.Date(2026, time.October, 9, 12, 34, 56, 789, time.UTC)

func TestRepositoryFreshRoundTripDeterministicAndPermissions(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "config", DefaultFileName)
	entropy := bytes.NewReader(bytes.Repeat([]byte{0x42}, 64))
	repository, err := NewRepository(path, WithClock(func() time.Time { return fixedStoreTime }), WithEntropy(entropy))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fresh store stat error = %v, want not-exist", err)
	}
	fresh := repository.Snapshot()
	if fresh.SchemaVersion != SchemaVersion || len(fresh.Devices) != 0 || fresh.Preferences != DefaultPreferences() {
		t.Fatalf("fresh snapshot = %#v", fresh)
	}

	saved, err := repository.Upsert(Device{
		Endpoint:            " HTTP://CAMERA.Local:80/onvif/device_service/ ",
		EPR:                 "urn:uuid:synthetic-camera",
		DisplayName:         " Front Door ",
		DiscoverySource:     DiscoverySourceWSDiscovery,
		CredentialReference: "secret_reference-1",
		AuthPolicy:          AuthPolicy{AllowBasic: true, AllowBasicOverHTTP: true, AllowLegacyDigest: true},
		CreatedAt:           time.Unix(1, 0),
		UpdatedAt:           time.Unix(2, 0),
	})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if !strings.HasPrefix(saved.ID, "dev_") {
		t.Fatalf("generated ID = %q", saved.ID)
	}
	if saved.Endpoint != "http://camera.local/onvif/device_service" {
		t.Fatalf("canonical endpoint = %q", saved.Endpoint)
	}
	if saved.DisplayName != "Front Door" {
		t.Fatalf("display name = %q", saved.DisplayName)
	}
	if !saved.CreatedAt.Equal(fixedStoreTime) || !saved.UpdatedAt.Equal(fixedStoreTime) {
		t.Fatalf("controlled timestamps = %v / %v", saved.CreatedAt, saved.UpdatedAt)
	}

	preferences := Preferences{
		SelectedDeviceID:      saved.ID,
		SelectedHostInterface: "en0",
		Locale:                LocaleBrazilianPortuguese,
		Theme:                 ThemeDark,
	}
	if err := repository.SetPreferences(preferences); err != nil {
		t.Fatalf("SetPreferences() error = %v", err)
	}
	// path is owned by this test's temporary directory.
	before, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if err := repository.SetPreferences(preferences); err != nil {
		t.Fatalf("second SetPreferences() error = %v", err)
	}
	after, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("second ReadFile() error = %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("identical state did not produce deterministic JSON")
	}

	directoryInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("directory stat error = %v", err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("directory mode = %o, want 700", got)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("file stat error = %v", err)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Fatalf("file mode = %o, want 600", got)
	}

	reopened, err := NewRepository(path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	if !reflect.DeepEqual(repository.Snapshot(), reopened.Snapshot()) {
		t.Fatalf("round trip mismatch:\nfirst=%#v\nsecond=%#v", repository.Snapshot(), reopened.Snapshot())
	}

	copyOfState := reopened.Snapshot()
	copyOfState.Devices[0].DisplayName = "mutated copy"
	copyOfState.Preferences.Theme = ThemeLight
	actual := reopened.Snapshot()
	if actual.Devices[0].DisplayName == "mutated copy" || actual.Preferences.Theme == ThemeLight {
		t.Fatal("Snapshot() exposed mutable repository state")
	}
}

func TestRepositoryOrdersConcurrentUpdatesAndSurvivesReload(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	repository, err := NewRepository(path, WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}

	const count = 48
	var wait sync.WaitGroup
	errorsChannel := make(chan error, count)
	for index := range count {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, updateErr := repository.Upsert(Device{
				ID:       fmt.Sprintf("device-%03d", index),
				Endpoint: fmt.Sprintf("http://camera-%03d.local/onvif/device_service", index),
			})
			errorsChannel <- updateErr
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for updateErr := range errorsChannel {
		if updateErr != nil {
			t.Fatalf("concurrent Upsert() error = %v", updateErr)
		}
	}

	snapshot := repository.Snapshot()
	if len(snapshot.Devices) != count {
		t.Fatalf("device count = %d, want %d", len(snapshot.Devices), count)
	}
	for index, device := range snapshot.Devices {
		want := fmt.Sprintf("device-%03d", index)
		if device.ID != want {
			t.Fatalf("device[%d].ID = %q, want %q", index, device.ID, want)
		}
	}
	reopened, err := NewRepository(path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	if !reflect.DeepEqual(snapshot, reopened.Snapshot()) {
		t.Fatal("concurrent state did not survive reload")
	}
}

func TestRepositoryRemoveClearsSelection(t *testing.T) {
	t.Parallel()
	repository, err := NewRepository(filepath.Join(t.TempDir(), "state.json"), WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	saved, err := repository.Upsert(Device{ID: "device-one", Endpoint: "camera.local"})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	if err := repository.Select(saved.ID); err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if _, err := repository.Remove(saved.ID); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if got := repository.Preferences().SelectedDeviceID; got != "" {
		t.Fatalf("selected device = %q, want empty", got)
	}
	if _, err := repository.Device(saved.ID); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("Device() error = %v, want ErrDeviceNotFound", err)
	}
}

func TestRepositoryFailureInjectionPreservesPublishedState(t *testing.T) {
	stages := []atomicStage{
		stageTempCreate,
		stageTempMode,
		stageEncode,
		stageFileSync,
		stageFileClose,
		stageFinalMode,
		stageRename,
	}
	for _, stage := range stages {
		stage := stage
		t.Run(string(stage), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "config", "state.json")
			repository, err := NewRepository(path, WithClock(func() time.Time { return fixedStoreTime }))
			if err != nil {
				t.Fatalf("NewRepository() error = %v", err)
			}
			baseline, err := repository.Upsert(Device{ID: "device-one", Endpoint: "camera.local", DisplayName: "before"})
			if err != nil {
				t.Fatalf("baseline Upsert() error = %v", err)
			}
			baselineBytes, err := os.ReadFile(path) //nolint:gosec // test-controlled path
			if err != nil {
				t.Fatalf("baseline ReadFile() error = %v", err)
			}
			repository.writer = atomicJSONWriter{before: func(current atomicStage) error {
				if current == stage {
					return errors.New("injected failure")
				}
				return nil
			}}
			_, err = repository.Upsert(Device{ID: baseline.ID, Endpoint: baseline.Endpoint, DisplayName: "after"})
			if !errors.Is(err, ErrPersistence) {
				t.Fatalf("Upsert() error = %v, want ErrPersistence", err)
			}
			if got := repository.Snapshot().Devices[0].DisplayName; got != "before" {
				t.Fatalf("memory changed to %q after pre-commit failure", got)
			}
			currentBytes, err := os.ReadFile(path) //nolint:gosec // test-controlled path
			if err != nil {
				t.Fatalf("ReadFile() error = %v", err)
			}
			if !bytes.Equal(currentBytes, baselineBytes) {
				t.Fatal("published file changed after pre-commit failure")
			}
			temporary, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".state-*.tmp"))
			if err != nil {
				t.Fatalf("Glob() error = %v", err)
			}
			if len(temporary) != 0 {
				t.Fatalf("temporary files remain: %v", temporary)
			}
		})
	}
}

func TestRepositoryParentSyncFailureKeepsMemoryConsistentWithRename(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	repository, err := NewRepository(path, WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	device, err := repository.Upsert(Device{ID: "device-one", Endpoint: "camera.local", DisplayName: "before"})
	if err != nil {
		t.Fatalf("baseline Upsert() error = %v", err)
	}
	repository.writer = atomicJSONWriter{before: func(stage atomicStage) error {
		if stage == stageParentSync {
			return errors.New("injected parent sync failure")
		}
		return nil
	}}
	_, err = repository.Upsert(Device{ID: device.ID, Endpoint: device.Endpoint, DisplayName: "after"})
	if !errors.Is(err, ErrPersistence) {
		t.Fatalf("Upsert() error = %v, want ErrPersistence", err)
	}
	if got := repository.Snapshot().Devices[0].DisplayName; got != "after" {
		t.Fatalf("in-memory name = %q, want renamed file state", got)
	}
	reopened, err := NewRepository(path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	if got := reopened.Snapshot().Devices[0].DisplayName; got != "after" {
		t.Fatalf("reloaded name = %q, want after", got)
	}
}

func TestRepositoriesSharingAPathMergeConcurrentUpdates(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	first, err := NewRepository(path, WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("first NewRepository() error = %v", err)
	}
	second, err := NewRepository(path, WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("second NewRepository() error = %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	go func() {
		<-start
		_, updateErr := first.Upsert(Device{ID: "device-one", Endpoint: "one.local"})
		results <- updateErr
	}()
	go func() {
		<-start
		_, updateErr := second.Upsert(Device{ID: "device-two", Endpoint: "two.local"})
		results <- updateErr
	}()
	close(start)
	for range 2 {
		if updateErr := <-results; updateErr != nil {
			t.Fatalf("concurrent handle update error = %v", updateErr)
		}
	}

	reopened, err := NewRepository(path)
	if err != nil {
		t.Fatalf("reopen error = %v", err)
	}
	if got := len(reopened.Snapshot().Devices); got != 2 {
		t.Fatalf("device count = %d, want 2", got)
	}
}

func TestRepositoryOversizedFileIsMalformed(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, bytes.Repeat([]byte{' '}, maxStoreBytes+1), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := NewRepository(path); !errors.Is(err, ErrMalformedStore) {
		t.Fatalf("NewRepository() error = %v, want ErrMalformedStore", err)
	}
}

func TestRepositoryDirectoryFailuresDoNotPublish(t *testing.T) {
	stages := []atomicStage{
		stageDirectoryCreate,
		stageDirectoryMode,
		stageDirectoryParentOpen,
		stageDirectoryParentSync,
		stageDirectoryParentClose,
	}
	for _, stage := range stages {
		stage := stage
		t.Run(string(stage), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "new-config", "state.json")
			repository, err := NewRepository(path, WithClock(func() time.Time { return fixedStoreTime }))
			if err != nil {
				t.Fatalf("NewRepository() error = %v", err)
			}
			repository.writer = atomicJSONWriter{before: func(current atomicStage) error {
				if current == stage {
					return errors.New("injected directory failure")
				}
				return nil
			}}
			if _, err := repository.Upsert(Device{ID: "device-one", Endpoint: "camera.local"}); !errors.Is(err, ErrPersistence) {
				t.Fatalf("Upsert() error = %v, want ErrPersistence", err)
			}
			if len(repository.Snapshot().Devices) != 0 {
				t.Fatal("directory durability failure changed memory")
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("directory durability failure published file: %v", err)
			}

			repository.writer = atomicJSONWriter{}
			if _, err := repository.Upsert(Device{ID: "device-one", Endpoint: "camera.local"}); err != nil {
				t.Fatalf("retry Upsert() error = %v", err)
			}
			directoryInfo, err := os.Stat(filepath.Dir(path))
			if err != nil {
				t.Fatalf("directory stat error = %v", err)
			}
			if got := directoryInfo.Mode().Perm(); got != 0o700 {
				t.Fatalf("directory mode after retry = %o, want 700", got)
			}
			fileInfo, err := os.Stat(path)
			if err != nil {
				t.Fatalf("file stat error = %v", err)
			}
			if got := fileInfo.Mode().Perm(); got != 0o600 {
				t.Fatalf("file mode after retry = %o, want 600", got)
			}
		})
	}
}

func TestLoadRejectsMalformedFileWithoutReplacingSnapshotOrDisk(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "state.json")
	repository, err := NewRepository(path, WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	if _, err := repository.Upsert(Device{ID: "device-one", Endpoint: "camera.local"}); err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}
	before := repository.Snapshot()
	malformed := []byte(`{"schema_version":1,"password":"synthetic-marker"}`)
	if err := os.WriteFile(path, malformed, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := repository.Load(); !errors.Is(err, ErrMalformedStore) {
		t.Fatalf("Load() error = %v, want ErrMalformedStore", err)
	}
	if !reflect.DeepEqual(repository.Snapshot(), before) {
		t.Fatal("failed Load() replaced the prior in-memory snapshot")
	}
	persisted, err := os.ReadFile(path) //nolint:gosec // test-controlled path
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if !bytes.Equal(persisted, malformed) {
		t.Fatal("failed Load() silently replaced the malformed file")
	}
}

func TestDefaultPathUsesUserConfigDirectory(t *testing.T) {
	t.Parallel()
	root, err := os.UserConfigDir()
	if err != nil {
		t.Fatalf("UserConfigDir() error = %v", err)
	}
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath() error = %v", err)
	}
	want := filepath.Join(root, "ovm", DefaultFileName)
	if path != want {
		t.Fatalf("DefaultPath() = %q, want %q", path, want)
	}
}
