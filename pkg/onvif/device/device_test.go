package device_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// newClients spins up a fake device and returns a bound device.Client plus a
// close function. The device client runs over the real SOAP transport, so these
// tests exercise actual fake-device responses end to end.
func newClients(t *testing.T, config fakedevice.Config) (*device.Client, func()) {
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
	deviceClient, err := device.NewClient(soapClient, server.URL)
	if err != nil {
		server.Close()
		t.Fatalf("device.NewClient() error = %v", err)
	}
	return deviceClient, server.Close
}

// prefixConfig returns a config using non-default, non-reserved envelope and
// operation prefixes to exercise prefix-tolerant parsing against live
// fake-device output.
func prefixConfig() fakedevice.Config {
	return fakedevice.Config{
		EnvelopePrefix:  "cameraEnvelope",
		OperationPrefix: "cameraDevice",
	}
}

func TestClientConstructionValidation(t *testing.T) {
	t.Parallel()
	if _, err := device.NewClient(nil, "http://device.invalid"); err == nil {
		t.Error("NewClient(nil transport) error = nil, want error")
	}
	soapClient, err := soap.NewClient(soap.Config{})
	if err != nil {
		t.Fatalf("soap.NewClient() error = %v", err)
	}
	if _, err := device.NewClient(soapClient, ""); err == nil {
		t.Error("NewClient(empty endpoint) error = nil, want error")
	}
	client, err := device.NewClient(soapClient, "http://device.invalid")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.Endpoint() != "http://device.invalid" {
		t.Errorf("Endpoint() = %q", client.Endpoint())
	}
}

func TestGetSystemDateAndTime(t *testing.T) {
	t.Parallel()
	base := time.Date(2026, 10, 8, 12, 3, 0, 0, time.UTC)

	t.Run("synthetic success", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{Clock: func() time.Time { return base }})
		defer closeFn()
		result, err := client.GetSystemDateAndTime(context.Background())
		if err != nil {
			t.Fatalf("GetSystemDateAndTime() error = %v", err)
		}
		if !result.UTCValid || !result.UTC.Equal(base) {
			t.Errorf("UTC = %v (valid=%v), want %v", result.UTC, result.UTCValid, base)
		}
		if result.DateTimeType != "Manual" {
			t.Errorf("DateTimeType = %q, want Manual", result.DateTimeType)
		}
	})

	t.Run("namespace prefix variation", func(t *testing.T) {
		t.Parallel()
		config := prefixConfig()
		config.Clock = func() time.Time { return base }
		client, closeFn := newClients(t, config)
		defer closeFn()
		result, err := client.GetSystemDateAndTime(context.Background())
		if err != nil {
			t.Fatalf("GetSystemDateAndTime() error = %v", err)
		}
		if !result.UTCValid || !result.UTC.Equal(base) {
			t.Errorf("UTC = %v (valid=%v), want %v", result.UTC, result.UTCValid, base)
		}
	})

	t.Run("fault", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{
			Faults: map[string]fakedevice.FaultSpec{
				fakedevice.ActionGetSystemDateAndTime: {Kind: fakedevice.FaultNotAuthorized, Reason: "synthetic"},
			},
		})
		defer closeFn()
		if _, err := client.GetSystemDateAndTime(context.Background()); !errors.Is(err, soap.ErrNotAuthorized) {
			t.Fatalf("error = %v, want ErrNotAuthorized", err)
		}
	})

	t.Run("missing SystemDateAndTime element", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK,
			soap12Envelope(`<tds:GetSystemDateAndTimeResponse xmlns:tds="`+device.Namespace+`"/>`))
		defer closeFn()
		if _, err := client.GetSystemDateAndTime(context.Background()); !errors.Is(err, device.ErrMissingField) {
			t.Fatalf("error = %v, want ErrMissingField", err)
		}
	})

	t.Run("optional UTCDateTime omitted stays valid header", func(t *testing.T) {
		t.Parallel()
		// DateTimeType and DaylightSavings are both required by the WSDL;
		// UTCDateTime is optional, so a response that supplies the required
		// fields but omits UTCDateTime decodes successfully with UTCValid false.
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK,
			soap12Envelope(`<tds:GetSystemDateAndTimeResponse xmlns:tds="`+device.Namespace+`"><tds:SystemDateAndTime><tt:DateTimeType xmlns:tt="`+device.SchemaNamespace+`">NTP</tt:DateTimeType><tt:DaylightSavings xmlns:tt="`+device.SchemaNamespace+`">false</tt:DaylightSavings></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`))
		defer closeFn()
		got, err := client.GetSystemDateAndTime(context.Background())
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if got.UTCValid {
			t.Errorf("UTCValid = true, want false when UTCDateTime omitted")
		}
		if got.DateTimeType != "NTP" {
			t.Errorf("DateTimeType = %q, want NTP", got.DateTimeType)
		}
	})
}

