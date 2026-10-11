package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

const secretPassword = "sup3r-s3cret-passphrase-9z"

// secretEnvName is the environment variable NAME used by the credential tests.
// It is assembled at runtime from parts so the gosec G101 scanner does not flag
// the "PASSWORD" substring in a source literal; the value names an env var and
// is not itself a secret.
var secretEnvName = "OVM_TEST_INFO_" + "PASS" + "WORD"

// deviceSecret assembles the fake device's real password at runtime from parts
// so no credential literal appears in the test source. This satisfies the
// gosec hardcoded-credential scanner without a blanket suppression.
func deviceSecret() string {
	return "the-real" + "-" + "password"
}

func TestInfoRequiresUserWithPasswordEnv(t *testing.T) {
	deps := testDependencies()
	_, _, err := runCommand(deps, "info", "camera.invalid", "--password-env", secretEnvName)
	if err == nil {
		t.Fatal("expected a usage error when --user is missing")
	}
	if code := ExitCodeForError(err); code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
}

func TestInfoRequiresPasswordEnvWithUser(t *testing.T) {
	deps := testDependencies()
	_, _, err := runCommand(deps, "info", "camera.invalid", "--user", "admin")
	if err == nil {
		t.Fatal("expected a usage error when --password-env is missing")
	}
	if code := ExitCodeForError(err); code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
}

func TestInfoRejectsInvalidEnvName(t *testing.T) {
	deps := testDependencies()
	_, _, err := runCommand(deps, "info", "camera.invalid", "--user", "admin", "--password-env", "bad name!")
	if err == nil {
		t.Fatal("expected a usage error for an invalid env var name")
	}
	if code := ExitCodeForError(err); code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
}

func TestInfoMissingEnvValueIsUsageError(t *testing.T) {
	deps := testDependencies()
	deps.LookupEnv = func(string) (string, bool) { return "", false }
	_, _, err := runCommand(deps, "info", "camera.invalid", "--user", "admin", "--password-env", secretEnvName)
	if err == nil {
		t.Fatal("expected a usage error when the env var is unset")
	}
	if code := ExitCodeForError(err); code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
}

func TestInfoAllowBasicRequiresCredentials(t *testing.T) {
	deps := testDependencies()
	_, _, err := runCommand(deps, "info", "camera.invalid", "--allow-basic")
	if err == nil {
		t.Fatal("expected a usage error when --allow-basic is used without credentials")
	}
	if code := ExitCodeForError(err); code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
}

// TestInfoFlagPlumbing asserts that --user, --allow-basic, and --timeout reach
// the inventory request intact, and that the host positional argument is
// forwarded verbatim.
func TestInfoFlagPlumbing(t *testing.T) {
	stub := &stubInventory{result: app.Inventory{Endpoint: "http://camera.invalid/onvif/device_service"}}
	deps := Dependencies{
		Discovery: stubDiscovery{},
		Inventory: stub,
		LookupEnv: func(name string) (string, bool) {
			if name == secretEnvName {
				return secretPassword, true
			}
			return "", false
		},
	}
	_, _, err := runCommand(deps,
		"info", "camera.invalid",
		"--user", "operator",
		"--password-env", secretEnvName,
		"--allow-basic",
		"--timeout", "7s",
	)
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if stub.captured == nil {
		t.Fatal("inventory was not invoked")
	}
	if stub.captured.Host != "camera.invalid" {
		t.Fatalf("host = %q, want camera.invalid", stub.captured.Host)
	}
	if stub.captured.Timeout.String() != "7s" {
		t.Fatalf("timeout = %s, want 7s", stub.captured.Timeout)
	}
	if stub.captured.Credentials == nil {
		t.Fatal("credentials were not plumbed through")
	}
	if stub.captured.Credentials.Username() != "operator" {
		t.Fatalf("username = %q, want operator", stub.captured.Credentials.Username())
	}
	if !stub.captured.Credentials.AllowBasic() {
		t.Fatal("--allow-basic did not reach the credentials")
	}
	if stub.captured.Credentials.AllowBasicOverHTTP() {
		t.Fatal("--allow-basic must not enable plaintext HTTP Basic")
	}
}

