package cli

import (
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/app"
)

// credsDependencies returns deps whose env lookup resolves a password so the
// info command can build credentials and reach the downgrade-warning path.
func credsDependencies(password string) (Dependencies, *stubInventory) {
	stub := &stubInventory{result: app.Inventory{Endpoint: "http://camera.invalid/onvif/device_service"}}
	deps := Dependencies{
		Discovery: stubDiscovery{},
		Inventory: stub,
		LookupEnv: func(name string) (string, bool) {
			if name == "OVM_PASS" {
				return password, true
			}
			return "", false
		},
	}
	return deps, stub
}

// TestAllowBasicEmitsOneStderrWarning proves --allow-basic emits exactly one
// warning to stderr and nothing to stdout's data stream.
func TestAllowBasicEmitsOneStderrWarning(t *testing.T) {
	deps, _ := credsDependencies("s3cret")
	stdout, stderr, err := runCommand(deps, "info", "camera.invalid", "--user", "admin", "--password-env", "OVM_PASS", "--allow-basic")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if n := strings.Count(stderr, "HTTP Basic authentication is enabled"); n != 1 {
		t.Fatalf("expected exactly one Basic warning on stderr, got %d: %q", n, stderr)
	}
	if strings.Contains(stdout, "warning:") {
		t.Fatalf("warning must not appear on stdout: %q", stdout)
	}
	if strings.Contains(stderr, "legacy no-qop") {
		t.Fatalf("legacy warning must not appear when only --allow-basic is set: %q", stderr)
	}
}

// TestAllowLegacyDigestEmitsOneStderrWarning proves --allow-legacy-digest emits
// exactly one warning to stderr.
func TestAllowLegacyDigestEmitsOneStderrWarning(t *testing.T) {
	deps, _ := credsDependencies("s3cret")
	_, stderr, err := runCommand(deps, "info", "camera.invalid", "--user", "admin", "--password-env", "OVM_PASS", "--allow-legacy-digest")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if n := strings.Count(stderr, "legacy no-qop (RFC 2069) Digest authentication is enabled"); n != 1 {
		t.Fatalf("expected exactly one legacy warning on stderr, got %d: %q", n, stderr)
	}
}

// TestBothDowngradeFlagsEmitBothWarnings proves both flags emit their own
// distinct warning.
func TestBothDowngradeFlagsEmitBothWarnings(t *testing.T) {
	deps, _ := credsDependencies("s3cret")
	_, stderr, err := runCommand(deps, "info", "camera.invalid", "--user", "admin", "--password-env", "OVM_PASS", "--allow-basic", "--allow-legacy-digest")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if !strings.Contains(stderr, "HTTP Basic authentication is enabled") {
		t.Fatalf("missing Basic warning: %q", stderr)
	}
	if !strings.Contains(stderr, "legacy no-qop (RFC 2069) Digest authentication is enabled") {
		t.Fatalf("missing legacy warning: %q", stderr)
	}
}

// TestDowngradeWarningsNeverLeakSecrets proves the warnings carry no password,
// username, or endpoint.
func TestDowngradeWarningsNeverLeakSecrets(t *testing.T) {
	const secret = "SUPER-SECRET-PASS"
	deps, _ := credsDependencies(secret)
	_, stderr, err := runCommand(deps, "info", "secret-host.invalid", "--user", "secret-admin", "--password-env", "OVM_PASS", "--allow-basic", "--allow-legacy-digest")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	for _, leak := range []string{secret, "secret-admin", "secret-host.invalid", "OVM_PASS"} {
		if strings.Contains(stderr, leak) {
			t.Fatalf("stderr warning leaked %q: %q", leak, stderr)
		}
	}
}

// TestDowngradeFlagsRequireCredentials proves each downgrade flag requires
// --user and --password-env and otherwise fails as a usage error without
// emitting a warning.
func TestDowngradeFlagsRequireCredentials(t *testing.T) {
	for _, flag := range []string{"--allow-basic", "--allow-insecure-basic", "--allow-legacy-digest"} {
		deps, _ := credsDependencies("s3cret")
		_, stderr, err := runCommand(deps, "info", "camera.invalid", flag)
		if err == nil {
			t.Fatalf("%s without credentials must be a usage error", flag)
		}
		if ExitCodeForError(err) != ExitUsage {
			t.Fatalf("%s without credentials: exit = %d, want %d", flag, ExitCodeForError(err), ExitUsage)
		}
		if strings.Contains(stderr, "warning:") {
			t.Fatalf("%s without credentials must not emit a warning: %q", flag, stderr)
		}
	}
}