func TestGetDeviceInformation(t *testing.T) {
	t.Parallel()

	t.Run("synthetic success", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{})
		defer closeFn()
		info, err := client.GetDeviceInformation(context.Background())
		if err != nil {
			t.Fatalf("GetDeviceInformation() error = %v", err)
		}
		if info.Manufacturer != "OVM Synthetic" || info.Model != "FakeCam 1000" {
			t.Errorf("info = %+v", info)
		}
	})

	t.Run("namespace prefix variation", func(t *testing.T) {
		t.Parallel()
		config := prefixConfig()
		config.Inventory.Information = fakedevice.DeviceInformationData{
			Manufacturer: "Acme", Model: "Z1", FirmwareVersion: "9.9", SerialNumber: "S9", HardwareID: "H9",
		}
		client, closeFn := newClients(t, config)
		defer closeFn()
		info, err := client.GetDeviceInformation(context.Background())
		if err != nil {
			t.Fatalf("GetDeviceInformation() error = %v", err)
		}
		if info.Manufacturer != "Acme" || info.FirmwareVersion != "9.9" {
			t.Errorf("info = %+v", info)
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		t.Parallel()
		fake, err := fakedevice.New(fakedevice.Config{})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		fake.SetUnsupported(fakedevice.ActionGetDeviceInformation, true)
		server := httptest.NewServer(fake)
		defer server.Close()
		soapClient, _ := soap.NewClient(soap.Config{})
		client, _ := device.NewClient(soapClient, server.URL)
		if _, err := client.GetDeviceInformation(context.Background()); !errors.Is(err, soap.ErrActionNotSupported) {
			t.Fatalf("error = %v, want ErrActionNotSupported", err)
		}
	})

	t.Run("missing fields", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK,
			soap12Envelope(`<tds:GetDeviceInformationResponse xmlns:tds="`+device.Namespace+`"/>`))
		defer closeFn()
		if _, err := client.GetDeviceInformation(context.Background()); !errors.Is(err, device.ErrMissingField) {
			t.Fatalf("error = %v, want ErrMissingField", err)
		}
	})
}

func TestGetCapabilities(t *testing.T) {
	t.Parallel()

	t.Run("synthetic success", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{})
		defer closeFn()
		caps, err := client.GetCapabilities(context.Background())
		if err != nil {
			t.Fatalf("GetCapabilities() error = %v", err)
		}
		if caps.DeviceXAddr == "" || caps.MediaXAddr == "" || caps.EventsXAddr == "" {
			t.Errorf("caps = %+v", caps)
		}
		if len(caps.RawXML) == 0 || !strings.Contains(string(caps.RawXML), "Capabilities") {
			t.Errorf("RawXML not preserved: %q", caps.RawXML)
		}
	})

	t.Run("namespace prefix variation", func(t *testing.T) {
		t.Parallel()
		config := prefixConfig()
		config.Inventory.Capabilities = fakedevice.CapabilitiesData{DeviceXAddr: "http://device.invalid/d"}
		client, closeFn := newClients(t, config)
		defer closeFn()
		caps, err := client.GetCapabilities(context.Background())
		if err != nil {
			t.Fatalf("GetCapabilities() error = %v", err)
		}
		if caps.DeviceXAddr != "http://device.invalid/d" {
			t.Errorf("DeviceXAddr = %q", caps.DeviceXAddr)
		}
	})

	t.Run("fault", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{
			Faults: map[string]fakedevice.FaultSpec{
				fakedevice.ActionGetCapabilities: {Kind: fakedevice.FaultInvalidArgVal},
			},
		})
		defer closeFn()
		if _, err := client.GetCapabilities(context.Background()); !errors.Is(err, soap.ErrInvalidArgVal) {
			t.Fatalf("error = %v, want ErrInvalidArgVal", err)
		}
	})

	t.Run("missing capabilities element", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK,
			soap12Envelope(`<tds:GetCapabilitiesResponse xmlns:tds="`+device.Namespace+`"/>`))
		defer closeFn()
		if _, err := client.GetCapabilities(context.Background()); !errors.Is(err, device.ErrMissingField) {
			t.Fatalf("error = %v, want ErrMissingField", err)
		}
	})
}

