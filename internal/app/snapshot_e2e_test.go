package app

import (
	"bytes"
	"context"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

// mediaFakeAuth starts a fake device that requires HTTP Digest and a
// WS-Security UsernameToken, advertising same-origin media services. It returns
// the device and the canonical device endpoint.
func mediaFakeAuth(t *testing.T, username, password string, media2 bool) (*fakedevice.Device, string) {
	t.Helper()

	// Build an unstarted server to learn its origin, then install a handler
	// whose GetServices advertises that same origin for every media service.
	srv := newUnstartedFake(t, func(base string) fakedevice.Config {
		services := []fakedevice.ServiceData{
			{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
			{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: base + "/onvif/media_service", VersionMajor: 25, VersionMinor: 6},
		}
		if media2 {
			services = append(services, fakedevice.ServiceData{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6})
		}
		return fakedevice.Config{
			HTTPAuth:             fakedevice.HTTPAuthDigest,
			DigestAlgorithm:      "SHA-256",
			RequireUsernameToken: true,
			Username:             username,
			Password:             password,
			Inventory:            fakedevice.Inventory{Services: services},
		}
	})
	return srv.device, srv.host
}

// TestSnapshotRetrievesJPEGAtomically proves the snapshot use case resolves the
// URI (Media2 preferred), performs an authenticated same-origin HTTP GET, and
// writes a credential-free JPEG to disk, returning only metadata.
func TestSnapshotRetrievesJPEGAtomically(t *testing.T) {
	t.Parallel()
	secret := "snapshot-secret-" + t.Name()
	_, host := mediaFakeAuth(t, "admin", secret, true)

	creds, err := NewCredentials("admin", secret, false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
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
	if meta.ContentType != "image/jpeg" {
		t.Fatalf("content type = %q, want image/jpeg", meta.ContentType)
	}
	if meta.Source != MediaServiceMedia2 {
		t.Fatalf("source = %q, want media2", meta.Source)
	}
	if meta.ProfileToken != "profile_main" {
		t.Fatalf("profile token = %q", meta.ProfileToken)
	}
	info, statErr := os.Stat(out)
	if statErr != nil {
		t.Fatalf("output not written: %v", statErr)
	}
	if info.Size() != meta.ByteSize || meta.ByteSize == 0 {
		t.Fatalf("size mismatch: stat %d, meta %d", info.Size(), meta.ByteSize)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("file perm = %o, want 600", perm)
	}
	//nolint:gosec // out is a test-owned temp path, not external input.
	data, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatalf("read snapshot: %v", readErr)
	}
	decoded, decodeErr := jpeg.Decode(bytes.NewReader(data))
	if decodeErr != nil || decoded.Bounds().Dx() <= 0 || decoded.Bounds().Dy() <= 0 {
		t.Fatalf("written file is not a complete JPEG: %v", decodeErr)
	}
}

// TestSnapshotMaxBytesRejectsOversize proves a small max is enforced via the
// Content-Length early check and no file is left behind.
func TestSnapshotMaxBytesRejectsOversize(t *testing.T) {
	t.Parallel()
	secret := "oversize-secret-" + t.Name()
	_, host := mediaFakeAuth(t, "admin", secret, true)
	creds, _ := NewCredentials("admin", secret, false)
	out := filepath.Join(t.TempDir(), "snap.jpg")

	_, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host:         host,
		Credentials:  creds,
		ProfileToken: "profile_main",
		OutputPath:   out,
		MaxBytes:     8, // smaller than the synthetic JPEG
		Timeout:      10 * time.Second,
	})
	if err == nil {
		t.Fatal("expected an oversize rejection")
	}
	if CategoryOf(err) != CategoryProtocol {
		t.Fatalf("category = %q, want protocol", CategoryOf(err))
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("temp/output file must not exist after oversize rejection, stat err = %v", statErr)
	}
	// No leftover temp files in the directory.
	entries, _ := os.ReadDir(filepath.Dir(out))
	if len(entries) != 0 {
		t.Fatalf("directory not clean after rejection: %v", entries)
	}
}

// TestSnapshotCancellationLeavesNoFile proves a cancelled context aborts the
// retrieval, returns a cancelled error, and leaves no temp or output file.
func TestSnapshotCancellationLeavesNoFile(t *testing.T) {
	t.Parallel()
	secret := "cancel-secret-" + t.Name()
	_, host := mediaFakeAuth(t, "admin", secret, true)
	creds, _ := NewCredentials("admin", secret, false)
	out := filepath.Join(t.TempDir(), "snap.jpg")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before starting

	_, err := NewProductionSnapshot().Snapshot(ctx, SnapshotRequest{
		Host:         host,
		Credentials:  creds,
		ProfileToken: "profile_main",
		OutputPath:   out,
		Timeout:      10 * time.Second,
	})
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("no file should exist after cancellation, stat err = %v", statErr)
	}
}

// TestSnapshotSelectsDeterministicPrimaryProfile proves an omitted token is
// resolved in the same authenticated session to the lexicographically first
// non-empty profile token, independent of device response order.
func TestSnapshotSelectsDeterministicPrimaryProfile(t *testing.T) {
	t.Parallel()
	_, host := mediaFake(t, true)
	out := filepath.Join(t.TempDir(), "primary.jpg")

	meta, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host:       host,
		OutputPath: out,
		Timeout:    10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if meta.ProfileToken != "profile_main" {
		t.Fatalf("profile token = %q, want profile_main", meta.ProfileToken)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("selected-profile snapshot was not written: %v", err)
	}
}

// TestSnapshotNoClobberUnlessOverwrite proves the default rejects an existing
// destination before network I/O and explicit overwrite atomically replaces it.
func TestSnapshotNoClobberUnlessOverwrite(t *testing.T) {
	t.Parallel()
	device, host := mediaFake(t, true)
	dir := t.TempDir()
	out := filepath.Join(dir, "snap.jpg")
	const original = "preserve-this-file"
	if err := os.WriteFile(out, []byte(original), 0o600); err != nil {
		t.Fatalf("seed output: %v", err)
	}

	_, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host:         host,
		ProfileToken: "profile_main",
		OutputPath:   out,
		Timeout:      10 * time.Second,
	})
	if err == nil || CategoryOf(err) != CategoryUsage {
		t.Fatalf("no-clobber error = %v, category = %q; want usage", err, CategoryOf(err))
	}
	data, readErr := os.ReadFile(out) //nolint:gosec // test-owned path.
	if readErr != nil || string(data) != original {
		t.Fatalf("existing output changed: data=%q err=%v", data, readErr)
	}
	if got := len(device.Requests()); got != 0 {
		t.Fatalf("device requests = %d, want 0 for preflight no-clobber rejection", got)
	}

	meta, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host:         host,
		ProfileToken: "profile_main",
		OutputPath:   out,
		Overwrite:    true,
		Timeout:      10 * time.Second,
	})
	if err != nil {
		t.Fatalf("overwrite Snapshot() error = %v", err)
	}
	data, readErr = os.ReadFile(out) //nolint:gosec // test-owned path.
	if readErr != nil || string(data) == original || int64(len(data)) != meta.ByteSize {
		t.Fatalf("output was not replaced atomically: size=%d meta=%d err=%v", len(data), meta.ByteSize, readErr)
	}
	info, statErr := os.Stat(out)
	if statErr != nil {
		t.Fatalf("stat overwritten output: %v", statErr)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("overwritten output mode = %v, want 0600", info.Mode().Perm())
	}
}
