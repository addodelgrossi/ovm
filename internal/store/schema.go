package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strings"
	"time"
)

type diskDocumentV1 struct {
	SchemaVersion int               `json:"schema_version"`
	Devices       []diskDeviceV1    `json:"devices"`
	Preferences   diskPreferencesV1 `json:"preferences"`
}

type diskDeviceV1 struct {
	ID                  string            `json:"id"`
	Endpoint            string            `json:"endpoint"`
	EPR                 string            `json:"epr,omitempty"`
	DisplayName         string            `json:"display_name,omitempty"`
	DiscoverySource     DiscoverySource   `json:"discovery_source,omitempty"`
	CredentialReference string            `json:"credential_reference,omitempty"`
	AuthPolicy          *diskAuthPolicyV1 `json:"auth_policy"`
	CreatedAt           string            `json:"created_at"`
	UpdatedAt           string            `json:"updated_at"`
}

type diskAuthPolicyV1 struct {
	AllowBasic         *bool `json:"allow_basic"`
	AllowBasicOverHTTP *bool `json:"allow_basic_over_http"`
	AllowLegacyDigest  *bool `json:"allow_legacy_digest"`
}

type diskPreferencesV1 struct {
	SelectedDeviceID      *string `json:"selected_device_id"`
	SelectedHostInterface *string `json:"selected_host_interface"`
	Locale                *Locale `json:"locale"`
	Theme                 *Theme  `json:"theme"`
}

type schemaHeader struct {
	SchemaVersion *int `json:"schema_version"`
}

type migration func(json.RawMessage) (json.RawMessage, error)

// migrationTable is deliberately explicit even while v1 has no predecessor.
// A future v2 must add the 1 -> 2 migration here rather than silently decoding
// an old shape as the current schema.
var migrationTable = map[int]migration{}

func stateToDisk(value repositoryState) diskDocumentV1 {
	devices := make([]Device, len(value.devices))
	copy(devices, value.devices)
	sort.Slice(devices, func(i, j int) bool { return devices[i].ID < devices[j].ID })

	diskDevices := make([]diskDeviceV1, 0, len(devices))
	for _, device := range devices {
		diskDevices = append(diskDevices, diskDeviceV1{
			ID:                  device.ID,
			Endpoint:            device.Endpoint,
			EPR:                 device.EPR,
			DisplayName:         device.DisplayName,
			DiscoverySource:     device.DiscoverySource,
			CredentialReference: device.CredentialReference,
			AuthPolicy:          authPolicyToDisk(device.AuthPolicy),
			CreatedAt:           formatTimestamp(device.CreatedAt),
			UpdatedAt:           formatTimestamp(device.UpdatedAt),
		})
	}
	return diskDocumentV1{
		SchemaVersion: SchemaVersion,
		Devices:       diskDevices,
		Preferences: diskPreferencesV1{
			SelectedDeviceID:      pointerTo(value.preferences.SelectedDeviceID),
			SelectedHostInterface: pointerTo(value.preferences.SelectedHostInterface),
			Locale:                pointerTo(value.preferences.Locale),
			Theme:                 pointerTo(value.preferences.Theme),
		},
	}
}

func decodeDocument(data []byte) (repositoryState, error) {
	if err := rejectDuplicateJSONNames(data); err != nil {
		return repositoryState{}, ErrMalformedStore
	}
	var header schemaHeader
	if err := json.Unmarshal(data, &header); err != nil || header.SchemaVersion == nil {
		return repositoryState{}, ErrMalformedStore
	}
	version := *header.SchemaVersion
	if version > SchemaVersion {
		return repositoryState{}, ErrFutureSchema
	}
	if version < 1 {
		return repositoryState{}, ErrUnsupportedSchema
	}

	migrated, err := migrateDocument(json.RawMessage(data), version)
	if err != nil {
		return repositoryState{}, err
	}
	var disk diskDocumentV1
	if err := decodeStrictJSON(migrated, &disk); err != nil {
		return repositoryState{}, ErrMalformedStore
	}
	if disk.SchemaVersion != SchemaVersion {
		return repositoryState{}, ErrUnsupportedSchema
	}
	if disk.Devices == nil || disk.Preferences.SelectedDeviceID == nil ||
		disk.Preferences.SelectedHostInterface == nil || disk.Preferences.Locale == nil ||
		disk.Preferences.Theme == nil {
		return repositoryState{}, ErrMalformedStore
	}

	value := repositoryState{
		devices: make([]Device, 0, len(disk.Devices)),
		preferences: Preferences{
			SelectedDeviceID:      *disk.Preferences.SelectedDeviceID,
			SelectedHostInterface: *disk.Preferences.SelectedHostInterface,
			Locale:                *disk.Preferences.Locale,
			Theme:                 *disk.Preferences.Theme,
		},
	}
	for _, encoded := range disk.Devices {
		created, parseErr := parseTimestamp(encoded.CreatedAt)
		if parseErr != nil {
			return repositoryState{}, ErrInvalidData
		}
		updated, parseErr := parseTimestamp(encoded.UpdatedAt)
		if parseErr != nil {
			return repositoryState{}, ErrInvalidData
		}
		policy, validPolicy := authPolicyFromDisk(encoded.AuthPolicy)
		if !validPolicy {
			return repositoryState{}, ErrMalformedStore
		}
		value.devices = append(value.devices, Device{
			ID:                  encoded.ID,
			Endpoint:            encoded.Endpoint,
			EPR:                 encoded.EPR,
			DisplayName:         encoded.DisplayName,
			DiscoverySource:     encoded.DiscoverySource,
			CredentialReference: encoded.CredentialReference,
			AuthPolicy:          policy,
			CreatedAt:           created,
			UpdatedAt:           updated,
		})
	}
	normalizeState(&value)
	if err := validateState(value); err != nil {
		return repositoryState{}, err
	}
	return value, nil
}

