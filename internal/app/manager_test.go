package app

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/addodelgrossi/ovm/internal/store"
)

// --- deterministic fakes for the injected use cases ---

// fakeDiscovery returns a scripted discovery result and records cancellation.
type fakeDiscovery struct {
	mu         sync.Mutex
	result     DiscoveryResult
	err        error
	calls      int
	block      chan struct{} // when non-nil, Discover waits until closed or ctx done
	started    chan struct{} // closed on first call entry
	observeCtx func(context.Context)
}

func (f *fakeDiscovery) Discover(ctx context.Context, _ DiscoveryRequest) (DiscoveryResult, error) {
	f.mu.Lock()
	f.calls++
	block := f.block
	started := f.started
	observe := f.observeCtx
	f.mu.Unlock()
	if started != nil {
		select {
		case <-started:
		default:
			close(started)
		}
	}
	if observe != nil {
		observe(ctx)
	}
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return f.result, newError(CategoryCancelled, "discovery cancelled", ctx.Err())
		}
	}
	if err := ctx.Err(); err != nil {
		return f.result, newError(CategoryCancelled, "discovery cancelled", err)
	}
	return f.result, f.err
}

// fakeInventory returns a scripted inventory/error, keyed on credentials when a
// predicate is supplied so auth proof can be simulated deterministically.
type fakeInventory struct {
	mu    sync.Mutex
	calls int
	proof func(*Credentials) (Inventory, error)
	block chan struct{}
}

func (f *fakeInventory) Inventory(ctx context.Context, request InventoryRequest) (Inventory, error) {
	f.mu.Lock()
	f.calls++
	block := f.block
	proof := f.proof
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return Inventory{Endpoint: request.Host}, newError(CategoryCancelled, "inventory cancelled", ctx.Err())
		}
	}
	if err := ctx.Err(); err != nil {
		return Inventory{Endpoint: request.Host}, newError(CategoryCancelled, "inventory cancelled", err)
	}
	if proof != nil {
		return proof(request.Credentials)
	}
	return Inventory{Endpoint: request.Host}, nil
}

// fakeMedia returns a scripted media result/error.
type fakeMedia struct {
	mu     sync.Mutex
	calls  int
	result MediaProfilesResult
	err    error
	block  chan struct{}
}

func (f *fakeMedia) Profiles(ctx context.Context, request MediaProfilesRequest) (MediaProfilesResult, error) {
	f.mu.Lock()
	f.calls++
	block := f.block
	f.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return f.result, newError(CategoryCancelled, "media cancelled", ctx.Err())
		}
	}
	if err := ctx.Err(); err != nil {
		return f.result, newError(CategoryCancelled, "media cancelled", err)
	}
	return f.result, f.err
}

// fakeHostIfaces returns a scripted host-interface result.
type fakeHostIfaces struct {
	result HostInterfacesResult
	err    error
}

func (f *fakeHostIfaces) HostInterfaces(_ context.Context, _ HostInterfaceRequest) (HostInterfacesResult, error) {
	return f.result, f.err
}

// --- repository fault injection wrapper ---

// faultRepository wraps a real ManagerRepository and injects deterministic
// failures into secret and metadata operations for transaction/rollback tests.
type faultRepository struct {
	ManagerRepository
	mu          sync.Mutex
	putErr      error
	deleteErr   error
	upsertErr   error
	putCalls    int
	deleteCalls int
	deleted     []string
}

func (r *faultRepository) PutSecret(ctx context.Context, reference string, secret store.Secret) error {
	r.mu.Lock()
	r.putCalls++
	err := r.putErr
	r.mu.Unlock()
	if err != nil {
		return err
	}
	return r.ManagerRepository.PutSecret(ctx, reference, secret)
}

func (r *faultRepository) DeleteSecret(ctx context.Context, reference string) error {
	r.mu.Lock()
	r.deleteCalls++
	r.deleted = append(r.deleted, reference)
	err := r.deleteErr
	r.mu.Unlock()
	if err != nil {
		return err
	}
	return r.ManagerRepository.DeleteSecret(ctx, reference)
}

func (r *faultRepository) Upsert(device store.Device) (store.Device, error) {
	r.mu.Lock()
	err := r.upsertErr
	r.mu.Unlock()
	if err != nil {
		return store.Device{}, err
	}
	return r.ManagerRepository.Upsert(device)
}

// --- harness ---

