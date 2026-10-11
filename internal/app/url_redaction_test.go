package app

import (
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/discovery"
)

func TestApplicationDTOsRedactCredentialBearingURLs(t *testing.T) {
	t.Parallel()

	identity := "camera-user-marker"
	sensitive := strings.Join([]string{"camera", "query", "marker"}, "-")
	credentialURL := "http://" + identity + ":ignored@camera.invalid/service?opaque=" + sensitive

	discovered := normalizeDiscoveryResult(discovery.Result{Devices: []discovery.Device{{
		EndpointReference: "urn:uuid:device",
		XAddrs:            []string{credentialURL},
	}}})
	if len(discovered.Devices) != 1 || len(discovered.Devices[0].Addresses) != 1 {
		t.Fatalf("discovery result = %+v", discovered)
	}
	assertURLMaterialRedacted(t, discovered.Devices[0].Addresses[0], identity, sensitive)

	capabilities := normalizeCapabilities(device.Capabilities{DeviceXAddr: credentialURL})
	assertURLMaterialRedacted(t, capabilities.DeviceXAddr, identity, sensitive)

	services := normalizeServices([]device.Service{{Namespace: "urn:service", XAddr: credentialURL}})
	if len(services) != 1 {
		t.Fatalf("services = %+v", services)
	}
	assertURLMaterialRedacted(t, services[0].XAddr, identity, sensitive)
}

func assertURLMaterialRedacted(t *testing.T, value string, forbidden ...string) {
	t.Helper()
	for _, marker := range forbidden {
		if strings.Contains(value, marker) {
			t.Fatalf("URL %q retained sensitive marker %q", value, marker)
		}
	}
	if !strings.Contains(value, "REDACTED") {
		t.Fatalf("URL %q has no redaction marker", value)
	}
}
