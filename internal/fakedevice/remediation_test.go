package fakedevice_test

import (
	"context"
	"crypto/sha1" // #nosec G505 -- test must reproduce the WS-Security PasswordDigest formula.
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

const (
	devNS  = "http://www.onvif.org/ver10/device/wsdl"
	wsseNS = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd"
	wsuNS  = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd"
	//nolint:gosec // Public WS-Security type URI, not a credential.
	pwDigestURI   = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest"
	base64TypeURI = "http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary"
)

// -----------------------------------------------------------------------------
// Requirement 1: tt:Capabilities category order and raw wire-order regression.
// -----------------------------------------------------------------------------

// TestGetCapabilitiesEmitsSchemaCategoryOrder confirms the device renders the
// modeled tt:Capabilities categories in the ONVIF schema sequence
// (Analytics, Device, Events, Imaging, Media, PTZ) on the raw wire, not merely
// that each category decodes. It asserts byte positions in the response.
func TestGetCapabilitiesEmitsSchemaCategoryOrder(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{
		Inventory: fakedevice.Inventory{
			Capabilities: fakedevice.CapabilitiesData{
				AnalyticsXAddr: "http://device.invalid/a",
				DeviceXAddr:    "http://device.invalid/d",
				EventsXAddr:    "http://device.invalid/e",
				ImagingXAddr:   "http://device.invalid/i",
				MediaXAddr:     "http://device.invalid/m",
				PTZXAddr:       "http://device.invalid/p",
			},
		},
	})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	response, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionGetCapabilities,
		BodyXML: []byte(`<tds:GetCapabilities xmlns:tds="` + devNS + `"/>`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}

	wire := string(response.BodyXML)
	order := []string{"<tt:Analytics>", "<tt:Device>", "<tt:Events>", "<tt:Imaging>", "<tt:Media>", "<tt:PTZ>"}
	previous := -1
	for _, element := range order {
		index := strings.Index(wire, element)
		if index < 0 {
			t.Fatalf("category %q missing from wire: %s", element, wire)
		}
		if index <= previous {
			t.Errorf("category %q at %d is out of schema order (previous %d): %s", element, index, previous, wire)
		}
		previous = index
	}
}

// TestGetCapabilitiesPartialSetKeepsOrder confirms that when only a subset of
// categories is configured, the emitted subset still follows schema order.
func TestGetCapabilitiesPartialSetKeepsOrder(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{
		Inventory: fakedevice.Inventory{
			Capabilities: fakedevice.CapabilitiesData{
				DeviceXAddr: "http://device.invalid/d",
				EventsXAddr: "http://device.invalid/e",
				MediaXAddr:  "http://device.invalid/m",
			},
		},
	})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	response, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionGetCapabilities,
		BodyXML: []byte(`<tds:GetCapabilities xmlns:tds="` + devNS + `"/>`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	wire := string(response.BodyXML)
	deviceIdx := strings.Index(wire, "<tt:Device>")
	eventsIdx := strings.Index(wire, "<tt:Events>")
	mediaIdx := strings.Index(wire, "<tt:Media>")
	if deviceIdx < 0 || eventsIdx < 0 || mediaIdx < 0 {
		t.Fatalf("expected Device/Events/Media on wire: %s", wire)
	}
	if deviceIdx >= eventsIdx || eventsIdx >= mediaIdx {
		t.Errorf("subset order Device(%d) Events(%d) Media(%d) not in schema sequence: %s", deviceIdx, eventsIdx, mediaIdx, wire)
	}
	if strings.Contains(wire, "<tt:Analytics>") || strings.Contains(wire, "<tt:Imaging>") || strings.Contains(wire, "<tt:PTZ>") {
		t.Errorf("unset categories were emitted: %s", wire)
	}
}

// -----------------------------------------------------------------------------
// Requirement 2: request-wrapper structural validation.
// -----------------------------------------------------------------------------

