package discovery

import "sort"

// Device is an aggregated WS-Discovery result keyed by EndpointReference. Its
// XAddrs, Scopes, and Types are merged across every datagram observed for the
// endpoint and sorted so that identical inputs always yield an identical
// Device, regardless of arrival order.
type Device struct {
	// EndpointReference is the stable WS-Addressing identity of the device.
	EndpointReference string
	// XAddrs holds the merged, deduplicated transport addresses.
	XAddrs []string
	// Scopes holds the merged, deduplicated scopes.
	Scopes []string
	// Types holds the merged, deduplicated advertised types as stable
	// expanded-name strings of the form "{namespace}local".
	Types []string
	// MetadataVersion is the highest metadata version observed, retained as a
	// best-effort hint. It is the textual form of the numerically selected
	// highest version so public output stays stable while selection is numeric.
	MetadataVersion string
}

// aggregator merges WS-Discovery messages into deduplicated devices keyed by
// EndpointReference.
type aggregator struct {
	devices map[string]*deviceState
	order   []string
}

// deviceState accumulates a device during aggregation. The numeric metadata
// version is tracked alongside its textual form so selection is numeric while
// the public Device keeps a string for compatibility.
type deviceState struct {
	device      Device
	metadataNum uint64
	hasMetadata bool
}

// newAggregator returns an empty aggregator.
func newAggregator() *aggregator {
	return &aggregator{devices: make(map[string]*deviceState)}
}

// add merges a parsed message into the aggregate. Messages without an
// EndpointReference are ignored because they cannot be keyed deterministically.
// MetadataVersion selection is numeric: the device retains the textual form of
// the highest numeric version observed, and messages whose version was
// malformed (and therefore rejected during parsing) never win selection.
func (a *aggregator) add(message parsedMessage) {
	if message.EndpointReference == "" {
		return
	}
	state, ok := a.devices[message.EndpointReference]
	if !ok {
		state = &deviceState{device: Device{EndpointReference: message.EndpointReference}}
		a.devices[message.EndpointReference] = state
		a.order = append(a.order, message.EndpointReference)
	}
	state.device.XAddrs = sortedCopy(append(state.device.XAddrs, message.XAddrs...))
	state.device.Scopes = sortedCopy(append(state.device.Scopes, message.Scopes...))
	state.device.Types = sortedCopy(append(state.device.Types, message.Types...))
	if message.HasMetadataVersion {
		if !state.hasMetadata || message.MetadataVersionNum > state.metadataNum {
			state.metadataNum = message.MetadataVersionNum
			state.hasMetadata = true
			state.device.MetadataVersion = message.MetadataVersion
		}
	}
}

// devicesSorted returns the aggregated devices sorted by EndpointReference so
// the result is independent of message arrival order.
func (a *aggregator) devicesSorted() []Device {
	out := make([]Device, 0, len(a.devices))
	for _, endpoint := range a.order {
		out = append(out, a.devices[endpoint].device)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].EndpointReference < out[j].EndpointReference
	})
	return out
}
