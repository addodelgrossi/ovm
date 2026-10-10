package cli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/addodelgrossi/ovm/internal/app"
)

func TestCLIGoldenDiscoverCancelledPartialJSON(t *testing.T) {
	result := app.DiscoveryResult{Devices: []app.DiscoveredDevice{{
		EndpointReference: "urn:uuid:camera-a",
		Addresses:         []string{"http://192.0.2.10/onvif/device_service"},
	}}}
	deps := testDependencies()
	deps.Discovery = stubDiscovery{
		result: result,
		err:    app.NewError(app.CategoryCancelled, "discovery was cancelled"),
	}

	stdout, stderr, err := runCommand(deps, "discover", "--json")
	if ExitCodeForError(err) != ExitCancelled {
		t.Fatalf("exit code = %d, want %d", ExitCodeForError(err), ExitCancelled)
	}
	const want = "{\"schema_version\":1,\"kind\":\"discovery\",\"result\":{\"devices\":[{\"endpoint_reference\":\"urn:uuid:camera-a\",\"addresses\":[\"http://192.0.2.10/onvif/device_service\"]}]},\"error\":{\"category\":\"cancelled\",\"message\":\"the operation was cancelled\"}}\n"
	if stdout != want {
		t.Fatalf("stdout golden mismatch\n got: %q\nwant: %q", stdout, want)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestCLIGoldenInfoUsefulPartialHuman(t *testing.T) {
	deps := testDependencies()
	deps.Inventory = &stubInventory{result: app.Inventory{
		Endpoint: "http://camera.invalid/onvif/device_service",
		Operations: []app.OperationStatus{
			{Operation: app.OpHostname, State: app.CapabilitySupported},
		},
		Hostname: &app.HostnameDTO{Name: "camera-1"},
	}}

	stdout, stderr, err := runCommand(deps, "info", "camera.invalid")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	const want = "endpoint: http://camera.invalid/onvif/device_service\noperations:\n  hostname               supported\nhostname: camera-1 (from DHCP: false)\n"
	if stdout != want {
		t.Fatalf("stdout golden mismatch\n got: %q\nwant: %q", stdout, want)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
}

func TestCLIGoldenExitCodeJoinedCancellation(t *testing.T) {
	err := app.NewError(app.CategoryAuthentication, "competing authentication failure")
	if got := ExitCodeForError(errorsJoin(err, context.Canceled)); got != ExitCancelled {
		t.Fatalf("exit code = %d, want %d", got, ExitCancelled)
	}
}

// errorsJoin is a tiny local seam so the exact golden test remains readable.
func errorsJoin(values ...error) error {
	return errors.Join(values...)
}

func TestCLIGoldenProfilesHuman(t *testing.T) {
	var stdout bytes.Buffer
	if err := writeProfilesHuman(&stdout, sampleProfilesResult()); err != nil {
		t.Fatalf("write profiles: %v", err)
	}
	const want = "media2 available: true\n" +
		"media1 available: true\n" +
		"profiles:\n" +
		"profile 1:\n" +
		"  token: profile_main\n" +
		"  name: main\n" +
		"  fixed: true\n" +
		"  source: media2\n" +
		"  video source config token: vsrccfg0\n" +
		"  video source token: vsrc0\n" +
		"  video encoder config token: venc_main\n" +
		"  video:\n" +
		"    config token: venc_main\n" +
		"    codec: H265\n" +
		"    resolution: 1920x1080\n" +
		"    frame rate: 30 fps\n" +
		"    bitrate: 4096 kbps\n" +
		"    gop length: 30\n" +
		"    codec profile: Main\n" +
		"  audio:\n" +
		"    config token: aenc0\n" +
		"    codec: PCMU\n" +
		"    bitrate: 64 kbps\n" +
		"    sample rate: 8 kHz\n" +
		"  stream tcp uri: rtsp://camera.invalid/stream\n" +
		"  stream udp uri: rtsp://camera.invalid/stream\n" +
		"  snapshot uri: http://camera.invalid/snapshot\n" +
		"  operations:\n" +
		"    profiles: supported (source=media2, fallback=false)\n" +
		"    video_source: supported (source=media1, fallback=false)\n" +
		"    video_encoder: supported (source=media2, fallback=false)\n" +
		"    audio_encoder: supported (source=media2, fallback=false)\n" +
		"    stream_tcp: supported (source=media2, fallback=false)\n" +
		"    stream_udp: supported (source=media2, fallback=false)\n" +
		"    snapshot_uri: supported (source=media2, fallback=false)\n"
	if stdout.String() != want {
		t.Fatalf("stdout golden mismatch\n got: %q\nwant: %q", stdout.String(), want)
	}
}

func TestCLIGoldenSnapshotJSONAndHuman(t *testing.T) {
	metadata := app.SnapshotMetadata{
		ProfileToken: "profile_main",
		OutputPath:   "/tmp/snapshot.jpg",
		ByteSize:     4,
		ContentType:  "image/jpeg",
		Source:       app.MediaServiceMedia2,
	}
	var jsonOutput bytes.Buffer
	if err := writeSnapshotJSON(&jsonOutput, metadata); err != nil {
		t.Fatalf("write snapshot JSON: %v", err)
	}
	const wantJSON = "{\"schema_version\":1,\"kind\":\"snapshot\",\"result\":{\"profile_token\":\"profile_main\",\"output_path\":\"/tmp/snapshot.jpg\",\"byte_size\":4,\"content_type\":\"image/jpeg\",\"source\":\"media2\"}}\n"
	if jsonOutput.String() != wantJSON {
		t.Fatalf("JSON golden mismatch\n got: %q\nwant: %q", jsonOutput.String(), wantJSON)
	}

	var humanOutput bytes.Buffer
	if err := writeSnapshotHuman(&humanOutput, metadata); err != nil {
		t.Fatalf("write snapshot human: %v", err)
	}
	const wantHuman = "profile token: profile_main\noutput path: /tmp/snapshot.jpg\nbyte size: 4\ncontent type: image/jpeg\nsource: media2\n"
	if humanOutput.String() != wantHuman {
		t.Fatalf("human golden mismatch\n got: %q\nwant: %q", humanOutput.String(), wantHuman)
	}
}
