package store

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRepositoryRejectsMalformedFutureAndInvalidDocuments(t *testing.T) {
	t.Parallel()
	validDevice := Device{
		ID:                  "device-one",
		Endpoint:            "http://camera.local/onvif/device_service",
		EPR:                 "urn:uuid:camera-one",
		CredentialReference: "secret-one",
		CreatedAt:           fixedStoreTime,
		UpdatedAt:           fixedStoreTime,
	}
	valid := mustDocumentJSON(t, repositoryState{devices: []Device{validDevice}, preferences: DefaultPreferences()})

	duplicateID := validDevice
	duplicateID.Endpoint = "http://other.local/onvif/device_service"
	duplicateID.EPR = "urn:uuid:camera-two"
	duplicateID.CredentialReference = "secret-two"
	duplicateEndpoint := validDevice
	duplicateEndpoint.ID = "device-two"
	duplicateEndpoint.EPR = "urn:uuid:camera-two"
	duplicateEndpoint.CredentialReference = "secret-two"
	duplicateReference := duplicateEndpoint
	duplicateReference.Endpoint = "http://other.local/onvif/device_service"
	duplicateReference.CredentialReference = validDevice.CredentialReference

	tests := []struct {
		name string
		data []byte
		want error
	}{
		{name: "syntax", data: []byte(`{"schema_version":1`), want: ErrMalformedStore},
		{name: "trailing document", data: append(append([]byte(nil), valid...), []byte(` {}`)...), want: ErrMalformedStore},
		{name: "unknown username field", data: []byte(strings.Replace(string(valid), `"endpoint":`, `"username":"synthetic-user-marker","endpoint":`, 1)), want: ErrMalformedStore},
		{name: "duplicate JSON name", data: []byte(strings.Replace(string(valid), `"schema_version": 1`, `"schema_version": 1,"schema_version": 1`, 1)), want: ErrMalformedStore},
		{name: "missing schema version", data: []byte(`{"devices":[],"preferences":{"selected_device_id":"","selected_host_interface":"","locale":"en","theme":"system"}}`), want: ErrMalformedStore},
		{name: "missing devices", data: []byte(`{"schema_version":1,"preferences":{"selected_device_id":"","selected_host_interface":"","locale":"en","theme":"system"}}`), want: ErrMalformedStore},
		{name: "missing selected preference", data: []byte(`{"schema_version":1,"devices":[],"preferences":{"selected_host_interface":"","locale":"en","theme":"system"}}`), want: ErrMalformedStore},
		{name: "uppercase schema alias", data: []byte(`{"SCHEMA_VERSION":1,"devices":[],"preferences":{"selected_device_id":"","selected_host_interface":"","locale":"en","theme":"system"}}`), want: ErrMalformedStore},
		{name: "conflicting folded versions", data: []byte(`{"schema_version":2,"SCHEMA_VERSION":1,"devices":[],"preferences":{"selected_device_id":"","selected_host_interface":"","locale":"en","theme":"system"}}`), want: ErrMalformedStore},
		{name: "missing auth flag", data: []byte(strings.Replace(string(valid), `"allow_legacy_digest": false`, ``, 1)), want: ErrMalformedStore},
		{name: "null auth policy", data: []byte(strings.Replace(string(valid), `"auth_policy": {`, `"auth_policy_ignored": {`, 1)), want: ErrMalformedStore},
		{name: "future", data: []byte(`{"schema_version":2}`), want: ErrFutureSchema},
		{name: "version zero", data: []byte(`{"schema_version":0}`), want: ErrUnsupportedSchema},
		{name: "duplicate ID", data: mustDocumentJSON(t, repositoryState{devices: []Device{validDevice, duplicateID}, preferences: DefaultPreferences()}), want: ErrDuplicate},
		{name: "duplicate endpoint", data: mustDocumentJSON(t, repositoryState{devices: []Device{validDevice, duplicateEndpoint}, preferences: DefaultPreferences()}), want: ErrDuplicate},
		{name: "duplicate credential reference", data: mustDocumentJSON(t, repositoryState{devices: []Device{validDevice, duplicateReference}, preferences: DefaultPreferences()}), want: ErrDuplicate},
		{name: "noncanonical endpoint", data: []byte(strings.Replace(string(valid), "http://camera.local/onvif/device_service", "HTTP://CAMERA.local:80/onvif/device_service/", 1)), want: ErrInvalidData},
		{name: "query endpoint", data: []byte(strings.Replace(string(valid), "http://camera.local/onvif/device_service", "http://camera.local/onvif/device_service?token=synthetic", 1)), want: ErrInvalidData},
		{name: "invalid timestamp order", data: mustDocumentJSON(t, repositoryState{devices: []Device{{ID: validDevice.ID, Endpoint: validDevice.Endpoint, CreatedAt: fixedStoreTime, UpdatedAt: fixedStoreTime.Add(-time.Second)}}, preferences: DefaultPreferences()}), want: ErrInvalidData},
		{name: "selected device absent", data: mustDocumentJSON(t, repositoryState{preferences: Preferences{SelectedDeviceID: "missing-device", Locale: LocaleEnglish, Theme: ThemeSystem}}), want: ErrInvalidData},
		{name: "invalid locale", data: mustDocumentJSON(t, repositoryState{preferences: Preferences{Locale: "fr", Theme: ThemeSystem}}), want: ErrInvalidData},
		{name: "invalid theme", data: mustDocumentJSON(t, repositoryState{preferences: Preferences{Locale: LocaleEnglish, Theme: "midnight"}}), want: ErrInvalidData},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "state.json")
			if err := os.WriteFile(path, test.data, 0o600); err != nil {
				t.Fatalf("WriteFile() error = %v", err)
			}
			_, err := NewRepository(path)
			if !errors.Is(err, test.want) {
				t.Fatalf("NewRepository() error = %v, want %v", err, test.want)
			}
			if strings.Contains(strings.ToLower(err.Error()), "synthetic-user-marker") || strings.Contains(strings.ToLower(err.Error()), "token=synthetic") {
				t.Fatalf("error leaked persisted content: %q", err)
			}
		})
	}
}

