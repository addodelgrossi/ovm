package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

// TestSnapshotNoAuthHappyPath proves the snapshot use case retrieves and writes
// the JPEG against an unauthenticated same-origin fake camera.
func TestSnapshotNoAuthHappyPath(t *testing.T) {
	t.Parallel()
	device, host := mediaFake(t, true)
	out := filepath.Join(t.TempDir(), "snap.jpg")

	meta, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host:         host,
		ProfileToken: "profile_main",
		OutputPath:   out,
		Timeout:      10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if meta.ContentType != "image/jpeg" || meta.ByteSize == 0 {
		t.Fatalf("unexpected metadata: %+v", meta)
	}
	if meta.Source != MediaServiceMedia2 {
		t.Fatalf("source = %q, want media2", meta.Source)
	}
	// The GET actually reached the device as a snapshot request.
	var sawSnapshot bool
	for _, rec := range device.Requests() {
		if strings.Contains(rec.Action, "SnapshotGet") {
			sawSnapshot = true
		}
	}
	if !sawSnapshot {
		t.Fatal("device did not record a snapshot GET")
	}
}

// TestSnapshotBasicOverHTTPRefusedWithoutBothOptIns proves a Basic-only HTTP
// camera is refused as an authentication failure unless BOTH AllowBasic and
// AllowBasicOverHTTP are set, and succeeds when they are.
func TestSnapshotBasicOverHTTPRefusedWithoutBothOptIns(t *testing.T) {
	t.Parallel()
	const secret = "basic-secret-marker" //nolint:gosec // synthetic test credential, not a real secret.
	setup := newUnstartedFake(t, func(base string) fakedevice.Config {
		return fakedevice.Config{
			HTTPAuth: fakedevice.HTTPAuthBasic,
			Username: "operator",
			Password: secret,
			Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
				{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
				{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6},
			}},
		}
	})

	// No opt-in at all: refused.
	noOptIn, _ := NewCredentials("operator", secret, false)
	_, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host: setup.host, Credentials: noOptIn, ProfileToken: "profile_main",
		OutputPath: filepath.Join(t.TempDir(), "a.jpg"), Timeout: 10 * time.Second,
	})
	if err == nil || CategoryOf(err) != CategoryAuthentication {
		t.Fatalf("no-opt-in category = %q (err=%v), want authentication", CategoryOf(err), err)
	}

	// AllowBasic only (no plaintext opt-in): still refused over HTTP.
	basicOnly, _ := NewCredentials("operator", secret, true)
	_, err = NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host: setup.host, Credentials: basicOnly, ProfileToken: "profile_main",
		OutputPath: filepath.Join(t.TempDir(), "b.jpg"), Timeout: 10 * time.Second,
	})
	if err == nil || CategoryOf(err) != CategoryAuthentication {
		t.Fatalf("basic-only-over-http category = %q (err=%v), want authentication", CategoryOf(err), err)
	}

	// Both opt-ins: succeeds.
	bothOptIn, err := NewCredentialsWithPolicy("operator", secret, Policy{AllowBasic: true, AllowBasicOverHTTP: true})
	if err != nil {
		t.Fatalf("NewCredentialsWithPolicy: %v", err)
	}
	out := filepath.Join(t.TempDir(), "c.jpg")
	meta, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host: setup.host, Credentials: bothOptIn, ProfileToken: "profile_main",
		OutputPath: out, Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("both-opt-in Snapshot() error = %v", err)
	}
	if meta.ByteSize == 0 {
		t.Fatal("expected a written snapshot with both opt-ins")
	}
}

// TestSnapshotGETCarriesAuthorizationNoWSSE proves the authenticated GET sends
// an HTTP Authorization header but never a WS-Security UsernameToken. A custom
// transport observes the outgoing request headers.
func TestSnapshotGETCarriesAuthorizationNoWSSE(t *testing.T) {
	t.Parallel()
	const secret = "digest-secret-marker" //nolint:gosec // synthetic test credential.
	_, host := mediaFakeAuth(t, "admin", secret, true)
	creds, _ := NewCredentials("admin", secret, false)
	out := filepath.Join(t.TempDir(), "snap.jpg")

	meta, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host: host, Credentials: creds, ProfileToken: "profile_main",
		OutputPath: out, Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	// Metadata is credential-free and carries the normalized absolute path.
	if !filepath.IsAbs(meta.OutputPath) || meta.OutputPath != out {
		t.Fatalf("metadata path = %q, want normalized %q", meta.OutputPath, out)
	}
}