func TestGetServices(t *testing.T) {
	t.Parallel()

	t.Run("synthetic success", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{})
		defer closeFn()
		services, err := client.GetServices(context.Background())
		if err != nil {
			t.Fatalf("GetServices() error = %v", err)
		}
		if len(services) != 3 || services[0].Namespace == "" || !services[0].HasVersion ||
			services[2].Namespace != "http://www.onvif.org/ver20/media/wsdl" {
			t.Errorf("services = %+v", services)
		}
	})

	t.Run("namespace prefix variation", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, prefixConfig())
		defer closeFn()
		services, err := client.GetServices(context.Background())
		if err != nil {
			t.Fatalf("GetServices() error = %v", err)
		}
		if len(services) == 0 || services[0].Version.Major != 25 {
			t.Errorf("services = %+v", services)
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{
			UnsupportedActions: map[string]bool{fakedevice.ActionGetServices: true},
		})
		defer closeFn()
		if _, err := client.GetServices(context.Background()); !errors.Is(err, soap.ErrActionNotSupported) {
			t.Fatalf("error = %v, want ErrActionNotSupported", err)
		}
	})

	t.Run("missing namespace field", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK,
			soap12Envelope(`<tds:GetServicesResponse xmlns:tds="`+device.Namespace+`"><tds:Service><tds:XAddr>http://device.invalid/x</tds:XAddr></tds:Service></tds:GetServicesResponse>`))
		defer closeFn()
		if _, err := client.GetServices(context.Background()); !errors.Is(err, device.ErrMissingField) {
			t.Fatalf("error = %v, want ErrMissingField", err)
		}
	})
}

func TestGetScopes(t *testing.T) {
	t.Parallel()

	t.Run("synthetic success", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{})
		defer closeFn()
		scopes, err := client.GetScopes(context.Background())
		if err != nil {
			t.Fatalf("GetScopes() error = %v", err)
		}
		if len(scopes) != 3 || scopes[0].Item == "" {
			t.Errorf("scopes = %+v", scopes)
		}
	})

	t.Run("namespace prefix variation", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, prefixConfig())
		defer closeFn()
		scopes, err := client.GetScopes(context.Background())
		if err != nil {
			t.Fatalf("GetScopes() error = %v", err)
		}
		if len(scopes) == 0 {
			t.Error("scopes empty")
		}
	})

	t.Run("fault", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{
			Faults: map[string]fakedevice.FaultSpec{fakedevice.ActionGetScopes: {Kind: fakedevice.FaultNotAuthorized}},
		})
		defer closeFn()
		if _, err := client.GetScopes(context.Background()); !errors.Is(err, soap.ErrNotAuthorized) {
			t.Fatalf("error = %v, want ErrNotAuthorized", err)
		}
	})

	t.Run("missing scope item", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK,
			soap12Envelope(`<tds:GetScopesResponse xmlns:tds="`+device.Namespace+`"><tds:Scopes xmlns:tt="`+device.SchemaNamespace+`"><tt:ScopeDef>Fixed</tt:ScopeDef></tds:Scopes></tds:GetScopesResponse>`))
		defer closeFn()
		if _, err := client.GetScopes(context.Background()); !errors.Is(err, device.ErrMissingField) {
			t.Fatalf("error = %v, want ErrMissingField", err)
		}
	})
}

