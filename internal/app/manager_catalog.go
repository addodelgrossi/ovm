package app

import (
	"context"
	"strings"
	"sync"

	"github.com/addodelgrossi/ovm/internal/store"
)

// Scan runs a single discovery round and reconciles the discovered devices into
// the catalog. At most one scan runs at a time; a newer scan cancels the prior.
// A caller's own cancellation is honored and partial results are preserved: the
// reconciled catalog, the per-item conflicts, and any discovery diagnostic are
// all returned.
//
// Reconciliation observes the identity/dedupe rules in reconcileDiscovered and
// never corrupts the catalog: a conflicting item is reported per item and the
// catalog is left consistent. The manager mutex is never held across a
// discovery or repository call.
func (m *Manager) Scan(ctx context.Context, request DiscoveryRequest) (ScanResult, error) {
	if err := m.ensureOpen(); err != nil {
		return ScanResult{}, err
	}
	scanCtx, finish, beginErr := m.beginScan(ctx)
	if beginErr != nil {
		return ScanResult{}, beginErr
	}
	defer finish()

	discovered, discErr := m.discovery.Discover(scanCtx, request)
	if discErr != nil {
		// Discovery failed to start or was cancelled. Preserve any partial
		// devices the discovery result already carried.
		managed := managerErrorFromApp(discErr)
		result := m.reconcileBounded(scanCtx, discovered.Devices)
		result.Diagnostic = discovered.Diagnostic
		return result, managed
	}

	result := m.reconcileBounded(scanCtx, discovered.Devices)
	result.Diagnostic = discovered.Diagnostic
	return result, nil
}

// reconcileBounded computes each discovered device's identity with bounded
// concurrency, then persists decisions sequentially in deterministic input
// order. The preparation stage is pure (canonicalization/EPR normalization) and
// touches no shared state, so it is safe to parallelize; the persist pass is
// sequential so each item observes prior upserts and the catalog is never
// corrupted. The manager mutex is never held during a repository call.
func (m *Manager) reconcileBounded(ctx context.Context, devices []DiscoveredDevice) ScanResult {
	identities := prepareIdentitiesBounded(ctx, devices)

	added := map[string]struct{}{}
	updated := map[string]struct{}{}
	conflicts := make([]ScanConflict, 0)

	for _, identity := range identities {
		if err := ctx.Err(); err != nil {
			break
		}
		snapshot := m.repository.Snapshot()
		decision := reconcileDiscovered(snapshot, identity)
		if decision.conflict != nil {
			conflicts = append(conflicts, *decision.conflict)
			continue
		}
		saved, err := m.repository.Upsert(*decision.upsert)
		if err != nil {
			conflicts = append(conflicts, scanPersistConflict(*decision.upsert, err))
			continue
		}
		if decision.isNew {
			added[saved.ID] = struct{}{}
		} else {
			updated[saved.ID] = struct{}{}
		}
	}

	final := m.repository.Snapshot()
	m.pruneStatuses(final)
	result := ScanResult{
		Devices:   m.overviewFrom(final).Devices,
		Added:     sortedStringSet(added),
		Updated:   sortedStringSet(updated),
		Conflicts: conflicts,
	}
	if len(result.Conflicts) == 0 {
		result.Conflicts = nil
	}
	return result
}

// prepareIdentitiesBounded canonicalizes every discovered device with a bounded
// worker pool and returns the identities in deterministic input order. Results
// are written into a pre-sized slice by index, so ordering never depends on
// goroutine scheduling.
func prepareIdentitiesBounded(ctx context.Context, devices []DiscoveredDevice) []preparedIdentity {
	results := make([]preparedIdentity, len(devices))
	if len(devices) == 0 {
		return results
	}
	limit := maxScanProcessingConcurrency
	if limit < 1 {
		limit = 1
	}
	semaphore := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for index, device := range devices {
		if ctx.Err() != nil {
			// Honor cancellation: still produce an identity for every index so
			// the deterministic persist pass can short-circuit uniformly. The
			// zero identity has an empty endpoint and becomes an invalid
			// conflict only if it is ever reconciled, which the persist pass
			// skips once the context is done.
			results[index] = prepareIdentity(device)
			continue
		}
		semaphore <- struct{}{}
		wg.Add(1)
		go func(idx int, dev DiscoveredDevice) {
			defer wg.Done()
			defer func() { <-semaphore }()
			results[idx] = prepareIdentity(dev)
		}(index, device)
	}
	wg.Wait()
	return results
}

// scanPersistConflict builds a deterministic persist conflict for a device the
// repository rejected, mapping a duplicate to the appropriate owned reason
// without leaking a backend error string.
func scanPersistConflict(device store.Device, err error) ScanConflict {
	reason := ScanConflictPersist
	detail := "the device could not be persisted"
	if store.ErrorCodeOf(err) == store.CodeDuplicate {
		reason = ScanConflictEndpointOwned
		detail = "the discovered device collides with an existing record"
	}
	epr, _ := normalizeEPR(device.EPR)
	return ScanConflict{
		EndpointReference: epr,
		Endpoint:          device.Endpoint,
		Reason:            reason,
		Detail:            detail,
	}
}

