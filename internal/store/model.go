package store

import (
	"crypto/rand"
	"encoding/base64"
	"io"
	"time"
)

const (
	// SchemaVersion is the current on-disk metadata schema.
	SchemaVersion = 1
	// DefaultFileName is the metadata file within the ovm user configuration directory.
	DefaultFileName = "state.json"
)

// Locale is a supported user-interface locale.
type Locale string

const (
	LocaleEnglish             Locale = "en"
	LocaleBrazilianPortuguese Locale = "pt-BR"
)

// Theme is a supported user-interface color preference.
type Theme string

const (
	ThemeSystem Theme = "system"
	ThemeLight  Theme = "light"
	ThemeDark   Theme = "dark"
)

// DiscoverySource records how metadata was first supplied. It is descriptive
// only and never carries runtime discovery status.
type DiscoverySource string

const (
	DiscoverySourceManual      DiscoverySource = "manual"
	DiscoverySourceWSDiscovery DiscoverySource = "ws_discovery"
)

// AuthPolicy stores three distinct authentication downgrade decisions.
type AuthPolicy struct {
	AllowBasic         bool `json:"allow_basic"`
	AllowBasicOverHTTP bool `json:"allow_basic_over_http"`
	AllowLegacyDigest  bool `json:"allow_legacy_digest"`
}

// Device is the complete non-secret metadata persisted for one camera. Endpoint
// is the canonical, query-free Device service URL. CredentialReference is an
// opaque OS-secret-store account name, never a username or password.
type Device struct {
	ID                  string
	Endpoint            string
	EPR                 string
	DisplayName         string
	DiscoverySource     DiscoverySource
	CredentialReference string
	AuthPolicy          AuthPolicy
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// Preferences contains only durable user choices. Empty selected values mean
// no current selection.
type Preferences struct {
	SelectedDeviceID      string
	SelectedHostInterface string
	Locale                Locale
	Theme                 Theme
}

// Snapshot is an immutable-by-convention copy of repository state. Devices is
// always ordered by ID.
type Snapshot struct {
	SchemaVersion int
	Devices       []Device
	Preferences   Preferences
}

// DefaultPreferences returns the secure, portable defaults for a fresh store.
func DefaultPreferences() Preferences {
	return Preferences{Locale: LocaleEnglish, Theme: ThemeSystem}
}

// DeviceByID returns a copy of a device from the snapshot.
func (s Snapshot) DeviceByID(id string) (Device, bool) {
	for _, device := range s.Devices {
		if device.ID == id {
			return device, true
		}
	}
	return Device{}, false
}

// NewDeviceID returns a cryptographically random stable device identifier.
func NewDeviceID() (string, error) {
	return generateOpaqueID(rand.Reader, "dev_", 16)
}

// NewSecretReference returns a cryptographically random opaque keyring account
// reference. It is independent of endpoint and username values.
func NewSecretReference() (string, error) {
	return generateOpaqueID(rand.Reader, "secret_", 24)
}

func generateOpaqueID(source io.Reader, prefix string, size int) (string, error) {
	buffer := make([]byte, size)
	if _, err := io.ReadFull(source, buffer); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(buffer), nil
}