func TestGetHostname(t *testing.T) {
	t.Parallel()

	t.Run("synthetic success", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{})
		defer closeFn()
		host, err := client.GetHostname(context.Background())
		if err != nil {
			t.Fatalf("GetHostname() error = %v", err)
		}
		if host.Name != "ovm-synthetic" || host.FromDHCP {
			t.Errorf("host = %+v", host)
		}
	})

	t.Run("namespace prefix variation with optional name omitted", func(t *testing.T) {
		t.Parallel()
		config := prefixConfig()
		config.Inventory.Hostname = fakedevice.HostnameData{FromDHCP: true}
		client, closeFn := newClients(t, config)
		defer closeFn()
		host, err := client.GetHostname(context.Background())
		if err != nil {
			t.Fatalf("GetHostname() error = %v", err)
		}
		if !host.FromDHCP || host.Name != "" {
			t.Errorf("host = %+v, want FromDHCP and empty Name", host)
		}
	})

	t.Run("unsupported", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{
			UnsupportedActions: map[string]bool{fakedevice.ActionGetHostname: true},
		})
		defer closeFn()
		if _, err := client.GetHostname(context.Background()); !errors.Is(err, soap.ErrActionNotSupported) {
			t.Fatalf("error = %v, want ErrActionNotSupported", err)
		}
	})

	t.Run("invalid boolean field", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK,
			soap12Envelope(`<tds:GetHostnameResponse xmlns:tds="`+device.Namespace+`"><tds:HostnameInformation xmlns:tt="`+device.SchemaNamespace+`"><tt:FromDHCP>maybe</tt:FromDHCP></tds:HostnameInformation></tds:GetHostnameResponse>`))
		defer closeFn()
		if _, err := client.GetHostname(context.Background()); !errors.Is(err, device.ErrInvalidField) {
			t.Fatalf("error = %v, want ErrInvalidField", err)
		}
	})
}

func TestGetNetworkInterfaces(t *testing.T) {
	t.Parallel()

	t.Run("synthetic success", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{})
		defer closeFn()
		ifaces, err := client.GetNetworkInterfaces(context.Background())
		if err != nil {
			t.Fatalf("GetNetworkInterfaces() error = %v", err)
		}
		if len(ifaces) != 1 {
			t.Fatalf("ifaces = %+v", ifaces)
		}
		iface := ifaces[0]
		if iface.Token != "eth0" || !iface.Enabled || !iface.HasMTU || iface.MTU != 1500 {
			t.Errorf("iface = %+v", iface)
		}
		if !iface.HasIPv4 || len(iface.IPv4Addresses) == 0 || iface.IPv4Addresses[0].Address != "203.0.113.10" {
			t.Errorf("iface IPv4 = %+v", iface)
		}
		if len(iface.RawXML) == 0 {
			t.Error("RawXML not preserved")
		}
	})

	t.Run("namespace prefix variation with optional fields omitted", func(t *testing.T) {
		t.Parallel()
		config := prefixConfig()
		config.Inventory.NetworkInterfaces = []fakedevice.NetworkInterfaceData{
			{Token: "wlan0", Enabled: false, Name: "wlan0", HwAddress: "00:00:5E:00:53:05"},
		}
		client, closeFn := newClients(t, config)
		defer closeFn()
		ifaces, err := client.GetNetworkInterfaces(context.Background())
		if err != nil {
			t.Fatalf("GetNetworkInterfaces() error = %v", err)
		}
		if len(ifaces) != 1 || ifaces[0].HasMTU || ifaces[0].HasIPv4 {
			t.Errorf("ifaces = %+v, want no MTU and no IPv4", ifaces)
		}
		if ifaces[0].Token != "wlan0" || ifaces[0].Enabled {
			t.Errorf("ifaces = %+v", ifaces)
		}
	})

	t.Run("fault", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{
			Faults: map[string]fakedevice.FaultSpec{fakedevice.ActionGetNetworkInterfaces: {Kind: fakedevice.FaultNotAuthorized}},
		})
		defer closeFn()
		if _, err := client.GetNetworkInterfaces(context.Background()); !errors.Is(err, soap.ErrNotAuthorized) {
			t.Fatalf("error = %v, want ErrNotAuthorized", err)
		}
	})

	t.Run("invalid MTU field", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK,
			soap12Envelope(`<tds:GetNetworkInterfacesResponse xmlns:tds="`+device.Namespace+`"><tds:NetworkInterfaces token="eth0" xmlns:tt="`+device.SchemaNamespace+`"><tt:Enabled>true</tt:Enabled><tt:Info><tt:Name>eth0</tt:Name><tt:HwAddress>00:00:5E:00:53:10</tt:HwAddress><tt:MTU>huge</tt:MTU></tt:Info></tds:NetworkInterfaces></tds:GetNetworkInterfacesResponse>`))
		defer closeFn()
		if _, err := client.GetNetworkInterfaces(context.Background()); !errors.Is(err, device.ErrInvalidField) {
			t.Fatalf("error = %v, want ErrInvalidField", err)
		}
	})
}

