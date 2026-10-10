package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

func TestRebootWaitsForTheCameraToReturn(t *testing.T) {
	t.Parallel()
	device, host := deviceFakeHost(t, func(config *fakedevice.Config) {
		config.Settings.RebootDuration = 150 * time.Millisecond
	})
	maintenance := &productionMedia{readbackPolicy: &readbackPolicy{attempts: 10, delay: 50 * time.Millisecond}}
	result, err := maintenance.Reboot(context.Background(), deviceRequest(t, host))
	if err != nil || !result.BackOnline || result.Message == "" || result.DowntimeSeconds < 0.15 {
		t.Fatalf("Reboot() = %+v, %v", result, err)
	}
	if device.Reboots() != 1 {
		t.Fatalf("reboots = %d", device.Reboots())
	}
}

func TestRebootReportsACameraThatDoesNotReturn(t *testing.T) {
	t.Parallel()
	_, host := deviceFakeHost(t, func(config *fakedevice.Config) {
		config.Settings.RebootDuration = time.Hour
	})
	maintenance := &productionMedia{readbackPolicy: &readbackPolicy{attempts: 2, delay: 20 * time.Millisecond}}
	result, err := maintenance.Reboot(context.Background(), deviceRequest(t, host))
	if err != nil || result.BackOnline {
		t.Fatalf("Reboot() = %+v, %v", result, err)
	}
}

func TestSoftFactoryResetAndSystemLog(t *testing.T) {
	t.Parallel()
	_, host := deviceFakeHost(t, func(config *fakedevice.Config) {
		config.Settings.SystemLog = "boot ok\x07\nnext line\n"
	})
	maintenance := &productionMedia{readbackPolicy: &readbackPolicy{attempts: 3, delay: 10 * time.Millisecond}}
	request := deviceRequest(t, host)
	if _, err := maintenance.ApplyTime(context.Background(), request, TimeChange{Mode: "NTP", NTPServers: []string{"192.0.2.123"}}); err != nil {
		t.Fatalf("ApplyTime() error = %v", err)
	}
	result, err := maintenance.SoftFactoryReset(context.Background(), request)
	if err != nil || !result.BackOnline {
		t.Fatalf("SoftFactoryReset() = %+v, %v", result, err)
	}
	if view, err := maintenance.ReadTime(context.Background(), request); err != nil || view.DateTimeType != "Manual" {
		t.Fatalf("time after reset = %+v, %v", view, err)
	}
	log, err := maintenance.SystemLog(context.Background(), request, "System")
	if err != nil || strings.ContainsRune(log.Text, '\x07') || !strings.Contains(log.Text, "factory default (soft) applied") || log.Truncated {
		t.Fatalf("SystemLog() = %+v, %v", log, err)
	}
	if _, err := maintenance.SystemLog(context.Background(), request, "Kernel"); err == nil {
		t.Fatal("SystemLog() accepted an unknown log type")
	}
	var appErr *Error
	if _, err := maintenance.SystemLog(context.Background(), request, "Kernel"); !errors.As(err, &appErr) || appErr.Category != CategoryUsage {
		t.Fatalf("unknown log type error = %v", err)
	}
}
