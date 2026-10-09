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