// TestErrorsIsCompatibility confirms SOAP faults and context cancellation stay
// errors.Is-compatible through the device client.
func TestErrorsIsCompatibility(t *testing.T) {
	t.Parallel()

	t.Run("soap fault", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{
			Faults: map[string]fakedevice.FaultSpec{fakedevice.ActionGetHostname: {Kind: fakedevice.FaultNotAuthorized}},
		})
		defer closeFn()
		_, err := client.GetHostname(context.Background())
		if !errors.Is(err, soap.ErrNotAuthorized) {
			t.Fatalf("error = %v, want ErrNotAuthorized", err)
		}
	})

	t.Run("cancellation", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newClients(t, fakedevice.Config{})
		defer closeFn()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := client.GetHostname(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	})

	t.Run("unexpected root element", func(t *testing.T) {
		t.Parallel()
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK,
			soap12Envelope(`<tds:WrongResponse xmlns:tds="`+device.Namespace+`"/>`))
		defer closeFn()
		if _, err := client.GetHostname(context.Background()); !errors.Is(err, device.ErrUnexpectedElement) {
			t.Fatalf("error = %v, want ErrUnexpectedElement", err)
		}
	})
}

// TestFixtureBackedResponses serves each project-authored fixture envelope and
// decodes it through the real transport, confirming fixtures parse to the
// expected typed values.
func TestFixtureBackedResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		check   func(t *testing.T, client *device.Client)
	}{
		{
			name:    "GetSystemDateAndTime 1.2",
			fixture: "soap12/get-system-date-and-time-success.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetSystemDateAndTime(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if !got.UTCValid || got.UTC.Hour() != 12 || got.UTC.Minute() != 3 {
					t.Errorf("result = %+v", got)
				}
			},
		},
		{
			name:    "GetSystemDateAndTime prefix 1.1",
			fixture: "soap11/get-system-date-and-time-prefix-variation.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetSystemDateAndTime(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if !got.UTCValid || got.DateTimeType != "NTP" {
					t.Errorf("result = %+v", got)
				}
			},
		},
		{
			name:    "GetDeviceInformation 1.2",
			fixture: "soap12/get-device-information-success.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetDeviceInformation(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if got.Manufacturer != "OVM Synthetic" || got.Model != "FakeCam 1000" {
					t.Errorf("result = %+v", got)
				}
			},
		},
		{
			name:    "GetDeviceInformation prefix 1.1",
			fixture: "soap11/get-device-information-prefix-variation.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetDeviceInformation(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if got.Model != "FakeCam 2000" {
					t.Errorf("result = %+v", got)
				}
			},
		},
		{
			name:    "GetCapabilities 1.2",
			fixture: "soap12/get-capabilities-success.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetCapabilities(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if got.DeviceXAddr == "" || got.MediaXAddr == "" || got.EventsXAddr == "" {
					t.Errorf("result = %+v", got)
				}
			},
		},
		{
			name:    "GetCapabilities prefix 1.1",
			fixture: "soap11/get-capabilities-prefix-variation.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetCapabilities(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if got.DeviceXAddr == "" || got.ImagingXAddr == "" {
					t.Errorf("result = %+v", got)
				}
			},
		},
		{
			name:    "GetServices 1.2",
			fixture: "soap12/get-services-success.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetServices(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if len(got) != 2 || got[0].Version.Major != 25 {
					t.Errorf("result = %+v", got)
				}
			},
		},
		{
			name:    "GetServices prefix 1.1",
			fixture: "soap11/get-services-prefix-variation.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetServices(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if len(got) != 1 || got[0].Version.Minor != 5 {
					t.Errorf("result = %+v", got)
				}
			},
		},
		{
			name:    "GetScopes 1.2",
			fixture: "soap12/get-scopes-success.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetScopes(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if len(got) != 2 || got[1].Definition != "Configurable" {
					t.Errorf("result = %+v", got)
				}
			},
		},
		{
			name:    "GetScopes prefix 1.1",
			fixture: "soap11/get-scopes-prefix-variation.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetScopes(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if len(got) != 1 || got[0].Definition != "Fixed" {
					t.Errorf("result = %+v", got)
				}
			},
		},
		{
			name:    "GetHostname 1.2",
			fixture: "soap12/get-hostname-success.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetHostname(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if got.Name != "ovm-synthetic" || got.FromDHCP {
					t.Errorf("result = %+v", got)
				}
			},
		},
		{
			name:    "GetHostname prefix optional 1.1",
			fixture: "soap11/get-hostname-prefix-variation.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetHostname(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if !got.FromDHCP || got.Name != "" {
					t.Errorf("result = %+v", got)
				}
			},
		},
		{
			name:    "GetNetworkInterfaces 1.2",
			fixture: "soap12/get-network-interfaces-success.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetNetworkInterfaces(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if len(got) != 1 || got[0].MTU != 1500 || len(got[0].IPv4Addresses) == 0 {
					t.Errorf("result = %+v", got)
				}
			},
		},
		{
			name:    "GetNetworkInterfaces prefix optional 1.1",
			fixture: "soap11/get-network-interfaces-prefix-variation.xml",
			check: func(t *testing.T, client *device.Client) {
				got, err := client.GetNetworkInterfaces(context.Background())
				if err != nil {
					t.Fatalf("error = %v", err)
				}
				if len(got) != 1 || got[0].HasMTU || got[0].HasIPv4 || got[0].Token != "eth1" {
					t.Errorf("result = %+v", got)
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := readFixture(t, tt.fixture)
			var (
				client  *device.Client
				closeFn func()
			)
			if strings.HasPrefix(tt.fixture, "soap11/") {
				// A SOAP 1.1 payload is served only after explicit HTTP 415
				// fallback evidence. Serving text/xml to the initial SOAP 1.2
				// request would be a cross-version media-type violation, not a
				// valid fallback test.
				client, closeFn = newSOAP11RawClient(t, body)
			} else {
				client, closeFn = newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, body)
			}
			defer closeFn()
			tt.check(t, client)
		})
	}
}

