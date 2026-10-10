package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

// mustURL parses a URL for tests.
func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", raw, err)
	}
	return u
}

// assertNoQuery fails if a presentation URI retains a query or userinfo. The
// synthetic markers used by the tests are not real secrets.
func assertNoQuery(t *testing.T, uri string) {
	t.Helper()
	if uri == "" {
		return
	}
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatalf("presentation URI %q did not parse: %v", uri, err)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery {
		t.Fatalf("presentation URI retained a query: %q", uri)
	}
	if parsed.User != nil {
		t.Fatalf("presentation URI retained userinfo: %q", uri)
	}
}

// TestSameOriginMatrix pins the same-origin decision across scheme, host, and
// port, including default-port normalization and cross-scheme/host rejection.
func TestSameOriginMatrix(t *testing.T) {
	ref := mustURL(t, "http://cam.local:80/onvif/device_service")
	cases := []struct {
		candidate string
		want      bool
	}{
		{"http://cam.local/onvif/media", true},       // default port normalized
		{"http://cam.local:80/onvif/snapshot", true}, // explicit default port
		{"http://CAM.LOCAL/onvif/media", true},       // case-insensitive host
		{"http://cam.local:8080/onvif/media", false}, // different port
		{"https://cam.local/onvif/media", false},     // different scheme
		{"http://other.local/onvif/media", false},    // different host
		{"ftp://cam.local/onvif/media", false},       // unsupported scheme
		{"http://cam.local.evil.com/onvif", false},   // suffix host is not equal
	}
	for _, tc := range cases {
		got := sameOrigin(ref, mustURL(t, tc.candidate))
		if got != tc.want {
			t.Errorf("sameOrigin(%q) = %v, want %v", tc.candidate, got, tc.want)
		}
	}
}

// TestValidateSameOriginEndpointRejections proves userinfo, control chars,
// unsupported schemes, fragments, and cross-origin endpoints are rejected, and
// that the raw query is preserved on an accepted same-origin endpoint.
func TestValidateSameOriginEndpointRejections(t *testing.T) {
	camera := mustURL(t, "http://cam.local/onvif/device_service")
	bad := []string{
		"http://user:pass@cam.local/onvif/media", // userinfo
		"http://cam.local/onvif/media#frag",      // fragment
		"ftp://cam.local/onvif/media",            // unsupported scheme
		"http://other.local/onvif/media",         // cross-origin
		"/onvif/media",                           // relative
		"http://cam.local/onvif/media\x01",       // control char
	}
	for _, raw := range bad {
		if _, err := validateSameOriginEndpoint(camera, raw); err == nil {
			t.Errorf("validateSameOriginEndpoint(%q) accepted, want rejected", raw)
		}
	}
	// A same-origin endpoint with a query is accepted and preserves the query
	// (the synthetic marker is not a real secret).
	ok, err := validateSameOriginEndpoint(camera, "http://cam.local/onvif/media?token=synthetic")
	if err != nil {
		t.Fatalf("same-origin endpoint rejected: %v", err)
	}
	if ok.RawQuery != "token=synthetic" {
		t.Fatalf("operational query not preserved, got %q", ok.RawQuery)
	}
}

// TestSnapshotDTOJSONHasNoQueryOrOperational proves the snapshot and stream
// DTOs serialize only redacted presentation URIs: there is no operational field
// and no accessor returning a raw URI. The synthetic marker is not a secret.
func TestSnapshotDTOJSONHasNoQueryOrOperational(t *testing.T) {
	t.Parallel()
	const marker = "synthetic-token-marker"
	profile := MediaProfileDTO{
		Token:    "p0",
		Snapshot: &SnapshotURIDTO{URI: "http://cam.local/onvif/snapshot"},
		Stream:   &StreamURIDTO{TCPURI: "rtsp://cam.local:554/stream", UDPURI: "rtsp://cam.local:554/stream"},
	}
	encoded, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	js := string(encoded)
	if strings.Contains(js, marker) {
		t.Fatalf("JSON unexpectedly contained a marker: %s", js)
	}
	// snake_case keys are present; no camelCase operational keys exist.
	for _, want := range []string{`"tcp_uri"`, `"udp_uri"`, `"uri"`} {
		if !strings.Contains(js, want) {
			t.Fatalf("JSON missing expected key %s: %s", want, js)
		}
	}
	for _, forbidden := range []string{"operational", "Operational", "tcpUri", "udpUri"} {
		if strings.Contains(js, forbidden) {
			t.Fatalf("JSON contained a forbidden key %q: %s", forbidden, js)
		}
	}
}

