package device_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestPreservedDeviceXMLIsCredentialRedacted(t *testing.T) {
	t.Parallel()

	identityMarker := "synthetic-user-marker"
	sensitiveMarker := strings.Join([]string{"synthetic", "extension", "marker"}, "-")
	credentialURL := "http://" + identityMarker + ":ignored@device.invalid/service?opaque=" + sensitiveMarker

	capabilitiesPayload := `<tds:GetCapabilitiesResponse` + tdsNS + ttNS + ` xmlns:v="urn:synthetic"><tds:Capabilities><tt:Device><tt:XAddr>` + credentialURL + `</tt:XAddr><tt:Extension><v:Username>` + identityMarker + `</v:Username></tt:Extension></tt:Device></tds:Capabilities></tds:GetCapabilitiesResponse>`
	capClient, capClose := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, soap12Envelope(capabilitiesPayload))
	defer capClose()
	capabilities, err := capClient.GetCapabilities(context.Background())
	if err != nil {
		t.Fatalf("GetCapabilities() error = %v", err)
	}
	assertPreservedXMLRedacted(t, capabilities.RawXML, identityMarker, sensitiveMarker)
	if strings.Contains(capabilities.DeviceXAddr, identityMarker) || strings.Contains(capabilities.DeviceXAddr, sensitiveMarker) {
		t.Fatalf("typed capability XAddr retained credential material: %q", capabilities.DeviceXAddr)
	}

	servicesPayload := `<tds:GetServicesResponse` + tdsNS + ttNS + ` xmlns:v="urn:synthetic"><tds:Service><tds:Namespace>urn:synthetic:service</tds:Namespace><tds:XAddr>http://device.invalid/service</tds:XAddr><tds:Capabilities><v:Password>` + sensitiveMarker + `</v:Password></tds:Capabilities><tds:Version><tt:Major>1</tt:Major><tt:Minor>0</tt:Minor></tds:Version></tds:Service></tds:GetServicesResponse>`
	serviceClient, serviceClose := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, soap12Envelope(servicesPayload))
	defer serviceClose()
	services, err := serviceClient.GetServices(context.Background())
	if err != nil {
		t.Fatalf("GetServices() error = %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("services = %+v", services)
	}
	assertPreservedXMLRedacted(t, services[0].CapabilitiesXML, sensitiveMarker)

	networkPayload := `<tds:GetNetworkInterfacesResponse` + tdsNS + ttNS + ` xmlns:v="urn:synthetic"><tds:NetworkInterfaces token="eth0"><tt:Enabled>true</tt:Enabled><tt:Extension><v:Nonce>` + sensitiveMarker + `</v:Nonce></tt:Extension></tds:NetworkInterfaces></tds:GetNetworkInterfacesResponse>`
	networkClient, networkClose := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, soap12Envelope(networkPayload))
	defer networkClose()
	interfaces, err := networkClient.GetNetworkInterfaces(context.Background())
	if err != nil {
		t.Fatalf("GetNetworkInterfaces() error = %v", err)
	}
	if len(interfaces) != 1 {
		t.Fatalf("interfaces = %+v", interfaces)
	}
	assertPreservedXMLRedacted(t, interfaces[0].RawXML, sensitiveMarker)
}

func assertPreservedXMLRedacted(t *testing.T, raw []byte, forbidden ...string) {
	t.Helper()
	text := string(raw)
	for _, marker := range forbidden {
		if strings.Contains(text, marker) {
			t.Fatalf("preserved XML retained sensitive marker %q: %s", marker, text)
		}
	}
	if !strings.Contains(text, "REDACTED") {
		t.Fatalf("preserved XML has no redaction marker: %s", text)
	}
}