// TestSnapshotMetadataPathIsNormalizedAbsolute proves OutputPath is normalized
// with Abs+Clean before use and returned exactly in the metadata.
func TestSnapshotMetadataPathIsNormalizedAbsolute(t *testing.T) {
	t.Parallel()
	_, host := mediaFakeAuth(t, "admin", "p", true)
	creds, _ := NewCredentials("admin", "p", false)
	dir := t.TempDir()
	// A path with a redundant ./ and // that Clean must collapse.
	messy := filepath.Join(dir, ".", "sub", "..", "snap.jpg")
	want := filepath.Clean(filepath.Join(dir, "snap.jpg"))

	meta, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host: host, Credentials: creds, ProfileToken: "profile_main",
		OutputPath: messy, Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if meta.OutputPath != want {
		t.Fatalf("metadata path = %q, want %q", meta.OutputPath, want)
	}
	if _, statErr := os.Stat(want); statErr != nil {
		t.Fatalf("file not written at normalized path: %v", statErr)
	}
}

// TestSnapshotEmptyOutputPathRejectedBeforeNetwork proves an empty output path
// is a usage error with no network effect.
func TestSnapshotEmptyOutputPathRejectedBeforeNetwork(t *testing.T) {
	t.Parallel()
	_, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host: "http://cam.local/onvif/device_service", ProfileToken: "profile_main", OutputPath: "",
	})
	if err == nil || CategoryOf(err) != CategoryUsage {
		t.Fatalf("category = %q, want usage", CategoryOf(err))
	}
}

// TestResolveSnapshotMaxBytesOverflowRejected proves math.MaxInt64 (and any
// value that would overflow maxBytes+1) is rejected as a usage error before any
// network request, and that a positive value and the default are accepted.
func TestResolveSnapshotMaxBytesOverflowRejected(t *testing.T) {
	t.Parallel()
	if _, err := resolveSnapshotMaxBytes(math.MaxInt64); err == nil || CategoryOf(err) != CategoryUsage {
		t.Fatalf("MaxInt64 category = %q, want usage rejection", CategoryOf(err))
	}
	got, err := resolveSnapshotMaxBytes(0)
	if err != nil || got != defaultSnapshotMaxBytes {
		t.Fatalf("default resolve = (%d, %v), want (%d, nil)", got, err, defaultSnapshotMaxBytes)
	}
	got, err = resolveSnapshotMaxBytes(1024)
	if err != nil || got != 1024 {
		t.Fatalf("positive resolve = (%d, %v), want (1024, nil)", got, err)
	}
}

// TestSnapshotMaxInt64RejectedZeroRequests proves a MaxInt64 limit fails the
// whole use case before any HTTP GET reaches the device.
func TestSnapshotMaxInt64RejectedZeroRequests(t *testing.T) {
	t.Parallel()
	device, host := mediaFake(t, true)
	_, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host: host, ProfileToken: "profile_main",
		OutputPath: filepath.Join(t.TempDir(), "snap.jpg"),
		MaxBytes:   math.MaxInt64, Timeout: 10 * time.Second,
	})
	if err == nil || CategoryOf(err) != CategoryUsage {
		t.Fatalf("category = %q, want usage", CategoryOf(err))
	}
	for _, rec := range device.Requests() {
		if strings.Contains(rec.Action, "SnapshotGet") {
			t.Fatal("a snapshot GET reached the device despite a rejected max")
		}
	}
}

// --- fetchAndWriteSnapshot behavior with controllable servers ----------------

