package device_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/device"
)

// ns returns the Device WSDL namespace declaration bound to tds and the shared
// schema namespace bound to tt, matching the prefixes used throughout the
// malformed matrices below.
const (
	tdsNS = ` xmlns:tds="http://www.onvif.org/ver10/device/wsdl"`
	ttNS  = ` xmlns:tt="http://www.onvif.org/ver10/schema"`
)

// invoke runs one operation against a raw server returning the given payload
// and reports the error. It centralizes the transport wiring so each matrix row
// only supplies a payload and the operation to call.
func invoke(t *testing.T, payload string, call func(*device.Client) error) error {
	t.Helper()
	client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, soap12Envelope(payload))
	defer closeFn()
	return call(client)
}

func callSystemDateAndTime(c *device.Client) error {
	_, err := c.GetSystemDateAndTime(context.Background())
	return err
}

func callDeviceInformation(c *device.Client) error {
	_, err := c.GetDeviceInformation(context.Background())
	return err
}

func callCapabilities(c *device.Client) error {
	_, err := c.GetCapabilities(context.Background())
	return err
}

func callServices(c *device.Client) error {
	_, err := c.GetServices(context.Background())
	return err
}

func callScopes(c *device.Client) error {
	_, err := c.GetScopes(context.Background())
	return err
}

func callHostname(c *device.Client) error {
	_, err := c.GetHostname(context.Background())
	return err
}

func callNetworkInterfaces(c *device.Client) error {
	_, err := c.GetNetworkInterfaces(context.Background())
	return err
}

type malformedCase struct {
	name    string
	payload string
	call    func(*device.Client) error
	wantErr error
}