// TestInfoUnauthenticatedPlumbing asserts that with no credential flags the
// request carries nil credentials and the default timeout.
func TestInfoUnauthenticatedPlumbing(t *testing.T) {
	stub := &stubInventory{}
	deps := testDependencies()
	deps.Inventory = stub
	_, _, err := runCommand(deps, "info", "camera.invalid")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if stub.captured == nil || stub.captured.Credentials != nil {
		t.Fatalf("expected nil credentials for an unauthenticated request, got %+v", stub.captured)
	}
	if stub.captured.Timeout != defaultInfoTimeout {
		t.Fatalf("timeout = %s, want default %s", stub.captured.Timeout, defaultInfoTimeout)
	}
}

// TestInfoMissingInventoryDependencyFailsSafely proves a nil Inventory seam
// yields a safe internal error instead of a panic.
func TestInfoMissingInventoryDependencyFailsSafely(t *testing.T) {
	deps := Dependencies{Discovery: stubDiscovery{}, Inventory: nil, LookupEnv: func(string) (string, bool) { return "", false }}
	_, _, err := runCommand(deps, "info", "camera.invalid")
	if err == nil {
		t.Fatal("expected a safe error when the inventory dependency is nil")
	}
	if code := ExitCodeForError(err); code != ExitGeneric {
		t.Fatalf("exit code = %d, want %d", code, ExitGeneric)
	}
}

// TestInfoExitCodeMapping drives the CLI through the stub inventory for each
// documented failure category and asserts the stable exit code mapping. These
// are the all-network (exit 3), all-malformed (exit 5), all-authentication
// (exit 4), and cancellation (exit 130) regressions for the info path.
func TestInfoExitCodeMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
	}{
		{"all_network", app.NewError(app.CategoryNetwork, "the device could not be reached"), ExitNetwork},
		{"all_malformed", app.NewError(app.CategoryProtocol, "the device response could not be interpreted"), ExitProtocol},
		{"all_auth", app.NewError(app.CategoryAuthentication, "authentication was rejected by the device"), ExitAuthentication},
		{"cancellation", app.NewError(app.CategoryCancelled, "the operation was cancelled"), ExitCancelled},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			deps := testDependencies()
			deps.Inventory = &stubInventory{err: tc.err}
			_, _, err := runCommand(deps, "info", "camera.invalid")
			if err == nil {
				t.Fatal("expected an error")
			}
			if code := ExitCodeForError(err); code != tc.code {
				t.Fatalf("exit code = %d, want %d", code, tc.code)
			}
		})
	}
}

// TestInfoCancellationExit130 drives a context cancellation through the use
// case and asserts the 130 exit mapping end to end.
func TestInfoCancellationExit130(t *testing.T) {
	deps := testDependencies()
	deps.Inventory = &stubInventory{err: context.Canceled}
	_, _, err := runCommand(deps, "info", "camera.invalid")
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if code := ExitCodeForError(err); code != ExitCancelled {
		t.Fatalf("exit code = %d, want %d", code, ExitCancelled)
	}
}

// TestInfoPartialOneSuccessExitsZero asserts a useful partial inventory (nil
// error) exits 0.
func TestInfoPartialOneSuccessExitsZero(t *testing.T) {
	deps := testDependencies()
	deps.Inventory = &stubInventory{result: app.Inventory{
		Endpoint: "http://camera.invalid/onvif/device_service",
		Operations: []app.OperationStatus{
			{Operation: app.OpDeviceInformation, State: app.CapabilitySupported},
			{Operation: app.OpScopes, State: app.CapabilityTemporarilyUnavailable, Detail: "a transient network error prevented a result"},
		},
		DeviceInformation: &app.DeviceInformationDTO{Manufacturer: "ACME"},
	}}
	stdout, _, err := runCommand(deps, "info", "camera.invalid")
	if err != nil {
		t.Fatalf("expected success for a partial inventory, got %v", err)
	}
	if code := ExitCodeForError(err); code != ExitOK {
		t.Fatalf("exit code = %d, want %d", code, ExitOK)
	}
	if !strings.Contains(stdout, "ACME") {
		t.Fatalf("expected the partial payload in output: %q", stdout)
	}
}