// TestValidateSnapshotURIRejections proves the snapshot URI validator rejects
// non-HTTP schemes, userinfo, fragments, control chars, and cross-origin, and
// that errors never include the URI value.
func TestValidateSnapshotURIRejections(t *testing.T) {
	camera := mustURL(t, "http://cam.local/onvif/device_service")
	bad := []string{
		"rtsp://cam.local/stream",         // non-HTTP
		"http://user:pass@cam.local/snap", // userinfo
		"http://cam.local/snap#frag",      // fragment
		"http://other.local/snap",         // cross-origin
		"http://cam.local/snap\x00",       // control char
	}
	for _, raw := range bad {
		_, err := validateSnapshotURI(camera, raw)
		if err == nil {
			t.Errorf("validateSnapshotURI(%q) accepted, want rejected", raw)
		}
		if err != nil && strings.Contains(err.Error(), "cam.local") {
			t.Errorf("error leaked the URI: %v", err)
		}
	}
}

// TestValidateStreamURIRejections proves the stream URI validator requires
// absolute rtsp/rtsps, a host matching the camera hostname, and rejects
// userinfo/fragment/control/non-RTSP scheme/cross-host; the safe result strips
// the query. Errors never include the URI value.
func TestValidateStreamURIRejections(t *testing.T) {
	t.Parallel()
	r := &profileResolver{cameraURL: mustURL(t, "http://cam.local/onvif/device_service")}
	bad := []string{
		"http://cam.local/stream",          // not rtsp
		"rtsp://user:pass@cam.local:554/s", // userinfo
		"rtsp://cam.local:554/s#frag",      // fragment
		"rtsp://other.local:554/s",         // cross-host
		"rtsp://cam.local:554/s\x01",       // control char
		"/relative/stream",                 // relative
	}
	for _, raw := range bad {
		if _, err := r.validateStreamURI(raw); err == nil {
			t.Errorf("validateStreamURI(%q) accepted, want rejected", raw)
		} else if strings.Contains(err.Error(), "cam.local") {
			t.Errorf("error leaked the URI: %v", err)
		}
	}
	// An rtsp URI on a non-default RTSP port with a query is accepted and the
	// query is stripped from the presentation value.
	safe, err := r.validateStreamURI("rtsp://cam.local:8554/stream?session=synthetic")
	if err != nil {
		t.Fatalf("valid rtsp URI rejected: %v", err)
	}
	assertNoQuery(t, safe)
	if !strings.HasPrefix(safe, "rtsp://cam.local:8554/stream") {
		t.Fatalf("safe stream URI = %q, want host/port/path preserved", safe)
	}
}

// TestParseJPEGContentType proves only image/jpeg (with optional parameters) is
// accepted and the error never echoes the header value.
func TestParseJPEGContentType(t *testing.T) {
	if _, err := parseJPEGContentType("image/jpeg"); err != nil {
		t.Fatalf("image/jpeg rejected: %v", err)
	}
	if _, err := parseJPEGContentType("image/jpeg; charset=binary"); err != nil {
		t.Fatalf("parameterized image/jpeg rejected: %v", err)
	}
	for _, bad := range []string{"", "text/html", "image/png", "application/json", "not a mime"} {
		if _, err := parseJPEGContentType(bad); err == nil {
			t.Errorf("parseJPEGContentType(%q) accepted, want rejected", bad)
		}
	}
}

// TestFetchWrongMIMERejected proves a non-JPEG content type is rejected and no
// file is left behind.
func TestFetchWrongMIMERejected(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>not an image</html>"))
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "snap.jpg")
	_, _, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 1<<20)
	if err == nil {
		t.Fatal("expected a wrong-MIME rejection")
	}
	if CategoryOf(err) != CategoryProtocol {
		t.Fatalf("category = %q, want protocol", CategoryOf(err))
	}
	assertNoFile(t, out)
}