func TestMalformedResponseMatrix(t *testing.T) {
	t.Parallel()

	cases := []malformedCase{
		// GetSystemDateAndTime
		{
			name:    "sdt missing DateTimeType",
			payload: `<tds:GetSystemDateAndTimeResponse` + tdsNS + `><tds:SystemDateAndTime` + ttNS + `><tt:DaylightSavings>false</tt:DaylightSavings></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`,
			call:    callSystemDateAndTime,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "sdt missing DaylightSavings",
			payload: `<tds:GetSystemDateAndTimeResponse` + tdsNS + `><tds:SystemDateAndTime` + ttNS + `><tt:DateTimeType>NTP</tt:DateTimeType></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`,
			call:    callSystemDateAndTime,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "sdt invalid DaylightSavings",
			payload: `<tds:GetSystemDateAndTimeResponse` + tdsNS + `><tds:SystemDateAndTime` + ttNS + `><tt:DateTimeType>NTP</tt:DateTimeType><tt:DaylightSavings>maybe</tt:DaylightSavings></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`,
			call:    callSystemDateAndTime,
			wantErr: device.ErrInvalidField,
		},
		{
			name:    "sdt UTCDateTime incomplete time",
			payload: `<tds:GetSystemDateAndTimeResponse` + tdsNS + `><tds:SystemDateAndTime` + ttNS + `><tt:DateTimeType>Manual</tt:DateTimeType><tt:DaylightSavings>false</tt:DaylightSavings><tt:UTCDateTime><tt:Time><tt:Hour>12</tt:Hour><tt:Minute>0</tt:Minute></tt:Time><tt:Date><tt:Year>2026</tt:Year><tt:Month>1</tt:Month><tt:Day>1</tt:Day></tt:Date></tt:UTCDateTime></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`,
			call:    callSystemDateAndTime,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "sdt UTCDateTime impossible calendar date",
			payload: `<tds:GetSystemDateAndTimeResponse` + tdsNS + `><tds:SystemDateAndTime` + ttNS + `><tt:DateTimeType>Manual</tt:DateTimeType><tt:DaylightSavings>false</tt:DaylightSavings><tt:UTCDateTime><tt:Time><tt:Hour>0</tt:Hour><tt:Minute>0</tt:Minute><tt:Second>0</tt:Second></tt:Time><tt:Date><tt:Year>2026</tt:Year><tt:Month>2</tt:Month><tt:Day>31</tt:Day></tt:Date></tt:UTCDateTime></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`,
			call:    callSystemDateAndTime,
			wantErr: device.ErrInvalidField,
		},
		{
			name:    "sdt UTCDateTime leap second 60 rejected honestly",
			payload: `<tds:GetSystemDateAndTimeResponse` + tdsNS + `><tds:SystemDateAndTime` + ttNS + `><tt:DateTimeType>Manual</tt:DateTimeType><tt:DaylightSavings>false</tt:DaylightSavings><tt:UTCDateTime><tt:Time><tt:Hour>23</tt:Hour><tt:Minute>59</tt:Minute><tt:Second>60</tt:Second></tt:Time><tt:Date><tt:Year>2026</tt:Year><tt:Month>6</tt:Month><tt:Day>30</tt:Day></tt:Date></tt:UTCDateTime></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`,
			call:    callSystemDateAndTime,
			wantErr: device.ErrInvalidField,
		},
		{
			name:    "sdt duplicate SystemDateAndTime",
			payload: `<tds:GetSystemDateAndTimeResponse` + tdsNS + ttNS + `><tds:SystemDateAndTime><tt:DateTimeType>NTP</tt:DateTimeType><tt:DaylightSavings>false</tt:DaylightSavings></tds:SystemDateAndTime><tds:SystemDateAndTime><tt:DateTimeType>Manual</tt:DateTimeType><tt:DaylightSavings>true</tt:DaylightSavings></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`,
			call:    callSystemDateAndTime,
			wantErr: device.ErrInvalidField,
		},

		// GetDeviceInformation
		{
			name:    "info missing HardwareId",
			payload: `<tds:GetDeviceInformationResponse` + tdsNS + `><tds:Manufacturer>M</tds:Manufacturer><tds:Model>X</tds:Model><tds:FirmwareVersion>1</tds:FirmwareVersion><tds:SerialNumber>S</tds:SerialNumber></tds:GetDeviceInformationResponse>`,
			call:    callDeviceInformation,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "info duplicate Model",
			payload: `<tds:GetDeviceInformationResponse` + tdsNS + `><tds:Manufacturer>M</tds:Manufacturer><tds:Model>X</tds:Model><tds:Model>Y</tds:Model><tds:FirmwareVersion>1</tds:FirmwareVersion><tds:SerialNumber>S</tds:SerialNumber><tds:HardwareId>H</tds:HardwareId></tds:GetDeviceInformationResponse>`,
			call:    callDeviceInformation,
			wantErr: device.ErrInvalidField,
		},

		// GetCapabilities
		{
			name:    "capabilities missing element",
			payload: `<tds:GetCapabilitiesResponse` + tdsNS + `/>`,
			call:    callCapabilities,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "capabilities duplicate element",
			payload: `<tds:GetCapabilitiesResponse` + tdsNS + `><tds:Capabilities/><tds:Capabilities/></tds:GetCapabilitiesResponse>`,
			call:    callCapabilities,
			wantErr: device.ErrInvalidField,
		},

		// GetServices
		{
			name:    "services missing XAddr",
			payload: `<tds:GetServicesResponse` + tdsNS + ttNS + `><tds:Service><tds:Namespace>urn:x</tds:Namespace><tds:Version><tt:Major>1</tt:Major><tt:Minor>0</tt:Minor></tds:Version></tds:Service></tds:GetServicesResponse>`,
			call:    callServices,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "services missing Version",
			payload: `<tds:GetServicesResponse` + tdsNS + `><tds:Service><tds:Namespace>urn:x</tds:Namespace><tds:XAddr>http://a.invalid</tds:XAddr></tds:Service></tds:GetServicesResponse>`,
			call:    callServices,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "services invalid Major",
			payload: `<tds:GetServicesResponse` + tdsNS + ttNS + `><tds:Service><tds:Namespace>urn:x</tds:Namespace><tds:XAddr>http://a.invalid</tds:XAddr><tds:Version><tt:Major>x</tt:Major><tt:Minor>0</tt:Minor></tds:Version></tds:Service></tds:GetServicesResponse>`,
			call:    callServices,
			wantErr: device.ErrInvalidField,
		},

		// GetScopes
		{
			name:    "scopes missing ScopeDef",
			payload: `<tds:GetScopesResponse` + tdsNS + ttNS + `><tds:Scopes><tt:ScopeItem>onvif://x</tt:ScopeItem></tds:Scopes></tds:GetScopesResponse>`,
			call:    callScopes,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "scopes missing ScopeItem",
			payload: `<tds:GetScopesResponse` + tdsNS + ttNS + `><tds:Scopes><tt:ScopeDef>Fixed</tt:ScopeDef></tds:Scopes></tds:GetScopesResponse>`,
			call:    callScopes,
			wantErr: device.ErrMissingField,
		},

		// GetHostname
		{
			name:    "hostname missing FromDHCP",
			payload: `<tds:GetHostnameResponse` + tdsNS + ttNS + `><tds:HostnameInformation><tt:Name>cam</tt:Name></tds:HostnameInformation></tds:GetHostnameResponse>`,
			call:    callHostname,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "hostname invalid FromDHCP",
			payload: `<tds:GetHostnameResponse` + tdsNS + ttNS + `><tds:HostnameInformation><tt:FromDHCP>yes</tt:FromDHCP></tds:HostnameInformation></tds:GetHostnameResponse>`,
			call:    callHostname,
			wantErr: device.ErrInvalidField,
		},
		{
			name:    "hostname duplicate FromDHCP",
			payload: `<tds:GetHostnameResponse` + tdsNS + ttNS + `><tds:HostnameInformation><tt:FromDHCP>true</tt:FromDHCP><tt:FromDHCP>false</tt:FromDHCP></tds:HostnameInformation></tds:GetHostnameResponse>`,
			call:    callHostname,
			wantErr: device.ErrInvalidField,
		},

		// GetNetworkInterfaces
		{
			name:    "network no interfaces",
			payload: `<tds:GetNetworkInterfacesResponse` + tdsNS + `/>`,
			call:    callNetworkInterfaces,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "network missing token attribute",
			payload: `<tds:GetNetworkInterfacesResponse` + tdsNS + ttNS + `><tds:NetworkInterfaces><tt:Enabled>true</tt:Enabled></tds:NetworkInterfaces></tds:GetNetworkInterfacesResponse>`,
			call:    callNetworkInterfaces,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "network foreign-namespace token lookalike rejected",
			payload: `<tds:GetNetworkInterfacesResponse` + tdsNS + ttNS + ` xmlns:evil="urn:evil"><tds:NetworkInterfaces evil:token="eth0"><tt:Enabled>true</tt:Enabled></tds:NetworkInterfaces></tds:GetNetworkInterfacesResponse>`,
			call:    callNetworkInterfaces,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "network missing Enabled",
			payload: `<tds:GetNetworkInterfacesResponse` + tdsNS + ttNS + `><tds:NetworkInterfaces token="eth0"><tt:Info><tt:Name>eth0</tt:Name></tt:Info></tds:NetworkInterfaces></tds:GetNetworkInterfacesResponse>`,
			call:    callNetworkInterfaces,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "network duplicate Enabled",
			payload: `<tds:GetNetworkInterfacesResponse` + tdsNS + ttNS + `><tds:NetworkInterfaces token="eth0"><tt:Enabled>true</tt:Enabled><tt:Enabled>false</tt:Enabled></tds:NetworkInterfaces></tds:GetNetworkInterfacesResponse>`,
			call:    callNetworkInterfaces,
			wantErr: device.ErrInvalidField,
		},
		{
			name:    "network invalid IPv4 address syntax",
			payload: `<tds:GetNetworkInterfacesResponse` + tdsNS + ttNS + `><tds:NetworkInterfaces token="eth0"><tt:Enabled>true</tt:Enabled><tt:IPv4><tt:Enabled>true</tt:Enabled><tt:Config><tt:Manual><tt:Address>999.1.1.1</tt:Address><tt:PrefixLength>24</tt:PrefixLength></tt:Manual><tt:DHCP>false</tt:DHCP></tt:Config></tt:IPv4></tds:NetworkInterfaces></tds:GetNetworkInterfacesResponse>`,
			call:    callNetworkInterfaces,
			wantErr: device.ErrInvalidField,
		},
		{
			name:    "network IPv4 prefix out of range",
			payload: `<tds:GetNetworkInterfacesResponse` + tdsNS + ttNS + `><tds:NetworkInterfaces token="eth0"><tt:Enabled>true</tt:Enabled><tt:IPv4><tt:Enabled>true</tt:Enabled><tt:Config><tt:Manual><tt:Address>203.0.113.5</tt:Address><tt:PrefixLength>40</tt:PrefixLength></tt:Manual><tt:DHCP>false</tt:DHCP></tt:Config></tt:IPv4></tds:NetworkInterfaces></tds:GetNetworkInterfacesResponse>`,
			call:    callNetworkInterfaces,
			wantErr: device.ErrInvalidField,
		},
		{
			name:    "network IPv4 address without prefix",
			payload: `<tds:GetNetworkInterfacesResponse` + tdsNS + ttNS + `><tds:NetworkInterfaces token="eth0"><tt:Enabled>true</tt:Enabled><tt:IPv4><tt:Enabled>true</tt:Enabled><tt:Config><tt:Manual><tt:Address>203.0.113.5</tt:Address></tt:Manual><tt:DHCP>false</tt:DHCP></tt:Config></tt:IPv4></tds:NetworkInterfaces></tds:GetNetworkInterfacesResponse>`,
			call:    callNetworkInterfaces,
			wantErr: device.ErrMissingField,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := invoke(t, tc.payload, tc.call)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestOperationSuccessMatrix proves that well-formed, WSDL-complete responses
// decode without error and populate the expected typed values, so the stricter
// validation does not over-reject genuine responses.
func TestOperationSuccessMatrix(t *testing.T) {
	t.Parallel()

	t.Run("sdt complete with valid UTCDateTime", func(t *testing.T) {
		t.Parallel()
		payload := `<tds:GetSystemDateAndTimeResponse` + tdsNS + ttNS + `><tds:SystemDateAndTime><tt:DateTimeType>Manual</tt:DateTimeType><tt:DaylightSavings>true</tt:DaylightSavings><tt:UTCDateTime><tt:Time><tt:Hour>23</tt:Hour><tt:Minute>59</tt:Minute><tt:Second>59</tt:Second></tt:Time><tt:Date><tt:Year>2026</tt:Year><tt:Month>12</tt:Month><tt:Day>31</tt:Day></tt:Date></tt:UTCDateTime></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, soap12Envelope(payload))
		defer closeFn()
		got, err := client.GetSystemDateAndTime(context.Background())
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if !got.UTCValid || !got.DaylightSavings || got.DateTimeType != "Manual" {
			t.Fatalf("result = %+v", got)
		}
		if got.UTC.Year() != 2026 || got.UTC.Month() != 12 || got.UTC.Day() != 31 || got.UTC.Second() != 59 {
			t.Fatalf("UTC = %v", got.UTC)
		}
	})

	t.Run("services include required entry", func(t *testing.T) {
		t.Parallel()
		payload := `<tds:GetServicesResponse` + tdsNS + ttNS + `><tds:Service><tds:Namespace>urn:synthetic:service</tds:Namespace><tds:XAddr>http://device.invalid/service</tds:XAddr><tds:Version><tt:Major>1</tt:Major><tt:Minor>0</tt:Minor></tds:Version></tds:Service></tds:GetServicesResponse>`
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, soap12Envelope(payload))
		defer closeFn()
		svcs, err := client.GetServices(context.Background())
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(svcs) != 1 || svcs[0].Namespace != "urn:synthetic:service" {
			t.Fatalf("services = %+v, want one complete service", svcs)
		}
	})

	t.Run("device information empty-but-present values accepted", func(t *testing.T) {
		t.Parallel()
		payload := `<tds:GetDeviceInformationResponse` + tdsNS + `><tds:Manufacturer></tds:Manufacturer><tds:Model></tds:Model><tds:FirmwareVersion></tds:FirmwareVersion><tds:SerialNumber></tds:SerialNumber><tds:HardwareId></tds:HardwareId></tds:GetDeviceInformationResponse>`
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, soap12Envelope(payload))
		defer closeFn()
		if _, err := client.GetDeviceInformation(context.Background()); err != nil {
			t.Fatalf("error = %v, want success for present-but-empty fields", err)
		}
	})

	t.Run("hostname name optional", func(t *testing.T) {
		t.Parallel()
		payload := `<tds:GetHostnameResponse` + tdsNS + ttNS + `><tds:HostnameInformation><tt:FromDHCP>true</tt:FromDHCP></tds:HostnameInformation></tds:GetHostnameResponse>`
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, soap12Envelope(payload))
		defer closeFn()
		got, err := client.GetHostname(context.Background())
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if !got.FromDHCP || got.Name != "" {
			t.Fatalf("hostname = %+v", got)
		}
	})

	t.Run("network dhcp provenance decoded", func(t *testing.T) {
		t.Parallel()
		payload := `<tds:GetNetworkInterfacesResponse` + tdsNS + ttNS + `><tds:NetworkInterfaces token="eth0"><tt:Enabled>true</tt:Enabled><tt:IPv4><tt:Enabled>true</tt:Enabled><tt:Config><tt:FromDHCP><tt:Address>203.0.113.20</tt:Address><tt:PrefixLength>24</tt:PrefixLength></tt:FromDHCP><tt:DHCP>true</tt:DHCP></tt:Config></tt:IPv4></tds:NetworkInterfaces></tds:GetNetworkInterfacesResponse>`
		client, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusOK, soap12Envelope(payload))
		defer closeFn()
		ifaces, err := client.GetNetworkInterfaces(context.Background())
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(ifaces) != 1 || len(ifaces[0].IPv4Addresses) != 1 {
			t.Fatalf("ifaces = %+v", ifaces)
		}
		addr := ifaces[0].IPv4Addresses[0]
		if !addr.FromDHCP || addr.Address != "203.0.113.20" || addr.PrefixLength != 24 {
			t.Fatalf("address = %+v, want DHCP-derived 203.0.113.20/24", addr)
		}
	})
}