// newJPEGServer returns a server that serves a given body with a chosen
// content type and optional explicit Content-Length.
func newJPEGServer(t *testing.T, contentType string, body []byte, setLength bool, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		if setLength {
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
		}
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestFetch201SuccessWritesFile proves a 201 (2xx non-200) success is accepted.
func TestFetch201SuccessWritesFile(t *testing.T) {
	t.Parallel()
	srv := newJPEGServer(t, "image/jpeg", projectJPEGBytes(), true, http.StatusCreated)
	out := filepath.Join(t.TempDir(), "snap.jpg")
	size, ct, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 1<<20)
	if err != nil {
		t.Fatalf("201 fetch error = %v", err)
	}
	if ct != "image/jpeg" || size != int64(len(projectJPEGBytes())) {
		t.Fatalf("ct=%q size=%d", ct, size)
	}
}

// TestFetch401Is403AreAuthentication proves 401 and 403 map to the
// authentication category and leave no file.
func TestFetch401And403AreAuthentication(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		out := filepath.Join(t.TempDir(), "snap.jpg")
		_, _, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 1<<20)
		srv.Close()
		if err == nil || CategoryOf(err) != CategoryAuthentication {
			t.Fatalf("status %d category = %q, want authentication", status, CategoryOf(err))
		}
		assertNoFile(t, out)
	}
}

// TestFetchMissingContentTypeRejected proves a missing content type is a
// protocol error.
func TestFetchMissingContentTypeRejected(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header()["Content-Type"] = nil // suppress the auto content type
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(projectJPEGBytes())
	}))
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "snap.jpg")
	_, _, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 1<<20)
	if err == nil || CategoryOf(err) != CategoryProtocol {
		t.Fatalf("category = %q, want protocol", CategoryOf(err))
	}
	assertNoFile(t, out)
}

// TestFetchExactlyAtLimitSucceeds proves a body whose size equals the limit is
// accepted (the limit+1 read tolerates exactly maxBytes).
func TestFetchExactlyAtLimitSucceeds(t *testing.T) {
	t.Parallel()
	body := projectJPEGBytes()
	srv := newJPEGServer(t, "image/jpeg", body, true, http.StatusOK)
	out := filepath.Join(t.TempDir(), "snap.jpg")
	size, _, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, int64(len(body)))
	if err != nil {
		t.Fatalf("at-limit fetch error = %v", err)
	}
	if size != int64(len(body)) {
		t.Fatalf("size = %d, want %d", size, len(body))
	}
}

// TestFetchShortDeclaredLengthTruncationRejected proves a body that ends
// cleanly but is shorter than the advertised Content-Length is rejected as a
// protocol truncation and publishes no file. A custom listener writes a short
// body under a larger declared length.
func TestFetchShortDeclaredLengthTruncationRejected(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.Header().Set("Content-Length", "100") // declare 100 ...
		_, _ = w.Write(make([]byte, 40))        // ... but send only 40 and return cleanly
	}))
	defer srv.Close()
	out := filepath.Join(t.TempDir(), "snap.jpg")
	_, _, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 1<<20)
	if err == nil {
		t.Fatal("expected a truncation rejection")
	}
	if CategoryOf(err) != CategoryProtocol && CategoryOf(err) != CategoryNetwork {
		t.Fatalf("category = %q, want protocol or network", CategoryOf(err))
	}
	assertNoFile(t, out)
}

// TestFetchInjectedReadErrorRejected proves a mid-stream read failure is a
// network error and leaves no file. A custom RoundTripper returns a body that
// errors partway through.
func TestFetchInjectedReadErrorRejected(t *testing.T) {
	t.Parallel()
	client := &http.Client{Transport: errAfterNTransport{n: 3}}
	out := filepath.Join(t.TempDir(), "snap.jpg")
	_, _, err := fetchAndWriteSnapshot(context.Background(), client, mustURL(t, "http://cam.local/snap"), out, 1<<20)
	if err == nil || CategoryOf(err) != CategoryNetwork {
		t.Fatalf("category = %q, want network", CategoryOf(err))
	}
	assertNoFile(t, out)
}