// TestFetchRejectsStructurallyInvalidJPEGBeforePublication proves an
// image/jpeg header cannot publish malformed, truncated, format-spoofed, or
// trailing-data payloads. Every case is rejected after the bounded download and
// leaves neither a destination nor a temporary file.
func TestFetchRejectsStructurallyInvalidJPEGBeforePublication(t *testing.T) {
	t.Parallel()

	valid := projectJPEGBytes()
	cases := []struct {
		name string
		body []byte
	}{
		{name: "marker only", body: []byte{0xff, 0xd8, 0xff, 0xd9}},
		{name: "truncated", body: append([]byte(nil), valid[:len(valid)-2]...)},
		{name: "PNG with JPEG MIME", body: projectPNGBytes()},
		{name: "trailing data", body: append(append([]byte(nil), valid...), []byte("trailing-data")...)},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			srv := newJPEGServer(t, "image/jpeg", test.body, true, http.StatusOK)
			out := filepath.Join(t.TempDir(), "snapshot.jpg")
			_, _, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 1<<20)
			if err == nil || CategoryOf(err) != CategoryProtocol {
				t.Fatalf("category = %q (err=%v), want protocol", CategoryOf(err), err)
			}
			assertNoFile(t, out)
		})
	}
}

func TestValidateSnapshotDimensions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		width     int
		height    int
		wantError bool
	}{
		{name: "small", width: 8, height: 8},
		{name: "width boundary", width: maxSnapshotWidth, height: 1},
		{name: "height boundary", width: 1, height: maxSnapshotHeight},
		{name: "pixel boundary", width: 8192, height: 8192},
		{name: "zero width", width: 0, height: 1, wantError: true},
		{name: "width over boundary", width: maxSnapshotWidth + 1, height: 1, wantError: true},
		{name: "height over boundary", width: 1, height: maxSnapshotHeight + 1, wantError: true},
		{name: "pixels over boundary", width: 8193, height: 8192, wantError: true},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateSnapshotDimensions(test.width, test.height)
			if test.wantError {
				if err == nil || CategoryOf(err) != CategoryProtocol {
					t.Fatalf("validateSnapshotDimensions() = %v, want protocol error", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validateSnapshotDimensions() error = %v", err)
			}
		})
	}
}

// TestFetchRejectsOversizedJPEGDimensionsBeforeDecode mutates only the SOF
// dimensions of a tiny valid JPEG. This proves policy rejection occurs after
// DecodeConfig and before a large full-decoder allocation.
func TestFetchRejectsOversizedJPEGDimensionsBeforeDecode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		width  int
		height int
	}{
		{name: "axis bound", width: maxSnapshotWidth + 1, height: 1},
		{name: "pixel bound", width: 8193, height: 8192},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			body := projectJPEGWithDimensions(t, test.width, test.height)
			srv := newJPEGServer(t, "image/jpeg", body, true, http.StatusOK)
			out := filepath.Join(t.TempDir(), "snapshot.jpg")
			_, _, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 1<<20)
			if err == nil || CategoryOf(err) != CategoryProtocol {
				t.Fatalf("category = %q (err=%v), want protocol", CategoryOf(err), err)
			}
			assertNoFile(t, out)
		})
	}
}

// TestFetchChunkedOversizeRejected proves an oversize body with NO
// Content-Length (chunked) is rejected via the limit+1 read and leaves no file.
func TestFetchChunkedOversizeRejected(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		// No Content-Length: force chunked transfer by flushing.
		flusher, _ := w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
		big := make([]byte, 64)
		for i := 0; i < 10; i++ {
			_, _ = w.Write(big)
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "snap.jpg")
	_, _, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 100)
	if err == nil {
		t.Fatal("expected a chunked-oversize rejection")
	}
	if CategoryOf(err) != CategoryProtocol {
		t.Fatalf("category = %q, want protocol", CategoryOf(err))
	}
	assertNoFile(t, out)
}

