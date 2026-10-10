package app

import "github.com/addodelgrossi/ovm/internal/store"

// Device-manager DTOs.
//
// Every type here is JSON/Wails-safe: English, deterministic, and free of
// credential references, secret material, raw unsafe URLs, and backend error
// strings. In particular, a DeviceDTO never exposes the opaque credential
// reference stored alongside the metadata; it only reports whether credentials
// are present. The canonical endpoint is always a query-free, safe HTTP(S) URL
// as produced by discovery.CanonicalizeEndpoint, so it can be rendered as-is.

// DeviceDiscoverySource mirrors the stable store discovery-source classification
// as a boundary-safe string.
type DeviceDiscoverySource string

const (
	// DeviceSourceManual means the device was added by a manual endpoint.
	DeviceSourceManual DeviceDiscoverySource = "manual"
	// DeviceSourceWSDiscovery means the device was first seen via WS-Discovery.
	DeviceSourceWSDiscovery DeviceDiscoverySource = "ws_discovery"
)

// AuthPolicyDTO reports the three independent, explicit authentication
// downgrade decisions persisted for a device. It never carries secret material.
type AuthPolicyDTO struct {
	// AllowBasic reports whether HTTP Basic is permitted (HTTPS still required
	// unless AllowBasicOverHTTP is also set).
	AllowBasic bool `json:"allow_basic"`
	// AllowBasicOverHTTP reports whether Basic over plaintext HTTP is permitted.
	AllowBasicOverHTTP bool `json:"allow_basic_over_http"`
	// AllowLegacyDigest reports whether legacy no-qop Digest is permitted.
	AllowLegacyDigest bool `json:"allow_legacy_digest"`
}

// DeviceDTO is the normalized, boundary-safe view of one persisted device. It
// carries the canonical safe endpoint, the WS-Addressing endpoint reference
// when known, and whether credentials are stored, but never the opaque
// credential reference itself.
type DeviceDTO struct {
	// ID is the stable device identifier.
	ID string `json:"id"`
	// Endpoint is the canonical, query-free, safe HTTP(S) device-service URL.
	Endpoint string `json:"endpoint"`
	// EndpointReference is the device's WS-Addressing identity when known.
	EndpointReference string `json:"endpoint_reference,omitempty"`
	// DisplayName is the operator-facing label when set.
	DisplayName string `json:"display_name,omitempty"`
	// DiscoverySource records how the device was first supplied.
	DiscoverySource DeviceDiscoverySource `json:"discovery_source,omitempty"`
	// HasCredentials reports whether a stored secret is referenced. The opaque
	// reference itself is never exposed.
	HasCredentials bool `json:"has_credentials"`
	// AuthPolicy reports the three persisted authentication decisions.
	AuthPolicy AuthPolicyDTO `json:"auth_policy"`
	// Selected reports whether this device is the currently selected device.
	Selected bool `json:"selected"`
	// Status reports the manager-memory runtime status of the device.
	Status DeviceRuntimeStatus `json:"status"`
	// CreatedAt is the RFC3339 UTC creation timestamp.
	CreatedAt string `json:"created_at,omitempty"`
	// UpdatedAt is the RFC3339 UTC last-update timestamp.
	UpdatedAt string `json:"updated_at,omitempty"`
}

// DeviceRuntimeStatus is a stable, manager-memory-only runtime classification.
// It is never persisted and is cleaned up when a device is removed.
type DeviceRuntimeStatus string

const (
	// StatusUnknown means no runtime operation has produced a status yet.
	StatusUnknown DeviceRuntimeStatus = "unknown"
	// StatusReachable means the last network operation succeeded.
	StatusReachable DeviceRuntimeStatus = "reachable"
	// StatusAuthenticationRequired means the last operation needed credentials.
	StatusAuthenticationRequired DeviceRuntimeStatus = "authentication_required"
	// StatusUnreachable means the last network operation failed to connect.
	StatusUnreachable DeviceRuntimeStatus = "unreachable"
	// StatusError means the last operation failed for another reason.
	StatusError DeviceRuntimeStatus = "error"
)

// Overview is the deterministic catalog view returned by the Manager: every
// persisted device as a boundary-safe DTO plus the durable preference summary.
type Overview struct {
	// Devices holds every persisted device in deterministic ID order.
	Devices []DeviceDTO `json:"devices"`
	// SelectedDeviceID is the currently selected device ID, if any.
	SelectedDeviceID string `json:"selected_device_id,omitempty"`
	// SelectedHostInterface is the durable host-interface preference, if any.
	SelectedHostInterface string `json:"selected_host_interface,omitempty"`
	// Locale is the durable locale preference.
	Locale string `json:"locale"`
	// Theme is the durable theme preference.
	Theme string `json:"theme"`
}

// ScanConflictReason is a stable, deterministic reason a discovered device could
// not be reconciled into the catalog without corrupting it.
type ScanConflictReason string

