package cli

import "github.com/addodelgrossi/ovm/internal/app"

// schemaVersion is the stable version of every JSON envelope this CLI emits.
// It is bumped only on a breaking change to an envelope's shape.
const schemaVersion = 1

// Envelope kinds are stable discriminators so a consumer can switch on kind
// without inspecting other fields.
const (
	kindDiscovery = "discovery"
	kindInventory = "inventory"
	kindProfiles  = "profiles"
	kindSnapshot  = "snapshot"
	kindProbe     = "probe"
)

// discoveryEnvelope is the stable JSON envelope for discover output. Fields are
// deterministic and credential-free.
type discoveryEnvelope struct {
	SchemaVersion int                 `json:"schema_version"`
	Kind          string              `json:"kind"`
	Result        app.DiscoveryResult `json:"result"`
	Error         *errorEnvelope      `json:"error,omitempty"`
}

// newDiscoveryEnvelope wraps a discovery result in its stable envelope.
func newDiscoveryEnvelope(result app.DiscoveryResult) discoveryEnvelope {
	return discoveryEnvelope{
		SchemaVersion: schemaVersion,
		Kind:          kindDiscovery,
		Result:        result,
	}
}

// inventoryEnvelope is the stable JSON envelope for info output.
type inventoryEnvelope struct {
	SchemaVersion int            `json:"schema_version"`
	Kind          string         `json:"kind"`
	Result        app.Inventory  `json:"result"`
	Error         *errorEnvelope `json:"error,omitempty"`
}

// newInventoryEnvelope wraps an inventory in its stable envelope.
func newInventoryEnvelope(result app.Inventory) inventoryEnvelope {
	return inventoryEnvelope{
		SchemaVersion: schemaVersion,
		Kind:          kindInventory,
		Result:        result,
	}
}

// profilesEnvelope is the stable JSON envelope for media profile output.
type profilesEnvelope struct {
	SchemaVersion int                     `json:"schema_version"`
	Kind          string                  `json:"kind"`
	Result        app.MediaProfilesResult `json:"result"`
	Error         *errorEnvelope          `json:"error,omitempty"`
}

func newProfilesEnvelope(result app.MediaProfilesResult) profilesEnvelope {
	return profilesEnvelope{
		SchemaVersion: schemaVersion,
		Kind:          kindProfiles,
		Result:        result,
	}
}

func newProfilesFailureEnvelope(result app.MediaProfilesResult, err error) profilesEnvelope {
	envelope := newProfilesEnvelope(result)
	envelope.Error = newErrorEnvelope(err)
	return envelope
}

// snapshotEnvelope is the stable JSON envelope for credential-free snapshot
// metadata. JPEG bytes and operational URIs cannot enter this type.
type snapshotEnvelope struct {
	SchemaVersion int                  `json:"schema_version"`
	Kind          string               `json:"kind"`
	Result        app.SnapshotMetadata `json:"result"`
	Error         *errorEnvelope       `json:"error,omitempty"`
}

func newSnapshotEnvelope(result app.SnapshotMetadata) snapshotEnvelope {
	return snapshotEnvelope{
		SchemaVersion: schemaVersion,
		Kind:          kindSnapshot,
		Result:        result,
	}
}

func newSnapshotFailureEnvelope(result app.SnapshotMetadata, err error) snapshotEnvelope {
	envelope := newSnapshotEnvelope(result)
	envelope.Error = newErrorEnvelope(err)
	return envelope
}

// probeEnvelope is the stable JSON envelope for stream measurements.
type probeEnvelope struct {
	SchemaVersion int             `json:"schema_version"`
	Kind          string          `json:"kind"`
	Result        app.ProbeResult `json:"result"`
	Error         *errorEnvelope  `json:"error,omitempty"`
}

func newProbeEnvelope(result app.ProbeResult) probeEnvelope {
	return probeEnvelope{SchemaVersion: schemaVersion, Kind: kindProbe, Result: result}
}

func newProbeFailureEnvelope(result app.ProbeResult, err error) probeEnvelope {
	envelope := newProbeEnvelope(result)
	envelope.Error = newErrorEnvelope(err)
	return envelope
}

// errorEnvelope is the stable, credential-free error projection included in a
// JSON envelope when a command returns a nonzero runtime result.
type errorEnvelope struct {
	Category app.ErrorCategory `json:"category"`
	Message  string            `json:"message"`
}

func newDiscoveryFailureEnvelope(result app.DiscoveryResult, err error) discoveryEnvelope {
	envelope := newDiscoveryEnvelope(result)
	envelope.Error = newErrorEnvelope(err)
	return envelope
}

func newInventoryFailureEnvelope(result app.Inventory, err error) inventoryEnvelope {
	envelope := newInventoryEnvelope(result)
	envelope.Error = newErrorEnvelope(err)
	return envelope
}

func newErrorEnvelope(err error) *errorEnvelope {
	category := app.CategoryOf(err)
	return &errorEnvelope{Category: category, Message: safeCategoryMessage(category)}
}

// safeCategoryMessage returns fixed text rather than rendering a lower-layer
// error, so JSON failure output cannot disclose endpoint or credential data.
func safeCategoryMessage(category app.ErrorCategory) string {
	switch category {
	case app.CategoryUsage:
		return "the request is not valid"
	case app.CategoryNetwork:
		return "the network operation failed"
	case app.CategoryAuthentication:
		return "authentication was rejected"
	case app.CategoryProtocol:
		return "the device response could not be interpreted"
	case app.CategoryCancelled:
		return "the operation was cancelled"
	default:
		return "the operation failed"
	}
}
