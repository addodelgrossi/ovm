package device_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func TestTimeAndNTPRoundTrip(t *testing.T) {
	t.Parallel()
	client, closeFn := newClients(t, prefixConfig())
	defer closeFn()
	ctx := context.Background()
	if err := client.SetNTP(ctx, false, []device.NetworkHost{{Type: "DNS", Address: "pool.ntp.org"}, {Type: "IPv4", Address: "192.0.2.123"}}); err != nil {
		t.Fatalf("SetNTP() error = %v", err)
	}
	info, err := client.GetNTP(ctx)
	if err != nil || info.FromDHCP || len(info.Manual) != 2 || info.Manual[0] != (device.NetworkHost{Type: "DNS", Address: "pool.ntp.org"}) {
		t.Fatalf("GetNTP() = %+v, %v", info, err)
	}
	if err := client.SetSystemDateAndTime(ctx, device.SystemDateTimeChange{DateTimeType: "NTP", TimeZone: "BRT3"}); err != nil {
		t.Fatalf("SetSystemDateAndTime(NTP) error = %v", err)
	}
	current, err := client.GetSystemDateAndTime(ctx)
	if err != nil || current.DateTimeType != "NTP" || current.TimeZone != "BRT3" {
		t.Fatalf("GetSystemDateAndTime() = %+v, %v", current, err)
	}
	manual := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := client.SetSystemDateAndTime(ctx, device.SystemDateTimeChange{DateTimeType: "Manual", UTC: manual}); err != nil {
		t.Fatalf("SetSystemDateAndTime(Manual) error = %v", err)
	}
	current, err = client.GetSystemDateAndTime(ctx)
	if err != nil || !current.UTCValid || current.UTC.Sub(manual) > 5*time.Second || current.UTC.Before(manual) {
		t.Fatalf("manual time = %+v, %v", current, err)
	}
}

func TestTimeAndNTPValidateInput(t *testing.T) {
	t.Parallel()
	client, closeFn := newClients(t, fakedevice.Config{})
	defer closeFn()
	ctx := context.Background()
	for name, call := range map[string]func() error{
		"bad host": func() error {
			return client.SetNTP(ctx, false, []device.NetworkHost{{Type: "DNS", Address: "bad host"}})
		},
		"bad type": func() error { return client.SetNTP(ctx, false, []device.NetworkHost{{Type: "URL", Address: "x"}}) },
		"bad mode": func() error {
			return client.SetSystemDateAndTime(ctx, device.SystemDateTimeChange{DateTimeType: "GPS"})
		},
		"no utc": func() error {
			return client.SetSystemDateAndTime(ctx, device.SystemDateTimeChange{DateTimeType: "Manual"})
		},
		"tz markup": func() error {
			return client.SetSystemDateAndTime(ctx, device.SystemDateTimeChange{DateTimeType: "NTP", TimeZone: "<x>"})
		},
		"no user": func() error { return client.SetUser(ctx, nil) },
		"anonymous": func() error {
			return client.CreateUsers(ctx, []device.UserChange{{Username: "a", Password: "p", UserLevel: "Anonymous"}})
		},
		"bad level": func() error {
			return client.CreateUsers(ctx, []device.UserChange{{Username: "a", Password: "p", UserLevel: "Root"}})
		},
		"empty pass": func() error { return client.SetUser(ctx, []device.UserChange{{Username: "a", UserLevel: "User"}}) },
		"bad ip": func() error {
			_, err := client.SetNetworkInterfaces(ctx, "eth0", device.IPv4Change{Address: "300.1.1.1", PrefixLength: 24})
			return err
		},
		"bad prefix": func() error {
			_, err := client.SetNetworkInterfaces(ctx, "eth0", device.IPv4Change{Address: "192.0.2.9", PrefixLength: 40})
			return err
		},
		"bad gw":    func() error { return client.SetNetworkDefaultGateway(ctx, []string{"gateway"}) },
		"bad dns":   func() error { return client.SetDNS(ctx, false, []string{"::1"}) },
		"bad reset": func() error { return client.SetSystemFactoryDefault(ctx, "Medium") },
		"bad log":   func() error { _, err := client.GetSystemLog(ctx, "Kernel", 100); return err },
	} {
		if err := call(); !errors.Is(err, device.ErrInvalidInput) {
			t.Errorf("%s: error = %v, want ErrInvalidInput", name, err)
		}
	}
}

