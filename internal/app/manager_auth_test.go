package app

import (
	"context"
	"errors"
	"testing"

	"github.com/addodelgrossi/ovm/internal/store"
)

func seedDevice(t *testing.T, h *harness, endpoint string) DeviceDTO {
	t.Helper()
	dto, err := h.manager.AddManualDevice(context.Background(), ManualDeviceRequest{Endpoint: endpoint})
	if err != nil {
		t.Fatalf("seed device: %v", err)
	}
	return dto
}

func TestAuthenticateSuccessPersistsSecretAndMetadata(t *testing.T) {
	h := newHarness(t)
	device := seedDevice(t, h, "192.168.5.10")
	h.inventory.proof = func(c *Credentials) (Inventory, error) {
		if c == nil || c.Username() != "admin" {
			return Inventory{}, newError(CategoryAuthentication, "bad creds", nil)
		}
		return Inventory{Endpoint: "http://192.168.5.10/onvif/device_service"}, nil
	}
	result, err := h.manager.AuthenticateDevice(context.Background(), AuthenticateRequest{
		DeviceID:   device.ID,
		Username:   "admin",
		Password:   "secret",
		AllowBasic: true,
	})
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if !result.Device.HasCredentials {
		t.Fatalf("expected credentials stored")
	}
	if !result.Device.AuthPolicy.AllowBasic || result.Device.AuthPolicy.AllowLegacyDigest {
		t.Fatalf("policy not persisted exactly: %+v", result.Device.AuthPolicy)
	}
	// A secret must exist in the provider.
	stored, _ := h.store.Device(device.ID)
	if stored.CredentialReference == "" {
		t.Fatalf("expected credential reference stored")
	}
	if _, gErr := h.secrets.Get(context.Background(), stored.CredentialReference); gErr != nil {
		t.Fatalf("expected secret retrievable: %v", gErr)
	}
}

func TestAuthenticateFailedProofPersistsNothing(t *testing.T) {
	h := newHarness(t)
	device := seedDevice(t, h, "192.168.5.11")
	partial := Inventory{Endpoint: "http://192.168.5.11/onvif/device_service"}
	h.inventory.proof = func(_ *Credentials) (Inventory, error) {
		return partial, newError(CategoryAuthentication, "not authorized", nil)
	}
	result, err := h.manager.AuthenticateDevice(context.Background(), AuthenticateRequest{
		DeviceID: device.ID,
		Username: "admin",
		Password: "bad",
	})
	managed := mustManagerError(t, err)
	if managed.Category != CategoryAuthentication {
		t.Fatalf("expected authentication, got %s", managed.Category)
	}
	// Partial inventory preserved.
	if result.Inventory.Endpoint != partial.Endpoint {
		t.Fatalf("expected partial inventory preserved")
	}
	// Nothing persisted.
	stored, _ := h.store.Device(device.ID)
	if stored.CredentialReference != "" {
		t.Fatalf("expected no credential stored on failed proof")
	}
}

func TestAuthenticatePolicyCombinationsPersistExactly(t *testing.T) {
	cases := []struct {
		name       string
		basic      bool
		basicHTTP  bool
		legacy     bool
		wantReject bool
	}{
		{"none", false, false, false, false},
		{"basic", true, false, false, false},
		{"basic+http", true, true, false, false},
		{"legacy", false, false, true, false},
		{"all", true, true, true, false},
		{"http-without-basic-rejected", false, true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			device := seedDevice(t, h, "192.168.6.10")
			h.inventory.proof = func(_ *Credentials) (Inventory, error) { return Inventory{}, nil }
			result, err := h.manager.AuthenticateDevice(context.Background(), AuthenticateRequest{
				DeviceID:           device.ID,
				Username:           "admin",
				Password:           "pw",
				AllowBasic:         tc.basic,
				AllowBasicOverHTTP: tc.basicHTTP,
				AllowLegacyDigest:  tc.legacy,
			})
			if tc.wantReject {
				managed := mustManagerError(t, err)
				if managed.Category != CategoryUsage {
					t.Fatalf("expected usage rejection, got %s", managed.Category)
				}
				return
			}
			if err != nil {
				t.Fatalf("authenticate: %v", err)
			}
			ap := result.Device.AuthPolicy
			if ap.AllowBasic != tc.basic || ap.AllowBasicOverHTTP != tc.basicHTTP || ap.AllowLegacyDigest != tc.legacy {
				t.Fatalf("policy mismatch: %+v want %v/%v/%v", ap, tc.basic, tc.basicHTTP, tc.legacy)
			}
		})
	}
}