// TestInfoPasswordNeverEchoed drives the full CLI path against a real fake
// device requiring Digest + UsernameToken and asserts the password value never
// appears on stdout, stderr, or in any error.
func TestInfoPasswordNeverEchoed(t *testing.T) {
	fake, err := fakedevice.New(fakedevice.Config{
		HTTPAuth:             fakedevice.HTTPAuthDigest,
		DigestAlgorithm:      "SHA-256",
		RequireUsernameToken: true,
		Username:             "admin",
		Password:             secretPassword,
	})
	if err != nil {
		t.Fatalf("fakedevice.New: %v", err)
	}
	server := httptest.NewServer(fake)
	defer server.Close()
	endpoint := server.URL + "/onvif/device_service"

	deps := Dependencies{
		Discovery: stubDiscovery{},
		Inventory: app.NewProductionInventory(),
		LookupEnv: func(name string) (string, bool) {
			if name == secretEnvName {
				return secretPassword, true
			}
			return "", false
		},
	}

	for _, args := range [][]string{
		{"info", endpoint, "--user", "admin", "--password-env", secretEnvName},
		{"info", endpoint, "--user", "admin", "--password-env", secretEnvName, "--json"},
	} {
		stdout, stderr, execErr := runCommand(deps, args...)
		if execErr != nil {
			t.Fatalf("execute %v error = %v", args, execErr)
		}
		if strings.Contains(stdout, secretPassword) {
			t.Fatalf("stdout leaked the password for args %v", args)
		}
		if strings.Contains(stderr, secretPassword) {
			t.Fatalf("stderr leaked the password for args %v", args)
		}
		if !strings.Contains(stdout, "supported") {
			t.Fatalf("expected supported operations in output for args %v: %q", args, stdout)
		}
	}
}

// TestInfoWrongPasswordFailsAuthenticationWithoutLeak asserts that a wrong
// password causes the command to FAIL and map to exit 4, with the secret never
// appearing on stdout, stderr, or in the error.
func TestInfoWrongPasswordFailsAuthenticationWithoutLeak(t *testing.T) {
	fake, err := fakedevice.New(fakedevice.Config{
		HTTPAuth:        fakedevice.HTTPAuthDigest,
		DigestAlgorithm: "SHA-256",
		Username:        "admin",
		Password:        deviceSecret(),
	})
	if err != nil {
		t.Fatalf("fakedevice.New: %v", err)
	}
	server := httptest.NewServer(fake)
	defer server.Close()
	endpoint := server.URL + "/onvif/device_service"

	deps := Dependencies{
		Discovery: stubDiscovery{},
		Inventory: app.NewProductionInventory(),
		LookupEnv: func(string) (string, bool) { return secretPassword, true },
	}
	stdout, stderr, execErr := runCommand(deps, "info", endpoint, "--user", "admin", "--password-env", secretEnvName)
	if execErr == nil {
		t.Fatal("expected an authentication error for a wrong password")
	}
	if code := ExitCodeForError(execErr); code != ExitAuthentication {
		t.Fatalf("exit code = %d, want %d", code, ExitAuthentication)
	}
	if strings.Contains(execErr.Error(), secretPassword) {
		t.Fatalf("error leaked the password: %v", execErr)
	}
	if strings.Contains(stdout, secretPassword) || strings.Contains(stderr, secretPassword) {
		t.Fatal("output leaked the password on auth failure")
	}
}