func TestUsersRoundTripAndPasswordChangeTakesEffect(t *testing.T) {
	t.Parallel()
	client, closeFn := newClients(t, fakedevice.Config{Username: "admin", Password: "old-secret"})
	defer closeFn()
	ctx := context.Background()
	users, err := client.GetUsers(ctx)
	if err != nil || len(users) != 1 || users[0] != (device.User{Username: "admin", UserLevel: "Administrator"}) {
		t.Fatalf("GetUsers() = %+v, %v", users, err)
	}
	viewer := device.UserChange{Username: "viewer", Password: "p&ss<word>", UserLevel: "User"} //nolint:gosec // synthetic test credential
	if err := client.CreateUsers(ctx, []device.UserChange{viewer}); err != nil {
		t.Fatalf("CreateUsers() error = %v", err)
	}
	if err := client.CreateUsers(ctx, []device.UserChange{{Username: "viewer", Password: "x", UserLevel: "User"}}); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("duplicate CreateUsers() error = %v", err)
	}
	if err := client.SetUser(ctx, []device.UserChange{{Username: "viewer", Password: "new-pass", UserLevel: "Operator"}}); err != nil {
		t.Fatalf("SetUser() error = %v", err)
	}
	users, _ = client.GetUsers(ctx)
	if len(users) != 2 || users[1].UserLevel != "Operator" {
		t.Fatalf("users after SetUser = %+v", users)
	}
	if err := client.DeleteUsers(ctx, []string{"viewer"}); err != nil {
		t.Fatalf("DeleteUsers() error = %v", err)
	}
	if users, _ = client.GetUsers(ctx); len(users) != 1 {
		t.Fatalf("users after delete = %+v", users)
	}
}

func TestNetworkDNSGatewayRoundTrip(t *testing.T) {
	t.Parallel()
	client, closeFn := newClients(t, fakedevice.Config{Settings: fakedevice.DeviceSettings{NetworkRebootNeeded: true}})
	defer closeFn()
	ctx := context.Background()
	interfaces, err := client.GetNetworkInterfaces(ctx)
	if err != nil || len(interfaces) == 0 {
		t.Fatalf("GetNetworkInterfaces() = %v", err)
	}
	reboot, err := client.SetNetworkInterfaces(ctx, interfaces[0].Token, device.IPv4Change{Address: "192.0.2.77", PrefixLength: 24})
	if err != nil || !reboot {
		t.Fatalf("SetNetworkInterfaces() = %v, %v", reboot, err)
	}
	interfaces, _ = client.GetNetworkInterfaces(ctx)
	if interfaces[0].DHCP || len(interfaces[0].IPv4Addresses) == 0 || interfaces[0].IPv4Addresses[0].Address != "192.0.2.77" {
		t.Fatalf("interface after change = %+v", interfaces[0])
	}
	if _, err := client.SetNetworkInterfaces(ctx, "missing", device.IPv4Change{DHCP: true}); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("unknown interface error = %v", err)
	}
	if err := client.SetNetworkDefaultGateway(ctx, []string{"192.0.2.254"}); err != nil {
		t.Fatalf("SetNetworkDefaultGateway() error = %v", err)
	}
	if gateways, err := client.GetNetworkDefaultGateway(ctx); err != nil || len(gateways) != 1 || gateways[0] != "192.0.2.254" {
		t.Fatalf("GetNetworkDefaultGateway() = %v, %v", gateways, err)
	}
	if err := client.SetDNS(ctx, false, []string{"192.0.2.53"}); err != nil {
		t.Fatalf("SetDNS() error = %v", err)
	}
	if dns, err := client.GetDNS(ctx); err != nil || dns.FromDHCP || len(dns.Manual) != 1 || dns.Manual[0] != "192.0.2.53" {
		t.Fatalf("GetDNS() = %+v, %v", dns, err)
	}
}

func TestMaintenanceRebootFactoryDefaultAndLog(t *testing.T) {
	t.Parallel()
	client, closeFn := newClients(t, fakedevice.Config{})
	defer closeFn()
	ctx := context.Background()
	if err := client.SetNTP(ctx, false, []device.NetworkHost{{Type: "IPv4", Address: "192.0.2.10"}}); err != nil {
		t.Fatalf("SetNTP() error = %v", err)
	}
	if err := client.SetSystemFactoryDefault(ctx, device.FactoryDefaultSoft); err != nil {
		t.Fatalf("SetSystemFactoryDefault() error = %v", err)
	}
	if info, _ := client.GetNTP(ctx); len(info.Manual) != 0 {
		t.Fatalf("NTP survived the factory reset: %+v", info)
	}
	log, err := client.GetSystemLog(ctx, "System", 1<<20)
	if err != nil || !strings.Contains(log, "factory default (soft) applied") {
		t.Fatalf("GetSystemLog() = %q, %v", log, err)
	}
	if short, _ := client.GetSystemLog(ctx, "System", 10); len(short) > 10 {
		t.Fatalf("log not truncated: %d bytes", len(short))
	}
	message, err := client.SystemReboot(ctx)
	if err != nil || message == "" {
		t.Fatalf("SystemReboot() = %q, %v", message, err)
	}
}