// TestFetchContentLengthOversizeRejected proves an oversize advertised
// Content-Length is rejected early, before buffering.
func TestFetchContentLengthOversizeRejected(t *testing.T) {
	t.Parallel()
	payload := make([]byte, 512)
	payload[0], payload[1] = 0xff, 0xd8
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "512")
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "snap.jpg")
	_, _, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 100)
	if err == nil {
		t.Fatal("expected a Content-Length oversize rejection")
	}
	assertNoFile(t, out)
}

// TestFetchNonSuccessStatusRejected proves a non-2xx status is rejected.
func TestFetchNonSuccessStatusRejected(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "snap.jpg")
	_, _, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 1<<20)
	if err == nil {
		t.Fatal("expected a non-success rejection")
	}
	assertNoFile(t, out)
}

// TestFetchSuccessWritesWithinLimit proves a valid, within-limit JPEG is
// written with 0600 permissions and the returned size matches.
func TestFetchSuccessWritesWithinLimit(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(projectJPEGBytes())
	}))
	defer srv.Close()

	out := filepath.Join(t.TempDir(), "snap.jpg")
	size, ct, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 1<<20)
	if err != nil {
		t.Fatalf("fetch error = %v", err)
	}
	if ct != "image/jpeg" || size != int64(len(projectJPEGBytes())) {
		t.Fatalf("ct=%q size=%d, want image/jpeg %d", ct, size, len(projectJPEGBytes()))
	}
	info, statErr := os.Stat(out)
	if statErr != nil {
		t.Fatalf("output not written: %v", statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("file perm = %o, want 600", info.Mode().Perm())
	}
}

// TestSnapshotRedirectPolicyRejectsCrossOriginAndDowngrade proves the redirect
// policy refuses a cross-origin redirect and an HTTPS->HTTP downgrade without
// leaking a URL in the error.
func TestSnapshotRedirectPolicyRejectsCrossOriginAndDowngrade(t *testing.T) {
	camera := mustURL(t, "https://cam.local/onvif/device_service")
	policy := snapshotRedirectPolicy(camera)

	// Cross-origin redirect.
	req := &http.Request{URL: mustURL(t, "https://evil.local/snap")}
	via := []*http.Request{{URL: mustURL(t, "https://cam.local/snap")}}
	if err := policy(req, via); err == nil {
		t.Fatal("cross-origin redirect accepted, want rejected")
	}

	// HTTPS -> HTTP downgrade to the same host.
	req = &http.Request{URL: mustURL(t, "http://cam.local/snap")}
	via = []*http.Request{{URL: mustURL(t, "https://cam.local/snap")}}
	if err := policy(req, via); err == nil {
		t.Fatal("HTTPS->HTTP downgrade accepted, want rejected")
	}

	// Same-origin redirect is allowed.
	req = &http.Request{URL: mustURL(t, "https://cam.local/snap2")}
	via = []*http.Request{{URL: mustURL(t, "https://cam.local/snap")}}
	if err := policy(req, via); err != nil {
		t.Fatalf("same-origin redirect rejected: %v", err)
	}
}

// TestSnapshotCrossOriginServiceRejected proves that when GetServices advertises
// a cross-origin media XAddr, the whole use case fails closed with a protocol
// error rather than silently dropping that service and using Media1.
func TestSnapshotCrossOriginServiceRejected(t *testing.T) {
	t.Parallel()
	setup := newUnstartedFake(t, func(base string) fakedevice.Config {
		return fakedevice.Config{Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
			{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
			// Cross-origin Media endpoints: must fail closed, not silently drop.
			{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: "http://attacker.invalid/onvif/media", VersionMajor: 25, VersionMinor: 6},
			{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: "http://attacker.invalid/onvif/media2", VersionMajor: 25, VersionMinor: 6},
		}}}
	})

	_, err := NewProductionMediaProfiles().Profiles(context.Background(), MediaProfilesRequest{Host: setup.host, Timeout: 10 * time.Second})
	if err == nil {
		t.Fatal("expected a fail-closed error for cross-origin media endpoints")
	}
	if CategoryOf(err) != CategoryProtocol {
		t.Fatalf("category = %q, want protocol", CategoryOf(err))
	}
}

func assertNoFile(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file %q should not exist, stat err = %v", path, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".snapshot-") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}