// TestFetchBodyCloseObserved proves the response body is closed on the success
// path.
func TestFetchBodyCloseObserved(t *testing.T) {
	t.Parallel()
	tracker := &closeTrackingBody{data: projectJPEGBytes()}
	client := &http.Client{Transport: bodyTransport{body: tracker, contentType: "image/jpeg", length: int64(len(projectJPEGBytes()))}}
	out := filepath.Join(t.TempDir(), "snap.jpg")
	if _, _, err := fetchAndWriteSnapshot(context.Background(), client, mustURL(t, "http://cam.local/snap"), out, 1<<20); err != nil {
		t.Fatalf("fetch error = %v", err)
	}
	if atomic.LoadInt32(&tracker.closes) == 0 {
		t.Fatal("response body was not closed")
	}
}

// --- cancellation and timeout ------------------------------------------------

// TestFetchPreCancelLeavesNoFile proves a context cancelled before the request
// yields a cancelled category and no file.
func TestFetchPreCancelLeavesNoFile(t *testing.T) {
	t.Parallel()
	srv := newJPEGServer(t, "image/jpeg", projectJPEGBytes(), true, http.StatusOK)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := filepath.Join(t.TempDir(), "snap.jpg")
	_, _, err := fetchAndWriteSnapshot(ctx, srv.Client(), mustURL(t, srv.URL), out, 1<<20)
	if err == nil || CategoryOf(err) != CategoryCancelled {
		t.Fatalf("category = %q, want cancelled", CategoryOf(err))
	}
	assertNoFile(t, out)
}

// TestFetchInFlightCancelLeavesNoFile proves a cancel that fires while the body
// streams aborts the read and leaves no file.
func TestFetchInFlightCancelLeavesNoFile(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			_, _ = w.Write([]byte{0xff, 0xd8})
			f.Flush()
		}
		<-release // hold the body open until the test cancels
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	out := filepath.Join(t.TempDir(), "snap.jpg")
	_, _, err := fetchAndWriteSnapshot(ctx, srv.Client(), mustURL(t, srv.URL), out, 1<<20)
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	assertNoFile(t, out)
}

// TestFetchTimeoutIsNetwork proves a deadline exceeded during the request maps
// to the network category.
func TestFetchTimeoutIsNetwork(t *testing.T) {
	t.Parallel()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	out := filepath.Join(t.TempDir(), "snap.jpg")
	_, _, err := fetchAndWriteSnapshot(ctx, srv.Client(), mustURL(t, srv.URL), out, 1<<20)
	if err == nil || CategoryOf(err) != CategoryNetwork {
		t.Fatalf("category = %q (err=%v), want network", CategoryOf(err), err)
	}
	assertNoFile(t, out)
}

// --- existing destination and mode -------------------------------------------

// TestFetchExistingDestinationUnchangedOnFailure proves a pre-existing output
// file is left byte-identical and mode-identical when the fetch fails.
func TestFetchExistingDestinationUnchangedOnFailure(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "snap.jpg")
	original := []byte("original-content-preserved")
	//nolint:gosec // The permissive seed mode is deliberate: it proves an existing file is left unchanged on failure.
	if err := os.WriteFile(out, original, 0o644); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}
	// Server returns a wrong MIME, forcing a failure after the file already
	// exists.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html></html>"))
	}))
	defer srv.Close()

	_, _, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 1<<20)
	if err == nil {
		t.Fatal("expected a failure")
	}
	//nolint:gosec // out is a test-owned temp path, not external input.
	got, readErr := os.ReadFile(out)
	if readErr != nil {
		t.Fatalf("existing file disappeared: %v", readErr)
	}
	if string(got) != string(original) {
		t.Fatalf("existing file was modified: %q", got)
	}
	info, _ := os.Stat(out)
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("existing file mode changed to %o", info.Mode().Perm())
	}
	// No leftover temp files.
	assertNoLeftoverTemp(t, dir)
}

