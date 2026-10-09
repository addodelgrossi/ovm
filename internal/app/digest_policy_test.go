package app

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// recordingRoundTripper records whether it was reached and returns a canned
// 204 response. It lets a test assert whether the policy transport allowed a
// request through to the (would-be) network.
type recordingRoundTripper struct {
	reached bool
}

func (r *recordingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.reached = true
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Body:       http.NoBody,
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func newRequestWithAuth(t *testing.T, authorization string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "http://camera.invalid/onvif/device_service", strings.NewReader(""))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if authorization != "" {
		req.Header.Set("Authorization", authorization)
	}
	return req
}

// TestDigestPolicyBlocksNoQOPByDefault proves a no-qop Digest Authorization is
// refused before the base transport is reached, and classified as
// authentication, when legacy is not permitted.
func TestDigestPolicyBlocksNoQOPByDefault(t *testing.T) {
	base := &recordingRoundTripper{}
	policy, err := newDigestPolicyTransport(base, false)
	if err != nil {
		t.Fatalf("newDigestPolicyTransport: %v", err)
	}
	noQOP := `Digest username="admin", realm="r", nonce="n", uri="/x", response="abc"`
	resp, roundErr := policy.RoundTrip(newRequestWithAuth(t, noQOP))
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !errors.Is(roundErr, ErrInsecureDigestBlocked) {
		t.Fatalf("error = %v, want ErrInsecureDigestBlocked", roundErr)
	}
	if base.reached {
		t.Fatal("base transport must not be reached when a no-qop Digest is blocked")
	}
	if !isAuthenticationError(roundErr) {
		t.Fatal("ErrInsecureDigestBlocked must classify as authentication")
	}
}

// TestDigestPolicyAllowsNoQOPWithLegacyOptIn proves the no-qop header is
// transmitted when the legacy opt-in is set.
func TestDigestPolicyAllowsNoQOPWithLegacyOptIn(t *testing.T) {
	base := &recordingRoundTripper{}
	policy, err := newDigestPolicyTransport(base, true)
	if err != nil {
		t.Fatalf("newDigestPolicyTransport: %v", err)
	}
	noQOP := `Digest username="admin", realm="r", nonce="n", uri="/x", response="abc"`
	resp, roundErr := policy.RoundTrip(newRequestWithAuth(t, noQOP))
	if roundErr != nil {
		t.Fatalf("unexpected error with legacy opt-in: %v", roundErr)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !base.reached {
		t.Fatal("base transport must be reached when legacy no-qop is permitted")
	}
}

// TestDigestPolicyAllowsQOPAuth proves a qop=auth Digest header always passes.
func TestDigestPolicyAllowsQOPAuth(t *testing.T) {
	base := &recordingRoundTripper{}
	policy, _ := newDigestPolicyTransport(base, false)
	qopAuth := `Digest username="admin", realm="r", nonce="n", uri="/x", qop=auth, nc=00000001, cnonce="c", response="abc"`
	resp, err := policy.RoundTrip(newRequestWithAuth(t, qopAuth))
	if err != nil {
		t.Fatalf("qop=auth must pass, got %v", err)
	}
	if resp != nil {
		_ = resp.Body.Close()
	}
	if !base.reached {
		t.Fatal("qop=auth Digest must reach the base transport")
	}
}

// TestDigestPolicyIgnoresBasicAndEmpty proves Basic and empty Authorization
// headers are never treated as a blockable no-qop Digest.
func TestDigestPolicyIgnoresBasicAndEmpty(t *testing.T) {
	for _, authorization := range []string{"", "Basic dXNlcjpwYXNz"} {
		base := &recordingRoundTripper{}
		policy, _ := newDigestPolicyTransport(base, false)
		resp, err := policy.RoundTrip(newRequestWithAuth(t, authorization))
		if err != nil {
			t.Fatalf("authorization %q must pass, got %v", authorization, err)
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		if !base.reached {
			t.Fatalf("authorization %q must reach the base transport", authorization)
		}
	}
}

// TestIsNoQOPDigestAuthorization table-tests the header classifier.
func TestIsNoQOPDigestAuthorization(t *testing.T) {
	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{"empty", "", false},
		{"basic", "Basic dXNlcjpwYXNz", false},
		{"digest no qop", `Digest username="a", nonce="n", response="r"`, true},
		{"digest with qop=auth", `Digest username="a", qop=auth, nc=1, response="r"`, false},
		{"digest with quoted qop", `Digest username="a", qop="auth", response="r"`, false},
		{"case-insensitive scheme", `digest username="a", response="r"`, true},
		{"value containing qop substring but no directive", `Digest username="aqop", response="r"`, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isNoQOPDigestAuthorization(c.value); got != c.want {
				t.Fatalf("isNoQOPDigestAuthorization(%q) = %v, want %v", c.value, got, c.want)
			}
		})
	}
}

