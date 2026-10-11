package fakedevice_test

import (
	"bytes"
	"context"
	"image/jpeg"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// snapshotGET issues a GET to the snapshot path with an optional client, and
// returns the status, content type, and body.
func snapshotGET(t *testing.T, client *http.Client, serverURL, path string) (int, string, []byte) {
	t.Helper()
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, serverURL+path, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return response.StatusCode, response.Header.Get("Content-Type"), body
}

func TestSnapshotGETNoAuthReturnsExactSyntheticJPEG(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{})
	server := httptest.NewServer(device)
	defer server.Close()

	status, contentType, body := snapshotGET(t, nil, server.URL, "/onvif/snapshot")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if contentType != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", contentType)
	}
	// The served bytes must equal the fixture bytes exactly and decode as JPEG.
	fixture := readJPEGFixture(t)
	if !bytes.Equal(body, fixture) {
		t.Errorf("served JPEG (%d bytes) does not equal the fixture (%d bytes)", len(body), len(fixture))
	}
	if _, err := jpeg.Decode(bytes.NewReader(body)); err != nil {
		t.Errorf("served JPEG does not decode: %v", err)
	}
}

func TestSnapshotGETBasicAuth(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{
		HTTPAuth: fakedevice.HTTPAuthBasic,
		Username: "operator",
		Password: "camera-secret",
	})
	server := httptest.NewServer(device)
	defer server.Close()

	// Unauthenticated GET is challenged.
	status, _, _ := snapshotGET(t, nil, server.URL, "/onvif/snapshot")
	if status != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", status)
	}

	// Authenticated GET succeeds.
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/onvif/snapshot", nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.SetBasicAuth("operator", "camera-secret")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("authenticated status = %d, want 200", response.StatusCode)
	}
}

func TestSnapshotGETDigestAuth(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{
		HTTPAuth:        fakedevice.HTTPAuthDigest,
		DigestAlgorithm: "SHA-256",
		Username:        "operator",
		Password:        "camera-secret",
	})
	server := httptest.NewServer(device)
	defer server.Close()

	scope, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	transport, err := auth.NewDigestTransport(auth.DigestTransportConfig{
		Scope:       scope,
		Credentials: auth.NewCredentials("operator", "camera-secret"),
	})
	if err != nil {
		t.Fatalf("NewDigestTransport() error = %v", err)
	}
	httpClient := &http.Client{Transport: transport}

	status, contentType, body := snapshotGET(t, httpClient, server.URL, "/onvif/snapshot")
	if status != http.StatusOK {
		t.Fatalf("digest status = %d, want 200", status)
	}
	if contentType != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", contentType)
	}
	if len(body) == 0 {
		t.Error("digest-authenticated snapshot returned empty body")
	}
}

func TestSnapshotGETDoesNotRequireWSSE(t *testing.T) {
	t.Parallel()

	// Even with RequireUsernameToken set (which gates SOAP operations), the
	// snapshot GET must not require a WS-Security header: it is a plain
	// authenticated GET gated only by the HTTP auth policy, which is none here.
	device := newDevice(t, fakedevice.Config{
		RequireUsernameToken: true,
		Username:             "operator",
		Password:             "camera-secret",
	})
	server := httptest.NewServer(device)
	defer server.Close()

	status, contentType, body := snapshotGET(t, nil, server.URL, "/onvif/snapshot")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no WSSE required on GET)", status)
	}
	if contentType != "image/jpeg" || len(body) == 0 {
		t.Errorf("unexpected snapshot response: type=%q len=%d", contentType, len(body))
	}
}

func TestSnapshotGETUnknownPathIsNotServed(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{})
	server := httptest.NewServer(device)
	defer server.Close()

	status, _, _ := snapshotGET(t, nil, server.URL, "/not-the-snapshot")
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 for a non-snapshot GET", status)
	}
}

func TestSnapshotGETRecordsAreBoundedAndSecretSafe(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{
		HTTPAuth:          fakedevice.HTTPAuthBasic,
		Username:          "operator",
		Password:          "camera-secret",
		MaxRequestHistory: 4,
	})
	server := httptest.NewServer(device)
	defer server.Close()

	// Issue more GETs than the history bound, each carrying credentials in the
	// Authorization header and a profile token in the query string.
	for i := 0; i < 10; i++ {
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/onvif/snapshot?profile=camera-secret", nil)
		if err != nil {
			t.Fatalf("NewRequest() error = %v", err)
		}
		request.SetBasicAuth("operator", "camera-secret")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("Do() error = %v", err)
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	}

	records := device.Requests()
	if len(records) > 4 {
		t.Fatalf("request history = %d entries, want <= 4 (bounded)", len(records))
	}
	for _, record := range records {
		if strings.Contains(record.Action, "camera-secret") {
			t.Fatalf("request record leaked a secret: %+v", record)
		}
		if strings.Contains(record.Action, "?") || strings.Contains(record.Action, "profile=") {
			t.Fatalf("request record retained query string: %+v", record)
		}
	}
}

// readJPEGFixture returns the bytes of the synthetic JPEG fixture. The path is
// a fixed repository-relative constant, not derived from external input.
func readJPEGFixture(t *testing.T) []byte {
	t.Helper()
	resolved := filepath.Join("..", "..", "testdata", "fixtures", "jpeg", "synthetic-snapshot.jpg")
	//nolint:gosec // The repository-owned test fixture path is a fixed constant.
	data, err := os.ReadFile(resolved)
	if err != nil {
		t.Fatalf("read JPEG fixture: %v", err)
	}
	return data
}

func TestAuthenticatedSnapshotURIEndToEnd(t *testing.T) {
	t.Parallel()
	device := newDevice(t, fakedevice.Config{
		HTTPAuth:        fakedevice.HTTPAuthDigest,
		DigestAlgorithm: "SHA-256",
		Username:        "operator",
		Password:        "camera-secret",
	})
	server := httptest.NewServer(device)
	defer server.Close()

	scope, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse server URL: %v", err)
	}
	digestTransport, err := auth.NewDigestTransport(auth.DigestTransportConfig{
		Scope:       scope,
		Credentials: auth.NewCredentials("operator", "camera-secret"),
	})
	if err != nil {
		t.Fatalf("NewDigestTransport() error = %v", err)
	}
	httpClient := &http.Client{Transport: digestTransport}
	soapTransport, err := soap.NewClient(soap.Config{HTTPClient: httpClient})
	if err != nil {
		t.Fatalf("soap.NewClient() error = %v", err)
	}
	client, err := media2.NewClient(soapTransport, server.URL)
	if err != nil {
		t.Fatalf("media2.NewClient() error = %v", err)
	}
	snapshotURI, err := client.GetSnapshotUri(context.Background(), "profile_main")
	if err != nil {
		t.Fatalf("GetSnapshotUri() error = %v", err)
	}
	status, contentType, body := snapshotGET(t, httpClient, "", snapshotURI)
	if status != http.StatusOK || contentType != "image/jpeg" {
		t.Fatalf("snapshot response = status %d, type %q", status, contentType)
	}
	if !bytes.Equal(body, readJPEGFixture(t)) {
		t.Fatal("snapshot URI did not return the exact project fixture bytes")
	}
}
