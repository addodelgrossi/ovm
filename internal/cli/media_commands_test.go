package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/buildinfo"
)

type stubMediaProfiles struct {
	result   app.MediaProfilesResult
	err      error
	captured *app.MediaProfilesRequest
	call     func(context.Context, app.MediaProfilesRequest) (app.MediaProfilesResult, error)
}

func (stub *stubMediaProfiles) Profiles(ctx context.Context, request app.MediaProfilesRequest) (app.MediaProfilesResult, error) {
	stub.captured = &request
	if stub.call != nil {
		return stub.call(ctx, request)
	}
	return stub.result, stub.err
}

type stubSnapshot struct {
	result   app.SnapshotMetadata
	err      error
	captured *app.SnapshotRequest
	call     func(context.Context, app.SnapshotRequest) (app.SnapshotMetadata, error)
}

func (stub *stubSnapshot) Snapshot(ctx context.Context, request app.SnapshotRequest) (app.SnapshotMetadata, error) {
	stub.captured = &request
	if stub.call != nil {
		return stub.call(ctx, request)
	}
	return stub.result, stub.err
}

//nolint:gosec // All values are project-authored synthetic media fixtures, not credentials.
func sampleProfilesResult() app.MediaProfilesResult {
	return app.MediaProfilesResult{
		Media2Available: true,
		Media1Available: true,
		Profiles: []app.MediaProfileDTO{{
			Token:                   "profile_main",
			Name:                    "main",
			Fixed:                   true,
			HasFixed:                true,
			Source:                  app.MediaServiceMedia2,
			VideoSourceConfigToken:  "vsrccfg0",
			VideoSourceToken:        "vsrc0",
			VideoEncoderConfigToken: "venc_main",
			Video: &app.VideoConfigDTO{
				ConfigToken: "venc_main", Codec: "H265", Width: 1920, Height: 1080,
				FrameRateFPS: 30, HasFrameRate: true, BitrateKbps: 4096, GOPLength: 30, Profile: "Main",
			},
			Audio:    &app.AudioConfigDTO{ConfigToken: "aenc0", Codec: "PCMU", BitrateKbps: 64, SampleRateKHz: 8},
			Stream:   &app.StreamURIDTO{TCPURI: "rtsp://camera.invalid/stream", UDPURI: "rtsp://camera.invalid/stream"},
			Snapshot: &app.SnapshotURIDTO{URI: "http://camera.invalid/snapshot"},
			Operations: []app.MediaOperationStatus{
				{Operation: app.MediaOpProfiles, State: app.CapabilitySupported, Source: app.MediaServiceMedia2},
				{Operation: app.MediaOpVideoSource, State: app.CapabilitySupported, Source: app.MediaServiceMedia1},
				{Operation: app.MediaOpVideoEncoder, State: app.CapabilitySupported, Source: app.MediaServiceMedia2},
				{Operation: app.MediaOpAudioEncoder, State: app.CapabilitySupported, Source: app.MediaServiceMedia2},
				{Operation: app.MediaOpStreamTCP, State: app.CapabilitySupported, Source: app.MediaServiceMedia2},
				{Operation: app.MediaOpStreamUDP, State: app.CapabilitySupported, Source: app.MediaServiceMedia2},
				{Operation: app.MediaOpSnapshotURI, State: app.CapabilitySupported, Source: app.MediaServiceMedia2},
			},
		}},
	}
}

