package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
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

// processFake starts the complete synthetic Device/Media/JPEG handler with
// same-origin service advertisements suitable for real-binary CLI tests.
type processFake struct {
	device *fakedevice.Device
	host   string
}

func startProcessFake(t *testing.T, withMedia2, authenticated bool) processFake {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	base := "http://" + server.Listener.Addr().String()
	services := []fakedevice.ServiceData{
		{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
		{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: base + "/onvif/media_service", VersionMajor: 25, VersionMinor: 6},
	}
	if withMedia2 {
		services = append(services, fakedevice.ServiceData{
			Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6,
		})
	}
	config := fakedevice.Config{
		Inventory:      fakedevice.Inventory{Services: services},
		MediaInventory: fakedevice.MediaInventory{SnapshotQuery: "sig=process-operational-query"},
	}
	if authenticated {
		config.HTTPAuth = fakedevice.HTTPAuthDigest
		config.RequireUsernameToken = true
		config.Username = "operator"
		config.Password = "process-secret-marker"
	}
	device, err := fakedevice.New(config)
	if err != nil {
		t.Fatalf("fakedevice.New: %v", err)
	}
	server.Config.Handler = device
	server.Start()
	t.Cleanup(server.Close)
	return processFake{device: device, host: base + "/onvif/device_service"}
}

func runProcess(t *testing.T, bin string, env []string, args ...string) (string, string, int) {
	t.Helper()
	//nolint:gosec // bin is a test-built binary and all arguments are test-controlled.
	command := exec.Command(bin, args...)
	command.Env = append(os.Environ(), env...)
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	exit := 0
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run ovm: %v", err)
		}
		exit = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), exit
}