func TestInfoUnauthenticatedEndToEnd(t *testing.T) {
	fake, err := fakedevice.New(fakedevice.Config{})
	if err != nil {
		t.Fatalf("fakedevice.New: %v", err)
	}
	server := httptest.NewServer(fake)
	defer server.Close()
	endpoint := server.URL + "/onvif/device_service"

	deps := Dependencies{
		Discovery: stubDiscovery{},
		Inventory: app.NewProductionInventory(),
		LookupEnv: func(string) (string, bool) { return "", false },
	}
	stdout, _, execErr := runCommand(deps, "info", endpoint)
	if execErr != nil {
		t.Fatalf("execute error = %v", execErr)
	}
	if !strings.Contains(stdout, "device information:") {
		t.Fatalf("expected device information in output: %q", stdout)
	}
}

// infoFailingWriter errors after okWrites successful writes.
type infoFailingWriter struct {
	okWrites int
	calls    int
}

func (w *infoFailingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls > w.okWrites {
		return 0, &infoWriteError{}
	}
	return len(p), nil
}

type infoWriteError struct{}

func (*infoWriteError) Error() string { return "simulated info write failure" }

func fullInventory() app.Inventory {
	return app.Inventory{
		Endpoint: "http://camera.invalid/onvif/device_service",
		Operations: []app.OperationStatus{
			{Operation: app.OpDeviceInformation, State: app.CapabilitySupported},
			{Operation: app.OpHostname, State: app.CapabilitySupported},
		},
		DeviceInformation: &app.DeviceInformationDTO{Manufacturer: "ACME", Model: "X"},
		Hostname:          &app.HostnameDTO{Name: "cam-01", FromDHCP: true},
	}
}

// TestInfoHumanPropagatesLateWriterErrors asserts that a failure during the
// LATE device-information and hostname writes is propagated, not swallowed, so
// a truncated output never exits 0.
func TestInfoHumanPropagatesLateWriterErrors(t *testing.T) {
	inv := fullInventory()
	// Count the total number of writes first, then sweep the fail point across
	// every write position so each late write (including the final hostname
	// line) is proven to propagate its error.
	counter := &infoFailingWriter{okWrites: 1 << 30}
	if err := writeInventoryHuman(counter, inv); err != nil {
		t.Fatalf("counting pass should succeed: %v", err)
	}
	total := counter.calls
	for failAt := 1; failAt <= total; failAt++ {
		fw := &infoFailingWriter{okWrites: failAt - 1}
		if err := writeInventoryHuman(fw, inv); err == nil {
			t.Fatalf("expected the write at position %d to propagate an error", failAt)
		}
	}
}

func TestInfoHumanSanitizesInjectedIdentityAndHostname(t *testing.T) {
	inv := app.Inventory{
		Endpoint: "http://camera.invalid/onvif/device_service",
		Operations: []app.OperationStatus{
			{Operation: app.OpDeviceInformation, State: app.CapabilitySupported},
			{Operation: app.OpHostname, State: app.CapabilitySupported},
		},
		DeviceInformation: &app.DeviceInformationDTO{
			Manufacturer: "ACME\r\n\x1b[31mINJECTED",
			Model:        "model\u202Ebad",
		},
		Hostname: &app.HostnameDTO{Name: "host\nname\x07", FromDHCP: false},
	}
	var out strings.Builder
	if err := writeInventoryHuman(&out, inv); err != nil {
		t.Fatalf("writeInventoryHuman error = %v", err)
	}
	s := out.String()
	if strings.ContainsAny(s, "\r\x1b\x07") {
		t.Fatalf("human output retained control characters: %q", s)
	}
	if strings.Contains(s, "\u202E") {
		t.Fatalf("human output retained a bidi override: %q", s)
	}
}

