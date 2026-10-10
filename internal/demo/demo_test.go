package demo

import (
	"context"
	"encoding/json"
	"net"
	"net/url"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/pkg/stream"
)

// newTestEnvironment builds a demo environment and registers an idempotent
// cleanup. It isolates HOME and the XDG config directory to empty temp
// directories so that any accidental attempt to read or write production state
// (a disk store or a keyring fallback) would be observable rather than silently
// touching the developer's real configuration.
func newTestEnvironment(t *testing.T) *Environment {
	t.Helper()
	isolateConfigHome(t)

	env, err := NewEnvironment(context.Background())
	if err != nil {
		t.Fatalf("NewEnvironment() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = env.Close(ctx)
	})
	return env
}

// isolateConfigHome points HOME and XDG_CONFIG_HOME at fresh temp directories
// for the duration of the test. The demo must never read or write these, so
// the test both asserts that behavior via the public API and structurally
// removes access to the real locations.
func isolateConfigHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	config := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", config)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// TestBootstrapOverviewProfilesEndToEnd drives Bootstrap, Overview, and
// Profiles end-to-end through the production adapters against the loopback fake
// camera, asserting the demo label, the real same-origin URLs, resolved
// profiles, and the absence of any secret or credential-reference leakage.
func TestBootstrapOverviewProfilesEndToEnd(t *testing.T) {
	env := newTestEnvironment(t)
	manager := env.Manager()
	runtimeView := env.Runtime()

	ctx := context.Background()

	// Bootstrap: the single demo device is present and clearly labeled.
	bootstrap, err := manager.Bootstrap(ctx)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if len(bootstrap.Devices) != 1 {
		t.Fatalf("Bootstrap devices = %d, want 1", len(bootstrap.Devices))
	}
	device := bootstrap.Devices[0]
	if device.DisplayName != DemoCameraLabel {
		t.Fatalf("display name = %q, want %q", device.DisplayName, DemoCameraLabel)
	}
	if !device.HasCredentials {
		t.Fatalf("device.HasCredentials = false, want true")
	}
	if device.ID != env.DeviceID() {
		t.Fatalf("device ID = %q, want %q", device.ID, env.DeviceID())
	}

	// The endpoint must be the real same-origin Device service URL on an IPv4
	// loopback host.
	if device.Endpoint != runtimeView.DeviceServiceURL {
		t.Fatalf("endpoint = %q, want %q", device.Endpoint, runtimeView.DeviceServiceURL)
	}
	assertLoopbackHTTP(t, device.Endpoint)
	assertSameOrigin(t, runtimeView)

	// Overview: identical catalog content.
	overview, err := manager.Overview(ctx)
	if err != nil {
		t.Fatalf("Overview() error = %v", err)
	}
	if len(overview.Devices) != 1 || overview.Devices[0].ID != device.ID {
		t.Fatalf("Overview devices = %+v, want the single demo device", overview.Devices)
	}

	// Profiles: resolved end-to-end through the production media adapter and
	// the authenticated loopback fake camera.
	result, err := manager.Profiles(ctx, device.ID)
	if err != nil {
		t.Fatalf("Profiles() error = %v", err)
	}
	if !result.Media1Available || !result.Media2Available {
		t.Fatalf("availability = (m1 %v, m2 %v), want both true", result.Media1Available, result.Media2Available)
	}
	if len(result.Profiles) == 0 {
		t.Fatalf("Profiles returned no profiles")
	}

	// No secret material or credential reference may appear anywhere in the
	// boundary views.
	assertNoSecretLeak(t, bootstrap, overview, result, runtimeView)
}

// TestCapabilitiesAdvertiseSyntheticStreamingOnly asserts that the demo
// advertises its synthetic RTSP server and preview but never VLC.
func TestCapabilitiesAdvertiseSyntheticStreamingOnly(t *testing.T) {
	env := newTestEnvironment(t)
	caps := env.Capabilities()
	if !caps.RTSPServerAvailable || !caps.PlaybackAvailable {
		t.Errorf("capabilities = %+v, want synthetic RTSP and playback", caps)
	}
	if caps.VLCAvailable {
		t.Errorf("VLCAvailable = true, want false")
	}
	if env.Runtime().Capabilities != caps {
		t.Errorf("Runtime capabilities differ from Capabilities()")
	}
}

// TestLoopbackListenerOnly asserts that the camera is bound to an IPv4 loopback
// address and nothing else is reachable.
func TestLoopbackListenerOnly(t *testing.T) {
	env := newTestEnvironment(t)
	addr := env.camera.listener.Addr()
	tcpAddr, ok := addr.(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address type = %T, want *net.TCPAddr", addr)
	}
	if !tcpAddr.IP.IsLoopback() {
		t.Fatalf("listener IP %v is not loopback", tcpAddr.IP)
	}
	if tcpAddr.IP.To4() == nil {
		t.Fatalf("listener IP %v is not IPv4", tcpAddr.IP)
	}
	if tcpAddr.IP.String() != "127.0.0.1" {
		t.Fatalf("listener IP = %q, want 127.0.0.1", tcpAddr.IP.String())
	}
}

