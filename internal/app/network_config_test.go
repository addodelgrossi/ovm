package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

func TestApplyNetworkKeepsAddressAndChangesGatewayAndDNS(t *testing.T) {
	t.Parallel()
	_, host := deviceFakeHost(t, nil)
	request := deviceRequest(t, host)
	before, err := fastEncoder().ReadNetwork(context.Background(), request)
	if err != nil || before.InterfaceToken == "" || before.Address == "" {
		t.Fatalf("ReadNetwork() = %+v, %v", before, err)
	}
	gateway := strings.Join(strings.Split(before.Address, ".")[:3], ".") + ".254"
	result, err := fastEncoder().ApplyNetwork(context.Background(), request, NetworkChange{
		Address: before.Address, PrefixLength: before.PrefixLength, Gateway: gateway, DNSServers: []string{"192.0.2.53"},
	})
	if err != nil || result.Report.Outcome != OutcomeExact || result.NewEndpoint != "" || result.After.Gateways[0] != gateway {
		t.Fatalf("ApplyNetwork() = %+v, %v", result, err)
	}
}

func TestApplyNetworkRebootsAndReportsTheMovedEndpoint(t *testing.T) {
	t.Parallel()
	device, host := deviceFakeHost(t, func(config *fakedevice.Config) {
		config.Settings.NetworkRebootNeeded = true
	})
	request := deviceRequest(t, host)
	request.Timeout = time.Second
	result, err := fastEncoder().ApplyNetwork(context.Background(), request, NetworkChange{Address: "192.0.2.77", PrefixLength: 24})
	if err != nil || result.Report.Outcome != OutcomeDisconnected || !result.RebootRequested {
		t.Fatalf("ApplyNetwork() = %+v, %v", result, err)
	}
	if !strings.HasPrefix(result.NewEndpoint, "http://192.0.2.77:") || !strings.HasSuffix(result.NewEndpoint, "/onvif/device_service") {
		t.Fatalf("NewEndpoint = %q", result.NewEndpoint)
	}
	if device.Reboots() != 1 {
		t.Fatalf("reboots = %d, want 1", device.Reboots())
	}
}

func TestApplyNetworkValidatesBeforeContactingTheCamera(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]NetworkChange{
		"loopback":     {Address: "127.0.0.5", PrefixLength: 24},
		"multicast":    {Address: "239.1.1.1", PrefixLength: 24},
		"network addr": {Address: "192.0.2.0", PrefixLength: 24},
		"prefix":       {Address: "192.0.2.10", PrefixLength: 31},
		"gateway away": {Address: "192.0.2.10", PrefixLength: 24, Gateway: "198.51.100.1"},
		"gateway self": {Address: "192.0.2.10", PrefixLength: 24, Gateway: "192.0.2.10"},
		"dns":          {DHCP: true, DNSServers: []string{"dns.example"}},
		"too many dns": {DHCP: true, DNSServers: []string{"192.0.2.1", "192.0.2.2", "192.0.2.3", "192.0.2.4"}},
	} {
		_, err := fastEncoder().ApplyNetwork(context.Background(), DeviceRequest{Host: "192.0.2.200"}, change)
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Category != CategoryUsage {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}

func TestMovedEndpointKeepsSchemePortAndPath(t *testing.T) {
	t.Parallel()
	got, err := movedEndpoint("https://192.0.2.10:8443/onvif/device_service", "192.0.2.20")
	if err != nil || got != "https://192.0.2.20:8443/onvif/device_service" {
		t.Fatalf("movedEndpoint() = %q, %v", got, err)
	}
}
