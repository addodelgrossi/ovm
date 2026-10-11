package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/app"
)

// partialCancelResult is the partial discovery result a cancelled round
// returns: one device aggregated before the context ended, no diagnostic.
func partialCancelResult() app.DiscoveryResult {
	return app.DiscoveryResult{
		Devices: []app.DiscoveredDevice{
			{
				EndpointReference: "urn:uuid:partial-a",
				Addresses:         []string{"http://10.0.0.3/onvif/device_service"},
			},
		},
	}
}

// TestDiscoverCancellationRendersPartialDevicesHuman proves the human failure
// path renders the devices discovered before cancellation to stdout and still
// maps to exit code 130. The error remains unpresented because the process entry
// point must print the cancellation message exactly once.
func TestDiscoverCancellationRendersPartialDevicesHuman(t *testing.T) {
	deps := testDependencies()
	deps.Discovery = stubDiscovery{
		result: partialCancelResult(),
		err:    app.NewError(app.CategoryCancelled, "discovery was cancelled"),
	}

	stdout, _, err := runCommand(deps, "discover")
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if code := ExitCodeForError(err); code != ExitCancelled {
		t.Fatalf("exit code = %d, want %d", code, ExitCancelled)
	}
	if IsPresented(err) {
		t.Fatal("partial human output did not render the error; it must remain unpresented for the process entry point")
	}
	if !strings.Contains(stdout, "urn:uuid:partial-a") {
		t.Fatalf("expected the partial device on stdout, got %q", stdout)
	}
	if !strings.Contains(stdout, "http://10.0.0.3/onvif/device_service") {
		t.Fatalf("expected the partial device address on stdout, got %q", stdout)
	}
}

// TestDiscoverCancellationIncludesPartialDevicesJSON proves the JSON failure
// path embeds the partial devices in the versioned failure envelope alongside
// the cancellation error object and still maps to exit code 130.
func TestDiscoverCancellationIncludesPartialDevicesJSON(t *testing.T) {
	deps := testDependencies()
	deps.Discovery = stubDiscovery{
		result: partialCancelResult(),
		err:    app.NewError(app.CategoryCancelled, "discovery was cancelled"),
	}

	stdout, _, err := runCommand(deps, "discover", "--json")
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if code := ExitCodeForError(err); code != ExitCancelled {
		t.Fatalf("exit code = %d, want %d", code, ExitCancelled)
	}
	if !IsPresented(err) {
		t.Fatal("the cancellation error must be marked presented after writing the JSON envelope")
	}

	var env discoveryEnvelope
	if derr := json.Unmarshal([]byte(stdout), &env); derr != nil {
		t.Fatalf("stdout is not a valid envelope: %v (%q)", derr, stdout)
	}
	if env.SchemaVersion != schemaVersion || env.Kind != kindDiscovery {
		t.Fatalf("unexpected envelope header: %+v", env)
	}
	if len(env.Result.Devices) != 1 || env.Result.Devices[0].EndpointReference != "urn:uuid:partial-a" {
		t.Fatalf("expected the partial device in the envelope, got %+v", env.Result.Devices)
	}
	if env.Error == nil || env.Error.Category != app.CategoryCancelled {
		t.Fatalf("expected a cancelled error object in the envelope, got %+v", env.Error)
	}
}

// TestDiscoverCancellationNoDevicesStillCancels confirms a cancellation with no
// partial devices and no diagnostic returns the raw error for the entry point
// to map, still at exit code 130.
func TestDiscoverCancellationNoDevicesStillCancels(t *testing.T) {
	deps := testDependencies()
	deps.Discovery = stubDiscovery{err: context.Canceled}

	stdout, _, err := runCommand(deps, "discover")
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if code := ExitCodeForError(err); code != ExitCancelled {
		t.Fatalf("exit code = %d, want %d", code, ExitCancelled)
	}
	if stdout != "" {
		t.Fatalf("stdout should be empty when there are no partial devices, got %q", stdout)
	}
}