// TestShutdownIdempotentAndListenerClosed asserts bounded, idempotent shutdown:
// repeated Close calls succeed, the listener is closed afterward, and the serve
// goroutine has exited.
func TestShutdownIdempotentAndListenerClosed(t *testing.T) {
	isolateConfigHome(t)
	env, err := NewEnvironment(context.Background())
	if err != nil {
		t.Fatalf("NewEnvironment() error = %v", err)
	}

	addr := env.camera.listener.Addr().String()

	ctx := context.Background()
	if err := env.Close(ctx); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	// Idempotence: a second (and third) Close returns the same clean outcome.
	if err := env.Close(ctx); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
	if err := env.camera.Close(); err != nil {
		t.Fatalf("third Close() error = %v", err)
	}

	// The serve goroutine must have exited.
	select {
	case <-env.camera.serveDone:
	case <-time.After(5 * time.Second):
		t.Fatalf("serve goroutine did not exit after shutdown")
	}

	// The listener must be closed: a dial to the old address must fail, and the
	// listener's own Close must report it is already closed.
	conn, dialErr := net.DialTimeout("tcp", addr, 250*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		t.Fatalf("listener still accepts connections at %s after shutdown", addr)
	}
	if closeErr := env.camera.listener.Close(); closeErr == nil {
		t.Fatalf("listener Close() after shutdown returned nil, want already-closed error")
	}
}

// TestNoGoroutineLeak asserts a stable goroutine count across a full
// create/use/shutdown cycle, so the serve goroutine and any session goroutines
// are fully reclaimed.
func TestNoGoroutineLeak(t *testing.T) {
	isolateConfigHome(t)

	// Let any lazily-started runtime goroutines settle before sampling.
	settle()
	before := runtime.NumGoroutine()

	env, err := NewEnvironment(context.Background())
	if err != nil {
		t.Fatalf("NewEnvironment() error = %v", err)
	}
	ctx := context.Background()
	if _, err := env.Manager().Bootstrap(ctx); err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if _, err := env.Manager().Profiles(ctx, env.DeviceID()); err != nil {
		t.Fatalf("Profiles() error = %v", err)
	}
	readLiveAccessUnits(t, env, 3)
	if err := env.Close(ctx); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	settle()
	after := runtime.NumGoroutine()
	// A tiny tolerance absorbs unrelated runtime/test-harness goroutines; the
	// serve goroutine (and any session goroutines) must not accumulate.
	if after > before+2 {
		t.Fatalf("goroutine count grew from %d to %d (leak suspected)", before, after)
	}
}

// settle gives background goroutines a brief, bounded window to exit before a
// goroutine-count sample.
func settle() {
	for i := 0; i < 20; i++ {
		runtime.Gosched()
		time.Sleep(5 * time.Millisecond)
	}
}

// assertLoopbackHTTP verifies a URL is an http:// URL on an IPv4 loopback host.
func assertLoopbackHTTP(t *testing.T, raw string) {
	t.Helper()
	parsed, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}
	if parsed.Scheme != "http" {
		t.Fatalf("scheme = %q, want http", parsed.Scheme)
	}
	host := parsed.Hostname()
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() || ip.To4() == nil {
		t.Fatalf("host %q is not an IPv4 loopback address", host)
	}
}

// assertSameOrigin verifies that every derived service URL shares the one real
// origin and matches the expected ONVIF service paths.
func assertSameOrigin(t *testing.T, r Runtime) {
	t.Helper()
	want := map[string]string{
		r.DeviceServiceURL: devicePath,
		r.Media1ServiceURL: media1Path,
		r.Media2ServiceURL: media2Path,
		r.SnapshotURL:      snapshotPath,
	}
	for raw, path := range want {
		parsed, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("url.Parse(%q) error = %v", raw, err)
		}
		gotOrigin := parsed.Scheme + "://" + parsed.Host
		if gotOrigin != r.Origin {
			t.Fatalf("URL %q origin = %q, want %q", raw, gotOrigin, r.Origin)
		}
		if parsed.Path != path {
			t.Fatalf("URL %q path = %q, want %q", raw, parsed.Path, path)
		}
	}
}

// assertNoSecretLeak serializes every boundary view and asserts that neither
// the synthetic username/password nor any credential reference prefix appears.
func assertNoSecretLeak(t *testing.T, views ...any) {
	t.Helper()
	needles := []string{demoUsername, demoPassword, "secret_"}
	for _, view := range views {
		encoded, err := json.Marshal(view)
		if err != nil {
			t.Fatalf("json.Marshal(%T) error = %v", view, err)
		}
		haystack := string(encoded)
		for _, needle := range needles {
			if strings.Contains(haystack, needle) {
				t.Fatalf("boundary view %T leaked %q", view, needle)
			}
		}
	}
}