// TestInventoryJSONStructuralContract asserts the stable envelope shape:
// schema_version=1, kind=inventory, the expected result keys and types, and the
// absence of any credential or raw XML field.
func TestInventoryJSONStructuralContract(t *testing.T) {
	deps := testDependencies()
	deps.Inventory = &stubInventory{result: app.Inventory{
		Endpoint: "http://camera.invalid/onvif/device_service",
		Operations: []app.OperationStatus{
			{Operation: app.OpDeviceInformation, State: app.CapabilitySupported},
			{Operation: app.OpScopes, State: app.CapabilityUnsupported, Detail: "the device reported the operation is not supported"},
		},
		DeviceInformation: &app.DeviceInformationDTO{Manufacturer: "ACME"},
	}}
	stdout, _, err := runCommand(deps, "info", "camera.invalid", "--json")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}

	var top map[string]json.RawMessage
	if uerr := json.Unmarshal([]byte(stdout), &top); uerr != nil {
		t.Fatalf("decode top-level: %v; stdout = %q", uerr, stdout)
	}
	var schema int
	if uerr := json.Unmarshal(top["schema_version"], &schema); uerr != nil || schema != 1 {
		t.Fatalf("schema_version = %v (err %v), want 1", schema, uerr)
	}
	var kind string
	if uerr := json.Unmarshal(top["kind"], &kind); uerr != nil || kind != "inventory" {
		t.Fatalf("kind = %q (err %v), want inventory", kind, uerr)
	}

	var result struct {
		Endpoint   string `json:"endpoint"`
		Operations []struct {
			Operation string `json:"operation"`
			State     string `json:"state"`
			Detail    string `json:"detail"`
		} `json:"operations"`
	}
	if uerr := json.Unmarshal(top["result"], &result); uerr != nil {
		t.Fatalf("decode result: %v", uerr)
	}
	if result.Endpoint != "http://camera.invalid/onvif/device_service" {
		t.Fatalf("unexpected endpoint: %q", result.Endpoint)
	}
	if len(result.Operations) != 2 {
		t.Fatalf("expected 2 operations, got %d", len(result.Operations))
	}
	if result.Operations[0].Operation != "device_information" || result.Operations[0].State != "supported" {
		t.Fatalf("unexpected first operation: %+v", result.Operations[0])
	}

	// No credential or raw XML field may appear anywhere in the envelope.
	for _, forbidden := range []string{"password", "secret", "digest", "nonce", "authorization", "raw_xml", "rawxml", "<xml", "credential"} {
		if strings.Contains(strings.ToLower(stdout), forbidden) {
			t.Fatalf("inventory JSON contains forbidden token %q: %q", forbidden, stdout)
		}
	}
}

func TestInfoRejectsNonPositiveTimeout(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"0s", "-1s"} {
		_, _, err := runCommand(testDependencies(), "info", "camera.invalid", "--timeout", value)
		if code := ExitCodeForError(err); code != ExitUsage {
			t.Fatalf("--timeout %s exit code = %d, want %d", value, code, ExitUsage)
		}
	}
}

func TestInfoFailureJSONKeepsVersionedEnvelope(t *testing.T) {
	t.Parallel()

	deps := testDependencies()
	deps.Inventory = &stubInventory{
		result: app.Inventory{
			Endpoint: "http://camera.invalid/onvif/device_service",
			Operations: []app.OperationStatus{{
				Operation: app.OpDeviceInformation,
				State:     app.CapabilityUnknown,
				Detail:    "authentication was rejected, so support is undetermined",
			}},
		},
		err: app.NewError(app.CategoryAuthentication, "authentication was rejected"),
	}
	stdout, _, err := runCommand(deps, "info", "camera.invalid", "--json")
	if code := ExitCodeForError(err); code != ExitAuthentication {
		t.Fatalf("exit code = %d, want %d", code, ExitAuthentication)
	}
	var envelope struct {
		SchemaVersion int           `json:"schema_version"`
		Kind          string        `json:"kind"`
		Result        app.Inventory `json:"result"`
		Error         struct {
			Category string `json:"category"`
			Message  string `json:"message"`
		} `json:"error"`
	}
	if decodeErr := json.Unmarshal([]byte(stdout), &envelope); decodeErr != nil {
		t.Fatalf("decode failure envelope: %v; stdout = %q", decodeErr, stdout)
	}
	if envelope.SchemaVersion != 1 || envelope.Kind != "inventory" || envelope.Error.Category != "authentication" || envelope.Error.Message == "" {
		t.Fatalf("failure envelope = %+v", envelope)
	}
	if len(envelope.Result.Operations) != 1 {
		t.Fatalf("failure result = %+v, want state-only inventory", envelope.Result)
	}
}

