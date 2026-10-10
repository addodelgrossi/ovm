package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

// fakeSetup is the result of starting a same-origin fake ONVIF device: the
// device handle (for runtime fault/unsupported configuration) and the canonical
// device-service endpoint used as the camera host.
type fakeSetup struct {
	device *fakedevice.Device
	host   string
}

// newUnstartedFake starts a fake ONVIF device whose GetServices advertises
// same-origin service endpoints. The config is built from the bound server
// origin so the application's same-origin validation accepts every advertised
// XAddr, mirroring a real camera that advertises its own origin.
func newUnstartedFake(t *testing.T, build func(base string) fakedevice.Config) fakeSetup {
	t.Helper()

	server := httptest.NewUnstartedServer(nil)
	base := "http://" + server.Listener.Addr().String()

	device, err := fakedevice.New(build(base))
	if err != nil {
		t.Fatalf("fakedevice.New() error = %v", err)
	}
	server.Config.Handler = device
	server.Start()
	t.Cleanup(server.Close)

	return fakeSetup{device: device, host: base + "/onvif/device_service"}
}

// newUnstartedFakeWithHandler is like newUnstartedFake but lets a test wrap the
// fake device in a custom http.Handler (for example an observer that records
// wire-level requests) built from the bound origin and the device handle.
func newUnstartedFakeWithHandler(t *testing.T, wrap func(base string, device *fakedevice.Device) http.Handler, build func(base string) fakedevice.Config) fakeSetup {
	t.Helper()

	server := httptest.NewUnstartedServer(nil)
	base := "http://" + server.Listener.Addr().String()

	device, err := fakedevice.New(build(base))
	if err != nil {
		t.Fatalf("fakedevice.New() error = %v", err)
	}
	server.Config.Handler = wrap(base, device)
	server.Start()
	t.Cleanup(server.Close)

	return fakeSetup{device: device, host: base + "/onvif/device_service"}
}

