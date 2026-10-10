package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

// TestCanonicalizeOperationalDeviceEndpoint proves the operational device
// endpoint helper preserves the full path and query for requests while applying
// the M1 discovery hardening to a query-free copy. The synthetic query marker
// is not a real secret.
func TestCanonicalizeOperationalDeviceEndpoint(t *testing.T) {
	t.Parallel()

	t.Run("preserves path and query", func(t *testing.T) {
		t.Parallel()
		got, err := canonicalizeOperationalDeviceEndpoint("http://cam.local/onvif/device_service?tenant=synthetic")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if got.RawQuery != "tenant=synthetic" {
			t.Fatalf("query = %q, want preserved", got.RawQuery)
		}
		if got.Path != "/onvif/device_service" {
			t.Fatalf("path = %q, want preserved", got.Path)
		}
	})

	t.Run("bare host defaults to device service path", func(t *testing.T) {
		t.Parallel()
		got, err := canonicalizeOperationalDeviceEndpoint("cam.local")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if got.Scheme != "http" || got.Host != "cam.local" || got.Path != "/onvif/device_service" {
			t.Fatalf("endpoint = %q, want http default device-service path", got.String())
		}
	})

	rejections := []struct {
		name string
		in   string
	}{
		{"userinfo", "http://user:pass@cam.local/onvif"},
		{"fragment", "http://cam.local/onvif#frag"},
		{"unsupported scheme", "ftp://cam.local/onvif"},
		{"control char", "http://cam.local/onvif\x01"},
		{"path traversal", "http://cam.local/onvif/../../etc"},
		{"empty", ""},
	}
	for _, tc := range rejections {
		tc := tc
		t.Run("rejects "+tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := canonicalizeOperationalDeviceEndpoint(tc.in); err == nil {
				t.Fatalf("accepted %q, want rejected", tc.in)
			}
		})
	}
}

// TestServiceQuerySentButAbsentFromResult proves that when a device advertises
// a Media service XAddr carrying a query, the orchestration can still reach it
// (profiles resolve), yet the query marker never appears in the result JSON,
// an error, fmt output, or a recursive unwrap. The marker is a synthetic
// non-secret leak probe.
func TestServiceQuerySentButAbsentFromResult(t *testing.T) {
	t.Parallel()
	const marker = "synthetic-service-query-marker"

	var sawMarker bool
	setup := newUnstartedFakeWithHandler(t, func(base string, device *fakedevice.Device) http.Handler {
		return &requiringQueryObserver{
			inner:  device,
			marker: marker,
			// Guard both Media service paths: a POST without the exact marker
			// query is rejected before the inner handler, so resolution is only
			// possible when the operational query actually reaches the wire.
			guardedPath: []string{"/onvif/media_service", "/onvif/media2_service"},
			sawMarker:   &sawMarker,
		}
	}, func(base string) fakedevice.Config {
		return fakedevice.Config{Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
			{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
			// Media endpoints carry a required query. The guarding observer
			// rejects any Media POST that lacks it, so resolution succeeding
			// PROVES the query reached the wire; it must still never surface.
			{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: base + "/onvif/media_service?access=" + marker, VersionMajor: 25, VersionMinor: 6},
			{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service?access=" + marker, VersionMajor: 25, VersionMinor: 6},
		}}}
	})

	result, err := NewProductionMediaProfiles().Profiles(context.Background(), MediaProfilesRequest{Host: setup.host})
	if err != nil {
		t.Fatalf("Profiles() error = %v", err)
	}
	if len(result.Profiles) == 0 {
		t.Fatal("no profiles resolved; the query-bearing service endpoint was not reached")
	}
	if !sawMarker {
		t.Fatal("the required Media service query marker never reached the wire")
	}

	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), marker) {
		t.Fatalf("result JSON leaked the service query marker: %s", encoded)
	}
	// fmt rendering of the whole result must not leak the marker either.
	if strings.Contains(fmt.Sprintf("%+v", result), marker) {
		t.Fatalf("result fmt output leaked the service query marker")
	}
}

// TestDeviceEndpointQuerySentButAbsentFromResult proves that a secret query on
// the DEVICE endpoint host is sent to the actual device request (the recording
// server observes it) yet never appears in the use case result JSON. The
// marker is a synthetic non-secret probe.
func TestDeviceEndpointQuerySentButAbsentFromResult(t *testing.T) {
	t.Parallel()
	const marker = "synthetic-device-query-marker"

	var sawQuery bool
	server := httptest.NewUnstartedServer(nil)
	base := "http://" + server.Listener.Addr().String()
	device, err := fakedevice.New(fakedevice.Config{Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
		{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
		{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: base + "/onvif/media_service", VersionMajor: 25, VersionMinor: 6},
	}}})
	if err != nil {
		t.Fatalf("fakedevice.New() error = %v", err)
	}
	server.Config.Handler = &queryObserver{inner: device, marker: marker, saw: &sawQuery}
	server.Start()
	t.Cleanup(server.Close)

	host := base + "/onvif/device_service?audit=" + marker
	result, err := NewProductionMediaProfiles().Profiles(context.Background(), MediaProfilesRequest{Host: host})
	if err != nil {
		t.Fatalf("Profiles() error = %v", err)
	}
	if !sawQuery {
		t.Fatal("the device request did not carry the operational query")
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), marker) {
		t.Fatalf("result JSON leaked the device query marker: %s", encoded)
	}
}

// TestUnsafeMedia2SafeMedia1FailsClosed proves that an advertised Media2
// endpoint that is unsafe (cross-origin) fails the whole use case closed, even
// when a safe same-origin Media1 is present. The implementation must NOT
// silently drop the unsafe Media2 and use Media1.
func TestUnsafeMedia2SafeMedia1FailsClosed(t *testing.T) {
	t.Parallel()
	setup := newUnstartedFake(t, func(base string) fakedevice.Config {
		return fakedevice.Config{Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
			{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
			// Safe same-origin Media1 ...
			{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: base + "/onvif/media_service", VersionMajor: 25, VersionMinor: 6},
			// ... but an UNSAFE cross-origin Media2 that must fail closed.
			{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: "http://attacker.invalid/onvif/media2", VersionMajor: 25, VersionMinor: 6},
		}}}
	})

	_, err := NewProductionMediaProfiles().Profiles(context.Background(), MediaProfilesRequest{Host: setup.host})
	if err == nil {
		t.Fatal("expected a fail-closed error for the unsafe Media2 endpoint")
	}
	if CategoryOf(err) != CategoryProtocol {
		t.Fatalf("category = %q, want protocol", CategoryOf(err))
	}
}