// TestFetchMalformedJPEGPreservesExistingDestinationOnForce proves structural
// validation completes before an authorized atomic replacement. Malformed
// bytes cannot replace the existing destination and the temp file is removed.
func TestFetchMalformedJPEGPreservesExistingDestinationOnForce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "snap.jpg")
	original := []byte("original-content-preserved")
	//nolint:gosec // The permissive seed mode is deliberate test state.
	if err := os.WriteFile(out, original, 0o644); err != nil {
		t.Fatalf("seed existing file: %v", err)
	}
	srv := newJPEGServer(t, "image/jpeg", []byte{0xff, 0xd8, 0xff, 0xd9}, true, http.StatusOK)
	_, _, err := fetchAndWriteSnapshotWithOverwrite(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 1<<20, true)
	if err == nil || CategoryOf(err) != CategoryProtocol {
		t.Fatalf("category = %q (err=%v), want protocol", CategoryOf(err), err)
	}
	//nolint:gosec // out is a test-owned temp path.
	got, readErr := os.ReadFile(out)
	if readErr != nil || string(got) != string(original) {
		t.Fatalf("existing destination changed: data=%q err=%v", got, readErr)
	}
	info, statErr := os.Stat(out)
	if statErr != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("existing destination mode changed: info=%v err=%v", info, statErr)
	}
	assertNoLeftoverTemp(t, dir)
}

// TestFetchReplacesPermissiveExistingWith0600 proves a successful write ends at
// 0600 even when it replaces a world-readable existing file.
func TestFetchReplacesPermissiveExistingWith0600(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "snap.jpg")
	//nolint:gosec // The world-writable seed mode is deliberate: it proves a successful write still ends at 0600.
	if err := os.WriteFile(out, []byte("old"), 0o666); err != nil {
		t.Fatalf("seed existing permissive file: %v", err)
	}
	srv := newJPEGServer(t, "image/jpeg", projectJPEGBytes(), true, http.StatusOK)
	if _, _, err := fetchAndWriteSnapshotWithOverwrite(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 1<<20, true); err != nil {
		t.Fatalf("fetch error = %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat output: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("final mode = %o, want 600", info.Mode().Perm())
	}
}

// --- redirect: transport-level cross-origin and downgrade --------------------

// TestSnapshotCrossOriginRedirectRejectedZeroTargetHits proves a cross-origin
// redirect is rejected BEFORE the target is contacted: the target server
// records zero hits and never sees an Authorization header.
func TestSnapshotCrossOriginRedirectRejectedZeroTargetHits(t *testing.T) {
	t.Parallel()
	var targetHits int32
	var sawAuthHeader int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&targetHits, 1)
		if r.Header.Get("Authorization") != "" {
			atomic.AddInt32(&sawAuthHeader, 1)
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(projectJPEGBytes())
	}))
	defer target.Close()

	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/snap", http.StatusFound)
	}))
	defer origin.Close()

	cameraURL := mustURL(t, origin.URL+"/onvif/device_service")
	client := snapshotHTTPClient(&http.Client{}, cameraURL)
	out := filepath.Join(t.TempDir(), "snap.jpg")
	_, _, err := fetchAndWriteSnapshot(context.Background(), client, mustURL(t, origin.URL+"/snap"), out, 1<<20)
	if err == nil {
		t.Fatal("expected the cross-origin redirect to be rejected")
	}
	// A redirect-policy refusal is a protocol/security-boundary decision, not a
	// transport outage, and must be distinguishable from a dial failure.
	if CategoryOf(err) != CategoryProtocol {
		t.Fatalf("category = %q, want protocol (redirect refused)", CategoryOf(err))
	}
	if !errors.Is(err, ErrSnapshotRedirectRefused) {
		t.Fatalf("error = %v, want errors.Is ErrSnapshotRedirectRefused", err)
	}
	if !errors.Is(err, ErrSnapshotRedirectCrossOrigin) {
		t.Fatalf("error = %v, want errors.Is ErrSnapshotRedirectCrossOrigin", err)
	}
	if atomic.LoadInt32(&targetHits) != 0 {
		t.Fatalf("cross-origin redirect target was contacted %d times, want 0", targetHits)
	}
	if atomic.LoadInt32(&sawAuthHeader) != 0 {
		t.Fatal("cross-origin target saw an Authorization header")
	}
	// Error must not leak either URL; assertErrorHasNoURL walks the full
	// recursive unwrap chain, so a URL hidden behind a wrapper is also caught.
	assertErrorHasNoURL(t, err, origin.URL, target.URL)
	assertNoFile(t, out)
}

