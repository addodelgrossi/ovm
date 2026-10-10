package app

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/addodelgrossi/ovm/internal/store"
	"github.com/addodelgrossi/ovm/pkg/onvif/discovery"
)

// defaultManagerTimeout bounds a single network operation when the caller does
// not supply one.
const defaultManagerTimeout = 30 * time.Second

// maxScanProcessingConcurrency bounds how many discovered devices the manager
// reconciles concurrently. Reconciliation touches the repository, so this is a
// small, deterministic bound rather than an unbounded fan-out.
const maxScanProcessingConcurrency = 4

// trackedCancel wraps a context.CancelFunc so the manager can compare the
// active operation by pointer identity. context.CancelFunc values are not
// comparable, so the manager stores *trackedCancel instead and only detaches an
// operation when the stored pointer still equals the one that started it.
type trackedCancel struct {
	cancel context.CancelFunc
}

// cancel invokes the wrapped cancel exactly once semantics are provided by
// context itself (CancelFunc is idempotent).
func (t *trackedCancel) run() {
	if t != nil && t.cancel != nil {
		t.cancel()
	}
}

// Manager is the injected device/use-case facade for the desktop/device
// surface. It owns catalog, discovery, authentication, selection, and media
// delegation while keeping every credential ephemeral and every DTO
// boundary-safe. It is safe for concurrent use.
//
// Lifecycle rules it enforces:
//   - At most one active Scan runs at a time; a newer Scan cancels the prior.
//   - At most one active network operation runs per device; a newer operation
//     or a Forget cancels the prior for that device.
//   - A caller's own cancellation is always honored.
//   - The manager never holds its mutex across a repository, provider, or
//     network call.
type Manager struct {
	repository ManagerRepository
	hostIfaces HostInterfaceUseCase
	discovery  DiscoveryUseCase
	inventory  InventoryUseCase
	media      MediaProfilesUseCase
	// liveStreams is optional; without it OpenLiveStream reports unavailable.
	liveStreams LiveStreamOpener

	mu sync.Mutex
	// activeScan is the single in-flight scan's tracked cancel, if any.
	activeScan *trackedCancel
	// deviceOps holds each device's single in-flight operation's tracked
	// cancel.
	deviceOps map[string]*trackedCancel
	// statuses holds manager-memory-only runtime status per device ID.
	statuses map[string]DeviceRuntimeStatus
	// closed is set by Close to reject further operations.
	closed bool
}

// ManagerDependencies groups the injected collaborators of a Manager. Every
// field is required; the Manager instantiates no production disk or keyring
// resources itself, so a composition root wires concrete adapters (or tests
// wire deterministic fakes) here.
type ManagerDependencies struct {
	// Repository is the narrow persistence seam (metadata + secrets).
	Repository ManagerRepository
	// HostInterfaces lists host network interfaces for the device surface.
	HostInterfaces HostInterfaceUseCase
	// Discovery performs a single WS-Discovery round.
	Discovery DiscoveryUseCase
	// Inventory retrieves a credential-free device inventory and is the proof
	// of authentication (its existing useful-inventory policy).
	Inventory InventoryUseCase
	// Media retrieves normalized media profiles.
	Media MediaProfilesUseCase
	// LiveStreams opens live RTSP streams. It is optional.
	LiveStreams LiveStreamOpener
}

// NewManager constructs a Manager from fully injected dependencies. It returns
// an internal error when any dependency is missing so composition fails loudly
// rather than panicking at first use.
func NewManager(deps ManagerDependencies) (*Manager, error) {
	if deps.Repository == nil || deps.HostInterfaces == nil || deps.Discovery == nil ||
		deps.Inventory == nil || deps.Media == nil {
		return nil, newManagerError(CategoryInternal, "the device manager is not fully configured", nil)
	}
	return &Manager{
		repository:  deps.Repository,
		hostIfaces:  deps.HostInterfaces,
		discovery:   deps.Discovery,
		inventory:   deps.Inventory,
		media:       deps.Media,
		liveStreams: deps.LiveStreams,
		deviceOps:   make(map[string]*trackedCancel),
		statuses:    make(map[string]DeviceRuntimeStatus),
	}, nil
}

// Close releases the manager's runtime state and cancels every in-flight
// operation. It is race-safe and idempotent: calling it more than once is a
// no-op, and operations started after Close fail with a stable usage error.
func (m *Manager) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	toCancel := make([]*trackedCancel, 0, len(m.deviceOps)+1)
	if m.activeScan != nil {
		toCancel = append(toCancel, m.activeScan)
		m.activeScan = nil
	}
	for id, op := range m.deviceOps {
		toCancel = append(toCancel, op)
		delete(m.deviceOps, id)
	}
	m.statuses = make(map[string]DeviceRuntimeStatus)
	m.mu.Unlock()
	for _, op := range toCancel {
		op.run()
	}
	return nil
}

