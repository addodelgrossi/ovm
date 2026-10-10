package cli

import (
	"bytes"
	"encoding/json"
	"image/jpeg"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

type cliFakeSetup struct {
	device *fakedevice.Device
	host   string
}

func newCLIFake(t *testing.T, build func(base string) fakedevice.Config) cliFakeSetup {
	t.Helper()
	server := httptest.NewUnstartedServer(nil)
	base := "http://" + server.Listener.Addr().String()
	device, err := fakedevice.New(build(base))
	if err != nil {
		t.Fatalf("fakedevice.New: %v", err)
	}
	server.Config.Handler = device
	server.Start()
	t.Cleanup(server.Close)
	return cliFakeSetup{device: device, host: base + "/onvif/device_service"}
}

func cliFakeConfig(base string, withMedia2 bool) fakedevice.Config {
	services := []fakedevice.ServiceData{
		{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
		{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: base + "/onvif/media_service", VersionMajor: 25, VersionMinor: 6},
	}
	if withMedia2 {
		services = append(services, fakedevice.ServiceData{
			Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6,
		})
	}
	return fakedevice.Config{
		Inventory:      fakedevice.Inventory{Services: services},
		MediaInventory: fakedevice.MediaInventory{SnapshotQuery: "sig=operational-query-marker"},
	}
}

func productionM2Dependencies() Dependencies {
	deps := testDependencies()
	deps.MediaProfiles = app.NewProductionMediaProfiles()
	deps.Snapshot = app.NewProductionSnapshot()
	return deps
}

func TestProfilesCLIEndToEndMediaServices(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		media2     bool
		partial    bool
		wantSource app.MediaSourceService
	}{
		{name: "media2", media2: true, wantSource: app.MediaServiceMedia2},
		{name: "media1", media2: false, wantSource: app.MediaServiceMedia1},
		{name: "partial_fallback", media2: true, partial: true, wantSource: app.MediaServiceMedia2},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			setup := newCLIFake(t, func(base string) fakedevice.Config { return cliFakeConfig(base, test.media2) })
			if test.partial {
				setup.device.SetUnsupported(fakedevice.ActionMedia2GetVideoEncoderConfigurations, true)
			}

			stdout, stderr, err := runCommand(productionM2Dependencies(), "profiles", setup.host, "--json")
			if err != nil {
				t.Fatalf("profiles execute: %v (stderr=%q)", err, stderr)
			}
			var envelope profilesEnvelope
			if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
				t.Fatalf("decode profiles JSON: %v; stdout=%q", err, stdout)
			}
			if envelope.SchemaVersion != 1 || envelope.Kind != "profiles" || len(envelope.Result.Profiles) != 2 {
				t.Fatalf("envelope = %+v", envelope)
			}
			if envelope.Result.Profiles[0].Token != "profile_main" || envelope.Result.Profiles[1].Token != "profile_sub" {
				t.Fatalf("profile order = %+v", envelope.Result.Profiles)
			}
			if envelope.Result.Profiles[0].Source != test.wantSource {
				t.Fatalf("source = %q, want %q", envelope.Result.Profiles[0].Source, test.wantSource)
			}
			if strings.Contains(stdout, "operational-query-marker") || strings.Contains(stdout, "?profile=") {
				t.Fatalf("profiles output leaked an operational query: %s", stdout)
			}
			if test.partial {
				status := findMediaStatus(envelope.Result.Profiles[0], app.MediaOpVideoEncoder)
				if status == nil || !status.FellBack || status.Source != app.MediaServiceMedia1 {
					t.Fatalf("video fallback status = %+v", status)
				}
			}

			// A second run against the same deterministic fake must be byte-stable.
			stdout2, stderr2, err2 := runCommand(productionM2Dependencies(), "profiles", setup.host, "--json")
			if err2 != nil || stderr2 != "" || stdout2 != stdout {
				t.Fatalf("profiles JSON was not stable: err=%v stderr=%q\nfirst=%q\nsecond=%q", err2, stderr2, stdout, stdout2)
			}
		})
	}
}