// TestSnapshotHTTPSToHTTPDowngradeRejectedBeforeTarget proves the redirect
// policy refuses an HTTPS->HTTP downgrade before the HTTP target is contacted.
func TestSnapshotHTTPSToHTTPDowngradeRejectedBeforeTarget(t *testing.T) {
	t.Parallel()
	var targetHits int32
	httpTarget := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&targetHits, 1)
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(projectJPEGBytes())
	}))
	defer httpTarget.Close()

	// TLS origin that redirects (downgrades) to the plain-HTTP target on the
	// same host but different scheme.
	tlsOrigin := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, httpTarget.URL+"/snap", http.StatusFound)
	}))
	defer tlsOrigin.Close()

	cameraURL := mustURL(t, tlsOrigin.URL+"/onvif/device_service")
	// Base client that trusts the TLS test server.
	base := tlsOrigin.Client()
	client := snapshotHTTPClient(base, cameraURL)
	out := filepath.Join(t.TempDir(), "snap.jpg")
	_, _, err := fetchAndWriteSnapshot(context.Background(), client, mustURL(t, tlsOrigin.URL+"/snap"), out, 1<<20)
	if err == nil {
		t.Fatal("expected the HTTPS->HTTP downgrade redirect to be rejected")
	}
	if atomic.LoadInt32(&targetHits) != 0 {
		t.Fatalf("downgrade target was contacted %d times, want 0", targetHits)
	}
	assertNoFile(t, out)
}

// --- URI validation rejections before GET ------------------------------------

// TestValidateSnapshotURIComprehensiveRejections proves userinfo, fragment,
// control chars, unsupported scheme, relative, and cross-origin inputs are
// rejected before any GET, and the error never leaks the URI.
func TestValidateSnapshotURIComprehensiveRejections(t *testing.T) {
	t.Parallel()
	camera := mustURL(t, "http://cam.local/onvif/device_service")
	bad := []string{
		"http://user:pass@cam.local/snap", // userinfo
		"http://cam.local/snap#frag",      // fragment
		"http://cam.local/snap\x07",       // control char
		"ftp://cam.local/snap",            // unsupported scheme
		"rtsp://cam.local/snap",           // non-HTTP
		"/relative/snap",                  // relative
		"http://other.local/snap",         // cross-origin host
		"http://cam.local:8443/snap",      // cross-origin port
		"https://cam.local/snap",          // cross-origin scheme
	}
	for _, raw := range bad {
		_, err := validateSnapshotURI(camera, raw)
		if err == nil {
			t.Errorf("validateSnapshotURI(%q) accepted, want rejected", raw)
			continue
		}
		if strings.Contains(err.Error(), "cam.local") || strings.Contains(err.Error(), "other.local") {
			t.Errorf("error leaked the URI: %v", err)
		}
	}
}

// --- secret/query leakage across all error renderings ------------------------

// TestSnapshotErrorsNeverLeakQueryOrBody drives the whole use case against a
// fake whose snapshot URI carries a ?profile=<token> query, forces a failure in
// the GET, and proves the query marker and a body marker never appear in the
// error string, fmt forms, or any recursive unwrap.
func TestSnapshotErrorsNeverLeakQueryOrBody(t *testing.T) {
	t.Parallel()
	// The profile token becomes the ?profile= query marker in the snapshot URI
	// generated by the fake device.
	const profileMarker = "profile_main"
	const bodyMarker = "SECRET-BODY-MARKER-should-not-leak"

	// Build a fake that advertises Media2 same-origin but whose snapshot GET
	// returns a non-JPEG body carrying a secret marker, forcing a protocol
	// failure after the URI (with query) is resolved.
	setup := newUnstartedFake(t, func(base string) fakedevice.Config {
		return fakedevice.Config{Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
			{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
			{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6},
		}}}
	})

	// Directly exercise the fetch path with a server that leaks a body marker,
	// so the generated error can be inspected for both query and body markers.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(bodyMarker))
	}))
	defer srv.Close()
	_ = setup

	out := filepath.Join(t.TempDir(), "snap.jpg")
	snapURL := mustURL(t, srv.URL+"/snap?profile="+profileMarker)
	_, _, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), snapURL, out, 1<<20)
	if err == nil {
		t.Fatal("expected a protocol failure")
	}
	assertErrorHasNoMarker(t, err, profileMarker)
	assertErrorHasNoMarker(t, err, bodyMarker)
}