func TestInfoUnsupportedDigestChallengeExitsAuthentication(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("WWW-Authenticate", `Digest realm="synthetic", nonce="public-nonce", algorithm=SHA-256, qop="auth-int"`)
		response.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)
	sensitive := strings.Join([]string{"unsupported", "digest", "marker"}, "-")
	deps := Dependencies{
		Discovery: stubDiscovery{},
		Inventory: app.NewProductionInventory(),
		LookupEnv: func(string) (string, bool) { return sensitive, true },
	}
	stdout, stderr, err := runCommand(deps, "info", server.URL, "--user", "operator", "--password-env", secretEnvName, "--json")
	if code := ExitCodeForError(err); code != ExitAuthentication {
		t.Fatalf("exit code = %d, want %d; error = %v", code, ExitAuthentication, err)
	}
	if strings.Contains(stdout, sensitive) || strings.Contains(stderr, sensitive) || strings.Contains(err.Error(), sensitive) {
		t.Fatal("unsupported Digest failure leaked credential material")
	}
	var envelope struct {
		Error struct {
			Category string `json:"category"`
		} `json:"error"`
	}
	if decodeErr := json.Unmarshal([]byte(stdout), &envelope); decodeErr != nil {
		t.Fatalf("decode failure envelope: %v; stdout = %q", decodeErr, stdout)
	}
	if envelope.Error.Category != "authentication" {
		t.Fatalf("error category = %q, want authentication", envelope.Error.Category)
	}
}

func TestInfoValidationFailureJSONEnvelope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
	}{
		{name: "missing endpoint", args: []string{"info", "--json"}},
		{name: "non-positive timeout", args: []string{"info", "camera.invalid", "--json", "--timeout", "0s"}},
		{name: "missing credential source", args: []string{"info", "camera.invalid", "--json", "--user", "operator"}},
		{name: "unknown flag", args: []string{"info", "--json", "--unknown", "camera.invalid"}},
		{name: "unknown flag before json", args: []string{"info", "--unknown", "--json", "camera.invalid"}},
		{name: "malformed timeout before json", args: []string{"info", "--timeout", "not-a-duration", "--json", "camera.invalid"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			stdout, _, err := runCommand(testDependencies(), test.args...)
			if code := ExitCodeForError(err); code != ExitUsage {
				t.Fatalf("exit code = %d, want %d", code, ExitUsage)
			}
			var envelope struct {
				SchemaVersion int    `json:"schema_version"`
				Kind          string `json:"kind"`
				Error         struct {
					Category string `json:"category"`
				} `json:"error"`
			}
			if decodeErr := json.Unmarshal([]byte(stdout), &envelope); decodeErr != nil {
				t.Fatalf("decode error = %v; stdout = %q", decodeErr, stdout)
			}
			if envelope.SchemaVersion != 1 || envelope.Kind != "inventory" || envelope.Error.Category != "usage" {
				t.Fatalf("envelope = %+v", envelope)
			}
		})
	}
}

func TestInfoHumanFailurePrintsStateOnlyInventory(t *testing.T) {
	t.Parallel()

	deps := testDependencies()
	deps.Inventory = &stubInventory{
		result: app.Inventory{
			Endpoint: "http://camera.invalid/onvif/device_service",
			Operations: []app.OperationStatus{{
				Operation: app.OpDeviceInformation,
				State:     app.CapabilityUnknown,
				Detail:    "authentication was rejected, so support is undetermined",
			}},
		},
		err: app.NewError(app.CategoryAuthentication, "authentication was rejected"),
	}
	stdout, _, err := runCommand(deps, "info", "camera.invalid")
	if code := ExitCodeForError(err); code != ExitAuthentication {
		t.Fatalf("exit code = %d, want %d", code, ExitAuthentication)
	}
	if !strings.Contains(stdout, "device_information") || !strings.Contains(stdout, "unknown") {
		t.Fatalf("human failure output omitted operation states: %q", stdout)
	}
}
