package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestProcessExitAndSinglePrint builds the ovm binary and runs it as a real
// subprocess to validate process-level behavior: the documented exit codes and
// that a failing command prints its safe error exactly once to stderr. Only
// network-free commands are exercised (version success and an unknown command
// usage failure), so the test is hermetic.
func TestProcessExitAndSinglePrint(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess build in -short mode")
	}

	bin := buildProcessBinary(t)

	cases := []struct {
		name     string
		args     []string
		wantExit int
		stderr   func(string) bool
	}{
		{
			name:     "version_success_exit0",
			args:     []string{"version"},
			wantExit: 0,
			stderr:   func(s string) bool { return s == "" },
		},
		{
			name:     "unknown_command_usage_exit2",
			args:     []string{"definitely-not-a-command"},
			wantExit: 2,
			// Exactly one non-empty error line, printed once.
			stderr: func(s string) bool {
				trimmed := strings.TrimRight(s, "\n")
				return trimmed != "" && !strings.Contains(trimmed, "\n")
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// G204: bin is the just-built test binary and args are fixed
			// in-test literals; no external input is involved.
			cmd := exec.Command(bin, tc.args...) //nolint:gosec // test-only fixed command
			var stderr strings.Builder
			cmd.Stderr = &stderr
			err := cmd.Run()
			exit := 0
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					exit = exitErr.ExitCode()
				} else {
					t.Fatalf("run ovm: %v", err)
				}
			}
			if exit != tc.wantExit {
				t.Fatalf("exit = %d, want %d (stderr: %q)", exit, tc.wantExit, stderr.String())
			}
			if !tc.stderr(stderr.String()) {
				t.Fatalf("unexpected stderr: %q", stderr.String())
			}
		})
	}
}

func buildProcessBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "ovm")
	// G204: the build command and arguments are fixed in-test; the output path
	// is a test-owned temporary directory.
	build := exec.Command("go", "build", "-o", bin, ".") //nolint:gosec // test-only fixed command
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build ovm: %v\n%s", err, out)
	}
	return bin
}

func TestProcessInterruptReturnsPartialAndExit130(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess signal test in -short mode")
	}

	started := make(chan struct{})
	release := make(chan struct{})
	var startedOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		startedOnce.Do(func() { close(started) })
		select {
		case <-request.Context().Done():
		case <-release:
		}
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	bin := buildProcessBinary(t)
	// G204: bin is the just-built test binary and every argument is controlled
	// by this test; no shell or untrusted input is involved.
	cmd := exec.Command(bin, "info", server.URL+"/onvif/device_service", "--timeout", "30s") //nolint:gosec // test-only command
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start ovm: %v", err)
	}

	select {
	case <-started:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal("ovm did not start an inventory request")
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatalf("interrupt ovm: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var runErr error
	select {
	case runErr = <-done:
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		runErr = <-done
		t.Fatalf("ovm did not exit after interrupt: %v", runErr)
	}
	var exitErr *exec.ExitError
	if !errors.As(runErr, &exitErr) || exitErr.ExitCode() != 130 {
		t.Fatalf("run error = %v, want process exit 130 (stderr=%q)", runErr, stderr.String())
	}
	if count := strings.Count(stderr.String(), "the operation was cancelled"); count != 1 {
		t.Fatalf("cancellation message count = %d, want 1; stderr=%q", count, stderr.String())
	}
	if !strings.Contains(stdout.String(), "operations:") {
		t.Fatalf("partial inventory states were not rendered before exit: %q", stdout.String())
	}
}