func TestProcessM2ProfilesAndSnapshotAgainstFullFake(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess build in -short mode")
	}
	bin := buildProcessBinary(t)
	authenticated := startProcessFake(t, true, true)
	env := []string{"OVM_PROCESS_PASSWORD=process-secret-marker"}
	authFlags := []string{"--user", "operator", "--password-env", "OVM_PROCESS_PASSWORD"}

	args := append([]string{"profiles", authenticated.host, "--json"}, authFlags...)
	stdout, stderr, exit := runProcess(t, bin, env, args...)
	if exit != 0 || stderr != "" {
		t.Fatalf("Media2 profiles exit=%d stderr=%q stdout=%q", exit, stderr, stdout)
	}
	if strings.Contains(stdout+stderr, "process-secret-marker") || strings.Contains(stdout, "process-operational-query") {
		t.Fatalf("Media2 process output leaked secret/query: stdout=%q stderr=%q", stdout, stderr)
	}
	var profilesEnvelope struct {
		SchemaVersion int    `json:"schema_version"`
		Kind          string `json:"kind"`
		Result        struct {
			Media2Available bool `json:"media2_available"`
			Profiles        []struct {
				Token      string `json:"token"`
				Operations []struct {
					Operation string `json:"operation"`
					Source    string `json:"source"`
					FellBack  bool   `json:"fell_back"`
				} `json:"operations"`
			} `json:"profiles"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &profilesEnvelope); err != nil || profilesEnvelope.SchemaVersion != 1 || profilesEnvelope.Kind != "profiles" || !profilesEnvelope.Result.Media2Available || len(profilesEnvelope.Result.Profiles) != 2 {
		t.Fatalf("Media2 envelope = %+v, err=%v", profilesEnvelope, err)
	}

	// The same process path exposes a narrow, per-operation Media1 fallback.
	authenticated.device.SetUnsupported(fakedevice.ActionMedia2GetVideoEncoderConfigurations, true)
	stdout, stderr, exit = runProcess(t, bin, env, args...)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, `"operation":"video_encoder","state":"supported","source":"media1","fell_back":true`) {
		t.Fatalf("partial fallback exit=%d stderr=%q stdout=%q", exit, stderr, stdout)
	}

	// A separate full fake with Media1 only proves direct provenance and no
	// fallback at the real process boundary.
	media1Only := startProcessFake(t, false, false)
	stdout, stderr, exit = runProcess(t, bin, nil, "profiles", media1Only.host, "--json")
	if exit != 0 || stderr != "" || !strings.Contains(stdout, `"media2_available":false`) || !strings.Contains(stdout, `"source":"media1"`) || strings.Contains(stdout, `"fell_back":true`) {
		t.Fatalf("Media1 profiles exit=%d stderr=%q stdout=%q", exit, stderr, stdout)
	}

	output := filepath.Join(t.TempDir(), "snapshot.jpg")
	snapshotArgs := append([]string{"snapshot", authenticated.host, "-o", output, "--json"}, authFlags...)
	stdout, stderr, exit = runProcess(t, bin, env, snapshotArgs...)
	if exit != 0 || stderr != "" {
		t.Fatalf("snapshot exit=%d stderr=%q stdout=%q", exit, stderr, stdout)
	}
	if strings.Contains(stdout, "process-operational-query") || !strings.Contains(stdout, `"profile_token":"profile_main"`) {
		t.Fatalf("snapshot metadata unsafe or missing default selection: %q", stdout)
	}
	data, err := os.ReadFile(output) //nolint:gosec // test-owned path.
	if err != nil || len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		t.Fatalf("snapshot output=%x err=%v", data, err)
	}

	const original = "process-existing-output"
	if err := os.WriteFile(output, []byte(original), 0o600); err != nil {
		t.Fatalf("seed existing output: %v", err)
	}
	stdout, stderr, exit = runProcess(t, bin, env, snapshotArgs...)
	if exit != 2 || !strings.Contains(stdout, `"category":"usage"`) || strings.Count(strings.TrimSpace(stderr), "\n") != 0 {
		t.Fatalf("no-force exit=%d stderr=%q stdout=%q", exit, stderr, stdout)
	}
	data, _ = os.ReadFile(output) //nolint:gosec // test-owned path.
	if string(data) != original {
		t.Fatalf("no-force process changed existing output: %q", data)
	}

	forceArgs := append(snapshotArgs, "--force")
	stdout, stderr, exit = runProcess(t, bin, env, forceArgs...)
	if exit != 0 || stderr != "" || !strings.Contains(stdout, `"kind":"snapshot"`) {
		t.Fatalf("force exit=%d stderr=%q stdout=%q", exit, stderr, stdout)
	}
	data, err = os.ReadFile(output) //nolint:gosec // test-owned path.
	if err != nil || string(data) == original || len(data) < 2 || data[0] != 0xff || data[1] != 0xd8 {
		t.Fatalf("forced process output=%x err=%v", data, err)
	}
}

func TestProcessProfilesInterruptAgainstFullFake(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess signal test in -short mode")
	}
	server := httptest.NewUnstartedServer(nil)
	base := "http://" + server.Listener.Addr().String()
	device, err := fakedevice.New(fakedevice.Config{Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
		{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
		{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: base + "/onvif/media_service", VersionMajor: 25, VersionMinor: 6},
		{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6},
	}}})
	if err != nil {
		t.Fatalf("fakedevice.New: %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var postCount atomic.Int32
	server.Config.Handler = http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost && postCount.Add(1) >= 2 {
			once.Do(func() { close(started) })
			select {
			case <-request.Context().Done():
			case <-release:
			}
			return
		}
		device.ServeHTTP(response, request)
	})
	server.Start()
	defer server.Close()
	defer close(release)

	bin := buildProcessBinary(t)
	//nolint:gosec // bin and arguments are controlled by this test.
	command := exec.Command(bin, "profiles", base+"/onvif/device_service", "--timeout", "30s", "--json")
	var stdout, stderr strings.Builder
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		t.Fatalf("start profiles: %v", err)
	}
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatal("profiles did not reach the blocking full fake request")
	}
	if err := command.Process.Signal(os.Interrupt); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		t.Fatalf("interrupt profiles: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- command.Wait() }()
	select {
	case err = <-done:
	case <-time.After(10 * time.Second):
		_ = command.Process.Kill()
		err = <-done
		t.Fatalf("profiles did not exit after interrupt: %v", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 130 {
		t.Fatalf("run error=%v stderr=%q stdout=%q, want exit 130", err, stderr.String(), stdout.String())
	}
	var envelope struct {
		Kind  string `json:"kind"`
		Error struct {
			Category string `json:"category"`
		} `json:"error"`
	}
	if decodeErr := json.Unmarshal([]byte(stdout.String()), &envelope); decodeErr != nil || envelope.Kind != "profiles" || envelope.Error.Category != "cancelled" {
		t.Fatalf("cancel envelope=%+v decodeErr=%v stdout=%q", envelope, decodeErr, stdout.String())
	}
	if lines := strings.Split(strings.TrimSpace(stderr.String()), "\n"); len(lines) != 1 || !strings.Contains(lines[0], "cancel") {
		t.Fatalf("cancellation stderr was not one safe line: %q", stderr.String())
	}
}

func TestProcessM2MalformedFlagValuesNeverEcho(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess build in -short mode")
	}
	bin := buildProcessBinary(t)
	const marker = "https://user:credential-marker@camera.invalid/path?token=operational-query-marker" //nolint:gosec // Synthetic leak probe.
	cases := []struct {
		name     string
		args     []string
		wantKind string
	}{
		{
			name:     "profiles duration",
			args:     []string{"profiles", "camera.invalid", "--timeout", marker, "--json"},
			wantKind: "profiles",
		},
		{
			name:     "snapshot duration",
			args:     []string{"snapshot", "camera.invalid", "--timeout", marker, "-o", "snap.jpg", "--json"},
			wantKind: "snapshot",
		},
		{
			name: "profiles JSON boolean",
			args: []string{"profiles", "camera.invalid", "--json=" + marker},
		},
		{
			name: "snapshot JSON boolean",
			args: []string{"snapshot", "camera.invalid", "-o", "snap.jpg", "--json=" + marker},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, exit := runProcess(t, bin, nil, test.args...)
			if exit != 2 {
				t.Fatalf("exit=%d, want 2; stdout=%q stderr=%q", exit, stdout, stderr)
			}
			if strings.Contains(stdout, marker) || strings.Contains(stderr, marker) {
				t.Fatalf("malformed value leaked: stdout=%q stderr=%q", stdout, stderr)
			}
			if lines := strings.Split(strings.TrimSpace(stderr), "\n"); len(lines) != 1 || lines[0] == "" {
				t.Fatalf("stderr was not one fixed safe line: %q", stderr)
			}
			if test.wantKind == "" {
				if stdout != "" {
					t.Fatalf("invalid JSON boolean unexpectedly enabled JSON: %q", stdout)
				}
				return
			}
			var envelope struct {
				Kind  string `json:"kind"`
				Error struct {
					Category string `json:"category"`
				} `json:"error"`
			}
			if err := json.Unmarshal([]byte(stdout), &envelope); err != nil || envelope.Kind != test.wantKind || envelope.Error.Category != "usage" {
				t.Fatalf("envelope=%+v err=%v stdout=%q", envelope, err, stdout)
			}
		})
	}
}

func TestProcessProfilesNoMediaServiceExitsProtocol(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess build in -short mode")
	}
	server := httptest.NewUnstartedServer(nil)
	base := "http://" + server.Listener.Addr().String()
	device, err := fakedevice.New(fakedevice.Config{Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{{
		Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6,
	}}}})
	if err != nil {
		t.Fatalf("fakedevice.New: %v", err)
	}
	server.Config.Handler = device
	server.Start()
	defer server.Close()

	bin := buildProcessBinary(t)
	stdout, stderr, exit := runProcess(t, bin, nil, "profiles", base+"/onvif/device_service", "--json")
	if exit != 5 || !strings.Contains(stdout, `"kind":"profiles"`) || !strings.Contains(stdout, `"category":"protocol"`) {
		t.Fatalf("exit=%d stdout=%q stderr=%q, want protocol envelope and exit 5", exit, stdout, stderr)
	}
	if lines := strings.Split(strings.TrimSpace(stderr), "\n"); len(lines) != 1 || lines[0] == "" {
		t.Fatalf("stderr was not one safe line: %q", stderr)
	}
}
