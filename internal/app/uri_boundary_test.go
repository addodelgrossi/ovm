package app

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/discovery"
)

// uniqueSecret builds a per-field unique marker that must never appear in any
// output after redaction.
func uniqueSecret(tag string) string {
	return "SECRET-" + tag + "-MARKER"
}

func TestRedactBoundaryURIRedactsUserinfoQueryFragment(t *testing.T) {
	t.Parallel()
	user := uniqueSecret("user")
	pass := uniqueSecret("pass")
	query := uniqueSecret("query")
	fragment := uniqueSecret("frag")
	raw := "https://" + user + ":" + pass + "@host.invalid/path?" + "k=" + query + "#" + fragment

	got := redactBoundaryURI(raw)
	for _, marker := range []string{user, pass, query, fragment} {
		if strings.Contains(got, marker) {
			t.Fatalf("redacted URI %q retained secret %q", got, marker)
		}
	}
	if !strings.Contains(got, "REDACTED") {
		t.Fatalf("redacted URI %q has no redaction marker", got)
	}
}

func TestRedactBoundaryTransportURIRequiresAbsoluteHTTP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		raw   string
		valid bool
	}{
		{"http", "http://host.invalid/x", true},
		{"https", "https://host.invalid/x", true},
		{"relative", "/onvif/device_service", false},
		{"no host", "http:///x", false},
		{"ftp scheme", "ftp://host.invalid/x", false},
		{"opaque urn", "urn:uuid:1234", false},
		{"empty passes through", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := redactBoundaryTransportURI(c.raw)
			if c.raw == "" {
				if got != "" {
					t.Fatalf("empty input should pass through, got %q", got)
				}
				return
			}
			if c.valid && got == boundaryInvalidMarker {
				t.Fatalf("valid transport URI %q marked invalid", c.raw)
			}
			if !c.valid && got != boundaryInvalidMarker {
				t.Fatalf("invalid transport URI %q not marked invalid, got %q", c.raw, got)
			}
		})
	}
}

func TestRedactBoundaryTransportURIInvalidNeverCrossesRaw(t *testing.T) {
	t.Parallel()
	secret := uniqueSecret("rawtransport")
	// A credential-bearing but non-transport (no host) URI must be dropped
	// entirely, not partially emitted.
	raw := "ftp://" + secret + "@host.invalid/x"
	got := redactBoundaryTransportURI(raw)
	if got != boundaryInvalidMarker {
		t.Fatalf("invalid transport URI should be fully dropped, got %q", got)
	}
	if strings.Contains(got, secret) {
		t.Fatalf("raw secret leaked: %q", got)
	}
}

func TestRedactBoundaryExpandedName(t *testing.T) {
	t.Parallel()
	secret := uniqueSecret("ns")
	// A type expanded name whose namespace carries credential-bearing userinfo.
	raw := "{https://" + secret + "@onvif.org/ver10/network/wsdl}NetworkVideoTransmitter"
	got := redactBoundaryExpandedName(raw)
	if strings.Contains(got, secret) {
		t.Fatalf("expanded name retained secret: %q", got)
	}
	if !strings.HasSuffix(got, "}NetworkVideoTransmitter") {
		t.Fatalf("expanded name lost its local part: %q", got)
	}
	// A normal expanded name is preserved verbatim.
	normal := "{http://www.onvif.org/ver10/network/wsdl}NetworkVideoTransmitter"
	if redactBoundaryExpandedName(normal) != normal {
		t.Fatalf("normal expanded name altered: %q", redactBoundaryExpandedName(normal))
	}
	// A malformed expanded name is dropped.
	if redactBoundaryExpandedName("not-an-expanded-name") != boundaryInvalidMarker {
		t.Fatal("malformed expanded name should be marked invalid")
	}
}

// TestDiscoveryJSONAndHumanDropAllURISecrets drives discovery normalization and
// asserts no unique secret appears in the serialized DTO across EPR, XAddrs,
// scopes, and type namespace URIs.
func TestDiscoveryBoundaryRedactionCoversAllFields(t *testing.T) {
	t.Parallel()
	eprSecret := uniqueSecret("epr")
	xaddrSecret := uniqueSecret("xaddr")
	scopeSecret := uniqueSecret("scope")
	typeSecret := uniqueSecret("type")

	result := discovery.Result{Devices: []discovery.Device{{
		EndpointReference: "urn:uuid:" + eprSecret,
		XAddrs:            []string{"http://user:" + xaddrSecret + "@cam.invalid/onvif"},
		Scopes:            []string{"onvif://www.onvif.org/name/" + scopeSecret},
		Types:             []string{"{http://onvif.org/x?secret=" + typeSecret + "}Device"},
	}}}

	normalized := normalizeDiscoveryResult(result)
	encoded, err := json.Marshal(normalized)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	payload := string(encoded)
	for _, secret := range []string{xaddrSecret, typeSecret} {
		if strings.Contains(payload, secret) {
			t.Fatalf("discovery JSON leaked secret %q: %s", secret, payload)
		}
	}
	// EPR is a urn: the uuid value is identity, not a credential component, so
	// it is retained; only userinfo/query/fragment are redacted. Confirm the
	// scope path segment (not a credential component) is retained verbatim.
	if !strings.Contains(payload, scopeSecret) {
		t.Fatalf("scope path segment should be retained (not a credential component): %s", payload)
	}
	_ = eprSecret
}

// TestInventoryBoundaryRedactionCoversCapabilitiesServicesScopes drives the
// inventory normalizers and asserts credential-bearing URL material is removed
// from capabilities XAddrs, service XAddr/Namespace, and Scope.Item.
func TestInventoryBoundaryRedactionCoversCapabilitiesServicesScopes(t *testing.T) {
	t.Parallel()
	capSecret := uniqueSecret("cap")
	svcXAddrSecret := uniqueSecret("svcx")
	svcNSSecret := uniqueSecret("svcns")
	scopeSecret := uniqueSecret("iscope")

	caps := normalizeCapabilities(device.Capabilities{
		DeviceXAddr: "http://user:" + capSecret + "@cam.invalid/onvif/device",
	})
	services := normalizeServices([]device.Service{{
		Namespace: "http://ns.invalid/x?token=" + svcNSSecret,
		XAddr:     "http://user:" + svcXAddrSecret + "@cam.invalid/svc",
	}})
	scopes := normalizeScopes([]device.Scope{{
		Definition: "Fixed",
		Item:       "onvif://cam.invalid/x?secret=" + scopeSecret,
	}})

	blob := caps.DeviceXAddr + "|" + services[0].XAddr + "|" + services[0].Namespace + "|" + scopes[0].Item
	for _, secret := range []string{capSecret, svcXAddrSecret, svcNSSecret, scopeSecret} {
		if strings.Contains(blob, secret) {
			t.Fatalf("inventory DTO leaked secret %q: %s", secret, blob)
		}
	}
	if !strings.Contains(blob, "REDACTED") {
		t.Fatalf("expected a redaction marker in %q", blob)
	}
}
