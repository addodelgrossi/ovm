package device_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func rawDevice(t *testing.T, payload string) *device.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope" xmlns:tds="http://www.onvif.org/ver10/device/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><env:Body>`+payload+`</env:Body></env:Envelope>`)
	}))
	t.Cleanup(server.Close)
	transport, err := soap.NewClient(soap.Config{})
	if err != nil {
		t.Fatalf("soap.NewClient() error = %v", err)
	}
	client, err := device.NewClient(transport, server.URL)
	if err != nil {
		t.Fatalf("device.NewClient() error = %v", err)
	}
	return client
}

func TestConfigOperationsRejectMalformedResponses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	cases := map[string]struct {
		payload string
		call    func(*device.Client) error
	}{
		"ntp host type": {`<tds:GetNTPResponse><tds:NTPInformation><tt:FromDHCP>false</tt:FromDHCP><tt:NTPManual><tt:Type>URL</tt:Type></tt:NTPManual></tds:NTPInformation></tds:GetNTPResponse>`,
			func(c *device.Client) error { _, err := c.GetNTP(ctx); return err }},
		"ntp bool": {`<tds:GetNTPResponse><tds:NTPInformation><tt:FromDHCP>maybe</tt:FromDHCP></tds:NTPInformation></tds:GetNTPResponse>`,
			func(c *device.Client) error { _, err := c.GetNTP(ctx); return err }},
		"ntp missing": {`<tds:GetNTPResponse/>`,
			func(c *device.Client) error { _, err := c.GetNTP(ctx); return err }},
		"user level": {`<tds:GetUsersResponse><tds:User><tt:Username>a</tt:Username><tt:UserLevel>Root</tt:UserLevel></tds:User></tds:GetUsersResponse>`,
			func(c *device.Client) error { _, err := c.GetUsers(ctx); return err }},
		"reboot needed": {`<tds:SetNetworkInterfacesResponse/>`,
			func(c *device.Client) error {
				_, err := c.SetNetworkInterfaces(ctx, "eth0", device.IPv4Change{DHCP: true})
				return err
			}},
		"gateway": {`<tds:GetNetworkDefaultGatewayResponse><tds:NetworkGateway><tt:IPv4Address>gw</tt:IPv4Address></tds:NetworkGateway></tds:GetNetworkDefaultGatewayResponse>`,
			func(c *device.Client) error { _, err := c.GetNetworkDefaultGateway(ctx); return err }},
		"dns domain": {`<tds:GetDNSResponse><tds:DNSInformation><tt:FromDHCP>true</tt:FromDHCP><tt:SearchDomain>bad domain</tt:SearchDomain></tds:DNSInformation></tds:GetDNSResponse>`,
			func(c *device.Client) error { _, err := c.GetDNS(ctx); return err }},
		"binary log": {`<tds:GetSystemLogResponse><tds:SystemLog><tt:Binary/></tds:SystemLog></tds:GetSystemLogResponse>`,
			func(c *device.Client) error { _, err := c.GetSystemLog(ctx, "System", 100); return err }},
		"non-empty set": {`<tds:SetNTPResponse><tds:Extra/></tds:SetNTPResponse>`,
			func(c *device.Client) error { return c.SetNTP(ctx, true, nil) }},
		"wrong root": {`<tds:SetDNSResponse/>`,
			func(c *device.Client) error { return c.SetNTP(ctx, true, nil) }},
	}
	for name, tc := range cases {
		err := tc.call(rawDevice(t, tc.payload))
		if err == nil || errors.Is(err, device.ErrInvalidInput) {
			t.Errorf("%s: error = %v, want a response error", name, err)
		}
	}
}
