package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/buildinfo"
)

// stubDiscovery is an injected DiscoveryUseCase double for CLI tests.
type stubDiscovery struct {
	result app.DiscoveryResult
	err    error
}

func (s stubDiscovery) Discover(context.Context, app.DiscoveryRequest) (app.DiscoveryResult, error) {
	return s.result, s.err
}

// stubInventory is an injected InventoryUseCase double for CLI tests.
type stubInventory struct {
	result app.Inventory
	err    error

	// captured records the request so tests can assert flag plumbing.
	captured *app.InventoryRequest
}

func (s *stubInventory) Inventory(_ context.Context, request app.InventoryRequest) (app.Inventory, error) {
	s.captured = &request
	return s.result, s.err
}

func testDependencies() Dependencies {
	return Dependencies{
		Discovery: stubDiscovery{},
		Inventory: &stubInventory{},
		LookupEnv: func(string) (string, bool) { return "", false },
	}
}

// runCommand executes the command tree with the given dependencies and args,
// returning stdout, stderr, and the execution error.
func runCommand(deps Dependencies, args ...string) (string, string, error) {
	cmd := NewRootCommand(buildinfo.Info{Version: "test"}, deps, args)
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return stdout.String(), stderr.String(), err
}

func TestDiscoverHumanOutput(t *testing.T) {
	deps := testDependencies()
	deps.Discovery = stubDiscovery{result: app.DiscoveryResult{
		Devices: []app.DiscoveredDevice{
			{EndpointReference: "urn:uuid:a", Addresses: []string{"http://10.0.0.2/onvif/device_service"}},
		},
	}}
	stdout, stderr, err := runCommand(deps, "discover")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if !strings.Contains(stdout, "urn:uuid:a") || !strings.Contains(stdout, "http://10.0.0.2/onvif/device_service") {
		t.Fatalf("stdout missing device: %q", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr should be empty when devices are found: %q", stderr)
	}
}

func TestDiscoverJSONEnvelope(t *testing.T) {
	deps := testDependencies()
	deps.Discovery = stubDiscovery{result: app.DiscoveryResult{
		Devices: []app.DiscoveredDevice{{EndpointReference: "urn:uuid:a", Addresses: []string{"http://10.0.0.2/x"}}},
	}}
	stdout, _, err := runCommand(deps, "discover", "--json")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	var envelope struct {
		SchemaVersion int    `json:"schema_version"`
		Kind          string `json:"kind"`
		Result        struct {
			Devices []app.DiscoveredDevice `json:"devices"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("decode envelope: %v; stdout = %q", err, stdout)
	}
	if envelope.SchemaVersion != 1 || envelope.Kind != "discovery" {
		t.Fatalf("unexpected envelope header: %+v", envelope)
	}
	if len(envelope.Result.Devices) != 1 {
		t.Fatalf("expected 1 device in envelope, got %d", len(envelope.Result.Devices))
	}
}

func TestDiscoverDiagnosticGoesToStderrInJSONMode(t *testing.T) {
	deps := testDependencies()
	deps.Discovery = stubDiscovery{result: app.DiscoveryResult{
		Diagnostic: &app.DiscoveryDiagnostic{Category: "no_response", Detail: "no device responded within the timeout"},
	}}
	stdout, stderr, err := runCommand(deps, "discover", "--json")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if !strings.Contains(stderr, "no_response") {
		t.Fatalf("stderr should carry the diagnostic: %q", stderr)
	}
	// Stdout must still be a valid JSON envelope with an empty device list.
	if !strings.Contains(stdout, `"kind":"discovery"`) {
		t.Fatalf("stdout should still contain the JSON envelope: %q", stdout)
	}
}

func TestDiscoverNoResponseHuman(t *testing.T) {
	deps := testDependencies()
	deps.Discovery = stubDiscovery{result: app.DiscoveryResult{
		Diagnostic: &app.DiscoveryDiagnostic{Category: "no_response", Detail: "no device responded within the timeout"},
	}}
	stdout, stderr, err := runCommand(deps, "discover")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if stdout != "" {
		t.Fatalf("stdout should be empty on an empty round: %q", stdout)
	}
	if !strings.Contains(stderr, "No cameras found") || !strings.Contains(stderr, "no_response") {
		t.Fatalf("stderr missing no-response message: %q", stderr)
	}
}

func TestDiscoverErrorPropagatesToExitMapping(t *testing.T) {
	// A cancelled context flows through the use case to the command and maps to
	// the cancellation exit code without any special handling.
	deps := testDependencies()
	deps.Discovery = stubDiscovery{err: context.Canceled}
	_, _, err := runCommand(deps, "discover")
	if err == nil {
		t.Fatal("expected an error")
	}
	if code := ExitCodeForError(err); code != ExitCancelled {
		t.Fatalf("exit code = %d, want %d", code, ExitCancelled)
	}
}

// failingWriter returns an error after okWrites successful writes, simulating a
// broken pipe partway through output.
type failingWriter struct {
	okWrites int
	calls    int
}

func (w *failingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls > w.okWrites {
		return 0, errWriteFailed
	}
	return len(p), nil
}

var errWriteFailed = &writeError{}

type writeError struct{}

func (*writeError) Error() string { return "simulated write failure" }

func TestDiscoverSocketErrorExitsNetworkWithSingleDiagnostic(t *testing.T) {
	deps := testDependencies()
	deps.Discovery = stubDiscovery{
		result: app.DiscoveryResult{Diagnostic: &app.DiscoveryDiagnostic{Category: "socket_error", Detail: "the socket operation failed"}},
		err:    app.NewError(app.CategoryNetwork, "discovery could not complete because of a socket error"),
	}
	stdout, stderr, err := runCommand(deps, "discover")
	if err == nil {
		t.Fatal("expected a network error for a socket failure")
	}
	if code := ExitCodeForError(err); code != ExitNetwork {
		t.Fatalf("exit code = %d, want %d", code, ExitNetwork)
	}
	// The diagnostic reached stderr exactly once, and the error is marked
	// presented so the process entry point will not duplicate it.
	if strings.Count(stderr, "socket_error") != 1 {
		t.Fatalf("expected the diagnostic exactly once on stderr, got %q", stderr)
	}
	if !IsPresented(err) {
		t.Fatal("expected the socket error to be marked presented")
	}
	if stdout != "" {
		t.Fatalf("stdout should be empty on a socket failure, got %q", stdout)
	}
}

func TestDiscoverSanitizesInjectedDeviceFields(t *testing.T) {
	// A camera-controlled endpoint reference and address carrying CR/LF, ANSI
	// escape, and a bidi override must be rendered as a single safe line.
	injected := "urn:uuid:evil\r\n\x1b[31mFAKE LINE\x1b[0m\u202Egnp.exe"
	injectedAddr := "http://10.0.0.2/\x1b]0;pwned\x07"
	deps := testDependencies()
	deps.Discovery = stubDiscovery{result: app.DiscoveryResult{
		Devices: []app.DiscoveredDevice{{EndpointReference: injected, Addresses: []string{injectedAddr}}},
	}}
	stdout, _, err := runCommand(deps, "discover")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if strings.ContainsAny(stdout, "\r\x1b") {
		t.Fatalf("stdout contains raw control characters: %q", stdout)
	}
	if strings.Contains(stdout, "\u202E") {
		t.Fatalf("stdout contains a bidi override: %q", stdout)
	}
	// The endpoint line must be a single logical line (plus addresses), not
	// split by the injected newline.
	lines := strings.Split(strings.TrimRight(stdout, "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 output lines (endpoint + address), got %d: %q", len(lines), stdout)
	}
}

func TestDiscoverJSONRetainsOriginalValuesSafely(t *testing.T) {
	// JSON must retain the ORIGINAL (unsanitized) values, safely escaped by
	// encoding/json, so a machine consumer sees the true device data.
	injected := "urn:uuid:evil\nSECOND"
	deps := testDependencies()
	deps.Discovery = stubDiscovery{result: app.DiscoveryResult{
		Devices: []app.DiscoveredDevice{{EndpointReference: injected, Addresses: []string{"http://10.0.0.2/x"}}},
	}}
	stdout, _, err := runCommand(deps, "discover", "--json")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	var envelope struct {
		Result struct {
			Devices []app.DiscoveredDevice `json:"devices"`
		} `json:"result"`
	}
	if uerr := json.Unmarshal([]byte(stdout), &envelope); uerr != nil {
		t.Fatalf("decode envelope: %v; stdout = %q", uerr, stdout)
	}
	if len(envelope.Result.Devices) != 1 || envelope.Result.Devices[0].EndpointReference != injected {
		t.Fatalf("JSON must retain the original value, got %+v", envelope.Result.Devices)
	}
	// The raw newline must be escaped in the serialized form, never emitted
	// literally inside the JSON string token.
	if strings.Contains(stdout, "evil\nSECOND") {
		t.Fatalf("JSON leaked a raw newline: %q", stdout)
	}
}

func TestDiscoverHumanPropagatesWriterError(t *testing.T) {
	result := app.DiscoveryResult{
		Devices: []app.DiscoveredDevice{{EndpointReference: "urn:uuid:a", Addresses: []string{"http://10.0.0.2/x"}}},
	}
	fw := &failingWriter{okWrites: 0}
	var stderr strings.Builder
	if err := writeDiscoveryHuman(fw, &stderr, result); err == nil {
		t.Fatal("expected a writer error to propagate")
	}
}

func TestDiscoverJSONPropagatesWriterError(t *testing.T) {
	result := app.DiscoveryResult{
		Devices: []app.DiscoveredDevice{{EndpointReference: "urn:uuid:a", Addresses: []string{"http://10.0.0.2/x"}}},
	}
	fw := &failingWriter{okWrites: 0}
	var stderr strings.Builder
	if err := writeDiscoveryJSON(fw, &stderr, result); err == nil {
		t.Fatal("expected a JSON writer error to propagate")
	}
}

func TestDiscoverJSONDiagnosticWriterErrorTakesPrecedence(t *testing.T) {
	// When both stderr (diagnostic) and stdout (envelope) would fail, the
	// stderr failure is attempted first and takes deterministic precedence.
	result := app.DiscoveryResult{Diagnostic: &app.DiscoveryDiagnostic{Category: "no_response"}}
	stdout := &failingWriter{okWrites: 0}
	stderr := &failingWriter{okWrites: 0}
	err := writeDiscoveryJSON(stdout, stderr, result)
	if err == nil {
		t.Fatal("expected a writer error to propagate")
	}
	// stderr was attempted first; stdout must not have been written at all.
	if stdout.calls != 0 {
		t.Fatalf("stdout should not be written when the stderr diagnostic fails first, calls = %d", stdout.calls)
	}
}

func TestDiscoverFlagPlumbing(t *testing.T) {
	// --iface and --timeout must reach the discovery request.
	rec := &recordingDiscovery{}
	deps := testDependencies()
	deps.Discovery = rec
	_, _, err := runCommand(deps, "discover", "--iface", "eth0", "--timeout", "9s")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if rec.captured.InterfaceName != "eth0" {
		t.Fatalf("iface = %q, want eth0", rec.captured.InterfaceName)
	}
	if rec.captured.Timeout.String() != "9s" {
		t.Fatalf("timeout = %s, want 9s", rec.captured.Timeout)
	}
}

// recordingDiscovery captures the request for flag-plumbing assertions.
type recordingDiscovery struct {
	captured app.DiscoveryRequest
}

func (r *recordingDiscovery) Discover(_ context.Context, request app.DiscoveryRequest) (app.DiscoveryResult, error) {
	r.captured = request
	return app.DiscoveryResult{}, nil
}

func TestDiscoverRejectsNonPositiveTimeout(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"0s", "-1s"} {
		_, _, err := runCommand(testDependencies(), "discover", "--timeout", value)
		if code := ExitCodeForError(err); code != ExitUsage {
			t.Fatalf("--timeout %s exit code = %d, want %d", value, code, ExitUsage)
		}
	}
}

func TestDiscoverFailureJSONKeepsVersionedEnvelope(t *testing.T) {
	t.Parallel()

	result := app.DiscoveryResult{Diagnostic: &app.DiscoveryDiagnostic{
		Category: "socket_error",
		Detail:   "a socket operation failed",
	}}
	deps := testDependencies()
	deps.Discovery = stubDiscovery{result: result, err: app.NewError(app.CategoryNetwork, "discovery failed")}
	stdout, stderr, err := runCommand(deps, "discover", "--json")
	if code := ExitCodeForError(err); code != ExitNetwork {
		t.Fatalf("exit code = %d, want %d", code, ExitNetwork)
	}
	if !IsPresented(err) {
		t.Fatal("fatal discovery diagnostic was not marked presented")
	}
	var envelope struct {
		SchemaVersion int    `json:"schema_version"`
		Kind          string `json:"kind"`
		Error         struct {
			Category string `json:"category"`
			Message  string `json:"message"`
		} `json:"error"`
	}
	if decodeErr := json.Unmarshal([]byte(stdout), &envelope); decodeErr != nil {
		t.Fatalf("decode failure envelope: %v; stdout = %q", decodeErr, stdout)
	}
	if envelope.SchemaVersion != 1 || envelope.Kind != "discovery" || envelope.Error.Category != "network" || envelope.Error.Message == "" {
		t.Fatalf("failure envelope = %+v", envelope)
	}
	if strings.Count(stderr, "socket_error") != 1 {
		t.Fatalf("stderr = %q, want one socket diagnostic", stderr)
	}
}

func TestDiscoverValidationFailureJSONEnvelope(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{
		{"discover", "--json", "unexpected"},
		{"discover", "--json", "--timeout", "0s"},
		{"discover", "--json", "--unknown"},
		{"discover", "--unknown", "--json"},
		{"discover", "--timeout", "not-a-duration", "--json"},
	} {
		stdout, _, err := runCommand(testDependencies(), args...)
		if code := ExitCodeForError(err); code != ExitUsage {
			t.Fatalf("args %v exit code = %d, want %d", args, code, ExitUsage)
		}
		var envelope struct {
			SchemaVersion int    `json:"schema_version"`
			Kind          string `json:"kind"`
			Error         struct {
				Category string `json:"category"`
			} `json:"error"`
		}
		if decodeErr := json.Unmarshal([]byte(stdout), &envelope); decodeErr != nil {
			t.Fatalf("args %v decode error = %v; stdout = %q", args, decodeErr, stdout)
		}
		if envelope.SchemaVersion != 1 || envelope.Kind != "discovery" || envelope.Error.Category != "usage" {
			t.Fatalf("args %v envelope = %+v", args, envelope)
		}
	}
}