func TestRequestWrapperValidationAcceptsValidWrappers(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	for _, tc := range []struct {
		name   string
		action string
		body   string
		expect string
	}{
		{"empty GetSystemDateAndTime", fakedevice.ActionGetSystemDateAndTime, `<tds:GetSystemDateAndTime xmlns:tds="` + devNS + `"/>`, "GetSystemDateAndTimeResponse"},
		{"empty GetDeviceInformation", fakedevice.ActionGetDeviceInformation, `<tds:GetDeviceInformation xmlns:tds="` + devNS + `"/>`, "GetDeviceInformationResponse"},
		{"empty GetScopes", fakedevice.ActionGetScopes, `<tds:GetScopes xmlns:tds="` + devNS + `"/>`, "GetScopesResponse"},
		{"empty GetHostname", fakedevice.ActionGetHostname, `<tds:GetHostname xmlns:tds="` + devNS + `"/>`, "GetHostnameResponse"},
		{"empty GetNetworkInterfaces", fakedevice.ActionGetNetworkInterfaces, `<tds:GetNetworkInterfaces xmlns:tds="` + devNS + `"/>`, "GetNetworkInterfacesResponse"},
		{"GetCapabilities no category", fakedevice.ActionGetCapabilities, `<tds:GetCapabilities xmlns:tds="` + devNS + `"/>`, "GetCapabilitiesResponse"},
		{"GetCapabilities one category", fakedevice.ActionGetCapabilities, `<tds:GetCapabilities xmlns:tds="` + devNS + `"><tds:Category>Device</tds:Category></tds:GetCapabilities>`, "GetCapabilitiesResponse"},
		{"GetCapabilities many categories", fakedevice.ActionGetCapabilities, `<tds:GetCapabilities xmlns:tds="` + devNS + `"><tds:Category>All</tds:Category><tds:Category>Media</tds:Category></tds:GetCapabilities>`, "GetCapabilitiesResponse"},
		{"GetServices include false", fakedevice.ActionGetServices, `<tds:GetServices xmlns:tds="` + devNS + `"><tds:IncludeCapability>false</tds:IncludeCapability></tds:GetServices>`, "GetServicesResponse"},
		{"GetServices include true", fakedevice.ActionGetServices, `<tds:GetServices xmlns:tds="` + devNS + `"><tds:IncludeCapability>true</tds:IncludeCapability></tds:GetServices>`, "GetServicesResponse"},
		{"GetServices include 1", fakedevice.ActionGetServices, `<tds:GetServices xmlns:tds="` + devNS + `"><tds:IncludeCapability>1</tds:IncludeCapability></tds:GetServices>`, "GetServicesResponse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			response, err := client.Call(context.Background(), server.URL, soap.Request{Action: tc.action, BodyXML: []byte(tc.body)})
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if !strings.Contains(string(response.BodyXML), tc.expect) {
				t.Errorf("BodyXML = %s, want %s", response.BodyXML, tc.expect)
			}
		})
	}
}