func TestMigrationTableHandlesCurrentVersionAndRejectsMissingPredecessor(t *testing.T) {
	t.Parallel()
	current := json.RawMessage(`{"schema_version":1,"devices":[],"preferences":{"selected_device_id":"","selected_host_interface":"","locale":"en","theme":"system"}}`)
	migrated, err := migrateDocument(current, SchemaVersion)
	if err != nil {
		t.Fatalf("migrate current error = %v", err)
	}
	if string(migrated) != string(current) {
		t.Fatalf("current migration changed bytes: %s", migrated)
	}
	if _, err := migrateDocument(json.RawMessage(`{"schema_version":0}`), 0); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("missing predecessor error = %v, want ErrUnsupportedSchema", err)
	}
}

func TestRepositoryRejectsEndpointSecretsWithoutLeakingThem(t *testing.T) {
	t.Parallel()
	repository, err := NewRepository(filepath.Join(t.TempDir(), "state.json"), WithClock(func() time.Time { return fixedStoreTime }))
	if err != nil {
		t.Fatalf("NewRepository() error = %v", err)
	}
	inputs := []string{
		"http://synthetic-user:synthetic-password@camera.local/onvif/device_service",
		"http://camera.local/onvif/device_service?credential=synthetic-password",
	}
	for _, input := range inputs {
		_, err := repository.Upsert(Device{Endpoint: input})
		if !errors.Is(err, ErrInvalidData) {
			t.Fatalf("Upsert(%q) error = %v, want ErrInvalidData", input, err)
		}
		if strings.Contains(err.Error(), "synthetic-user") || strings.Contains(err.Error(), "synthetic-password") {
			t.Fatalf("error leaked endpoint material: %q", err)
		}
	}
	if _, err := os.Stat(repository.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid endpoints published a file: %v", err)
	}
}

func mustDocumentJSON(t *testing.T, value repositoryState) []byte {
	t.Helper()
	normalizeState(&value)
	encoded, err := json.MarshalIndent(stateToDisk(value), "", "  ")
	if err != nil {
		t.Fatalf("json.MarshalIndent() error = %v", err)
	}
	return append(encoded, '\n')
}