func TestDemoScanAndInterfacesStayInsideSyntheticLoopback(t *testing.T) {
	env := newTestEnvironment(t)
	interfaces, err := env.Manager().ListHostInterfaces(context.Background(), app.HostInterfaceRequest{})
	if err != nil {
		t.Fatalf("ListHostInterfaces() error = %v", err)
	}
	if len(interfaces.Interfaces) != 1 {
		t.Fatalf("demo interfaces = %d, want 1", len(interfaces.Interfaces))
	}
	iface := interfaces.Interfaces[0]
	if iface.Name != demoHostInterfaceName || !iface.Loopback || !iface.Usable || len(iface.IPv4) != 1 || iface.IPv4[0] != "127.0.0.1" {
		t.Fatalf("unexpected demo interface: %+v", iface)
	}

	result, err := env.Manager().Scan(context.Background(), app.DiscoveryRequest{InterfaceName: demoHostInterfaceName})
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if len(result.Devices) != 1 || result.Devices[0].ID != env.DeviceID() {
		t.Fatalf("demo scan devices = %+v, want seeded loopback device", result.Devices)
	}

	if _, err := env.Manager().Scan(context.Background(), app.DiscoveryRequest{InterfaceName: "production-interface"}); err == nil {
		t.Fatal("demo scan accepted a production interface")
	}
}

// TestLiveStreamRunsThroughAuthenticatedSyntheticRTSP opens the demo profile
// through the production Manager and RTSP client and decodes access units.
func TestLiveStreamRunsThroughAuthenticatedSyntheticRTSP(t *testing.T) {
	env := newTestEnvironment(t)
	if codec := readLiveAccessUnits(t, env, 10); codec != stream.CodecH265 {
		t.Fatalf("main profile codec = %q, want H.265", codec)
	}
	if codec := readProfileAccessUnits(t, env, "profile_sub", 10); codec != stream.CodecH264 {
		t.Fatalf("sub profile codec = %q, want H.264", codec)
	}
}

func readLiveAccessUnits(t *testing.T, env *Environment, count int) string {
	t.Helper()
	return readProfileAccessUnits(t, env, "profile_main", count)
}

func readProfileAccessUnits(t *testing.T, env *Environment, profile string, count int) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	source, err := env.Manager().OpenLiveStream(ctx, app.StreamSelection{
		DeviceID:     env.DeviceID(),
		ProfileToken: profile,
		Transport:    app.StreamTransportTCP,
	})
	if err != nil {
		t.Fatalf("OpenLiveStream() error = %v", err)
	}
	defer source.Close()
	if len(source.ParameterSets()) == 0 {
		t.Fatal("the synthetic RTSP session description carries no parameter sets")
	}
	for i := 0; i < count; i++ {
		if _, _, err := source.ReadAccessUnit(ctx); err != nil {
			t.Fatalf("ReadAccessUnit(%d) error = %v", i, err)
		}
	}
	return source.Codec()
}

// TestSnapshotFeedServesDemoJPEGs exercises the JPEG preview fallback.
func TestSnapshotFeedServesDemoJPEGs(t *testing.T) {
	env := newTestEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	feed, err := env.Manager().OpenSnapshotFeed(ctx, app.StreamSelection{
		DeviceID: env.DeviceID(), ProfileToken: "profile_main", Transport: app.StreamTransportTCP,
	})
	if err != nil {
		t.Fatalf("OpenSnapshotFeed() error = %v", err)
	}
	image, err := feed.Fetch(ctx)
	if err != nil || len(image) < 4 || image[0] != 0xFF || image[1] != 0xD8 {
		t.Fatalf("Fetch() = %d bytes, error = %v", len(image), err)
	}
}

// TestEncoderChangeSwitchesTheStreamedCodec changes the main profile to H.264
// through the production encoder use case and verifies the stream follows.
func TestEncoderChangeSwitchesTheStreamedCodec(t *testing.T) {
	env := newTestEnvironment(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	view, err := env.Manager().ReadEncoder(ctx, env.DeviceID(), "profile_main")
	if err != nil || view.Current.Encoding != "H265" || len(view.Codecs) != 2 {
		t.Fatalf("ReadEncoder() = %+v, %v", view, err)
	}
	result, err := env.Manager().ApplyEncoder(ctx, env.DeviceID(), "profile_main", app.EncoderChange{
		Encoding: "H264", Width: 1280, Height: 720, FrameRate: 25, BitrateKbps: 2048, GOPLength: 50, CodecProfile: "High",
	})
	if err != nil || result.Report.Outcome != app.OutcomeExact {
		t.Fatalf("ApplyEncoder() = %+v, %v", result, err)
	}
	if codec := readLiveAccessUnits(t, env, 5); codec != stream.CodecH264 {
		t.Fatalf("main profile codec after change = %q, want H.264", codec)
	}
}