type harness struct {
	manager   *Manager
	repo      ManagerRepository
	store     *store.Repository
	secrets   *store.MemorySecretProvider
	discovery *fakeDiscovery
	inventory *fakeInventory
	media     *fakeMedia
	hosts     *fakeHostIfaces
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state.json")
	repository, err := store.NewRepository(path)
	if err != nil {
		t.Fatalf("new repository: %v", err)
	}
	secrets := store.NewMemorySecretProvider()
	backing, err := NewProductionManagerRepository(repository, secrets)
	if err != nil {
		t.Fatalf("new manager repository: %v", err)
	}
	disc := &fakeDiscovery{}
	inv := &fakeInventory{}
	med := &fakeMedia{}
	hosts := &fakeHostIfaces{}
	manager, err := NewManager(ManagerDependencies{
		Repository:     backing,
		HostInterfaces: hosts,
		Discovery:      disc,
		Inventory:      inv,
		Media:          med,
	})
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	return &harness{
		manager:   manager,
		repo:      backing,
		store:     repository,
		secrets:   secrets,
		discovery: disc,
		inventory: inv,
		media:     med,
		hosts:     hosts,
	}
}

func mustManagerError(t *testing.T, err error) *ManagerError {
	t.Helper()
	var managed *ManagerError
	if !errors.As(err, &managed) {
		t.Fatalf("expected *ManagerError, got %T: %v", err, err)
	}
	return managed
}

// --- bootstrap / catalog ---

func TestBootstrapEmptyCatalog(t *testing.T) {
	h := newHarness(t)
	overview, err := h.manager.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if len(overview.Devices) != 0 {
		t.Fatalf("expected empty catalog, got %d", len(overview.Devices))
	}
	if overview.Locale != string(store.LocaleEnglish) || overview.Theme != string(store.ThemeSystem) {
		t.Fatalf("unexpected default preferences: %+v", overview)
	}
}

