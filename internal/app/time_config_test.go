package app

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

func deviceFakeHost(t *testing.T, mutate func(*fakedevice.Config)) (*fakedevice.Device, string) {
	t.Helper()
	srv := newUnstartedFake(t, func(base string) fakedevice.Config {
		config := fakedevice.Config{
			HTTPAuth: fakedevice.HTTPAuthDigest, DigestAlgorithm: "SHA-256", RequireUsernameToken: true,
			Username: "admin", Password: "encoder-secret",
			Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
				{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
			}},
		}
		if mutate != nil {
			mutate(&config)
		}
		return config
	})
	return srv.device, srv.host
}

func deviceRequest(t *testing.T, host string) DeviceRequest {
	t.Helper()
	request := encoderRequest(t, host)
	return DeviceRequest{Host: request.Host, Credentials: request.Credentials, Timeout: request.Timeout}
}

func TestApplyTimeConfiguresNTPWithReadback(t *testing.T) {
	t.Parallel()
	_, host := deviceFakeHost(t, nil)
	view, err := fastEncoder().ReadTime(context.Background(), deviceRequest(t, host))
	if err != nil || view.DateTimeType != "Manual" || view.ClockOffsetSeconds == nil || view.UncertaintySeconds < 1 {
		t.Fatalf("ReadTime() = %+v, %v", view, err)
	}
	result, err := fastEncoder().ApplyTime(context.Background(), deviceRequest(t, host), TimeChange{
		Mode: "NTP", NTPServers: []string{"pool.ntp.org", "192.0.2.123"}, TimeZone: "BRT3",
	})
	if err != nil || result.Report.Outcome != OutcomeExact || result.After.DateTimeType != "NTP" || len(result.After.NTPServers) != 2 {
		t.Fatalf("ApplyTime(NTP) = %+v, %v", result, err)
	}
}

func TestApplyTimeSyncsAManuallySkewedClock(t *testing.T) {
	t.Parallel()
	_, host := deviceFakeHost(t, func(config *fakedevice.Config) { config.ClockSkew = 90 * time.Minute })
	before, err := fastEncoder().ReadTime(context.Background(), deviceRequest(t, host))
	if err != nil || before.ClockOffsetSeconds == nil || math.Abs(*before.ClockOffsetSeconds-5400) > 3 {
		t.Fatalf("skewed ReadTime() = %+v, %v", before, err)
	}
	result, err := fastEncoder().ApplyTime(context.Background(), deviceRequest(t, host), TimeChange{Mode: "Manual"})
	if err != nil || result.Report.Outcome != OutcomeExact {
		t.Fatalf("ApplyTime(Manual) = %+v, %v", result, err)
	}
	if offset := *result.After.ClockOffsetSeconds; math.Abs(offset) > manualSyncTolerance {
		t.Fatalf("offset after sync = %f", offset)
	}
}

func TestApplyTimeValidatesInput(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]TimeChange{
		"mode":        {Mode: "GPS"},
		"no servers":  {Mode: "NTP"},
		"bad server":  {Mode: "NTP", NTPServers: []string{"not a host"}},
		"too many":    {Mode: "NTP", NTPServers: []string{"a.b", "c.d", "e.f", "g.h", "i.j"}},
		"zone markup": {Mode: "Manual", TimeZone: "<UTC>"},
	} {
		_, err := fastEncoder().ApplyTime(context.Background(), DeviceRequest{Host: "192.0.2.1"}, change)
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Category != CategoryUsage {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}
