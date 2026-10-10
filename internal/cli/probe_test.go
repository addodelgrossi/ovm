package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/app"
)

type stubProbe struct {
	result   app.ProbeResult
	err      error
	captured *app.ProbeRequest
}

func (stub *stubProbe) Probe(_ context.Context, request app.ProbeRequest) (app.ProbeResult, error) {
	stub.captured = &request
	return stub.result, stub.err
}

func sampleProbeResult() app.ProbeResult {
	return app.ProbeResult{
		ProfileToken: "profile_main", Transport: "tcp", Codec: "h264", Width: 1920, Height: 1080,
		FramesPerSecond: 25, BitrateKbps: 4096.4, GOPFrames: 50, GOPSeconds: 2,
		TimeToFirstFrameMillis: 412, Frames: 750, Keyframes: 15, LostPackets: 3,
		DurationMillis: 30010, Complete: true,
	}
}

func TestProbeFlagPlumbingAndJSONEnvelope(t *testing.T) {
	const secret = "probe-secret-marker"
	stub := &stubProbe{result: sampleProbeResult()}
	deps := testDependencies()
	deps.Probe = stub
	deps.LookupEnv = func(name string) (string, bool) { return secret, name == "OVM_PASSWORD" }

	stdout, stderr, err := runCommand(deps, "probe", "camera.invalid", "--user", "operator", "--password-env", "OVM_PASSWORD",
		"--profile", "profile_sub", "--transport", "udp", "--duration", "45s", "--timeout", "7s", "--json")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	request := stub.captured
	if request == nil || request.Host != "camera.invalid" || request.ProfileToken != "profile_sub" ||
		request.Transport != app.StreamTransportUDP || request.Duration != 45*time.Second || request.Timeout != 7*time.Second {
		t.Fatalf("captured request = %+v", request)
	}
	if request.Credentials == nil || request.Credentials.Username() != "operator" {
		t.Fatal("credentials were not supplied")
	}
	if strings.Contains(stdout+stderr, secret) {
		t.Fatal("output leaked the environment-supplied password")
	}
	var envelope struct {
		SchemaVersion int             `json:"schema_version"`
		Kind          string          `json:"kind"`
		Result        app.ProbeResult `json:"result"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("decode: %v; stdout=%q", err, stdout)
	}
	if envelope.SchemaVersion != 1 || envelope.Kind != "probe" || envelope.Result != sampleProbeResult() {
		t.Fatalf("envelope = %+v", envelope)
	}
}

func TestProbeHumanOutputAndDefaults(t *testing.T) {
	stub := &stubProbe{result: sampleProbeResult()}
	deps := testDependencies()
	deps.Probe = stub
	stdout, _, err := runCommand(deps, "probe", "camera.invalid")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stub.captured.Duration != app.DefaultProbeDuration || stub.captured.Transport != app.StreamTransportTCP || stub.captured.ProfileToken != "" {
		t.Fatalf("defaults = %+v", stub.captured)
	}
	for _, want := range []string{"codec: h264", "resolution: 1920x1080", "frame rate: 25.00 fps", "bitrate: 4096 kbps", "gop: 50 frames (2.00 s)", "time to first frame: 412 ms", "lost packets: 3", "complete: true"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("human output missing %q:\n%s", want, stdout)
		}
	}
}

func TestProbeValidationAndFailureEnvelopes(t *testing.T) {
	for _, args := range [][]string{
		{"probe", "camera.invalid", "--duration", "500ms", "--json"},
		{"probe", "camera.invalid", "--duration", "11m", "--json"},
		{"probe", "camera.invalid", "--transport", "sctp", "--json"},
		{"probe", "camera.invalid", "--profile", "bad\x01token", "--json"},
		{"probe", "--json"},
	} {
		stub := &stubProbe{}
		deps := testDependencies()
		deps.Probe = stub
		stdout, _, err := runCommand(deps, args...)
		if ExitCodeForError(err) != ExitUsage || stub.captured != nil {
			t.Fatalf("%v: exit = %d, use case called = %v", args, ExitCodeForError(err), stub.captured != nil)
		}
		assertFailureEnvelope(t, stdout, "probe", app.CategoryUsage)
	}
	for _, category := range []app.ErrorCategory{app.CategoryNetwork, app.CategoryAuthentication, app.CategoryProtocol, app.CategoryCancelled} {
		partial := sampleProbeResult()
		partial.Complete = false
		stub := &stubProbe{result: partial, err: app.NewError(category, "safe failure")}
		deps := testDependencies()
		deps.Probe = stub
		stdout, _, err := runCommand(deps, "probe", "camera.invalid", "--json")
		if ExitCodeForError(err) != expectedExitForCategory(category) {
			t.Fatalf("category %s exit = %d", category, ExitCodeForError(err))
		}
		assertFailureEnvelope(t, stdout, "probe", category)
		if !strings.Contains(stdout, `"frames":750`) {
			t.Fatalf("failure envelope dropped the partial measurement: %s", stdout)
		}
	}
}
