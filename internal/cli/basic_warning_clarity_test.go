package cli

import (
	"strings"
	"testing"
)

// TestBasicWarningExplainsHTTPSRestriction proves the ordinary Basic opt-in is
// described as HTTPS-only and never claims that it also enabled plaintext HTTP.
func TestBasicWarningExplainsHTTPSRestriction(t *testing.T) {
	const secret = "SUPER-SECRET-PASS"
	deps, _ := credsDependencies(secret)
	_, stderr, err := runCommand(deps, "info", "https://secret-host.invalid", "--user", "secret-admin", "--password-env", "OVM_PASS", "--allow-basic")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	lower := strings.ToLower(stderr)
	if !strings.Contains(lower, "https endpoints") {
		t.Fatalf("Basic warning must explain its HTTPS restriction, got %q", stderr)
	}
	if strings.Contains(lower, "plaintext http is enabled") {
		t.Fatalf("HTTPS-only Basic warning claimed plaintext was enabled: %q", stderr)
	}
	for _, leak := range []string{secret, "secret-admin", "secret-host.invalid", "OVM_PASS"} {
		if strings.Contains(stderr, leak) {
			t.Fatalf("Basic warning leaked %q: %q", leak, stderr)
		}
	}
}

// TestBasicAndLegacyWarningsAreSeparate proves the Basic and legacy no-qop
// Digest warnings are distinct lines, so enabling both yields two notices.
func TestBasicAndLegacyWarningsAreSeparate(t *testing.T) {
	deps, _ := credsDependencies("s3cret")
	_, stderr, err := runCommand(deps, "info", "https://camera.invalid", "--user", "admin", "--password-env", "OVM_PASS", "--allow-basic", "--allow-legacy-digest")
	if err != nil {
		t.Fatalf("execute error = %v", err)
	}
	lines := 0
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		if strings.HasPrefix(line, "warning:") {
			lines++
		}
	}
	if lines != 2 {
		t.Fatalf("expected two distinct warning lines, got %d: %q", lines, stderr)
	}
	if !strings.Contains(stderr, "separate downgrade") {
		t.Fatalf("legacy warning should note it is separate from Basic, got %q", stderr)
	}
}
