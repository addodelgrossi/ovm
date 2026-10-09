package fakedevice_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// newDeviceClient wires a device.Client to a fresh fake device over the real
// SOAP transport so these tests exercise the end-to-end decode path.
func newDeviceClient(t *testing.T, config fakedevice.Config) (*device.Client, func()) {
	t.Helper()
	fake, err := fakedevice.New(config)
	if err != nil {
		t.Fatalf("fakedevice.New() error = %v", err)
	}
	server := httptest.NewServer(fake)
	soapClient, err := soap.NewClient(soap.Config{})
	if err != nil {
		server.Close()
		t.Fatalf("soap.NewClient() error = %v", err)
	}
	client, err := device.NewClient(soapClient, server.URL)
	if err != nil {
		server.Close()
		t.Fatalf("device.NewClient() error = %v", err)
	}
	return client, server.Close
}

// TestFakeDHCPWireShapeDecodesAsDHCP proves that a DHCP-derived address
// configured on the fake device is emitted under <tt:FromDHCP> and decoded by
// the real device client as a DHCP-provenance IPAddress, and that a manual
// address is decoded as non-DHCP.
func TestFakeDHCPWireShapeDecodesAsDHCP(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		fromDHCP bool
	}{
		{"dhcp derived", true},
		{"manual", false},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			client, closeFn := newDeviceClient(t, fakedevice.Config{
				Inventory: fakedevice.Inventory{
					NetworkInterfaces: []fakedevice.NetworkInterfaceData{
						{
							Token:        "eth0",
							Enabled:      true,
							Name:         "eth0",
							HwAddress:    "00:00:5E:00:53:01",
							MTU:          1500,
							IPv4Enabled:  true,
							IPv4Address:  "203.0.113.42",
							IPv4Prefix:   24,
							IPv4FromDHCP: tc.fromDHCP,
						},
					},
				},
			})
			defer closeFn()

			ifaces, err := client.GetNetworkInterfaces(context.Background())
			if err != nil {
				t.Fatalf("GetNetworkInterfaces() error = %v", err)
			}
			if len(ifaces) != 1 || len(ifaces[0].IPv4Addresses) != 1 {
				t.Fatalf("ifaces = %+v", ifaces)
			}
			addr := ifaces[0].IPv4Addresses[0]
			if addr.Address != "203.0.113.42" || addr.PrefixLength != 24 {
				t.Fatalf("address = %+v", addr)
			}
			if addr.FromDHCP != tc.fromDHCP {
				t.Fatalf("FromDHCP = %v, want %v", addr.FromDHCP, tc.fromDHCP)
			}
			// The RawXML must carry the matching address element so the wire
			// shape is observable and provenance is unambiguous.
			raw := string(ifaces[0].RawXML)
			wantElement := "Manual"
			if tc.fromDHCP {
				wantElement = "FromDHCP"
			}
			if !strings.Contains(raw, wantElement) {
				t.Fatalf("RawXML = %s, want %q element", raw, wantElement)
			}
		})
	}
}

// TestInventorySlicesAreDeepCopied proves that New takes ownership of the
// configured inventory slices: mutating the caller's slice entries after New
// must not change the device's rendered responses. This guards the
// configuration-ownership and race-safety contract.
func TestInventorySlicesAreDeepCopied(t *testing.T) {
	t.Parallel()

	services := []fakedevice.ServiceData{
		{Namespace: "urn:original", XAddr: "http://device.invalid/one", VersionMajor: 1, VersionMinor: 0},
	}
	scopes := []fakedevice.ScopeData{
		{Definition: "Fixed", Item: "onvif://www.onvif.org/type/original"},
	}
	ifaces := []fakedevice.NetworkInterfaceData{
		{Token: "eth0", Enabled: true, Name: "eth0", HwAddress: "00:00:5E:00:53:09"},
	}

	client, closeFn := newDeviceClient(t, fakedevice.Config{
		Inventory: fakedevice.Inventory{
			Services:          services,
			Scopes:            scopes,
			NetworkInterfaces: ifaces,
		},
	})
	defer closeFn()

	// Mutate the caller's slices after construction. A deep copy inside New
	// means these mutations are invisible to the device.
	services[0].Namespace = "urn:mutated"
	scopes[0].Item = "onvif://www.onvif.org/type/mutated"
	ifaces[0].Token = "mutated"

	gotServices, err := client.GetServices(context.Background())
	if err != nil {
		t.Fatalf("GetServices() error = %v", err)
	}
	if len(gotServices) != 1 || gotServices[0].Namespace != "urn:original" {
		t.Fatalf("services = %+v, want original namespace preserved", gotServices)
	}

	gotScopes, err := client.GetScopes(context.Background())
	if err != nil {
		t.Fatalf("GetScopes() error = %v", err)
	}
	if len(gotScopes) != 1 || gotScopes[0].Item != "onvif://www.onvif.org/type/original" {
		t.Fatalf("scopes = %+v, want original item preserved", gotScopes)
	}

	gotIfaces, err := client.GetNetworkInterfaces(context.Background())
	if err != nil {
		t.Fatalf("GetNetworkInterfaces() error = %v", err)
	}
	if len(gotIfaces) != 1 || gotIfaces[0].Token != "eth0" {
		t.Fatalf("interfaces = %+v, want original token preserved", gotIfaces)
	}
}

// TestFakeCapabilitiesExposesExtendedCategories proves the expanded fake
// capabilities surface Imaging, PTZ, and Analytics endpoints by default, so a
// decoder exercising those categories has synthetic coverage.
func TestFakeCapabilitiesExposesExtendedCategories(t *testing.T) {
	t.Parallel()

	client, closeFn := newDeviceClient(t, fakedevice.Config{})
	defer closeFn()

	caps, err := client.GetCapabilities(context.Background())
	if err != nil {
		t.Fatalf("GetCapabilities() error = %v", err)
	}
	if caps.DeviceXAddr == "" || caps.MediaXAddr == "" || caps.EventsXAddr == "" {
		t.Fatalf("core capabilities missing: %+v", caps)
	}
	if caps.ImagingXAddr == "" || caps.PTZXAddr == "" || caps.AnalyticsXAddr == "" {
		t.Fatalf("extended capabilities missing: %+v", caps)
	}
}
