package app

import (
	"context"
	"sync"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// SOAPConsoleEntry is one redacted SOAP exchange for diagnostics. Credentials,
// nonces, usernames, and URL userinfo/queries are removed before recording.
type SOAPConsoleEntry struct {
	ID          uint64 `json:"id"`
	Time        string `json:"time"`
	Endpoint    string `json:"endpoint"`
	Action      string `json:"action"`
	Version     string `json:"version"`
	StatusCode  int    `json:"status_code"`
	Outcome     string `json:"outcome"`
	DurationMs  int64  `json:"duration_ms"`
	RequestXML  string `json:"request_xml"`
	ResponseXML string `json:"response_xml"`
}

// SOAPConsole keeps the most recent redacted SOAP exchanges in memory only.
// It is safe for concurrent use.
type SOAPConsole struct {
	capacity int
	now      func() time.Time

	mu      sync.Mutex
	entries []SOAPConsoleEntry
	nextID  uint64
	enabled bool
}

// NewSOAPConsole keeps up to capacity entries (at least 1). Recording starts
// disabled; nothing is captured until Enable.
func NewSOAPConsole(capacity int) *SOAPConsole {
	return &SOAPConsole{capacity: max(capacity, 1), now: time.Now}
}

// SetEnabled turns capture on or off. Disabling also clears the buffer.
func (c *SOAPConsole) SetEnabled(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.enabled = enabled
	if !enabled {
		c.entries = nil
	}
}

// Enabled reports whether capture is on.
func (c *SOAPConsole) Enabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled
}

// Record stores one trace event when capture is enabled.
func (c *SOAPConsole) Record(_ context.Context, event soap.TraceEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.enabled {
		return
	}
	c.nextID++
	c.entries = append(c.entries, SOAPConsoleEntry{
		ID:          c.nextID,
		Time:        c.now().UTC().Format(time.RFC3339Nano),
		Endpoint:    event.Endpoint,
		Action:      event.Action,
		Version:     string(event.Version),
		StatusCode:  event.StatusCode,
		Outcome:     event.Outcome,
		DurationMs:  event.Duration.Milliseconds(),
		RequestXML:  string(event.RequestXML),
		ResponseXML: string(event.ResponseXML),
	})
	if overflow := len(c.entries) - c.capacity; overflow > 0 {
		c.entries = append([]SOAPConsoleEntry(nil), c.entries[overflow:]...)
	}
}

// Entries returns the entries with an ID greater than afterID, oldest first.
func (c *SOAPConsole) Entries(afterID uint64) []SOAPConsoleEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []SOAPConsoleEntry{}
	for _, entry := range c.entries {
		if entry.ID > afterID {
			out = append(out, entry)
		}
	}
	return out
}

// Clear removes every entry.
func (c *SOAPConsole) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = nil
}

// trace returns the session trace hook, or nil without a console.
func (c *SOAPConsole) trace() soap.TraceFunc {
	if c == nil {
		return nil
	}
	return c.Record
}

// ProductionOptions configures the production adapters.
type ProductionOptions struct {
	// Console, when set, receives redacted SOAP exchanges.
	Console *SOAPConsole
}

// MediaServices implements the media, live, snapshot, probe, and
// configuration use cases over one set of options.
type MediaServices struct {
	*productionMedia
}

// NewProductionMediaServices returns the production media and configuration
// use cases sharing options.
func NewProductionMediaServices(options ProductionOptions) MediaServices {
	return MediaServices{&productionMedia{console: options.Console}}
}

// NewProductionInventoryWith returns the production inventory use case with
// options.
func NewProductionInventoryWith(options ProductionOptions) InventoryUseCase {
	return &productionInventory{console: options.Console}
}

// ProductionUseCases returns ManagerDependencies with every device use case
// filled by production adapters sharing options. Repository, HostInterfaces,
// and Discovery are left for the caller.
func ProductionUseCases(options ProductionOptions) ManagerDependencies {
	services := NewProductionMediaServices(options)
	return ManagerDependencies{
		Inventory:     NewProductionInventoryWith(options),
		Media:         services,
		LiveStreams:   NewProductionLiveStreams(),
		SnapshotFeeds: services,
		Probe:         &probeUseCase{media: services, open: NewProductionLiveStreams(), now: time.Now},
		Encoder:       services,
		Imaging:       services,
		Time:          services,
		Users:         services,
		Network:       services,
		Maintenance:   services,
		Events:        services,
	}
}