// TestRequiredCardinalityAndEnumRegressions pins current ver10 WSDL/XSD
// requirements that are easy to accidentally treat as vendor-optional.
func TestRequiredCardinalityAndEnumRegressions(t *testing.T) {
	t.Parallel()

	cases := []malformedCase{
		{
			name:    "system date time type enum",
			payload: `<tds:GetSystemDateAndTimeResponse` + tdsNS + ttNS + `><tds:SystemDateAndTime><tt:DateTimeType>GPS</tt:DateTimeType><tt:DaylightSavings>false</tt:DaylightSavings></tds:SystemDateAndTime></tds:GetSystemDateAndTimeResponse>`,
			call:    callSystemDateAndTime,
			wantErr: device.ErrInvalidField,
		},
		{
			name:    "capability category requires XAddr",
			payload: `<tds:GetCapabilitiesResponse` + tdsNS + ttNS + `><tds:Capabilities><tt:Media/></tds:Capabilities></tds:GetCapabilitiesResponse>`,
			call:    callCapabilities,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "services list is non-empty",
			payload: `<tds:GetServicesResponse` + tdsNS + `/>`,
			call:    callServices,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "scopes list is non-empty",
			payload: `<tds:GetScopesResponse` + tdsNS + `/>`,
			call:    callScopes,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "scope definition enum",
			payload: `<tds:GetScopesResponse` + tdsNS + ttNS + `><tds:Scopes><tt:ScopeDef>Mutable</tt:ScopeDef><tt:ScopeItem>onvif://www.onvif.org/name/Test</tt:ScopeItem></tds:Scopes></tds:GetScopesResponse>`,
			call:    callScopes,
			wantErr: device.ErrInvalidField,
		},
		{
			name:    "IPv4 requires Enabled",
			payload: `<tds:GetNetworkInterfacesResponse` + tdsNS + ttNS + `><tds:NetworkInterfaces token="eth0"><tt:Enabled>true</tt:Enabled><tt:IPv4><tt:Config><tt:DHCP>false</tt:DHCP></tt:Config></tt:IPv4></tds:NetworkInterfaces></tds:GetNetworkInterfacesResponse>`,
			call:    callNetworkInterfaces,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "IPv4 requires Config",
			payload: `<tds:GetNetworkInterfacesResponse` + tdsNS + ttNS + `><tds:NetworkInterfaces token="eth0"><tt:Enabled>true</tt:Enabled><tt:IPv4><tt:Enabled>true</tt:Enabled></tt:IPv4></tds:NetworkInterfaces></tds:GetNetworkInterfacesResponse>`,
			call:    callNetworkInterfaces,
			wantErr: device.ErrMissingField,
		},
		{
			name:    "IPv4 configuration requires DHCP",
			payload: `<tds:GetNetworkInterfacesResponse` + tdsNS + ttNS + `><tds:NetworkInterfaces token="eth0"><tt:Enabled>true</tt:Enabled><tt:IPv4><tt:Enabled>true</tt:Enabled><tt:Config/></tt:IPv4></tds:NetworkInterfaces></tds:GetNetworkInterfacesResponse>`,
			call:    callNetworkInterfaces,
			wantErr: device.ErrMissingField,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := invoke(t, tc.payload, tc.call); !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