// ListHostInterfaces delegates to the injected host-interface use case and
// returns the normalized, deterministic assessment unchanged.
func (m *Manager) ListHostInterfaces(ctx context.Context, request HostInterfaceRequest) (HostInterfacesResult, error) {
	if err := m.ensureOpen(); err != nil {
		return HostInterfacesResult{}, err
	}
	result, err := m.hostIfaces.HostInterfaces(ctx, request)
	if err != nil {
		return HostInterfacesResult{}, managerErrorFromApp(err)
	}
	return result, nil
}

// Bootstrap loads the catalog and repairs a stale selection before returning
// the overview. If the persisted snapshot points its selection at a device that
// no longer exists, Bootstrap clears the selection so the catalog is coherent.
func (m *Manager) Bootstrap(ctx context.Context) (Overview, error) {
	if err := m.ensureOpen(); err != nil {
		return Overview{}, err
	}
	if err := contextErr(ctx); err != nil {
		return Overview{}, managerErrorFromContext(err)
	}
	snapshot := m.repository.Snapshot()
	selected := snapshot.Preferences.SelectedDeviceID
	if selected != "" && !snapshotHasDevice(snapshot, selected) {
		// Repair a stale selection. Select tolerates an empty ID (clear) and is
		// a repository call made without holding the manager mutex.
		if err := m.repository.Select(""); err != nil {
			return Overview{}, managerErrorFromStore(err)
		}
		snapshot = m.repository.Snapshot()
	}
	m.pruneStatuses(snapshot)
	return m.overviewFrom(snapshot), nil
}

// Overview returns the current catalog as a deterministic, boundary-safe view
// without performing any network work.
func (m *Manager) Overview(ctx context.Context) (Overview, error) {
	if err := m.ensureOpen(); err != nil {
		return Overview{}, err
	}
	if err := contextErr(ctx); err != nil {
		return Overview{}, managerErrorFromContext(err)
	}
	snapshot := m.repository.Snapshot()
	m.pruneStatuses(snapshot)
	return m.overviewFrom(snapshot), nil
}

// Profiles delegates to the media-profiles use case for the selected or named
// device, returning the existing MediaProfilesResult DTO unchanged. Partial
// results and the typed error category are preserved.
func (m *Manager) Profiles(ctx context.Context, deviceID string) (MediaProfilesResult, error) {
	if err := m.ensureOpen(); err != nil {
		return MediaProfilesResult{}, err
	}
	device, resolveErr := m.resolveDevice(deviceID)
	if resolveErr != nil {
		return MediaProfilesResult{}, resolveErr
	}
	opCtx, finish, beginErr := m.beginDeviceOp(ctx, device.ID)
	if beginErr != nil {
		return MediaProfilesResult{}, beginErr
	}
	defer finish()

	credentials, release, credErr := m.loadCredentials(opCtx, device)
	if credErr != nil {
		m.setStatus(device.ID, statusForCategory(credErr.Category))
		return MediaProfilesResult{}, credErr
	}
	defer release()

	result, profErr := m.media.Profiles(opCtx, MediaProfilesRequest{
		Host:        device.Endpoint,
		Credentials: credentials,
	})
	release()
	if profErr != nil {
		managed := managerErrorFromApp(profErr)
		m.setStatus(device.ID, statusForCategory(managed.Category))
		// Preserve the partial MediaProfilesResult alongside the typed error.
		return result, managed
	}
	m.setStatus(device.ID, StatusReachable)
	return result, nil
}

// overviewFrom projects a snapshot into the boundary-safe Overview, attaching
// the selection flag and manager-memory status to each device.
func (m *Manager) overviewFrom(snapshot store.Snapshot) Overview {
	selected := snapshot.Preferences.SelectedDeviceID
	devices := make([]DeviceDTO, 0, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		devices = append(devices, deviceToDTO(device, device.ID == selected, m.statusOf(device.ID)))
	}
	return Overview{
		Devices:               devices,
		SelectedDeviceID:      selected,
		SelectedHostInterface: snapshot.Preferences.SelectedHostInterface,
		Locale:                string(snapshot.Preferences.Locale),
		Theme:                 string(snapshot.Preferences.Theme),
	}
}