// TestDowngradeWarningJSONModeKeepsStdoutClean proves that in JSON mode the
// warnings still go to stderr and stdout remains valid JSON (no warning text).
func TestDowngradeWarningJSONModeKeepsStdoutClean(t *testing.T) {
	deps, _ := credsDependencies("s3cret")
	stdout, stderr, err := runCommand(deps, "info", "camera.invalid", "--user", "admin", "--password-env", "OVM_PASS", "--allow-basic", "--json")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if !strings.Contains(stderr, "HTTP Basic authentication is enabled") {
		t.Fatalf("JSON mode must still warn on stderr: %q", stderr)
	}
	if strings.Contains(stdout, "warning") {
		t.Fatalf("JSON stdout must not contain warning text: %q", stdout)
	}
	if !strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Fatalf("JSON stdout should be a JSON object: %q", stdout)
	}
}

// TestUnknownCommandDoesNotEchoRawToken proves the root unknown-command error
// never reflects a raw (possibly credential-bearing) first token.
func TestUnknownCommandDoesNotEchoRawToken(t *testing.T) {
	const token = "SECRET-TOKEN-abc123"
	deps := testDependencies()
	_, _, err := runCommand(deps, token)
	if err == nil {
		t.Fatal("expected an unknown-command usage error")
	}
	if ExitCodeForError(err) != ExitUsage {
		t.Fatalf("exit = %d, want usage", ExitCodeForError(err))
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("unknown-command error echoed the raw token: %q", err.Error())
	}
}

func TestAllowInsecureBasicRequiresAllowBasic(t *testing.T) {
	deps, _ := credsDependencies("s3cret")
	_, stderr, err := runCommand(
		deps,
		"info", "camera.invalid",
		"--user", "admin",
		"--password-env", "OVM_PASS",
		"--allow-insecure-basic",
	)
	if err == nil {
		t.Fatal("expected --allow-insecure-basic without --allow-basic to fail")
	}
	if ExitCodeForError(err) != ExitUsage {
		t.Fatalf("exit = %d, want usage", ExitCodeForError(err))
	}
	if strings.Contains(stderr, "warning:") {
		t.Fatalf("invalid policy must not emit a warning: %q", stderr)
	}
}

func TestAllowInsecureBasicIsAnIndependentWarningAndPolicy(t *testing.T) {
	deps, stub := credsDependencies("s3cret")
	stdout, stderr, err := runCommand(
		deps,
		"info", "camera.invalid",
		"--user", "admin",
		"--password-env", "OVM_PASS",
		"--allow-basic",
		"--allow-insecure-basic",
		"--json",
	)
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if !stub.captured.Credentials.AllowBasic() {
		t.Fatal("--allow-basic did not reach the application policy")
	}
	if !stub.captured.Credentials.AllowBasicOverHTTP() {
		t.Fatal("--allow-insecure-basic did not reach the application policy")
	}
	if n := strings.Count(stderr, "HTTP Basic authentication over plaintext HTTP is enabled"); n != 1 {
		t.Fatalf("expected one plaintext Basic warning, got %d: %q", n, stderr)
	}
	if strings.Contains(stdout, "warning:") {
		t.Fatalf("warning must not appear on JSON stdout: %q", stdout)
	}
}

func TestAllowBasicAloneDoesNotEnablePlaintextTransport(t *testing.T) {
	deps, stub := credsDependencies("s3cret")
	_, _, err := runCommand(
		deps,
		"info", "https://camera.invalid/onvif/device_service",
		"--user", "admin",
		"--password-env", "OVM_PASS",
		"--allow-basic",
	)
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	if !stub.captured.Credentials.AllowBasic() {
		t.Fatal("Basic should be enabled")
	}
	if stub.captured.Credentials.AllowBasicOverHTTP() {
		t.Fatal("Basic HTTPS opt-in must not enable plaintext HTTP")
	}
}