func TestRequestWrapperValidationRejectsInvalidWrappers(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	for _, tc := range []struct {
		name   string
		action string
		body   string
	}{
		// No-child wrappers reject any element child.
		{"GetSystemDateAndTime forbidden child", fakedevice.ActionGetSystemDateAndTime, `<tds:GetSystemDateAndTime xmlns:tds="` + devNS + `"><tds:Extra/></tds:GetSystemDateAndTime>`},
		{"GetSystemDateAndTime non-whitespace text", fakedevice.ActionGetSystemDateAndTime, `<tds:GetSystemDateAndTime xmlns:tds="` + devNS + `">unexpected</tds:GetSystemDateAndTime>`},
		{"GetDeviceInformation forbidden child", fakedevice.ActionGetDeviceInformation, `<tds:GetDeviceInformation xmlns:tds="` + devNS + `"><tds:Extra/></tds:GetDeviceInformation>`},
		{"GetScopes forbidden child", fakedevice.ActionGetScopes, `<tds:GetScopes xmlns:tds="` + devNS + `"><tds:Extra/></tds:GetScopes>`},
		{"GetHostname forbidden child", fakedevice.ActionGetHostname, `<tds:GetHostname xmlns:tds="` + devNS + `"><tds:Extra/></tds:GetHostname>`},
		{"GetNetworkInterfaces forbidden child", fakedevice.ActionGetNetworkInterfaces, `<tds:GetNetworkInterfaces xmlns:tds="` + devNS + `"><tds:Extra/></tds:GetNetworkInterfaces>`},
		// GetCapabilities: wrong child, wrong namespace Category, bad enum.
		{"GetCapabilities foreign child", fakedevice.ActionGetCapabilities, `<tds:GetCapabilities xmlns:tds="` + devNS + `"><tds:Bogus/></tds:GetCapabilities>`},
		{"GetCapabilities wrong-namespace Category", fakedevice.ActionGetCapabilities, `<tds:GetCapabilities xmlns:tds="` + devNS + `" xmlns:x="urn:other"><x:Category>Device</x:Category></tds:GetCapabilities>`},
		{"GetCapabilities invalid enum", fakedevice.ActionGetCapabilities, `<tds:GetCapabilities xmlns:tds="` + devNS + `"><tds:Category>NotACategory</tds:Category></tds:GetCapabilities>`},
		{"GetCapabilities whitespace-altered enum", fakedevice.ActionGetCapabilities, `<tds:GetCapabilities xmlns:tds="` + devNS + `"><tds:Category> Device </tds:Category></tds:GetCapabilities>`},
		{"GetCapabilities nested category content", fakedevice.ActionGetCapabilities, `<tds:GetCapabilities xmlns:tds="` + devNS + `"><tds:Category>Device<tds:Extra/></tds:Category></tds:GetCapabilities>`},
		{"GetCapabilities attributed category", fakedevice.ActionGetCapabilities, `<tds:GetCapabilities xmlns:tds="` + devNS + `"><tds:Category custom="value">Device</tds:Category></tds:GetCapabilities>`},
		{"GetCapabilities mixed valid and foreign", fakedevice.ActionGetCapabilities, `<tds:GetCapabilities xmlns:tds="` + devNS + `"><tds:Category>Device</tds:Category><tds:Bogus/></tds:GetCapabilities>`},
		// GetServices: missing, duplicate, wrong name, wrong namespace, bad boolean, extra sibling.
		{"GetServices missing IncludeCapability", fakedevice.ActionGetServices, `<tds:GetServices xmlns:tds="` + devNS + `"/>`},
		{"GetServices duplicate IncludeCapability", fakedevice.ActionGetServices, `<tds:GetServices xmlns:tds="` + devNS + `"><tds:IncludeCapability>false</tds:IncludeCapability><tds:IncludeCapability>true</tds:IncludeCapability></tds:GetServices>`},
		{"GetServices wrong child name", fakedevice.ActionGetServices, `<tds:GetServices xmlns:tds="` + devNS + `"><tds:Nope>false</tds:Nope></tds:GetServices>`},
		{"GetServices wrong namespace child", fakedevice.ActionGetServices, `<tds:GetServices xmlns:tds="` + devNS + `" xmlns:x="urn:other"><x:IncludeCapability>false</x:IncludeCapability></tds:GetServices>`},
		{"GetServices non-boolean value", fakedevice.ActionGetServices, `<tds:GetServices xmlns:tds="` + devNS + `"><tds:IncludeCapability>maybe</tds:IncludeCapability></tds:GetServices>`},
		{"GetServices nested boolean content", fakedevice.ActionGetServices, `<tds:GetServices xmlns:tds="` + devNS + `"><tds:IncludeCapability>true<tds:Extra/></tds:IncludeCapability></tds:GetServices>`},
		{"GetServices attributed boolean", fakedevice.ActionGetServices, `<tds:GetServices xmlns:tds="` + devNS + `"><tds:IncludeCapability custom="value">true</tds:IncludeCapability></tds:GetServices>`},
		{"GetServices extra sibling", fakedevice.ActionGetServices, `<tds:GetServices xmlns:tds="` + devNS + `"><tds:IncludeCapability>false</tds:IncludeCapability><tds:Extra/></tds:GetServices>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := client.Call(context.Background(), server.URL, soap.Request{Action: tc.action, BodyXML: []byte(tc.body)})
			if !errors.Is(err, soap.ErrInvalidArgVal) {
				t.Fatalf("Call() error = %v, want ErrInvalidArgVal", err)
			}
		})
	}
}

// TestRequestWrapperValidationKeepsSyntheticOperationsDistinct confirms the
// structural wrapper validation does not disturb the synthetic GetState/SetState
// operations, which keep their own parsing.
func TestRequestWrapperValidationKeepsSyntheticOperationsDistinct(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{InitialState: map[string]string{"mode": "day"}})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	response, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionGetState,
		BodyXML: []byte(`<ovm:GetState xmlns:ovm="urn:ovm:synthetic"><ovm:Key>mode</ovm:Key></ovm:GetState>`),
	})
	if err != nil {
		t.Fatalf("GetState Call() error = %v", err)
	}
	if !strings.Contains(string(response.BodyXML), "day") {
		t.Errorf("GetState BodyXML = %s, want day", response.BodyXML)
	}
}

// -----------------------------------------------------------------------------
// Requirement 3: HTTP Digest replay by highest accepted nc per nonce/cnonce.
// -----------------------------------------------------------------------------

func TestDigestReplayHighestNonceCount(t *testing.T) {
	t.Parallel()

	const (
		username = "operator"
		password = "camera-secret"
		realm    = "ovm-synthetic-device"
		nonce    = "0123456789abcdef0123456789abcdef"
		uri      = "/onvif/device_service"
		cnonceA  = "aaaaaaaa"
		cnonceB  = "bbbbbbbb"
	)
	device := newDevice(t, fakedevice.Config{
		HTTPAuth:        fakedevice.HTTPAuthDigest,
		DigestAlgorithm: "MD5",
		DigestRealm:     realm,
		DigestNonce:     nonce,
		Username:        username,
		Password:        password,
	})
	server := httptest.NewServer(device)
	defer server.Close()

	do := func(cnonce, nc string) int {
		header := digestAuthHeader(username, realm, nonce, uri, cnonce, nc, password)
		return rawDigestStatus(t, server.URL, uri, header)
	}

	// First accepted nc establishes the high-water mark for cnonce A.
	if got := do(cnonceA, "00000001"); got != http.StatusOK {
		t.Fatalf("nc=1 status = %d, want 200", got)
	}
	// Reuse of the same nc is a replay.
	if got := do(cnonceA, "00000001"); got != http.StatusUnauthorized {
		t.Fatalf("nc=1 reuse status = %d, want 401", got)
	}
	// Rollback below the mark is rejected.
	if got := do(cnonceA, "00000001"); got != http.StatusUnauthorized {
		t.Fatalf("nc rollback status = %d, want 401", got)
	}
	// A strictly larger forward jump is accepted and moves the mark.
	if got := do(cnonceA, "0000000a"); got != http.StatusOK {
		t.Fatalf("nc forward-jump status = %d, want 200", got)
	}
	// A value between the previous and the jumped mark is now a rollback.
	if got := do(cnonceA, "00000005"); got != http.StatusUnauthorized {
		t.Fatalf("nc between status = %d, want 401", got)
	}
	// A different cnonce is an independent sequence and may restart at nc=1.
	if got := do(cnonceB, "00000001"); got != http.StatusOK {
		t.Fatalf("independent cnonce nc=1 status = %d, want 200", got)
	}
}

// TestDigestCapacityCountsPairsAndUpdatesAtCapacity confirms capacity bounds the
// number of distinct nonce/cnonce pairs, and that an update to an existing pair
// succeeds even once capacity is reached.
func TestDigestCapacityCountsPairsAndUpdatesAtCapacity(t *testing.T) {
	t.Parallel()

	const (
		username = "operator"
		password = "camera-secret"
		realm    = "ovm-synthetic-device"
		nonce    = "0123456789abcdef0123456789abcdef"
		uri      = "/onvif/device_service"
	)
	device := newDevice(t, fakedevice.Config{
		HTTPAuth:            fakedevice.HTTPAuthDigest,
		DigestAlgorithm:     "MD5",
		DigestRealm:         realm,
		DigestNonce:         nonce,
		Username:            username,
		Password:            password,
		MaxAuthStateEntries: 2,
	})
	server := httptest.NewServer(device)
	defer server.Close()

	do := func(cnonce, nc string) int {
		return rawDigestStatus(t, server.URL, uri, digestAuthHeader(username, realm, nonce, uri, cnonce, nc, password))
	}

	// Two distinct pairs fill capacity.
	if got := do("c0000001", "00000001"); got != http.StatusOK {
		t.Fatalf("pair 1 status = %d, want 200", got)
	}
	if got := do("c0000002", "00000001"); got != http.StatusOK {
		t.Fatalf("pair 2 status = %d, want 200", got)
	}
	// A third distinct pair is refused: capacity counts pairs, not requests.
	if got := do("c0000003", "00000001"); got != http.StatusUnauthorized {
		t.Fatalf("pair 3 at capacity status = %d, want 401", got)
	}
	// Updating an existing pair with a higher nc still succeeds at capacity.
	if got := do("c0000001", "00000002"); got != http.StatusOK {
		t.Fatalf("existing-pair update at capacity status = %d, want 200", got)
	}
}

// TestDigestStaleResetClearsNonceCounts confirms a one-shot stale challenge
// rotates the nonce and resets the per-pair nc high-water marks, so the client
// restarts the sequence under the new nonce.
func TestDigestStaleResetClearsNonceCounts(t *testing.T) {
	t.Parallel()

	const (
		username = "operator"
		password = "camera-secret"
		realm    = "ovm-synthetic-device"
		nonce    = "0123456789abcdef0123456789abcdef"
		uri      = "/onvif/device_service"
		cnonce   = "aaaaaaaa"
	)
	device := newDevice(t, fakedevice.Config{
		HTTPAuth:        fakedevice.HTTPAuthDigest,
		DigestAlgorithm: "MD5",
		DigestRealm:     realm,
		DigestNonce:     nonce,
		Username:        username,
		Password:        password,
		DigestStaleOnce: true,
	})
	server := httptest.NewServer(device)
	defer server.Close()

	// The first authenticated attempt triggers the one-shot stale challenge and
	// returns a 401 carrying stale=true and a rotated nonce.
	status, newNonce := rawDigestStatusWithNonce(t, server.URL, uri, digestAuthHeader(username, realm, nonce, uri, cnonce, "00000001", password))
	if status != http.StatusUnauthorized {
		t.Fatalf("stale challenge status = %d, want 401", status)
	}
	if newNonce == "" || newNonce == nonce {
		t.Fatalf("stale challenge nonce = %q, want a rotated nonce", newNonce)
	}
	// Under the new nonce, nc=1 is accepted (the mark was reset).
	if got := rawDigestStatus(t, server.URL, uri, digestAuthHeader(username, realm, newNonce, uri, cnonce, "00000001", password)); got != http.StatusOK {
		t.Fatalf("post-stale nc=1 status = %d, want 200", got)
	}
}

// -----------------------------------------------------------------------------
// Requirement 4: UsernameToken Created permissive default and strict mode.
// -----------------------------------------------------------------------------

func TestUsernameTokenCreatedPermissiveByDefault(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		created string
	}{
		{"canonical UTC", "2026-10-08T12:00:00Z"},
		{"fractional seconds", "2026-10-08T12:00:00.250Z"},
		{"positive offset", "2026-10-08T14:00:00+02:00"},
		{"negative offset", "2026-10-08T07:00:00-05:00"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device := newDevice(t, fakedevice.Config{
				RequireUsernameToken: true,
				Username:             "operator",
				Password:             "camera-secret",
				Clock:                func() time.Time { return base },
			})
			server := httptest.NewServer(device)
			defer server.Close()
			client := newSOAPClient(t, nil)

			header := usernameTokenHeader("operator", "camera-secret", tc.created, usernameTokenNonce())
			response, err := client.Call(context.Background(), server.URL, soap.Request{
				Action:    fakedevice.ActionGetSystemDateAndTime,
				HeaderXML: header,
				BodyXML:   []byte(`<tds:GetSystemDateAndTime xmlns:tds="` + devNS + `"/>`),
			})
			if err != nil {
				t.Fatalf("Call() error = %v", err)
			}
			if response.HTTPStatus != http.StatusOK {
				t.Errorf("HTTPStatus = %d, want 200", response.HTTPStatus)
			}
		})
	}
}

func TestUsernameTokenCreatedStrictMode(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name    string
		created string
		accept  bool
	}{
		{"canonical UTC accepted", "2026-10-08T12:00:00Z", true},
		{"fractional rejected", "2026-10-08T12:00:00.250Z", false},
		{"zero fraction lexical form rejected", "2026-10-08T12:00:00.000Z", false},
		{"offset rejected", "2026-10-08T14:00:00+02:00", false},
		{"zero offset spelling rejected", "2026-10-08T12:00:00+00:00", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			device := newDevice(t, fakedevice.Config{
				RequireUsernameToken:       true,
				UsernameTokenStrictCreated: true,
				Username:                   "operator",
				Password:                   "camera-secret",
				Clock:                      func() time.Time { return base },
			})
			server := httptest.NewServer(device)
			defer server.Close()
			client := newSOAPClient(t, nil)

			header := usernameTokenHeader("operator", "camera-secret", tc.created, usernameTokenNonce())
			_, err := client.Call(context.Background(), server.URL, soap.Request{
				Action:    fakedevice.ActionGetSystemDateAndTime,
				HeaderXML: header,
				BodyXML:   []byte(`<tds:GetSystemDateAndTime xmlns:tds="` + devNS + `"/>`),
			})
			if tc.accept {
				if err != nil {
					t.Fatalf("Call() error = %v, want success", err)
				}
			} else if !errors.Is(err, soap.ErrNotAuthorized) {
				t.Fatalf("Call() error = %v, want ErrNotAuthorized", err)
			}
		})
	}
}

// TestUsernameTokenDigestUsesTransmittedCreatedBytes confirms the digest is
// computed over the exact transmitted Created string: a token whose digest was
// built from a different lexical form of the same instant is rejected.
func TestUsernameTokenDigestUsesTransmittedCreatedBytes(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	device := newDevice(t, fakedevice.Config{
		RequireUsernameToken: true,
		Username:             "operator",
		Password:             "camera-secret",
		Clock:                func() time.Time { return base },
	})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	// Transmit Created with an offset but build the digest from the equivalent
	// UTC rendering. The bytes differ, so verification must fail.
	nonce := usernameTokenNonce()
	transmitted := "2026-10-08T14:00:00+02:00"
	mismatchedDigest := passwordDigest(nonce, "2026-10-08T12:00:00Z", "camera-secret")
	header := usernameTokenHeaderWithDigest("operator", mismatchedDigest, transmitted, nonce)
	_, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:    fakedevice.ActionGetSystemDateAndTime,
		HeaderXML: header,
		BodyXML:   []byte(`<tds:GetSystemDateAndTime xmlns:tds="` + devNS + `"/>`),
	})
	if !errors.Is(err, soap.ErrNotAuthorized) {
		t.Fatalf("mismatched-bytes digest Call() error = %v, want ErrNotAuthorized", err)
	}
}

// TestUsernameTokenFreshnessWindow confirms a Created outside the freshness
// window is rejected even though it is a valid RFC3339 timestamp.
func TestUsernameTokenFreshnessWindow(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	device := newDevice(t, fakedevice.Config{
		RequireUsernameToken: true,
		Username:             "operator",
		Password:             "camera-secret",
		UsernameTokenWindow:  1 * time.Minute,
		Clock:                func() time.Time { return base },
	})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	// Ten minutes stale, far outside the one-minute window.
	stale := "2026-10-08T11:50:00Z"
	header := usernameTokenHeader("operator", "camera-secret", stale, usernameTokenNonce())
	_, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:    fakedevice.ActionGetSystemDateAndTime,
		HeaderXML: header,
		BodyXML:   []byte(`<tds:GetSystemDateAndTime xmlns:tds="` + devNS + `"/>`),
	})
	if !errors.Is(err, soap.ErrNotAuthorized) {
		t.Fatalf("stale Created Call() error = %v, want ErrNotAuthorized", err)
	}

	// A fresh offset timestamp inside the window is accepted (permissive).
	fresh := "2026-10-08T14:00:30+02:00" // == 12:00:30Z, 30s from base
	header = usernameTokenHeader("operator", "camera-secret", fresh, usernameTokenNonce())
	response, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:    fakedevice.ActionGetSystemDateAndTime,
		HeaderXML: header,
		BodyXML:   []byte(`<tds:GetSystemDateAndTime xmlns:tds="` + devNS + `"/>`),
	})
	if err != nil {
		t.Fatalf("fresh offset Call() error = %v", err)
	}
	if response.HTTPStatus != http.StatusOK {
		t.Errorf("fresh offset HTTPStatus = %d, want 200", response.HTTPStatus)
	}
}

// -----------------------------------------------------------------------------
// Requirement 5: fake-side request body limit typing/behavior.
// -----------------------------------------------------------------------------

// TestConfiguredRequestLimitRejectsOversizeBody confirms Config.MaxRequestBytes
// (an int64) bounds the fake device's readRequestBody and returns HTTP 413 for
// an oversize body, with a modest limit value proving the typed field is honored.
func TestConfiguredRequestLimitRejectsOversizeBody(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{MaxRequestBytes: 64})
	server := httptest.NewServer(device)
	defer server.Close()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, strings.NewReader(strings.Repeat("x", 4096)))
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Content-Type", "application/soap+xml")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", response.StatusCode)
	}
}

// -----------------------------------------------------------------------------
// Test helpers.
// -----------------------------------------------------------------------------

// nonceCounter yields deterministic, collision-free 16-byte nonces across
// parallel tests so repeated calls never collide in the device UsernameToken
// replay cache. It is atomic because parallel subtests share it.
var nonceCounter atomic.Uint64

func usernameTokenNonce() string {
	n := nonceCounter.Add(1)
	raw := make([]byte, 16)
	binary.BigEndian.PutUint64(raw, n)
	binary.BigEndian.PutUint64(raw[8:], n*2654435761)
	return base64.StdEncoding.EncodeToString(raw)
}

// passwordDigest reproduces Base64(SHA1(nonce + created + password)) using the
// decoded nonce bytes and the exact created string.
func passwordDigest(nonceBase64, created, password string) string {
	rawNonce, err := base64.StdEncoding.DecodeString(nonceBase64)
	if err != nil {
		panic(err)
	}
	input := make([]byte, 0, len(rawNonce)+len(created)+len(password))
	input = append(input, rawNonce...)
	input = append(input, created...)
	input = append(input, password...)
	sum := sha1.Sum(input) // #nosec G401 -- WS-Security PasswordDigest formula.
	return base64.StdEncoding.EncodeToString(sum[:])
}

func usernameTokenHeader(username, password, created, nonceBase64 string) []byte {
	return usernameTokenHeaderWithDigest(username, passwordDigest(nonceBase64, created, password), created, nonceBase64)
}

func usernameTokenHeaderWithDigest(username, digest, created, nonceBase64 string) []byte {
	return []byte(`<wsse:Security xmlns:wsse="` + wsseNS + `" xmlns:wsu="` + wsuNS + `">` +
		`<wsse:UsernameToken>` +
		`<wsse:Username>` + username + `</wsse:Username>` +
		`<wsse:Password Type="` + pwDigestURI + `">` + digest + `</wsse:Password>` +
		`<wsse:Nonce EncodingType="` + base64TypeURI + `">` + nonceBase64 + `</wsse:Nonce>` +
		`<wsu:Created>` + created + `</wsu:Created>` +
		`</wsse:UsernameToken>` +
		`</wsse:Security>`)
}

func TestRequestLimitRejectsOverflowSentinel(t *testing.T) {
	t.Parallel()

	if _, err := fakedevice.New(fakedevice.Config{MaxRequestBytes: int64(^uint64(0) >> 1)}); err == nil {
		t.Fatal("New accepted a request limit whose one-byte probe would overflow")
	}
}
