package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

// snapshotQueryFake starts a fake device that requires the snapshot GET to
// carry BOTH a configured synthetic snapshot query and the generated profile
// parameter. It returns the device, the canonical device endpoint, and a flag
// pointer set true once both markers are observed on the wire. The markers are
// synthetic non-secret probes.
func snapshotQueryFake(t *testing.T, username, password, snapshotMarker string, sawBoth *bool, forceWrongMIME bool) string {
	t.Helper()

	const snapshotPath = "/onvif/snapshot"
	setup := newUnstartedFakeWithHandler(t, func(base string, device *fakedevice.Device) http.Handler {
		var inner http.Handler = device
		if forceWrongMIME {
			// Serve a non-JPEG body at the snapshot path so the public use case
			// fails on MIME while still proving the query reached the wire.
			inner = wrongMIMESnapshotHandler{inner: device, snapshotPath: snapshotPath}
		}
		return &requiringSnapshotObserver{
			inner:           inner,
			snapshotPath:    snapshotPath,
			configuredQuery: snapshotMarker,
			profileParam:    "profile",
			sawBoth:         sawBoth,
		}
	}, func(base string) fakedevice.Config {
		return fakedevice.Config{
			HTTPAuth:             fakedevice.HTTPAuthDigest,
			DigestAlgorithm:      "SHA-256",
			RequireUsernameToken: true,
			Username:             username,
			Password:             password,
			Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
				{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
				{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: base + "/onvif/media_service", VersionMajor: 25, VersionMinor: 6},
				{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6},
			}},
			MediaInventory: fakedevice.MediaInventory{SnapshotQuery: snapshotMarker},
		}
	})
	return setup.host
}

// wrongMIMESnapshotHandler serves a non-JPEG body at the snapshot path so a
// snapshot GET that passes the query guard still fails the public use case's
// MIME check. Every other request is delegated unchanged.
type wrongMIMESnapshotHandler struct {
	inner        http.Handler
	snapshotPath string
}

func (h wrongMIMESnapshotHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == h.snapshotPath {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not-a-jpeg-body-marker"))
		return
	}
	h.inner.ServeHTTP(w, r)
}

// TestSnapshotWireQueryProvenAndAbsentFromMetadata proves Finding 6(b): the
// configured snapshot query AND the generated profile parameter both reach the
// wire (the requiring observer rejects a GET missing either), the public use
// case succeeds, and the configured marker never appears in the returned
// metadata JSON, Error(), fmt forms, or any recursive unwrap.
func TestSnapshotWireQueryProvenAndAbsentFromMetadata(t *testing.T) {
	t.Parallel()
	const snapshotMarker = "sig=synthetic-snapshot-query-marker"
	const leakProbe = "synthetic-snapshot-query-marker"
	secret := "snapshot-secret-" + t.Name()

	var sawBoth bool
	host := snapshotQueryFake(t, "admin", secret, snapshotMarker, &sawBoth, false)

	creds, err := NewCredentials("admin", secret, false)
	if err != nil {
		t.Fatalf("NewCredentials() error = %v", err)
	}
	out := filepath.Join(t.TempDir(), "snap.jpg")
	meta, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host:         host,
		Credentials:  creds,
		ProfileToken: "profile_main",
		OutputPath:   out,
		Timeout:      10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if !sawBoth {
		t.Fatal("the snapshot GET did not carry both the configured query and the profile parameter")
	}
	if _, statErr := os.Stat(out); statErr != nil {
		t.Fatalf("snapshot not written: %v", statErr)
	}

	// The configured query marker must never appear in the metadata JSON or
	// any fmt rendering.
	encoded, err := json.Marshal(meta)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), leakProbe) {
		t.Fatalf("metadata JSON leaked the snapshot query marker: %s", encoded)
	}
	if strings.Contains(fmt.Sprintf("%+v", meta), leakProbe) {
		t.Fatal("metadata fmt output leaked the snapshot query marker")
	}
	if strings.Contains(meta.OutputPath, leakProbe) {
		t.Fatal("output path leaked the snapshot query marker")
	}
}

// TestSnapshotWireQueryProvenOnFailurePath proves the same wire-query contract
// through a FAILING public use case: the query guard still requires both
// markers (so the query reached the wire), but the snapshot path serves a
// non-JPEG body, so Snapshot fails on MIME. The returned error must carry
// neither the configured query marker nor the body marker in Error(), fmt
// forms, or any recursive unwrap.
func TestSnapshotWireQueryProvenOnFailurePath(t *testing.T) {
	t.Parallel()
	const snapshotMarker = "sig=synthetic-snapshot-fail-marker"
	const leakProbe = "synthetic-snapshot-fail-marker"
	const bodyMarker = "not-a-jpeg-body-marker"
	secret := "snapshot-secret-" + t.Name()

	var sawBoth bool
	host := snapshotQueryFake(t, "admin", secret, snapshotMarker, &sawBoth, true)

	creds, err := NewCredentials("admin", secret, false)
	if err != nil {
		t.Fatalf("NewCredentials() error = %v", err)
	}
	out := filepath.Join(t.TempDir(), "snap.jpg")
	_, err = NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host:         host,
		Credentials:  creds,
		ProfileToken: "profile_main",
		OutputPath:   out,
		Timeout:      10 * time.Second,
	})
	if err == nil {
		t.Fatal("expected a MIME failure for the non-JPEG snapshot body")
	}
	if CategoryOf(err) != CategoryProtocol {
		t.Fatalf("category = %q, want protocol (not a JPEG)", CategoryOf(err))
	}
	if !sawBoth {
		t.Fatal("the snapshot GET did not carry both markers even on the failure path")
	}
	// No marker (query or body) may survive anywhere in the error.
	for _, marker := range []string{leakProbe, bodyMarker} {
		if strings.Contains(err.Error(), marker) {
			t.Fatalf("Error() leaked marker %q: %v", marker, err)
		}
		if strings.Contains(fmt.Sprintf("%v", err), marker) || strings.Contains(fmt.Sprintf("%+v", err), marker) {
			t.Fatalf("fmt rendering leaked marker %q", marker)
		}
		for cur := err; cur != nil; cur = errors.Unwrap(cur) {
			if strings.Contains(cur.Error(), marker) {
				t.Fatalf("unwrap chain leaked marker %q at %v", marker, cur)
			}
		}
	}
	// The failed snapshot must not publish a file.
	assertNoFile(t, out)
}