// TestDigestPolicyRejectsNilBase proves a miswired nil base fails loudly rather
// than silently bypassing the policy.
func TestDigestPolicyRejectsNilBase(t *testing.T) {
	if _, err := newDigestPolicyTransport(nil, false); err == nil {
		t.Fatal("expected an error for a nil base transport")
	}
}

// --- end-to-end: a legacy no-qop camera is blocked unless opted in ---------

// newLegacyNoQOPServer offers only a no-qop (RFC 2069) Digest challenge. It
// records whether a Digest Authorization reached the server and returns a
// minimal valid DeviceInformation response when the caller explicitly permits
// the legacy exchange.
func newLegacyNoQOPServer(t *testing.T) (*httptest.Server, *atomic.Bool) {
	t.Helper()
	var sawAuthorization atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if !strings.HasPrefix(request.Header.Get("Authorization"), "Digest ") {
			w.Header().Set("WWW-Authenticate", `Digest realm="cam", nonce="deadbeef"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		sawAuthorization.Store(true)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body><tds:GetDeviceInformationResponse xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:Manufacturer>Legacy Camera</tds:Manufacturer><tds:Model>RFC2069</tds:Model><tds:FirmwareVersion>1</tds:FirmwareVersion><tds:SerialNumber>synthetic</tds:SerialNumber><tds:HardwareId>synthetic</tds:HardwareId></tds:GetDeviceInformationResponse></env:Body></env:Envelope>`))
	}))
	t.Cleanup(server.Close)
	return server, &sawAuthorization
}

// TestInventoryLegacyNoQOPDeniedByDefault proves that, against a legacy no-qop
// server, the default (no legacy opt-in) run never succeeds and classifies as
// authentication rather than silently downgrading.
func TestInventoryLegacyNoQOPDeniedByDefault(t *testing.T) {
	server, sawAuthorization := newLegacyNoQOPServer(t)
	creds, err := NewCredentials("admin", "s3cret", false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	p := &productionInventory{httpClient: &http.Client{}}
	seam, err := p.newSeam(server.URL+"/onvif/device_service", 5*time.Second, creds)
	if err != nil {
		t.Fatalf("newSeam: %v", err)
	}
	_, callErr := seam.DeviceInformation(context.Background())
	if callErr == nil {
		t.Fatal("expected the legacy no-qop exchange to be refused")
	}
	if !isAuthenticationError(callErr) {
		t.Fatalf("legacy no-qop refusal must classify as authentication, got %v", callErr)
	}
	if sawAuthorization.Load() {
		t.Fatal("legacy Digest Authorization reached the server without explicit policy")
	}
}

func TestInventoryLegacyNoQOPExplicitOptInUsesNativePolicy(t *testing.T) {
	server, sawAuthorization := newLegacyNoQOPServer(t)
	creds, err := NewCredentialsWithPolicy("admin", "s3cret", Policy{AllowLegacyDigest: true})
	if err != nil {
		t.Fatalf("NewCredentialsWithPolicy: %v", err)
	}
	seam, err := (&productionInventory{httpClient: &http.Client{}}).newSeam(
		server.URL+"/onvif/device_service",
		5*time.Second,
		creds,
	)
	if err != nil {
		t.Fatalf("newSeam: %v", err)
	}
	info, err := seam.DeviceInformation(context.Background())
	if err != nil {
		t.Fatalf("DeviceInformation with explicit legacy policy: %v", err)
	}
	if info.Manufacturer != "Legacy Camera" {
		t.Fatalf("manufacturer = %q, want Legacy Camera", info.Manufacturer)
	}
	if !sawAuthorization.Load() {
		t.Fatal("explicit legacy policy did not reach the native RFC2069 authentication path")
	}
}