func TestAuthenticateMetadataFailureRollsBackSecret(t *testing.T) {
	h := newHarness(t)
	device := seedDevice(t, h, "192.168.7.10")
	fault := &faultRepository{ManagerRepository: h.repo, upsertErr: store.ErrPersistence}
	manager, err := NewManager(ManagerDependencies{
		Repository: fault, HostInterfaces: h.hosts, Discovery: h.discovery,
		Inventory: h.inventory, Media: h.media,
	})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	h.inventory.proof = func(_ *Credentials) (Inventory, error) { return Inventory{}, nil }
	_, authErr := manager.AuthenticateDevice(context.Background(), AuthenticateRequest{
		DeviceID: device.ID, Username: "admin", Password: "pw",
	})
	managed := mustManagerError(t, authErr)
	if managed.Category != CategoryInternal {
		t.Fatalf("expected internal on persistence failure, got %s", managed.Category)
	}
	// The new secret must have been cleaned up (deleted).
	fault.mu.Lock()
	defer fault.mu.Unlock()
	if fault.deleteCalls == 0 {
		t.Fatalf("expected secret cleanup delete to run")
	}
}

func TestAuthenticateRotationDeletesPreviousSecret(t *testing.T) {
	h := newHarness(t)
	device := seedDevice(t, h, "192.168.8.10")
	h.inventory.proof = func(_ *Credentials) (Inventory, error) { return Inventory{}, nil }
	first, err := h.manager.AuthenticateDevice(context.Background(), AuthenticateRequest{DeviceID: device.ID, Username: "admin", Password: "pw1"})
	if err != nil {
		t.Fatalf("auth1: %v", err)
	}
	before, _ := h.store.Device(first.Device.ID)
	oldRef := before.CredentialReference

	second, err := h.manager.AuthenticateDevice(context.Background(), AuthenticateRequest{DeviceID: device.ID, Username: "admin", Password: "pw2"})
	if err != nil {
		t.Fatalf("auth2: %v", err)
	}
	after, _ := h.store.Device(second.Device.ID)
	if after.CredentialReference == oldRef {
		t.Fatalf("expected fresh reference on rotation")
	}
	// Old secret must be gone.
	if _, gErr := h.secrets.Get(context.Background(), oldRef); !errors.Is(gErr, store.ErrSecretNotFound) {
		t.Fatalf("expected old secret deleted, got %v", gErr)
	}
}

func TestProfilesMissingSecretReturnsAuthRequired(t *testing.T) {
	h := newHarness(t)
	device := seedDevice(t, h, "192.168.9.10")
	if err := h.repo.Select(device.ID); err != nil {
		t.Fatalf("select: %v", err)
	}
	_, err := h.manager.Profiles(context.Background(), device.ID)
	managed := mustManagerError(t, err)
	if managed.Category != CategoryAuthentication {
		t.Fatalf("expected authentication for missing secret, got %s", managed.Category)
	}
}

func TestProfilesDelegatesAndPreservesPartial(t *testing.T) {
	h := newHarness(t)
	device := seedDevice(t, h, "192.168.9.20")
	h.inventory.proof = func(_ *Credentials) (Inventory, error) { return Inventory{}, nil }
	if _, err := h.manager.AuthenticateDevice(context.Background(), AuthenticateRequest{DeviceID: device.ID, Username: "admin", Password: "pw", AllowBasic: true}); err != nil {
		t.Fatalf("auth: %v", err)
	}
	h.media.result = MediaProfilesResult{Media2Available: true, Profiles: []MediaProfileDTO{{Token: "p0"}}}
	h.media.err = newError(CategoryProtocol, "partial", nil)
	result, err := h.manager.Profiles(context.Background(), device.ID)
	managed := mustManagerError(t, err)
	if managed.Category != CategoryProtocol {
		t.Fatalf("expected protocol, got %s", managed.Category)
	}
	if len(result.Profiles) != 1 || !result.Media2Available {
		t.Fatalf("expected partial media result preserved, got %+v", result)
	}
}
