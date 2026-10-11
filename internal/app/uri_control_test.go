package app

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

// encodedControlInputs is the exhaustive set of percent-encoded control markers
// the URI boundaries must reject after decoding, alongside their literal forms.
// %00 (NUL), %0A (LF), %0D (CR), and %7F (DEL) are the response-splitting and
// log-injection vectors called out by the review.
var encodedControlPathInputs = []string{
	"/onvif/%00media",
	"/onvif/%0Amedia",
	"/onvif/%0Dmedia",
	"/onvif/%7Fmedia",
}

var encodedControlQueryInputs = []string{
	"x=%00",
	"x=%0A",
	"x=%0D",
	"x=%7F",
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}
	return u
}

// TestParseSafeOperationalURLRejectsEncodedControls proves the advertised
// service endpoint validator rejects controls that are hidden behind percent-
// encoding in the path or the retained query, in addition to literal controls.
func TestParseSafeOperationalURLRejectsEncodedControls(t *testing.T) {
	t.Parallel()
	for _, p := range encodedControlPathInputs {
		raw := "http://cam.local" + p
		if _, err := parseSafeOperationalURL(raw); err == nil {
			t.Fatalf("parseSafeOperationalURL accepted encoded control path %q", raw)
		}
	}
	for _, q := range encodedControlQueryInputs {
		raw := "http://cam.local/onvif?" + q
		if _, err := parseSafeOperationalURL(raw); err == nil {
			t.Fatalf("parseSafeOperationalURL accepted encoded control query %q", raw)
		}
	}
	// Literal controls are rejected too.
	if _, err := parseSafeOperationalURL("http://cam.local/onvif\n"); err == nil {
		t.Fatal("parseSafeOperationalURL accepted a literal control")
	}
}

// TestCanonicalizeOperationalDeviceEndpointRejectsEncodedControls proves the
// device endpoint canonicalizer rejects encoded controls in both the path and
// the preserved operational query.
func TestCanonicalizeOperationalDeviceEndpointRejectsEncodedControls(t *testing.T) {
	t.Parallel()
	for _, p := range encodedControlPathInputs {
		raw := "http://cam.local" + p
		if _, err := canonicalizeOperationalDeviceEndpoint(raw); err == nil {
			t.Fatalf("canonicalize accepted encoded control path %q", raw)
		}
	}
	for _, q := range encodedControlQueryInputs {
		raw := "http://cam.local/onvif/device_service?" + q
		if _, err := canonicalizeOperationalDeviceEndpoint(raw); err == nil {
			t.Fatalf("canonicalize accepted encoded control query %q", raw)
		}
	}
}

// TestValidateStreamURIRejectsEncodedControls proves the stream URI validator
// rejects encoded controls in the path and query. The camera host matches so
// only the control check can fail.
func TestValidateStreamURIRejectsEncodedControls(t *testing.T) {
	t.Parallel()
	r := &profileResolver{cameraURL: mustParse(t, "http://cam.local/onvif/device_service")}
	for _, p := range encodedControlPathInputs {
		raw := "rtsp://cam.local:554" + p
		if _, err := r.validateStreamURI(raw); err == nil {
			t.Fatalf("validateStreamURI accepted encoded control path %q", raw)
		}
	}
	for _, q := range encodedControlQueryInputs {
		raw := "rtsp://cam.local:554/stream?" + q
		if _, err := r.validateStreamURI(raw); err == nil {
			t.Fatalf("validateStreamURI accepted encoded control query %q", raw)
		}
	}
}

// TestValidateSnapshotURIRejectsEncodedControls proves the snapshot URI
// validator rejects encoded controls in the path and query.
func TestValidateSnapshotURIRejectsEncodedControls(t *testing.T) {
	t.Parallel()
	camera := mustParse(t, "http://cam.local/onvif/device_service")
	for _, p := range encodedControlPathInputs {
		raw := "http://cam.local" + p
		if _, err := validateSnapshotURI(camera, raw); err == nil {
			t.Fatalf("validateSnapshotURI accepted encoded control path %q", raw)
		}
	}
	for _, q := range encodedControlQueryInputs {
		raw := "http://cam.local/snap?" + q
		if _, err := validateSnapshotURI(camera, raw); err == nil {
			t.Fatalf("validateSnapshotURI accepted encoded control query %q", raw)
		}
	}
}

// TestSnapshotRedirectPolicyRejectsEncodedControls proves the redirect policy
// refuses a same-origin redirect target that carries an encoded control, with
// the typed unsafe-target sentinel and no leaked URL.
func TestSnapshotRedirectPolicyRejectsEncodedControls(t *testing.T) {
	t.Parallel()
	camera := mustParse(t, "http://cam.local/onvif/device_service")
	policy := snapshotRedirectPolicy(camera)
	via := []*http.Request{{URL: camera}}
	for _, p := range encodedControlPathInputs {
		target := mustParse(t, "http://cam.local"+p)
		err := policy(&http.Request{URL: target}, via)
		if err == nil {
			t.Fatalf("redirect policy accepted encoded control path %q", p)
		}
		if !errors.Is(err, ErrSnapshotRedirectUnsafeTarget) {
			t.Fatalf("error = %v, want ErrSnapshotRedirectUnsafeTarget", err)
		}
	}
	for _, q := range encodedControlQueryInputs {
		target := mustParse(t, "http://cam.local/snap?"+q)
		err := policy(&http.Request{URL: target}, via)
		if err == nil {
			t.Fatalf("redirect policy accepted encoded control query %q", q)
		}
		if !errors.Is(err, ErrSnapshotRedirectUnsafeTarget) {
			t.Fatalf("error = %v, want ErrSnapshotRedirectUnsafeTarget", err)
		}
	}
}