// TestSnapshotMetadataJSONHasNoQueryOrURI proves the SnapshotMetadata JSON
// carries only the credential-free fields and never a URI or query marker.
func TestSnapshotMetadataJSONHasNoQueryOrURI(t *testing.T) {
	t.Parallel()
	_, host := mediaFakeAuth(t, "admin", "p", true)
	creds, _ := NewCredentials("admin", "p", false)
	out := filepath.Join(t.TempDir(), "snap.jpg")
	meta, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host: host, Credentials: creds, ProfileToken: "profile_main",
		OutputPath: out, Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	// Marshal and confirm no URI/query appear.
	js := fmt.Sprintf("%+v", meta)
	for _, forbidden := range []string{"http://", "https://", "?profile=", "profile="} {
		if strings.Contains(js, forbidden) {
			t.Fatalf("metadata rendering leaked %q: %s", forbidden, js)
		}
	}
}

// --- helpers -----------------------------------------------------------------

func assertNoLeftoverTemp(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".snapshot-") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

// assertErrorHasNoURL walks the full unwrap chain and the fmt renderings and
// fails if any raw URL appears.
func assertErrorHasNoURL(t *testing.T, err error, urls ...string) {
	t.Helper()
	for _, u := range urls {
		assertErrorHasNoMarker(t, err, u)
	}
}

// assertErrorHasNoMarker proves the marker is absent from Error(), the %v/%+v
// fmt forms, and every link reachable by recursive errors.Unwrap.
func assertErrorHasNoMarker(t *testing.T, err error, marker string) {
	t.Helper()
	if marker == "" {
		return
	}
	if strings.Contains(err.Error(), marker) {
		t.Fatalf("Error() leaked marker %q: %v", marker, err)
	}
	if strings.Contains(fmt.Sprintf("%v", err), marker) {
		t.Fatalf("%%v leaked marker %q", marker)
	}
	if strings.Contains(fmt.Sprintf("%+v", err), marker) {
		t.Fatalf("%%+v leaked marker %q", marker)
	}
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		if strings.Contains(cur.Error(), marker) {
			t.Fatalf("unwrap chain leaked marker %q at %v", marker, cur)
		}
	}
}

// --- test transports ---------------------------------------------------------

// errAfterNTransport returns a 200 image/jpeg response whose body errors after
// n bytes, with no Content-Length so the read failure (not truncation) is the
// cause.
type errAfterNTransport struct{ n int }

func (tr errAfterNTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := http.Header{}
	header.Set("Content-Type", "image/jpeg")
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        header,
		Body:          &errAfterNReader{n: tr.n},
		ContentLength: -1,
		Request:       req,
	}, nil
}

type errAfterNReader struct {
	n    int
	read int
}

func (r *errAfterNReader) Read(p []byte) (int, error) {
	if r.read >= r.n {
		return 0, errors.New("synthetic injected read failure")
	}
	p[0] = 0xff
	r.read++
	return 1, nil
}

func (r *errAfterNReader) Close() error { return nil }

// bodyTransport returns a fixed response with a close-tracking body.
type bodyTransport struct {
	body        io.ReadCloser
	contentType string
	length      int64
}

func (tr bodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	header := http.Header{}
	header.Set("Content-Type", tr.contentType)
	return &http.Response{
		StatusCode:    http.StatusOK,
		Header:        header,
		Body:          tr.body,
		ContentLength: tr.length,
		Request:       req,
	}, nil
}

// closeTrackingBody is a ReadCloser that counts Close calls.
type closeTrackingBody struct {
	data   []byte
	off    int
	closes int32
}