// resolveDevice returns the device for an explicit ID, or the selected device
// when the ID is empty. It never performs network work.
func (m *Manager) resolveDevice(deviceID string) (store.Device, *ManagerError) {
	snapshot := m.repository.Snapshot()
	id := strings.TrimSpace(deviceID)
	if id == "" {
		id = snapshot.Preferences.SelectedDeviceID
	}
	if id == "" {
		return store.Device{}, newManagerError(CategoryUsage, "no device is selected", nil)
	}
	device, ok := snapshot.DeviceByID(id)
	if !ok {
		return store.Device{}, newManagerError(CategoryUsage, "the device was not found", nil)
	}
	return device, nil
}

// ensureOpen rejects operations after Close.
func (m *Manager) ensureOpen() *ManagerError {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return newManagerError(CategoryUsage, "the device manager is closed", nil)
	}
	return nil
}

// --- runtime status (manager-memory only) ---

func (m *Manager) setStatus(id string, status DeviceRuntimeStatus) {
	if id == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	m.statuses[id] = status
}

func (m *Manager) statusOf(id string) DeviceRuntimeStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	if status, ok := m.statuses[id]; ok {
		return status
	}
	return StatusUnknown
}

// pruneStatuses drops runtime status for devices no longer present in the
// snapshot so removed records leave no stale memory.
func (m *Manager) pruneStatuses(snapshot store.Snapshot) {
	present := make(map[string]struct{}, len(snapshot.Devices))
	for _, device := range snapshot.Devices {
		present[device.ID] = struct{}{}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.statuses {
		if _, ok := present[id]; !ok {
			delete(m.statuses, id)
		}
	}
}

// --- lifecycle helpers ---

// beginScan installs a fresh scan context, cancelling any prior scan. The
// returned context is derived from the caller's so caller cancellation is
// preserved. finish detaches this scan if it is still the active one and
// cancels its context.
func (m *Manager) beginScan(ctx context.Context) (context.Context, func(), *ManagerError) {
	scanCtx, cancel := context.WithCancel(ctx)
	tracked := &trackedCancel{cancel: cancel}

	// Install the new operation in the same critical section that observes the
	// previous one. This prevents two simultaneous callers from both observing
	// an empty slot and leaving one scan running untracked.
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		return nil, nil, newManagerError(CategoryUsage, "the device manager is closed", nil)
	}
	prior := m.activeScan
	m.activeScan = tracked
	m.mu.Unlock()
	if prior != nil {
		prior.run()
	}

	finish := func() {
		m.mu.Lock()
		if m.activeScan == tracked {
			m.activeScan = nil
		}
		m.mu.Unlock()
		cancel()
	}
	return scanCtx, finish, nil
}

// beginDeviceOp installs a fresh per-device operation context, cancelling any
// prior operation for that device. The returned context is derived from the
// caller's. finish detaches this operation if it is still active and cancels
// its context.
func (m *Manager) beginDeviceOp(ctx context.Context, deviceID string) (context.Context, func(), *ManagerError) {
	opCtx, cancel := context.WithCancel(ctx)
	tracked := &trackedCancel{cancel: cancel}

	// Observe and replace the active operation atomically. Cancelling the prior
	// operation after releasing the mutex avoids calling external cancellation
	// paths while locked without opening a lost-tracker race.
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		cancel()
		return nil, nil, newManagerError(CategoryUsage, "the device manager is closed", nil)
	}
	prior := m.deviceOps[deviceID]
	m.deviceOps[deviceID] = tracked
	m.mu.Unlock()
	if prior != nil {
		prior.run()
	}

	finish := func() {
		m.mu.Lock()
		if m.deviceOps[deviceID] == tracked {
			delete(m.deviceOps, deviceID)
		}
		m.mu.Unlock()
		cancel()
	}
	return opCtx, finish, nil
}

// cancelDeviceOp cancels and detaches any in-flight operation for a device,
// used by Forget before touching persistence so the device's active operation
// is stopped first.
func (m *Manager) cancelDeviceOp(deviceID string) {
	m.mu.Lock()
	op := m.deviceOps[deviceID]
	delete(m.deviceOps, deviceID)
	m.mu.Unlock()
	if op != nil {
		op.run()
	}
}

// --- small shared utilities ---

func contextErr(ctx context.Context) error {
	if ctx == nil {
		return context.Canceled
	}
	return ctx.Err()
}

func snapshotHasDevice(snapshot store.Snapshot, id string) bool {
	_, ok := snapshot.DeviceByID(id)
	return ok
}

func sortedStringSet(values map[string]struct{}) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// canonicalEndpointOf returns the canonical, query-free safe endpoint string
// for a discovered transport address, or empty when none is safe.
func canonicalEndpointOf(address string) string {
	endpoint, err := discovery.CanonicalizeEndpoint(address)
	if err != nil {
		return ""
	}
	return endpoint.String()
}