// AddManualDevice validates and canonicalizes a manual endpoint and adds it to
// the catalog, deduplicating by canonical endpoint. A blank or unsafe endpoint
// is a usage error. When the canonical endpoint already exists, the existing
// record is refreshed (its display name updated when provided) WITHOUT erasing
// its EPR or credential reference. The manager mutex is never held across the
// repository call.
func (m *Manager) AddManualDevice(ctx context.Context, request ManualDeviceRequest) (DeviceDTO, error) {
	if err := m.ensureOpen(); err != nil {
		return DeviceDTO{}, err
	}
	if err := contextErr(ctx); err != nil {
		return DeviceDTO{}, managerErrorFromContext(err)
	}
	endpoint, canonErr := canonicalizeManualEndpoint(request.Endpoint)
	if canonErr != nil {
		return DeviceDTO{}, canonErr
	}

	snapshot := m.repository.Snapshot()
	displayName := strings.TrimSpace(request.DisplayName)

	var toUpsert store.Device
	if existing, ok := deviceByEndpoint(snapshot, endpoint); ok {
		// Refresh in place; never erase EPR, credentials, or policy.
		toUpsert = existing
		if displayName != "" {
			toUpsert.DisplayName = displayName
		}
	} else {
		toUpsert = store.Device{
			Endpoint:        endpoint,
			DisplayName:     displayName,
			DiscoverySource: store.DiscoverySourceManual,
		}
	}

	saved, err := m.repository.Upsert(toUpsert)
	if err != nil {
		if managed := managerErrorFromStore(err); managed != nil {
			return DeviceDTO{}, managed
		}
		return DeviceDTO{}, newManagerError(CategoryInternal, "the device could not be added", err)
	}
	selected := m.repository.Preferences().SelectedDeviceID
	return deviceToDTO(saved, saved.ID == selected, m.statusOf(saved.ID)), nil
}

// canonicalizeManualEndpoint canonicalizes a manual endpoint to the query-free
// safe form, rejecting any query so a stored endpoint identity never carries a
// credential-bearing query. It reuses the shared discovery canonicalizer rather
// than duplicating URL logic.
func canonicalizeManualEndpoint(input string) (string, *ManagerError) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", newManagerError(CategoryUsage, "the device endpoint is required", nil)
	}
	endpoint := canonicalEndpointOf(trimmed)
	if endpoint == "" {
		return "", newManagerError(CategoryUsage, "the device endpoint is not valid", nil)
	}
	return endpoint, nil
}

// SelectDevice sets the selected device by ID. An empty ID clears the
// selection. A non-existent ID is a usage error. The manager mutex is never
// held across the repository call.
func (m *Manager) SelectDevice(ctx context.Context, deviceID string) (Overview, error) {
	if err := m.ensureOpen(); err != nil {
		return Overview{}, err
	}
	if err := contextErr(ctx); err != nil {
		return Overview{}, managerErrorFromContext(err)
	}
	id := strings.TrimSpace(deviceID)
	if err := m.repository.Select(id); err != nil {
		if store.ErrorCodeOf(err) == store.CodeDeviceNotFound {
			return Overview{}, newManagerError(CategoryUsage, "the device was not found", err)
		}
		if managed := managerErrorFromStore(err); managed != nil {
			return Overview{}, managed
		}
		return Overview{}, newManagerError(CategoryInternal, "the selection could not be updated", err)
	}
	snapshot := m.repository.Snapshot()
	m.pruneStatuses(snapshot)
	return m.overviewFrom(snapshot), nil
}

// UpdatePreferences atomically updates the durable, non-selection preferences
// (locale, theme, selected host interface). The selected device is managed only
// through SelectDevice/Forget/Bootstrap, so this method preserves the current
// selection. The manager mutex is never held across the repository call.
func (m *Manager) UpdatePreferences(ctx context.Context, request PreferencesRequest) (Overview, error) {
	if err := m.ensureOpen(); err != nil {
		return Overview{}, err
	}
	if err := contextErr(ctx); err != nil {
		return Overview{}, managerErrorFromContext(err)
	}
	current := m.repository.Preferences()
	next := current
	if request.Locale != nil {
		next.Locale = store.Locale(strings.TrimSpace(*request.Locale))
	}
	if request.Theme != nil {
		next.Theme = store.Theme(strings.TrimSpace(*request.Theme))
	}
	if request.SelectedHostInterface != nil {
		next.SelectedHostInterface = strings.TrimSpace(*request.SelectedHostInterface)
	}
	if err := m.repository.SetPreferences(next); err != nil {
		if managed := managerErrorFromStore(err); managed != nil {
			return Overview{}, managed
		}
		return Overview{}, newManagerError(CategoryInternal, "the preferences could not be updated", err)
	}
	snapshot := m.repository.Snapshot()
	m.pruneStatuses(snapshot)
	return m.overviewFrom(snapshot), nil
}

// PreferencesRequest parameterizes UpdatePreferences. Each pointer is applied
// only when non-nil, so a caller can update one preference without inferring
// the others. The selected device is never changed here.
type PreferencesRequest struct {
	// Locale, when non-nil, sets the durable locale (validated by the store).
	Locale *string
	// Theme, when non-nil, sets the durable theme (validated by the store).
	Theme *string
	// SelectedHostInterface, when non-nil, sets the durable host-interface
	// preference.
	SelectedHostInterface *string
}
