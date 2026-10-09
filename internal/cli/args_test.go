package cli

import "testing"

func TestInfoMissingArgIsUsageExit(t *testing.T) {
	deps := testDependencies()
	_, _, err := runCommand(deps, "info")
	if err == nil {
		t.Fatal("expected a usage error when the host argument is missing")
	}
	if code := ExitCodeForError(err); code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
}

func TestUnknownFlagIsUsageExit(t *testing.T) {
	deps := testDependencies()
	_, _, err := runCommand(deps, "discover", "--nope")
	if err == nil {
		t.Fatal("expected a usage error for an unknown flag")
	}
	if code := ExitCodeForError(err); code != ExitUsage {
		t.Fatalf("exit code = %d, want %d", code, ExitUsage)
	}
}