func TestProfilesCLIBasicPolicyAndSecretNonEcho(t *testing.T) {
	t.Parallel()
	const secret = "full-fake-basic-secret-marker" //nolint:gosec // Synthetic test credential.
	setup := newCLIFake(t, func(base string) fakedevice.Config {
		config := cliFakeConfig(base, true)
		config.HTTPAuth = fakedevice.HTTPAuthBasic
		config.RequireUsernameToken = true
		config.Username = "operator"
		config.Password = secret
		return config
	})
	deps := productionM2Dependencies()
	deps.LookupEnv = func(name string) (string, bool) { return secret, name == "OVM_PASSWORD" }

	stdout, stderr, err := runCommand(deps, "profiles", setup.host, "--user", "operator", "--password-env", "OVM_PASSWORD", "--json")
	if ExitCodeForError(err) != ExitAuthentication {
		t.Fatalf("secure-default exit = %d, want authentication; err=%v", ExitCodeForError(err), err)
	}
	assertFailureEnvelope(t, stdout, "profiles", app.CategoryAuthentication)
	if strings.Contains(stdout+stderr+errorString(err), secret) {
		t.Fatal("secure-default failure leaked the password")
	}

	stdout, stderr, err = runCommand(
		deps, "profiles", setup.host, "--user", "operator", "--password-env", "OVM_PASSWORD",
		"--allow-basic", "--allow-insecure-basic", "--json",
	)
	if err != nil {
		t.Fatalf("opted-in Basic profiles: %v (stderr=%q)", err, stderr)
	}
	if strings.Count(stderr, "warning:") != 2 || strings.Contains(stdout+stderr, secret) {
		t.Fatalf("warnings or secret handling failed: stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.Contains(stdout, `"kind":"profiles"`) {
		t.Fatalf("stdout = %q", stdout)
	}
}

func TestSnapshotCLIEndToEndAtomicNoClobberAndForce(t *testing.T) {
	t.Parallel()
	setup := newCLIFake(t, func(base string) fakedevice.Config { return cliFakeConfig(base, true) })
	deps := productionM2Dependencies()
	dir := t.TempDir()
	output := filepath.Join(dir, "snapshot.jpg")

	stdout, stderr, err := runCommand(deps, "snapshot", setup.host, "-o", output, "--json")
	if err != nil {
		t.Fatalf("snapshot execute: %v (stderr=%q)", err, stderr)
	}
	var envelope snapshotEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("decode snapshot JSON: %v; stdout=%q", err, stdout)
	}
	if envelope.SchemaVersion != 1 || envelope.Kind != "snapshot" || envelope.Result.ProfileToken != "profile_main" || envelope.Result.ByteSize == 0 {
		t.Fatalf("envelope = %+v", envelope)
	}
	if strings.Contains(stdout, "operational-query-marker") || strings.Contains(stdout, "?profile=") {
		t.Fatalf("snapshot metadata leaked an operational query: %s", stdout)
	}
	data, readErr := os.ReadFile(output) //nolint:gosec // test-owned path.
	if readErr != nil {
		t.Fatalf("read snapshot: %v", readErr)
	}
	assertDecodableJPEG(t, data)
	info, statErr := os.Stat(output)
	if statErr != nil {
		t.Fatalf("stat snapshot: %v", statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %v, want 0600", info.Mode().Perm())
	}

	const original = "existing-file-must-survive"
	if err := os.WriteFile(output, []byte(original), 0o600); err != nil {
		t.Fatalf("replace test seed: %v", err)
	}
	stdout, _, err = runCommand(deps, "snapshot", setup.host, "-o", output, "--json")
	if ExitCodeForError(err) != ExitUsage {
		t.Fatalf("no-force exit = %d, want usage; err=%v", ExitCodeForError(err), err)
	}
	assertFailureEnvelope(t, stdout, "snapshot", app.CategoryUsage)
	data, _ = os.ReadFile(output) //nolint:gosec // test-owned path.
	if string(data) != original {
		t.Fatalf("no-force invocation changed existing output: %q", data)
	}

	_, stderr, err = runCommand(deps, "snapshot", setup.host, "-o", output, "--force", "--json")
	if err != nil {
		t.Fatalf("forced snapshot: %v (stderr=%q)", err, stderr)
	}
	data, readErr = os.ReadFile(output) //nolint:gosec // test-owned path.
	if readErr != nil {
		t.Fatalf("read forced snapshot: %v", readErr)
	}
	if string(data) == original {
		t.Fatal("forced snapshot did not replace the original destination")
	}
	assertDecodableJPEG(t, data)
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".snapshot-") {
			t.Fatalf("leftover temporary file: %s", entry.Name())
		}
	}
}

func assertDecodableJPEG(t *testing.T, data []byte) {
	t.Helper()
	decoded, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil || decoded.Bounds().Dx() <= 0 || decoded.Bounds().Dy() <= 0 {
		t.Fatalf("snapshot is not a complete JPEG: %v", err)
	}
}

func findMediaStatus(profile app.MediaProfileDTO, operation app.MediaOperationName) *app.MediaOperationStatus {
	for index := range profile.Operations {
		if profile.Operations[index].Operation == operation {
			return &profile.Operations[index]
		}
	}
	return nil
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