func authPolicyToDisk(policy AuthPolicy) *diskAuthPolicyV1 {
	return &diskAuthPolicyV1{
		AllowBasic:         pointerTo(policy.AllowBasic),
		AllowBasicOverHTTP: pointerTo(policy.AllowBasicOverHTTP),
		AllowLegacyDigest:  pointerTo(policy.AllowLegacyDigest),
	}
}

func authPolicyFromDisk(policy *diskAuthPolicyV1) (AuthPolicy, bool) {
	if policy == nil || policy.AllowBasic == nil || policy.AllowBasicOverHTTP == nil || policy.AllowLegacyDigest == nil {
		return AuthPolicy{}, false
	}
	return AuthPolicy{
		AllowBasic:         *policy.AllowBasic,
		AllowBasicOverHTTP: *policy.AllowBasicOverHTTP,
		AllowLegacyDigest:  *policy.AllowLegacyDigest,
	}, true
}

func pointerTo[T any](value T) *T {
	return &value
}

func migrateDocument(document json.RawMessage, version int) (json.RawMessage, error) {
	current := append(json.RawMessage(nil), document...)
	for version < SchemaVersion {
		migrate, exists := migrationTable[version]
		if !exists {
			return nil, ErrUnsupportedSchema
		}
		next, err := migrate(current)
		if err != nil {
			return nil, ErrMalformedStore
		}
		current = next
		version++
	}
	return current, nil
}

func formatTimestamp(value time.Time) string {
	return value.UTC().Round(0).Format(time.RFC3339Nano)
}

func parseTimestamp(value string) (time.Time, error) {
	if value == "" || !strings.HasSuffix(value, "Z") {
		return time.Time{}, ErrInvalidData
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil || parsed.IsZero() || formatTimestamp(parsed) != value {
		return time.Time{}, ErrInvalidData
	}
	return parsed.UTC(), nil
}

func decodeStrictJSON(data []byte, destination any) error {
	if err := rejectDuplicateJSONNames(data); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON has trailing content")
	}
	return nil
}

func rejectDuplicateJSONNames(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := consumeJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("JSON has trailing content")
		}
		return err
	}
	return nil
}

func consumeJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			nameToken, nameErr := decoder.Token()
			if nameErr != nil {
				return nameErr
			}
			name, ok := nameToken.(string)
			if !ok || !canonicalJSONName(name) {
				return errors.New("JSON object name is invalid")
			}
			if _, exists := seen[name]; exists {
				return errors.New("JSON object name is duplicated")
			}
			seen[name] = struct{}{}
			if valueErr := consumeJSONValue(decoder); valueErr != nil {
				return valueErr
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim('}') {
			return errors.New("JSON object is not terminated")
		}
	case '[':
		for decoder.More() {
			if valueErr := consumeJSONValue(decoder); valueErr != nil {
				return valueErr
			}
		}
		end, endErr := decoder.Token()
		if endErr != nil || end != json.Delim(']') {
			return errors.New("JSON array is not terminated")
		}
	default:
		return errors.New("JSON delimiter is invalid")
	}
	return nil
}

func canonicalJSONName(name string) bool {
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		return false
	}
	for index := 1; index < len(name); index++ {
		character := name[index]
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}
