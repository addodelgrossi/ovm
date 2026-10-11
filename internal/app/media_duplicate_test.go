package app

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

// authObserver wraps a fakedevice handler and records whether any request was
// made to one of the given ambiguous media paths carrying an Authorization
// header or a WS-Security UsernameToken body. It proves that a fail-closed
// duplicate-service rejection sends NO credentials to either ambiguous Media
// endpoint.
type authObserver struct {
	inner        http.Handler
	mediaPaths   []string
	sawMediaHit  *int32
	sawMediaAuth *int32
}

func (o *authObserver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	for _, p := range o.mediaPaths {
		if r.URL.Path == p {
			atomic.AddInt32(o.sawMediaHit, 1)
			if r.Header.Get("Authorization") != "" {
				atomic.AddInt32(o.sawMediaAuth, 1)
			}
			// A WS-Security UsernameToken would appear in the SOAP body; a GET
			// has none. Any POST to an ambiguous media path is itself a leak of
			// intent, captured by sawMediaHit above.
		}
	}
	o.inner.ServeHTTP(w, r)
}

// TestDuplicateMediaServicesRejectedAmbiguous proves Finding 2's fix: duplicate
// same-namespace Media1 or Media2 GetServices advertisements are rejected as a
// protocol (ambiguous) error BEFORE any Media operation, regardless of response
// order, and no request (and therefore no credential) ever reaches either
// ambiguous Media endpoint.
func TestDuplicateMediaServicesRejectedAmbiguous(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		namespace string
	}{
		{"duplicate media1", "http://www.onvif.org/ver10/media/wsdl"},
		{"duplicate media2", "http://www.onvif.org/ver20/media/wsdl"},
	}
	for _, tc := range cases {
		tc := tc
		for _, reversed := range []bool{false, true} {
			reversed := reversed
			label := tc.name
			if reversed {
				label += " reversed"
			}
			t.Run(label, func(t *testing.T) {
				t.Parallel()
				runDuplicateServiceCase(t, tc.namespace, reversed)
			})
		}
	}
}

func runDuplicateServiceCase(t *testing.T, dupNamespace string, reversed bool) {
	t.Helper()
	var mediaHits, mediaAuth int32

	// The two duplicate endpoints and the paths an auth observer watches.
	pathA, pathB := "/onvif/dupA", "/onvif/dupB"

	setup := newUnstartedFakeWithHandler(t, func(base string, device *fakedevice.Device) http.Handler {
		return &authObserver{
			inner:        device,
			mediaPaths:   []string{pathA, pathB},
			sawMediaHit:  &mediaHits,
			sawMediaAuth: &mediaAuth,
		}
	}, func(base string) fakedevice.Config {
		services := []fakedevice.ServiceData{
			{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
		}
		// Always advertise the counterpart namespace once so the camera has one
		// usable media service besides the duplicated pair.
		other := "http://www.onvif.org/ver20/media/wsdl"
		if dupNamespace == other {
			other = "http://www.onvif.org/ver10/media/wsdl"
		}
		services = append(services, fakedevice.ServiceData{Namespace: other, XAddr: base + "/onvif/media_other", VersionMajor: 25, VersionMinor: 6})
		dupA := fakedevice.ServiceData{Namespace: dupNamespace, XAddr: base + pathA, VersionMajor: 25, VersionMinor: 6}
		dupB := fakedevice.ServiceData{Namespace: dupNamespace, XAddr: base + pathB, VersionMajor: 25, VersionMinor: 6}
		if reversed {
			services = append(services, dupB, dupA)
		} else {
			services = append(services, dupA, dupB)
		}
		return fakedevice.Config{
			RequireUsernameToken: true,
			Username:             "operator",
			Password:             "synthetic-secret",
			Inventory:            fakedevice.Inventory{Services: services},
		}
	})

	creds, err := NewCredentials("operator", "synthetic-secret", false)
	if err != nil {
		t.Fatalf("NewCredentials() error = %v", err)
	}
	_, err = NewProductionMediaProfiles().Profiles(context.Background(), MediaProfilesRequest{Host: setup.host, Credentials: creds})
	if err == nil {
		t.Fatal("expected an ambiguous-duplicate rejection")
	}
	if CategoryOf(err) != CategoryProtocol {
		t.Fatalf("category = %q, want protocol", CategoryOf(err))
	}
	if atomic.LoadInt32(&mediaHits) != 0 {
		t.Fatalf("an ambiguous media endpoint was contacted %d times, want 0", mediaHits)
	}
	if atomic.LoadInt32(&mediaAuth) != 0 {
		t.Fatal("an ambiguous media endpoint received an Authorization header")
	}
	// The error must not leak a path marker.
	if strings.Contains(err.Error(), pathA) || strings.Contains(err.Error(), pathB) {
		t.Fatalf("error leaked an endpoint path: %v", err)
	}
}