const (
	// ScanConflictEPRMismatch means the discovered endpoint already belongs to
	// a stored device whose non-empty EPR differs from the discovered EPR.
	ScanConflictEPRMismatch ScanConflictReason = "epr_mismatch"
	// ScanConflictEndpointOwned means the discovered EPR matched one stored
	// device while its canonical endpoint is already owned by a different
	// stored device, so adopting it would collide two records.
	ScanConflictEndpointOwned ScanConflictReason = "endpoint_owned"
	// ScanConflictInvalid means the discovered device carried no safely
	// canonicalizable endpoint.
	ScanConflictInvalid ScanConflictReason = "invalid"
	// ScanConflictPersist means reconciling the device failed to persist for a
	// stable storage reason.
	ScanConflictPersist ScanConflictReason = "persist"
)

// ScanConflict reports, deterministically and per item, why one discovered
// device was left out of the catalog. It never corrupts the catalog and never
// carries a raw unsafe URL or backend error string.
type ScanConflict struct {
	// EndpointReference is the discovered device's WS-Addressing identity when
	// known.
	EndpointReference string `json:"endpoint_reference,omitempty"`
	// Endpoint is the canonical safe endpoint derived from the discovered
	// addresses when one could be derived; empty when none was safe.
	Endpoint string `json:"endpoint,omitempty"`
	// Reason is the stable conflict classification.
	Reason ScanConflictReason `json:"reason"`
	// Detail is a safe, English explanation.
	Detail string `json:"detail,omitempty"`
}

// ScanResult is the normalized outcome of a Scan. It preserves partial results:
// the devices that were reconciled into the catalog, the per-item conflicts
// that were skipped without corruption, and the discovery diagnostic for an
// empty or partial round.
type ScanResult struct {
	// Devices holds the reconciled catalog devices in deterministic ID order.
	Devices []DeviceDTO `json:"devices"`
	// Added holds the IDs of devices newly created by this scan, in order.
	Added []string `json:"added,omitempty"`
	// Updated holds the IDs of existing devices refreshed by this scan.
	Updated []string `json:"updated,omitempty"`
	// Conflicts holds the per-item conflicts in deterministic order.
	Conflicts []ScanConflict `json:"conflicts,omitempty"`
	// Diagnostic explains an empty or partial discovery round, if any.
	Diagnostic *DiscoveryDiagnostic `json:"diagnostic,omitempty"`
}

// ManualDeviceRequest parameterizes AddManualDevice. The endpoint is validated
// and canonicalized; a blank or duplicate endpoint is rejected deterministically
// without erasing any existing EPR or credential.
type ManualDeviceRequest struct {
	// Endpoint is the manual device-service endpoint (host, host:port, or URL).
	Endpoint string
	// DisplayName is an optional operator-facing label.
	DisplayName string
}

// AuthenticateRequest parameterizes AuthenticateDevice. The three policy
// booleans are independent and are never inferred: each must be set explicitly
// by the caller.
type AuthenticateRequest struct {
	// DeviceID selects the device to authenticate.
	DeviceID string
	// Username is the credential username; required.
	Username string
	// Password is the credential password.
	Password string
	// AllowBasic permits HTTP Basic (HTTPS still required unless the plaintext
	// decision is also set).
	AllowBasic bool
	// AllowBasicOverHTTP permits Basic over plaintext HTTP.
	AllowBasicOverHTTP bool
	// AllowLegacyDigest permits legacy no-qop Digest.
	AllowLegacyDigest bool
	// Timeout bounds the proof inventory; non-positive uses the adapter default.
	Timeout int64
}

// AuthenticateResult is the credential-free result of AuthenticateDevice. It
// returns the updated device and the proof inventory unchanged (useful partial
// results are preserved by the inventory use case).
type AuthenticateResult struct {
	// Device is the updated, boundary-safe device view.
	Device DeviceDTO `json:"device"`
	// Inventory is the proof inventory returned unchanged by the use case.
	Inventory Inventory `json:"inventory"`
}

// deviceToDTO maps a persisted store.Device to the boundary-safe DeviceDTO,
// attaching the selection flag and the manager-memory runtime status. It never
// copies the opaque credential reference into the DTO.
func deviceToDTO(device store.Device, selected bool, status DeviceRuntimeStatus) DeviceDTO {
	if status == "" {
		status = StatusUnknown
	}
	dto := DeviceDTO{
		ID:                device.ID,
		Endpoint:          device.Endpoint,
		EndpointReference: device.EPR,
		DisplayName:       device.DisplayName,
		DiscoverySource:   DeviceDiscoverySource(device.DiscoverySource),
		HasCredentials:    device.CredentialReference != "",
		AuthPolicy: AuthPolicyDTO{
			AllowBasic:         device.AuthPolicy.AllowBasic,
			AllowBasicOverHTTP: device.AuthPolicy.AllowBasicOverHTTP,
			AllowLegacyDigest:  device.AuthPolicy.AllowLegacyDigest,
		},
		Selected: selected,
		Status:   status,
	}
	if !device.CreatedAt.IsZero() {
		dto.CreatedAt = device.CreatedAt.UTC().Format(rfc3339UTC)
	}
	if !device.UpdatedAt.IsZero() {
		dto.UpdatedAt = device.UpdatedAt.UTC().Format(rfc3339UTC)
	}
	return dto
}

// rfc3339UTC is the deterministic timestamp layout used for boundary-safe
// device timestamps.
const rfc3339UTC = "2006-01-02T15:04:05Z07:00"