func TestProfilesCredentialAndFlagPlumbing(t *testing.T) {
	const secret = "profiles-secret-marker"
	stub := &stubMediaProfiles{result: app.MediaProfilesResult{Profiles: []app.MediaProfileDTO{}}}
	deps := testDependencies()
	deps.MediaProfiles = stub
	deps.LookupEnv = func(name string) (string, bool) { return secret, name == "OVM_PASSWORD" }

	stdout, stderr, err := runCommand(
		deps, "profiles", "camera.invalid", "--user", "operator", "--password-env", "OVM_PASSWORD",
		"--allow-basic", "--allow-insecure-basic", "--allow-legacy-digest", "--timeout", "7s", "--json",
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stub.captured == nil || stub.captured.Host != "camera.invalid" || stub.captured.Timeout != 7*time.Second {
		t.Fatalf("captured request = %+v", stub.captured)
	}
	credentials := stub.captured.Credentials
	if credentials == nil || credentials.Username() != "operator" || !credentials.AllowBasic() || !credentials.AllowBasicOverHTTP() || !credentials.AllowLegacyDigest() {
		t.Fatalf("credential policy was not preserved: %v", credentials)
	}
	if strings.Count(stderr, "warning:") != 3 {
		t.Fatalf("stderr warnings = %q, want 3", stderr)
	}
	if strings.Contains(stdout+stderr, secret) {
		t.Fatal("output leaked the environment-supplied password")
	}
	if !strings.Contains(stdout, `"schema_version":1`) || !strings.Contains(stdout, `"kind":"profiles"`) {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestProfilesDefaultTimeoutAndNoCredentials(t *testing.T) {
	stub := &stubMediaProfiles{result: app.MediaProfilesResult{Profiles: []app.MediaProfileDTO{}}}
	deps := testDependencies()
	deps.MediaProfiles = stub
	if _, _, err := runCommand(deps, "profiles", "camera.invalid"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stub.captured.Timeout != defaultMediaCommandTimeout || stub.captured.Credentials != nil {
		t.Fatalf("captured request = %+v", stub.captured)
	}
}

func TestProfilesValidationFailureUsesRawJSONIntent(t *testing.T) {
	t.Parallel()
	cases := [][]string{
		{"profiles", "--unknown", "--json"},
		{"profiles", "camera.invalid", "--timeout", "not-a-duration", "--json"},
		{"profiles", "--json", "camera.invalid", "extra"},
		{"profiles", "camera.invalid", "--user", "--json"},
	}
	for _, args := range cases {
		stdout, _, err := runCommand(testDependencies(), args...)
		if code := ExitCodeForError(err); code != ExitUsage {
			t.Fatalf("args %v exit = %d, want %d", args, code, ExitUsage)
		}
		// In the final case --json is the value consumed by --user, not intent.
		if args[len(args)-2] == "--user" {
			if stdout != "" {
				t.Fatalf("args %v incorrectly treated a flag value as JSON intent: %q", args, stdout)
			}
			continue
		}
		assertFailureEnvelope(t, stdout, "profiles", app.CategoryUsage)
	}
}

func TestProfilesFailureCategoriesAndPartialCancellation(t *testing.T) {
	for _, category := range []app.ErrorCategory{
		app.CategoryUsage, app.CategoryNetwork, app.CategoryAuthentication,
		app.CategoryProtocol, app.CategoryCancelled, app.CategoryInternal,
	} {
		category := category
		t.Run(string(category), func(t *testing.T) {
			result := sampleProfilesResult()
			stub := &stubMediaProfiles{result: result, err: app.NewError(category, "safe failure")}
			deps := testDependencies()
			deps.MediaProfiles = stub
			stdout, _, err := runCommand(deps, "profiles", "camera.invalid", "--json")
			if got := ExitCodeForError(err); got != expectedExitForCategory(category) {
				t.Fatalf("exit = %d, want %d", got, expectedExitForCategory(category))
			}
			assertFailureEnvelope(t, stdout, "profiles", category)
			if !strings.Contains(stdout, "profile_main") {
				t.Fatal("failure envelope discarded the partial profile result")
			}
		})
	}
}

func TestProfilesHumanIncludesRequiredFieldsAndSanitizes(t *testing.T) {
	result := sampleProfilesResult()
	result.Profiles[0].Name = "main\nforged"
	result.Profiles = append(result.Profiles, app.MediaProfileDTO{
		Token: "profile_sub", Name: "sub", Source: app.MediaServiceMedia1,
	})
	stub := &stubMediaProfiles{result: result}
	deps := testDependencies()
	deps.MediaProfiles = stub
	stdout, stderr, err := runCommand(deps, "profiles", "camera.invalid")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	for _, value := range []string{
		"profile_main", "profile 2:", "profile_sub", "source: media2", "codec: H265", "resolution: 1920x1080", "frame rate: 30 fps",
		"bitrate: 4096 kbps", "gop length: 30", "rtsp://camera.invalid/stream", "http://camera.invalid/snapshot",
		"video_source: supported (source=media1, fallback=false)",
	} {
		if !strings.Contains(stdout, value) {
			t.Fatalf("stdout missing %q: %s", value, stdout)
		}
	}
	if strings.Contains(stdout, "main\nforged") || stderr != "" {
		t.Fatalf("unsafe or unexpected output: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestSnapshotRequiresExplicitOutputAndValidFlags(t *testing.T) {
	t.Parallel()
	cases := [][]string{
		{"snapshot", "camera.invalid", "--json"},
		{"snapshot", "camera.invalid", "--output=", "--json"},
		{"snapshot", "camera.invalid", "-o", "a.jpg", "--profile=", "--json"},
		{"snapshot", "camera.invalid", "-o", "a.jpg", "--timeout", "0s", "--json"},
		{"snapshot", "camera.invalid", "--unknown", "--json"},
		{"snapshot", "camera.invalid", "-o", "--json", "--unknown"},
	}
	for _, args := range cases {
		stdout, _, err := runCommand(testDependencies(), args...)
		if code := ExitCodeForError(err); code != ExitUsage {
			t.Fatalf("args %v exit = %d, want %d", args, code, ExitUsage)
		}
		// The final --json is consumed as -o's value, so raw intent is false.
		if len(args) >= 4 && args[2] == "-o" && args[3] == "--json" {
			if stdout != "" {
				t.Fatalf("args %v incorrectly treated output value as JSON intent: %q", args, stdout)
			}
			continue
		}
		assertFailureEnvelope(t, stdout, "snapshot", app.CategoryUsage)
	}
}

func TestSnapshotInvalidExplicitProfileIsUsageBeforeUseCase(t *testing.T) {
	t.Parallel()

	invalid := []string{
		"",
		"   ",
		string([]byte{0xff}),
		"profile\x00main",
		"profile\u0085main",
		"profile\ufffdmain",
		strings.Repeat("界", 65),
	}
	for _, jsonMode := range []bool{false, true} {
		mode := "human"
		if jsonMode {
			mode = "json"
		}
		t.Run(mode, func(t *testing.T) {
			for index, token := range invalid {
				stub := &stubSnapshot{}
				deps := testDependencies()
				deps.Snapshot = stub
				args := []string{"snapshot", "camera.invalid", "--profile", token, "-o", "snapshot.jpg"}
				if jsonMode {
					args = append(args, "--json")
				}
				stdout, stderr, err := runCommand(deps, args...)
				if code := ExitCodeForError(err); code != ExitUsage {
					t.Fatalf("case %d exit = %d, want %d", index, code, ExitUsage)
				}
				if stub.captured != nil {
					t.Fatalf("case %d reached the snapshot use case: %+v", index, stub.captured)
				}
				if err == nil || err.Error() != "the explicit profile token is not valid" {
					t.Fatalf("case %d error = %v", index, err)
				}
				if stderr != "" {
					t.Fatalf("case %d stderr = %q, want empty", index, stderr)
				}
				if jsonMode {
					assertFailureEnvelope(t, stdout, "snapshot", app.CategoryUsage)
				} else if stdout != "" {
					t.Fatalf("case %d human stdout = %q, want empty", index, stdout)
				}
			}
		})
	}
}

func TestSnapshotXMLSpecialProfileReachesUseCaseUnchanged(t *testing.T) {
	t.Parallel()
	const token = `profile<&>"'` //nolint:gosec // Synthetic profile identifier, not a credential.
	stub := &stubSnapshot{result: app.SnapshotMetadata{
		ProfileToken: token, OutputPath: "/tmp/snapshot.jpg", ByteSize: 1, ContentType: "image/jpeg", Source: app.MediaServiceMedia2,
	}}
	deps := testDependencies()
	deps.Snapshot = stub
	if _, _, err := runCommand(deps, "snapshot", "camera.invalid", "--profile", token, "-o", "snapshot.jpg"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stub.captured == nil || stub.captured.ProfileToken != token {
		t.Fatalf("captured request = %+v, want unchanged XML-special token", stub.captured)
	}
}

func TestSnapshotFlagPlumbingAndMetadataOnlyOutput(t *testing.T) {
	const secret = "snapshot-secret-marker"
	output := filepath.Join(t.TempDir(), "snap.jpg")
	stub := &stubSnapshot{result: app.SnapshotMetadata{
		ProfileToken: "profile_sub", OutputPath: output, ByteSize: 321, ContentType: "image/jpeg", Source: app.MediaServiceMedia1,
	}}
	deps := testDependencies()
	deps.Snapshot = stub
	deps.LookupEnv = func(name string) (string, bool) { return secret, name == "OVM_PASSWORD" }

	stdout, stderr, err := runCommand(
		deps, "snapshot", "camera.invalid", "--profile", "profile_sub", "-o", output, "--force", "--timeout", "9s",
		"--user", "operator", "--password-env", "OVM_PASSWORD", "--allow-basic", "--allow-insecure-basic", "--json",
	)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	request := stub.captured
	if request == nil || request.ProfileToken != "profile_sub" || request.OutputPath != output || !request.Overwrite || request.Timeout != 9*time.Second {
		t.Fatalf("captured request = %+v", request)
	}
	if request.Credentials == nil || !request.Credentials.AllowBasicOverHTTP() {
		t.Fatalf("credentials = %v", request.Credentials)
	}
	if strings.Contains(stdout+stderr, secret) || strings.Contains(stdout, "base64") || strings.Contains(stdout, "uri") {
		t.Fatalf("snapshot output leaked bytes, URI, or secret: stdout=%q stderr=%q", stdout, stderr)
	}
	var envelope snapshotEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	if envelope.SchemaVersion != 1 || envelope.Kind != "snapshot" || envelope.Result.ByteSize != 321 {
		t.Fatalf("envelope = %+v", envelope)
	}
}

func TestSnapshotOmittedProfileReachesUseCase(t *testing.T) {
	stub := &stubSnapshot{result: app.SnapshotMetadata{
		ProfileToken: "profile_main", OutputPath: "/tmp/snap.jpg", ByteSize: 4, ContentType: "image/jpeg", Source: app.MediaServiceMedia2,
	}}
	deps := testDependencies()
	deps.Snapshot = stub
	if _, _, err := runCommand(deps, "snapshot", "camera.invalid", "-o", "snap.jpg"); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if stub.captured.ProfileToken != "" || stub.captured.Overwrite {
		t.Fatalf("captured request = %+v", stub.captured)
	}
}

func TestSnapshotFailureEnvelopeAndExitCategories(t *testing.T) {
	for _, category := range []app.ErrorCategory{
		app.CategoryUsage, app.CategoryNetwork, app.CategoryAuthentication,
		app.CategoryProtocol, app.CategoryCancelled, app.CategoryInternal,
	} {
		stub := &stubSnapshot{err: app.NewError(category, "safe failure")}
		deps := testDependencies()
		deps.Snapshot = stub
		stdout, _, err := runCommand(deps, "snapshot", "camera.invalid", "-o", "snap.jpg", "--json")
		if got := ExitCodeForError(err); got != expectedExitForCategory(category) {
			t.Fatalf("category %s exit = %d, want %d", category, got, expectedExitForCategory(category))
		}
		assertFailureEnvelope(t, stdout, "snapshot", category)
	}
}

func TestM2CommandsPropagateWriterErrors(t *testing.T) {
	if err := writeProfilesHuman(&failingWriter{}, sampleProfilesResult()); err == nil {
		t.Fatal("profiles human writer error did not propagate")
	}
	if err := writeProfilesJSON(&failingWriter{}, sampleProfilesResult()); err == nil {
		t.Fatal("profiles JSON writer error did not propagate")
	}
	metadata := app.SnapshotMetadata{ProfileToken: "p", OutputPath: "/tmp/p.jpg", ContentType: "image/jpeg", Source: app.MediaServiceMedia2}
	if err := writeSnapshotHuman(&failingWriter{}, metadata); err == nil {
		t.Fatal("snapshot human writer error did not propagate")
	}
	if err := writeSnapshotJSON(&failingWriter{}, metadata); err == nil {
		t.Fatal("snapshot JSON writer error did not propagate")
	}
}

func TestM2CancellationUsesCommandContext(t *testing.T) {
	stub := &stubMediaProfiles{call: func(ctx context.Context, _ app.MediaProfilesRequest) (app.MediaProfilesResult, error) {
		<-ctx.Done()
		return app.MediaProfilesResult{Profiles: []app.MediaProfileDTO{}}, app.NewError(app.CategoryCancelled, "cancelled")
	}}
	deps := testDependencies()
	deps.MediaProfiles = stub
	command := NewRootCommand(buildinfo.Info{Version: "test"}, deps, []string{"profiles", "camera.invalid", "--json"})
	var stdout, stderr bytes.Buffer
	command.SetOut(&stdout)
	command.SetErr(&stderr)
	command.SetArgs([]string{"profiles", "camera.invalid", "--json"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := command.ExecuteContext(ctx)
	if ExitCodeForError(err) != ExitCancelled {
		t.Fatalf("exit = %d, want %d", ExitCodeForError(err), ExitCancelled)
	}
	assertFailureEnvelope(t, stdout.String(), "profiles", app.CategoryCancelled)
}

func assertFailureEnvelope(t *testing.T, stdout, kind string, category app.ErrorCategory) {
	t.Helper()
	var envelope struct {
		SchemaVersion int    `json:"schema_version"`
		Kind          string `json:"kind"`
		Error         struct {
			Category app.ErrorCategory `json:"category"`
			Message  string            `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("decode failure envelope: %v; stdout=%q", err, stdout)
	}
	if envelope.SchemaVersion != 1 || envelope.Kind != kind || envelope.Error.Category != category || envelope.Error.Message == "" {
		t.Fatalf("failure envelope = %+v", envelope)
	}
}

func expectedExitForCategory(category app.ErrorCategory) int {
	switch category {
	case app.CategoryUsage:
		return ExitUsage
	case app.CategoryNetwork:
		return ExitNetwork
	case app.CategoryAuthentication:
		return ExitAuthentication
	case app.CategoryProtocol:
		return ExitProtocol
	case app.CategoryCancelled:
		return ExitCancelled
	default:
		return ExitGeneric
	}
}

func TestM2FlagErrorsNeverEchoMalformedValues(t *testing.T) {
	t.Parallel()
	const marker = "https://user:credential-marker@camera.invalid/path?token=operational-query-marker" //nolint:gosec // Synthetic leak probe.
	cases := []struct {
		name     string
		args     []string
		wantJSON bool
	}{
		{
			name:     "profiles duration with later JSON intent",
			args:     []string{"profiles", "camera.invalid", "--timeout", marker, "--json"},
			wantJSON: true,
		},
		{
			name:     "snapshot duration with later JSON intent",
			args:     []string{"snapshot", "camera.invalid", "--timeout", marker, "-o", "snap.jpg", "--json"},
			wantJSON: true,
		},
		{
			name: "profiles malformed JSON boolean",
			args: []string{"profiles", "camera.invalid", "--json=" + marker},
		},
		{
			name: "snapshot malformed JSON boolean",
			args: []string{"snapshot", "camera.invalid", "-o", "snap.jpg", "--json=" + marker},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			stdout, stderr, err := runCommand(testDependencies(), test.args...)
			if ExitCodeForError(err) != ExitUsage {
				t.Fatalf("exit = %d, want usage", ExitCodeForError(err))
			}
			if strings.Contains(stdout, marker) || strings.Contains(stderr, marker) || strings.Contains(errorString(err), marker) {
				t.Fatalf("malformed flag value leaked: stdout=%q stderr=%q err=%v", stdout, stderr, err)
			}
			if test.wantJSON {
				kind := "profiles"
				if test.args[0] == "snapshot" {
					kind = "snapshot"
				}
				assertFailureEnvelope(t, stdout, kind, app.CategoryUsage)
			} else if stdout != "" {
				t.Fatalf("invalid --json assignment must not request JSON output: %q", stdout)
			}
		})
	}
}