// mediaFake starts an unauthenticated fake with Device, Media1, and optionally
// Media2 same-origin services.
func mediaFake(t *testing.T, media2 bool) (*fakedevice.Device, string) {
	t.Helper()
	setup := newUnstartedFake(t, func(base string) fakedevice.Config {
		services := []fakedevice.ServiceData{
			{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
			{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: base + "/onvif/media_service", VersionMajor: 25, VersionMinor: 6},
		}
		if media2 {
			services = append(services, fakedevice.ServiceData{
				Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6,
			})
		}
		return fakedevice.Config{Inventory: fakedevice.Inventory{Services: services}}
	})
	return setup.device, setup.host
}

// TestMediaProfilesFullMedia2 proves that with a Media2 service advertised,
// every operation is answered by Media2, provenance records media2, no
// operation reports a fallback, and every normalized field is populated. It
// also asserts the deterministic operation order and that profiles are sorted
// by token.
func TestMediaProfilesFullMedia2(t *testing.T) {
	t.Parallel()
	_, host := mediaFake(t, true)

	uc := NewProductionMediaProfiles()
	result, err := uc.Profiles(context.Background(), MediaProfilesRequest{Host: host})
	if err != nil {
		t.Fatalf("Profiles() error = %v", err)
	}
	if !result.Media2Available || !result.Media1Available {
		t.Fatalf("availability = (m2 %v, m1 %v), want both true", result.Media2Available, result.Media1Available)
	}
	if len(result.Profiles) != 2 {
		t.Fatalf("profiles = %d, want 2", len(result.Profiles))
	}
	if result.Profiles[0].Token != "profile_main" || result.Profiles[1].Token != "profile_sub" {
		t.Fatalf("profile order = [%q %q], want sorted by token", result.Profiles[0].Token, result.Profiles[1].Token)
	}

	main := result.Profiles[0]
	// Identity and associations.
	if main.Name != "main" || !main.Fixed || !main.HasFixed {
		t.Fatalf("identity = {name:%q fixed:%v has:%v}, want main/true/true", main.Name, main.Fixed, main.HasFixed)
	}
	if main.Source != MediaServiceMedia2 {
		t.Fatalf("source = %q, want media2", main.Source)
	}
	if main.VideoSourceConfigToken != "vsrccfg0" || main.VideoSourceToken != "vsrc0" || main.VideoEncoderConfigToken != "venc_main" {
		t.Fatalf("associations = {vscfg:%q vs:%q venc:%q}", main.VideoSourceConfigToken, main.VideoSourceToken, main.VideoEncoderConfigToken)
	}
	// Video encoder summary (Media2 venc_main: H265 1920x1080 fps30 bitrate4096 gop30 profile Main).
	if main.Video == nil {
		t.Fatalf("video summary missing")
	}
	if main.Video.Codec != "H265" || main.Video.Width != 1920 || main.Video.Height != 1080 {
		t.Fatalf("video codec/resolution = {%q %dx%d}", main.Video.Codec, main.Video.Width, main.Video.Height)
	}
	if !main.Video.HasFrameRate || main.Video.FrameRateFPS != 30 {
		t.Fatalf("video fps = {has:%v %v}, want 30", main.Video.HasFrameRate, main.Video.FrameRateFPS)
	}
	if main.Video.BitrateKbps != 4096 || main.Video.GOPLength != 30 || main.Video.Profile != "Main" {
		t.Fatalf("video rate/gop/profile = {%d %d %q}", main.Video.BitrateKbps, main.Video.GOPLength, main.Video.Profile)
	}
	// Audio encoder summary (Media2 aenc0: PCMU 64 kbps 8 kHz).
	if main.Audio == nil || main.Audio.Codec != "PCMU" || main.Audio.BitrateKbps != 64 || main.Audio.SampleRateKHz != 8 {
		t.Fatalf("audio summary = %+v", main.Audio)
	}
	// Stream URIs keep the camera's query; the snapshot URI is query-stripped.
	if main.Stream == nil || main.Stream.TCPURI == "" || main.Stream.UDPURI == "" {
		t.Fatalf("stream URIs missing: %+v", main.Stream)
	}
	if !strings.Contains(main.Stream.TCPURI, "?profile=profile_main") || !strings.Contains(main.Stream.UDPURI, "?profile=profile_main") {
		t.Fatalf("stream URIs lost the camera query: %+v", main.Stream)
	}
	if main.Snapshot == nil || main.Snapshot.URI == "" {
		t.Fatalf("snapshot URI missing")
	}
	assertNoQuery(t, main.Snapshot.URI)

	// Operation order and provenance: every op is Media2, none fell back.
	assertOperationOrder(t, main)
	for _, s := range main.Operations {
		if s.State != CapabilitySupported {
			t.Fatalf("op %q state = %q, want supported", s.Operation, s.State)
		}
		if s.FellBack {
			t.Fatalf("op %q reported a fallback with full Media2", s.Operation)
		}
		// video_source has no Media2 equivalent and is answered by Media1
		// directly (not a fallback). Every other op must be Media2.
		if s.Operation == MediaOpVideoSource {
			if s.Source != MediaServiceMedia1 {
				t.Fatalf("video_source source = %q, want media1 (direct, no Media2 equivalent)", s.Source)
			}
			continue
		}
		if s.Source != MediaServiceMedia2 {
			t.Fatalf("op %q source = %q, want media2", s.Operation, s.Source)
		}
	}
}

// TestMediaProfilesNoMedia2 proves that without a Media2 service advertised,
// Media1 is used directly for every operation (no fallback), every field is
// still populated, and no operation reports fell_back=true.
func TestMediaProfilesNoMedia2(t *testing.T) {
	t.Parallel()
	_, host := mediaFake(t, false)

	uc := NewProductionMediaProfiles()
	result, err := uc.Profiles(context.Background(), MediaProfilesRequest{Host: host})
	if err != nil {
		t.Fatalf("Profiles() error = %v", err)
	}
	if result.Media2Available {
		t.Fatalf("Media2Available = true, want false")
	}
	if !result.Media1Available {
		t.Fatalf("Media1Available = false, want true")
	}
	if len(result.Profiles) != 2 {
		t.Fatalf("profiles = %d, want 2", len(result.Profiles))
	}
	main := result.Profiles[0]
	if main.Source != MediaServiceMedia1 {
		t.Fatalf("source = %q, want media1", main.Source)
	}
	// Media1 venc_main is H264 1920x1080 fps30 bitrate4096 gop30 profile Main.
	if main.Video == nil || main.Video.Codec != "H264" || main.Video.Width != 1920 || main.Video.Height != 1080 {
		t.Fatalf("video summary = %+v", main.Video)
	}
	if !main.Video.HasFrameRate || main.Video.FrameRateFPS != 30 || main.Video.BitrateKbps != 4096 || main.Video.GOPLength != 30 {
		t.Fatalf("video rate detail = %+v", main.Video)
	}
	if main.Audio == nil || main.Audio.Codec != "G711" || main.Audio.BitrateKbps != 64 || main.Audio.SampleRateKHz != 8 {
		t.Fatalf("audio summary = %+v", main.Audio)
	}
	if main.Stream == nil || main.Stream.TCPURI == "" || main.Stream.UDPURI == "" {
		t.Fatalf("stream URIs missing: %+v", main.Stream)
	}
	if main.Snapshot == nil || main.Snapshot.URI == "" {
		t.Fatalf("snapshot URI missing")
	}
	assertOperationOrder(t, main)
	for _, s := range main.Operations {
		if s.State != CapabilitySupported {
			t.Fatalf("op %q state = %q, want supported", s.Operation, s.State)
		}
		if s.Source != MediaServiceMedia1 {
			t.Fatalf("op %q source = %q, want media1", s.Operation, s.Source)
		}
		if s.FellBack {
			t.Fatalf("op %q reported fell_back=true; direct Media1 is not a fallback", s.Operation)
		}
	}
}

// TestMediaProfilesPartialMedia2 proves that when selected Media2 operations
// return ActionNotSupported, ONLY those operations fall back to Media1 with
// source=media1 and fell_back=true, while every other operation remains Media2
// and does not report a fallback.
func TestMediaProfilesPartialMedia2(t *testing.T) {
	t.Parallel()
	device, host := mediaFake(t, true)

	// Mark the Media2 snapshot and TCP stream operations unsupported.
	device.SetUnsupported("http://www.onvif.org/ver20/media/wsdl/GetSnapshotUri", true)
	device.SetUnsupported("http://www.onvif.org/ver20/media/wsdl/GetStreamUri", true)

	uc := NewProductionMediaProfiles()
	result, err := uc.Profiles(context.Background(), MediaProfilesRequest{Host: host})
	if err != nil {
		t.Fatalf("Profiles() error = %v", err)
	}
	for _, p := range result.Profiles {
		if p.Source != MediaServiceMedia2 {
			t.Fatalf("profile %q source = %q, want media2", p.Token, p.Source)
		}
		snap := opStatus(t, p, MediaOpSnapshotURI)
		if snap.Source != MediaServiceMedia1 || !snap.FellBack {
			t.Fatalf("snapshot status = %+v, want media1 fell_back", snap)
		}
		tcp := opStatus(t, p, MediaOpStreamTCP)
		if tcp.Source != MediaServiceMedia1 || !tcp.FellBack {
			t.Fatalf("stream_tcp status = %+v, want media1 fell_back", tcp)
		}
		// GetStreamUri is a single Media2 action, so marking it unsupported
		// makes BOTH stream_tcp and stream_udp fall back to Media1. The
		// video/audio encoder operations remain Media2 and do not fall back.
		udp := opStatus(t, p, MediaOpStreamUDP)
		if udp.Source != MediaServiceMedia1 || !udp.FellBack {
			t.Fatalf("stream_udp status = %+v, want media1 fell_back", udp)
		}
		venc := opStatus(t, p, MediaOpVideoEncoder)
		if venc.Source != MediaServiceMedia2 || venc.FellBack {
			t.Fatalf("video_encoder status = %+v, want media2 no-fallback", venc)
		}
		if p.Snapshot == nil || p.Snapshot.URI == "" {
			t.Fatalf("profile %q snapshot should have fallen back to Media1", p.Token)
		}
	}
}

// TestMediaProfilesNoFallbackOnNotAuthorized proves that a Media2 NotAuthorized
// fault does NOT trigger a Media1 fallback for that operation.
func TestMediaProfilesNoFallbackOnNotAuthorized(t *testing.T) {
	t.Parallel()
	device, host := mediaFake(t, true)

	if err := device.SetFault("http://www.onvif.org/ver20/media/wsdl/GetSnapshotUri", fakedevice.FaultSpec{
		Kind:   fakedevice.FaultNotAuthorized,
		Reason: "not authorized",
	}); err != nil {
		t.Fatalf("SetFault() error = %v", err)
	}

	uc := NewProductionMediaProfiles()
	result, err := uc.Profiles(context.Background(), MediaProfilesRequest{Host: host})
	if err != nil {
		t.Fatalf("Profiles() error = %v", err)
	}
	for _, p := range result.Profiles {
		status := opStatus(t, p, MediaOpSnapshotURI)
		if status.Source == MediaServiceMedia1 || status.FellBack {
			t.Fatalf("profile %q snapshot fell back on NotAuthorized; fallback must not occur: %+v", p.Token, status)
		}
		if status.State == CapabilitySupported {
			t.Fatalf("profile %q snapshot reported supported despite NotAuthorized", p.Token)
		}
		if p.Snapshot != nil {
			t.Fatalf("profile %q must not expose a snapshot URI after NotAuthorized", p.Token)
		}
	}
}

func assertOperationOrder(t *testing.T, p MediaProfileDTO) {
	t.Helper()
	want := []MediaOperationName{
		MediaOpProfiles, MediaOpVideoSource, MediaOpVideoEncoder, MediaOpAudioEncoder,
		MediaOpStreamTCP, MediaOpStreamUDP, MediaOpSnapshotURI,
	}
	if len(p.Operations) != len(want) {
		t.Fatalf("profile %q op count = %d, want %d (%+v)", p.Token, len(p.Operations), len(want), p.Operations)
	}
	for i, op := range want {
		if p.Operations[i].Operation != op {
			t.Fatalf("profile %q op[%d] = %q, want %q", p.Token, i, p.Operations[i].Operation, op)
		}
	}
}

func opStatus(t *testing.T, p MediaProfileDTO, op MediaOperationName) MediaOperationStatus {
	t.Helper()
	for _, s := range p.Operations {
		if s.Operation == op {
			return s
		}
	}
	t.Fatalf("profile %q has no status for op %q", p.Token, op)
	return MediaOperationStatus{}
}

// TestMediaProfilesNoAdvertisedMediaIsProtocol proves a contacted camera that
// advertises no Media service is a device-response failure, not caller usage.
func TestMediaProfilesNoAdvertisedMediaIsProtocol(t *testing.T) {
	t.Parallel()
	setup := newUnstartedFake(t, func(base string) fakedevice.Config {
		return fakedevice.Config{Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{{
			Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6,
		}}}}
	})
	result, err := NewProductionMediaProfiles().Profiles(context.Background(), MediaProfilesRequest{Host: setup.host})
	if err == nil || CategoryOf(err) != CategoryProtocol {
		t.Fatalf("result=%+v err=%v category=%q, want protocol", result, err, CategoryOf(err))
	}
}
