package cli

import (
	"context"
	"errors"
	"testing"

	"github.com/addodelgrossi/ovm/internal/app"
)

func TestCLIGoldenDiscoverCancelledPartialJSON(t *testing.T) {
	result := app.DiscoveryResult{Devices: []app.DiscoveredDevice{{
		EndpointReference: "urn:uuid:camera-a",
		Addresses:         []string{"http://192.0.2.10/onvif/device_service"},
	}}}
	deps := testDependencies()
	deps.Discovery = stubDiscovery{
		result: result,
		err:    app.NewError(app.CategoryCancelled, "discovery was cancelled"),
	}

	stdout, stderr, err := runCommand(deps, "discover", "--json")
	if ExitCodeForError(err) != ExitCancelled {
		t.Fatalf("exit code = %d, want %d", ExitCodeForError(err), ExitCancelled)
	}
	const want = "{\"schema_version\":1,\"kind\":\"discovery\",\"result\":{\"devices\":[{\"endpoint_reference\":\"urn:uuid:camera-a\",\"addresses\":[\"http://192.0.2.10/onvif/device_service\"]}]},\"error\":{\"category\":\"cancelled\",\"message\":\"the operation was cancelled\"}}\n"
	if stdout != want {
		t.Fatalf("stdout golden mismatch\n got: %q\nwant: %q", stdout, want)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestCLIGoldenInfoUsefulPartialHuman(t *testing.T) {
	deps := testDependencies()
	deps.Inventory = &stubInventory{result: app.Inventory{
		Endpoint: "http://camera.invalid/onvif/device_service",
		Operations: []app.OperationStatus{
			{Operation: app.OpHostname, State: app.CapabilitySupported},
		},
		Hostname: &app.HostnameDTO{Name: "camera-1"},
	}}

	stdout, stderr, err := runCommand(deps, "info", "camera.invalid")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	const want = "endpoint: http://camera.invalid/onvif/device_service\noperations:\n  hostname               supported\nhostname: camera-1 (from DHCP: false)\n"
	if stdout != want {
		t.Fatalf("stdout golden mismatch\n got: %q\nwant: %q", stdout, want)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestCLIGoldenExitCodeJoinedCancellation(t *testing.T) {
	err := app.NewError(app.CategoryAuthentication, "competing authentication failure")
	if got := ExitCodeForError(errorsJoin(err, context.Canceled)); got != ExitCancelled {
		t.Fatalf("exit code = %d, want %d", got, ExitCancelled)
	}
}

// errorsJoin is a tiny local seam so the exact golden test remains readable.
func errorsJoin(values ...error) error {
	return errors.Join(values...)
}