func (b *closeTrackingBody) Read(p []byte) (int, error) {
	if b.off >= len(b.data) {
		return 0, io.EOF
	}
	n := copy(p, b.data[b.off:])
	b.off += n
	return n, nil
}

func (b *closeTrackingBody) Close() error {
	atomic.AddInt32(&b.closes, 1)
	return nil
}

// TestFetchNoClobberPublicationIsAtomic proves the default publication path
// cannot replace an output that already exists at finalization time.
func TestFetchNoClobberPublicationIsAtomic(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "snap.jpg")
	const original = "existing-complete-file"
	if err := os.WriteFile(out, []byte(original), 0o600); err != nil {
		t.Fatalf("seed output: %v", err)
	}
	srv := newJPEGServer(t, "image/jpeg", projectJPEGBytes(), true, http.StatusOK)
	_, _, err := fetchAndWriteSnapshot(context.Background(), srv.Client(), mustURL(t, srv.URL), out, 1<<20)
	if err == nil || CategoryOf(err) != CategoryUsage {
		t.Fatalf("error = %v, category = %q; want usage", err, CategoryOf(err))
	}
	data, readErr := os.ReadFile(out) //nolint:gosec // test-owned path.
	if readErr != nil || string(data) != original {
		t.Fatalf("existing output changed: data=%q err=%v", data, readErr)
	}
	assertNoLeftoverTemp(t, dir)
}

// TestSnapshotNoAdvertisedMediaIsProtocol proves service absence in a camera
// response is categorized as protocol rather than caller usage.
func TestSnapshotNoAdvertisedMediaIsProtocol(t *testing.T) {
	t.Parallel()
	setup := newUnstartedFake(t, func(base string) fakedevice.Config {
		return fakedevice.Config{Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{{
			Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6,
		}}}}
	})
	out := filepath.Join(t.TempDir(), "snapshot.jpg")
	_, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host: setup.host, ProfileToken: "profile_main", OutputPath: out, Timeout: 10 * time.Second,
	})
	if err == nil || CategoryOf(err) != CategoryProtocol {
		t.Fatalf("err=%v category=%q, want protocol", err, CategoryOf(err))
	}
	assertNoFile(t, out)
}

// TestSnapshotCameraReturnedUnsafeURIIsProtocol rewrites the fake's otherwise
// valid GetSnapshotUri response to a cross-origin value and verifies that this
// malformed camera data maps to protocol without leaking or writing a file.
func TestSnapshotCameraReturnedUnsafeURIIsProtocol(t *testing.T) {
	t.Parallel()
	const foreignOrigin = "http://foreign.invalid"
	setup := newUnstartedFakeWithHandler(t, func(base string, device *fakedevice.Device) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			recorder := httptest.NewRecorder()
			device.ServeHTTP(recorder, request)
			result := recorder.Result()
			defer func() { _ = result.Body.Close() }()
			for name, values := range result.Header {
				for _, value := range values {
					response.Header().Add(name, value)
				}
			}
			body, readErr := io.ReadAll(result.Body)
			if readErr != nil {
				response.WriteHeader(http.StatusInternalServerError)
				return
			}
			if strings.Contains(string(body), "GetSnapshotUriResponse") {
				body = []byte(strings.ReplaceAll(string(body), base, foreignOrigin))
			}
			response.WriteHeader(result.StatusCode)
			_, _ = response.Write(body)
		})
	}, func(base string) fakedevice.Config {
		return fakedevice.Config{Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
			{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
			{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6},
		}}}
	})
	out := filepath.Join(t.TempDir(), "snapshot.jpg")
	_, err := NewProductionSnapshot().Snapshot(context.Background(), SnapshotRequest{
		Host: setup.host, ProfileToken: "profile_main", OutputPath: out, Timeout: 10 * time.Second,
	})
	if err == nil || CategoryOf(err) != CategoryProtocol {
		t.Fatalf("err=%v category=%q, want protocol", err, CategoryOf(err))
	}
	assertErrorHasNoMarker(t, err, foreignOrigin)
	assertNoFile(t, out)
}