// TestFixtureBackedFaults serves fault fixtures and confirms the device client
// surfaces them as errors.Is-compatible SOAP faults.
func TestFixtureBackedFaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		target  error
	}{
		{"not authorized", "soap12/fault-not-authorized.xml", soap.ErrNotAuthorized},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := readFixture(t, tt.fixture)
			client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusInternalServerError, body)
			defer closeFn()
			if _, err := client.GetHostname(context.Background()); !errors.Is(err, tt.target) {
				t.Fatalf("error = %v, want %v", err, tt.target)
			}
		})
	}
}

// newRawClient serves a fixed response body and returns a device client over
// the real SOAP transport.
func newRawClient(t *testing.T, contentType string, status int, body string) (*device.Client, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
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

// newSOAP11RawClient provides coherent fallback evidence: the initial SOAP 1.2
// request receives HTTP 415, then the SOAP 1.1 retry receives the text/xml
// fixture. A text/xml success response to the initial SOAP 1.2 attempt is
// intentionally invalid and must not be used as implicit fallback evidence.
func newSOAP11RawClient(t *testing.T, body string) (*device.Client, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "application/soap+xml") {
			w.WriteHeader(http.StatusUnsupportedMediaType)
			return
		}
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, body)
	}))
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

func soap12Envelope(payload string) string {
	return `<?xml version="1.0" encoding="UTF-8"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body>` + payload + `</env:Body></env:Envelope>`
}

func readFixture(t *testing.T, rel string) string {
	t.Helper()
	//nolint:gosec // Test-only, repository-owned fixture path.
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "fixtures", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read fixture %q: %v", rel, err)
	}
	return string(data)
}