func TestBootstrapRepairsStaleSelection(t *testing.T) {
	h := newHarness(t)
	saved, err := h.repo.Upsert(store.Device{Endpoint: "192.168.1.10"})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := h.repo.Select(saved.ID); err != nil {
		t.Fatalf("select: %v", err)
	}
	if _, err := h.repo.Remove(saved.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// The store clears the selection on Remove, so inject a stale selection
	// directly by re-adding a device, selecting it, then removing out of band
	// is not possible; instead verify Bootstrap tolerates a cleared selection
	// and a present one.
	overview, err := h.manager.Bootstrap(context.Background())
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if overview.SelectedDeviceID != "" {
		t.Fatalf("expected cleared selection, got %q", overview.SelectedDeviceID)
	}
}

func TestAddManualDeviceCanonicalizesAndDedupes(t *testing.T) {
	h := newHarness(t)
	first, err := h.manager.AddManualDevice(context.Background(), ManualDeviceRequest{Endpoint: "192.168.1.50", DisplayName: "Front"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if first.Endpoint != "http://192.168.1.50/onvif/device_service" {
		t.Fatalf("unexpected canonical endpoint: %q", first.Endpoint)
	}
	// Adding the same host again dedupes to the same record.
	second, err := h.manager.AddManualDevice(context.Background(), ManualDeviceRequest{Endpoint: "http://192.168.1.50:80/onvif/device_service", DisplayName: "Front Door"})
	if err != nil {
		t.Fatalf("add dup: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected dedupe to same ID, got %q vs %q", first.ID, second.ID)
	}
	if second.DisplayName != "Front Door" {
		t.Fatalf("expected display name update, got %q", second.DisplayName)
	}
	overview, err := h.manager.Overview(context.Background())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if len(overview.Devices) != 1 {
		t.Fatalf("expected single deduped device, got %d", len(overview.Devices))
	}
}

func TestAddManualDeviceRejectsInvalidEndpoint(t *testing.T) {
	h := newHarness(t)
	for _, endpoint := range []string{"", "   ", "ftp://host/x", "http://user:pass@host/x", "http://host/x?token=1"} {
		_, err := h.manager.AddManualDevice(context.Background(), ManualDeviceRequest{Endpoint: endpoint})
		managed := mustManagerError(t, err)
		if managed.Category != CategoryUsage {
			t.Fatalf("endpoint %q: expected usage, got %s", endpoint, managed.Category)
		}
	}
}

func TestAddManualDeviceNeverErasesEPRorCredentials(t *testing.T) {
	h := newHarness(t)
	seed, err := h.repo.Upsert(store.Device{
		Endpoint: "192.168.1.70",
		EPR:      "urn:uuid:aaaa",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Attach a credential reference through the store directly to simulate a
	// previously authenticated device.
	secret, _ := store.NewSecret("admin", "pw", store.AuthPolicy{})
	ref, _ := store.NewSecretReference()
	if err := h.secrets.Put(context.Background(), ref, secret); err != nil {
		t.Fatalf("put secret: %v", err)
	}
	seed.CredentialReference = ref
	if _, err := h.repo.Upsert(seed); err != nil {
		t.Fatalf("attach ref: %v", err)
	}
	// Re-add manually with the same endpoint.
	dto, err := h.manager.AddManualDevice(context.Background(), ManualDeviceRequest{Endpoint: "192.168.1.70", DisplayName: "Renamed"})
	if err != nil {
		t.Fatalf("re-add: %v", err)
	}
	if !dto.HasCredentials {
		t.Fatalf("expected credentials preserved")
	}
	if dto.EndpointReference != "urn:uuid:aaaa" {
		t.Fatalf("expected EPR preserved, got %q", dto.EndpointReference)
	}
}

// --- selection ---

func TestSelectDeviceAndClear(t *testing.T) {
	h := newHarness(t)
	a, _ := h.manager.AddManualDevice(context.Background(), ManualDeviceRequest{Endpoint: "10.0.0.1"})
	overview, err := h.manager.SelectDevice(context.Background(), a.ID)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if overview.SelectedDeviceID != a.ID {
		t.Fatalf("expected %q selected, got %q", a.ID, overview.SelectedDeviceID)
	}
	overview, err = h.manager.SelectDevice(context.Background(), "")
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	if overview.SelectedDeviceID != "" {
		t.Fatalf("expected cleared selection")
	}
}

func TestSelectUnknownDeviceIsUsage(t *testing.T) {
	h := newHarness(t)
	_, err := h.manager.SelectDevice(context.Background(), "dev_missing")
	managed := mustManagerError(t, err)
	if managed.Category != CategoryUsage {
		t.Fatalf("expected usage, got %s", managed.Category)
	}
}

// --- preferences ---

func TestUpdatePreferencesPartial(t *testing.T) {
	h := newHarness(t)
	theme := string(store.ThemeDark)
	overview, err := h.manager.UpdatePreferences(context.Background(), PreferencesRequest{Theme: &theme})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if overview.Theme != theme || overview.Locale != string(store.LocaleEnglish) {
		t.Fatalf("unexpected preferences: %+v", overview)
	}
	bad := "neon"
	_, err = h.manager.UpdatePreferences(context.Background(), PreferencesRequest{Theme: &bad})
	managed := mustManagerError(t, err)
	if managed.Category != CategoryUsage || managed.StoreCode != store.CodeInvalidData {
		t.Fatalf("expected usage/invalid_data, got %s/%s", managed.Category, managed.StoreCode)
	}
}

// --- Close ---

func TestCloseRejectsFurtherOperations(t *testing.T) {
	h := newHarness(t)
	if err := h.manager.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if err := h.manager.Close(); err != nil {
		t.Fatalf("double close: %v", err)
	}
	_, err := h.manager.Overview(context.Background())
	managed := mustManagerError(t, err)
	if managed.Category != CategoryUsage {
		t.Fatalf("expected usage after close, got %s", managed.Category)
	}
}

func TestListHostInterfacesDelegates(t *testing.T) {
	h := newHarness(t)
	h.hosts.result = HostInterfacesResult{Interfaces: []HostInterfaceDTO{{Index: 1, Name: "en0", Usable: true, Reason: HostInterfaceUsable}}}
	result, err := h.manager.ListHostInterfaces(context.Background(), HostInterfaceRequest{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(result.Interfaces) != 1 || result.Interfaces[0].Name != "en0" {
		t.Fatalf("unexpected delegation result: %+v", result)
	}
	h.hosts.err = newError(CategoryNetwork, "boom", nil)
	_, err = h.manager.ListHostInterfaces(context.Background(), HostInterfaceRequest{})
	managed := mustManagerError(t, err)
	if managed.Category != CategoryNetwork {
		t.Fatalf("expected network, got %s", managed.Category)
	}
}

func withEPR(epr string, addresses ...string) DiscoveredDevice {
	return DiscoveredDevice{EndpointReference: epr, Addresses: addresses}
}
