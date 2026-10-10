package app

import (
	"context"
	"errors"
	"testing"

	"github.com/addodelgrossi/ovm/internal/store"
)

func TestForgetDeletesSecretBeforeMetadataAndClearsSelection(t *testing.T) {
	h := newHarness(t)
	device := seedDevice(t, h, "192.168.10.10")
	h.inventory.proof = func(_ *Credentials) (Inventory, error) { return Inventory{}, nil }
	if _, err := h.manager.AuthenticateDevice(context.Background(), AuthenticateRequest{DeviceID: device.ID, Username: "admin", Password: "pw"}); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if _, err := h.manager.SelectDevice(context.Background(), device.ID); err != nil {
		t.Fatalf("select: %v", err)
	}
	stored, _ := h.store.Device(device.ID)
	ref := stored.CredentialReference

	overview, err := h.manager.ForgetDevice(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if len(overview.Devices) != 0 {
		t.Fatalf("expected empty catalog, got %d", len(overview.Devices))
	}
	if overview.SelectedDeviceID != "" {
		t.Fatalf("expected cleared selection")
	}
	if _, gErr := h.secrets.Get(context.Background(), ref); !errors.Is(gErr, store.ErrSecretNotFound) {
		t.Fatalf("expected secret deleted, got %v", gErr)
	}
}

func TestForgetMissingSecretIsIdempotent(t *testing.T) {
	h := newHarness(t)
	device := seedDevice(t, h, "192.168.10.20")
	// No credential reference; forget should still succeed.
	overview, err := h.manager.ForgetDevice(context.Background(), device.ID)
	if err != nil {
		t.Fatalf("forget: %v", err)
	}
	if len(overview.Devices) != 0 {
		t.Fatalf("expected empty catalog")
	}
}

func TestForgetUnknownDeviceIsUsage(t *testing.T) {
	h := newHarness(t)
	_, err := h.manager.ForgetDevice(context.Background(), "dev_missing")
	managed := mustManagerError(t, err)
	if managed.Category != CategoryUsage {
		t.Fatalf("expected usage, got %s", managed.Category)
	}
}

func TestForgetSecretDeletionFailureLeavesMetadataIntact(t *testing.T) {
	h := newHarness(t)
	device := seedDevice(t, h, "192.168.10.30")
	h.inventory.proof = func(_ *Credentials) (Inventory, error) { return Inventory{}, nil }
	if _, err := h.manager.AuthenticateDevice(context.Background(), AuthenticateRequest{DeviceID: device.ID, Username: "admin", Password: "pw"}); err != nil {
		t.Fatalf("auth: %v", err)
	}
	fault := &faultRepository{ManagerRepository: h.repo, deleteErr: store.ErrSecretDenied}
	manager, err := NewManager(ManagerDependencies{
		Repository: fault, HostInterfaces: h.hosts, Discovery: h.discovery,
		Inventory: h.inventory, Media: h.media,
	})
	if err != nil {
		t.Fatalf("manager: %v", err)
	}
	_, forgetErr := manager.ForgetDevice(context.Background(), device.ID)
	managed := mustManagerError(t, forgetErr)
	if managed.SecretKind != store.SecretDenied {
		t.Fatalf("expected secret denied kind preserved, got %s", managed.SecretKind)
	}
	// Metadata must still be present (delete failed before Remove).
	if _, lookErr := h.store.Device(device.ID); lookErr != nil {
		t.Fatalf("expected metadata intact, got %v", lookErr)
	}
}

func TestForgetCancelsActiveDeviceOperation(t *testing.T) {
	h := newHarness(t)
	device := seedDevice(t, h, "192.168.10.40")
	h.inventory.proof = func(_ *Credentials) (Inventory, error) { return Inventory{}, nil }
	if _, err := h.manager.AuthenticateDevice(context.Background(), AuthenticateRequest{DeviceID: device.ID, Username: "admin", Password: "pw"}); err != nil {
		t.Fatalf("auth: %v", err)
	}

	// Start a long-running Profiles operation that blocks until cancelled.
	block := make(chan struct{})
	h.media.block = block
	profileErr := make(chan error, 1)
	go func() {
		_, err := h.manager.Profiles(context.Background(), device.ID)
		profileErr <- err
	}()
	// Give the goroutine a moment to enter the media call.
	waitForMediaCall(t, h.media)

	// Forget cancels the in-flight operation first.
	if _, err := h.manager.ForgetDevice(context.Background(), device.ID); err != nil {
		t.Fatalf("forget: %v", err)
	}
	select {
	case err := <-profileErr:
		managed := mustManagerError(t, err)
		if managed.Category != CategoryCancelled {
			t.Fatalf("expected cancelled profiles, got %s", managed.Category)
		}
	case <-timeoutChan():
		t.Fatalf("profiles did not cancel after forget")
	}
	close(block)
}
