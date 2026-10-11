//go:build manual

// This manual, real-camera test is excluded from the default build and from
// public CI by the "manual" build tag. It is never compiled or run unless a
// maintainer opts in explicitly on a trusted, isolated network.
//
// Safe invocation (English): first populate OVM_TEST_HOST, OVM_TEST_USER, and
// OVM_TEST_PASS through trusted, non-echoing input; do not put a literal secret
// in shell history. Then run:
//
//	export OVM_TEST_HOST OVM_TEST_USER OVM_TEST_PASS
//	go test -tags manual -run TestManualRealCameraInventory ./internal/app -v
//	unset OVM_TEST_HOST OVM_TEST_USER OVM_TEST_PASS
//
// Evidence limits:
//   - This test proves only that the application layer completed an inventory
//     against ONE camera on the operator's own network at test time. It is not
//     a conformance test and it is not a compatibility claim.
//   - It must never run in public CI and never with shared or production
//     credentials. Run it only on an isolated LAN you control.
//   - A pass here does NOT upgrade any compatibility entry to
//     "hardware-verified"; that status requires the documented two-camera
//     release acceptance, not this smoke test.
//   - The password is read only from OVM_TEST_PASS and is never printed. Do not
//     add logging that echoes credentials.
package app_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/app"
)

func TestManualRealCameraInventory(t *testing.T) {
	host := os.Getenv("OVM_TEST_HOST")
	user := os.Getenv("OVM_TEST_USER")
	pass := os.Getenv("OVM_TEST_PASS")
	if host == "" {
		t.Skip("OVM_TEST_HOST is not set; skipping manual real-camera test")
	}

	var creds *app.Credentials
	if user != "" {
		built, err := app.NewCredentials(user, pass, false)
		if err != nil {
			t.Fatalf("NewCredentials: %v", err)
		}
		creds = built
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	inv, err := app.NewProductionInventory().Inventory(ctx, app.InventoryRequest{
		Host:        host,
		Credentials: creds,
		Timeout:     20 * time.Second,
	})
	if err != nil {
		t.Fatalf("Inventory error = %v", err)
	}

	// Report per-operation states for the operator's manual inspection. The
	// password is never part of this output.
	if len(inv.Operations) != 7 {
		t.Fatalf("expected 7 operations, got %d", len(inv.Operations))
	}
	for _, status := range inv.Operations {
		t.Logf("operation %-22s %s %s", status.Operation, status.State, status.Detail)
	}
	// Per the minimum-useful-inventory policy, a successful clock read alone is
	// NOT a useful result: Inventory would have returned an error above if only
	// GetSystemDateAndTime had succeeded. A nil error therefore already implies
	// a useful inventory. As an explicit, independent check we assert that at
	// least one NON-clock operation is supported (or that the device answered
	// every non-clock operation as definitively unsupported), never treating
	// clock-only success as the liveness signal.
	anyNonClockSupported := false
	allNonClockUnsupported := true
	for _, status := range inv.Operations {
		if status.Operation == app.OpSystemDateAndTime {
			continue
		}
		if status.State == app.CapabilitySupported {
			anyNonClockSupported = true
		}
		if status.State != app.CapabilityUnsupported {
			allNonClockUnsupported = false
		}
	}
	if !anyNonClockSupported && !allNonClockUnsupported {
		t.Fatal("no non-clock operation was supported and the device did not report all non-clock operations unsupported; a clock-only result is not a useful inventory")
	}
}
