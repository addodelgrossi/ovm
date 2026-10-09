package fakedevice_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// TestConfigRejectsReservedPrefixCollisions covers the hardening fix that
// forbids envelope or operation prefixes colliding with the fixed tt/ter/ovm
// response prefixes, which would otherwise bind one prefix to two namespaces.
func TestConfigRejectsReservedPrefixCollisions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		config fakedevice.Config
	}{
		{"operation prefix tt", fakedevice.Config{OperationPrefix: "tt"}},
		{"operation prefix ter", fakedevice.Config{OperationPrefix: "ter"}},
		{"operation prefix ovm", fakedevice.Config{OperationPrefix: "ovm"}},
		{"envelope prefix tt", fakedevice.Config{EnvelopePrefix: "tt"}},
		{"envelope prefix ovm", fakedevice.Config{EnvelopePrefix: "ovm"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := fakedevice.New(tc.config); err == nil {
				t.Fatalf("New(%+v) error = nil, want reserved-prefix rejection", tc.config)
			}
		})
	}
}

// TestConfigRejectsDigestNonceWithoutDigestAuth covers the hardening fix that a
// Digest nonce is only meaningful under HTTP Digest authentication.
func TestConfigRejectsDigestNonceWithoutDigestAuth(t *testing.T) {
	t.Parallel()

	if _, err := fakedevice.New(fakedevice.Config{DigestNonce: "deadbeef"}); err == nil {
		t.Fatal("New() error = nil, want rejection of Digest nonce without Digest auth")
	}
	// A Digest nonce is accepted when Digest auth is enabled.
	if _, err := fakedevice.New(fakedevice.Config{
		HTTPAuth:    fakedevice.HTTPAuthDigest,
		Username:    "operator",
		Password:    "secret",
		DigestNonce: "deadbeef",
	}); err != nil {
		t.Fatalf("New() with Digest auth error = %v", err)
	}
}

// TestDeviceRejectsUnknownOperationQName covers the hardening fix that an
// unknown or foreign operation QName is rejected rather than dispatched on the
// strength of a SOAP header action alone (header-only dispatch).
func TestDeviceRejectsUnknownOperationQName(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	// Header advertises a known action, but the body operation is unknown. The
	// device must reject it instead of serving the header action's response.
	_, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionGetSystemDateAndTime,
		BodyXML: []byte(`<tds:TotallyUnknownOperation xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`),
	})
	if !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("unknown-QName Call() error = %v, want ErrInvalidArgVal", err)
	}

	// A known local name in a foreign namespace must likewise be rejected.
	_, err = client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionGetDeviceInformation,
		BodyXML: []byte(`<evil:GetDeviceInformation xmlns:evil="urn:foreign"/>`),
	})
	if !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("foreign-namespace Call() error = %v, want ErrInvalidArgVal", err)
	}
}

// TestDeviceServesAllDeviceOperations is a smoke table confirming every modeled
// Device action produces a success envelope through the real transport. The
// GetServices entry carries the single required tds:IncludeCapability child the
// WSDL request message mandates; the others carry no element children, matching
// their empty request messages.
func TestDeviceServesAllDeviceOperations(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	for _, tc := range []struct {
		action string
		body   string
		expect string
	}{
		{fakedevice.ActionGetDeviceInformation, `<tds:GetDeviceInformation xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`, "GetDeviceInformationResponse"},
		{fakedevice.ActionGetCapabilities, `<tds:GetCapabilities xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`, "GetCapabilitiesResponse"},
		{fakedevice.ActionGetServices, `<tds:GetServices xmlns:tds="http://www.onvif.org/ver10/device/wsdl"><tds:IncludeCapability>false</tds:IncludeCapability></tds:GetServices>`, "GetServicesResponse"},
		{fakedevice.ActionGetScopes, `<tds:GetScopes xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`, "GetScopesResponse"},
		{fakedevice.ActionGetHostname, `<tds:GetHostname xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`, "GetHostnameResponse"},
		{fakedevice.ActionGetNetworkInterfaces, `<tds:GetNetworkInterfaces xmlns:tds="http://www.onvif.org/ver10/device/wsdl"/>`, "GetNetworkInterfacesResponse"},
	} {
		response, err := client.Call(context.Background(), server.URL, soap.Request{
			Action:  tc.action,
			BodyXML: []byte(tc.body),
		})
		if err != nil {
			t.Fatalf("Call(%s) error = %v", tc.action, err)
		}
		if !strings.Contains(string(response.BodyXML), tc.expect) {
			t.Errorf("Call(%s) BodyXML = %s, want %s", tc.action, response.BodyXML, tc.expect)
		}
		if response.HTTPStatus != http.StatusOK {
			t.Errorf("Call(%s) status = %d, want 200", tc.action, response.HTTPStatus)
		}
	}
}
