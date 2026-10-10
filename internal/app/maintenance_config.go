package app

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/addodelgrossi/ovm/pkg/onvif/device"
)

// MaxSystemLogBytes bounds a retrieved system log.
const MaxSystemLogBytes = 256 << 10

// MaintenanceResult reports a reboot or reset and whether the camera came
// back within the wait.
type MaintenanceResult struct {
	Message         string  `json:"message"`
	BackOnline      bool    `json:"back_online"`
	DowntimeSeconds float64 `json:"downtime_seconds"`
}

// SystemLogResult is a bounded, sanitized system log.
type SystemLogResult struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Truncated bool   `json:"truncated"`
}

// MaintenanceUseCase reboots, soft-resets, and reads logs.
type MaintenanceUseCase interface {
	Reboot(ctx context.Context, request DeviceRequest) (MaintenanceResult, error)
	SoftFactoryReset(ctx context.Context, request DeviceRequest) (MaintenanceResult, error)
	SystemLog(ctx context.Context, request DeviceRequest, logType string) (SystemLogResult, error)
}

// NewProductionMaintenance returns the production MaintenanceUseCase.
func NewProductionMaintenance() MaintenanceUseCase { return &productionMedia{} }

// recoveryWait is how long a reboot or reset waits for the camera to answer
// again; it stays below the desktop call timeout.
var recoveryWait = readbackPolicy{attempts: 22, delay: 2 * time.Second}

func (p *productionMedia) recoveryPolicy() readbackPolicy {
	if p.readbackPolicy != nil {
		return *p.readbackPolicy
	}
	return recoveryWait
}

// waitForRecovery polls an authenticated clock read until the camera answers.
// The first poll happens only after one delay so a camera that has not yet
// gone down is not mistaken for one that is back.
func (p *productionMedia) waitForRecovery(ctx context.Context, request DeviceRequest, started time.Time) (bool, float64) {
	policy := p.recoveryPolicy()
	for attempt := 0; attempt < policy.attempts; attempt++ {
		timer := time.NewTimer(policy.delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, 0
		case <-timer.C:
		}
		probe := request
		probe.Timeout = min(request.Timeout, 5*time.Second)
		if probe.Timeout <= 0 {
			probe.Timeout = 5 * time.Second
		}
		seam, opCtx, cancel, err := p.openDevice(ctx, probe)
		if err != nil {
			continue
		}
		_, err = seam.SystemDateAndTime(opCtx)
		cancel()
		if err == nil {
			return true, p.now().Sub(started).Seconds()
		}
	}
	return false, 0
}

// Reboot restarts the camera and waits for it to answer again.
func (p *productionMedia) Reboot(ctx context.Context, request DeviceRequest) (MaintenanceResult, error) {
	seam, opCtx, cancel, err := p.openDevice(ctx, request)
	if err != nil {
		return MaintenanceResult{}, err
	}
	defer cancel()
	started := p.now()
	var message string
	err = seam.call(opCtx, func(ctx context.Context, opts []device.RequestOptions) error {
		var callErr error
		message, callErr = seam.client.SystemReboot(ctx, opts...)
		return callErr
	})
	if err != nil {
		return MaintenanceResult{}, mapSetError(err, "the camera refused to reboot")
	}
	result := MaintenanceResult{Message: sanitizeDeviceText(message)}
	result.BackOnline, result.DowntimeSeconds = p.waitForRecovery(ctx, request, started)
	return result, nil
}

// SoftFactoryReset restores factory settings while keeping network settings,
// then waits for the camera. A hard reset is deliberately not offered: it can
// leave the camera unreachable on an isolated network.
func (p *productionMedia) SoftFactoryReset(ctx context.Context, request DeviceRequest) (MaintenanceResult, error) {
	seam, opCtx, cancel, err := p.openDevice(ctx, request)
	if err != nil {
		return MaintenanceResult{}, err
	}
	defer cancel()
	started := p.now()
	err = seam.call(opCtx, func(ctx context.Context, opts []device.RequestOptions) error {
		return seam.client.SetSystemFactoryDefault(ctx, device.FactoryDefaultSoft, opts...)
	})
	if err != nil {
		return MaintenanceResult{}, mapSetError(err, "the camera refused the factory reset")
	}
	result := MaintenanceResult{Message: "soft factory reset accepted"}
	result.BackOnline, result.DowntimeSeconds = p.waitForRecovery(ctx, request, started)
	return result, nil
}

// SystemLog returns the System or Access log, bounded and sanitized.
func (p *productionMedia) SystemLog(ctx context.Context, request DeviceRequest, logType string) (SystemLogResult, error) {
	if logType != "System" && logType != "Access" {
		return SystemLogResult{}, usageError("the log type must be System or Access")
	}
	seam, opCtx, cancel, err := p.openDevice(ctx, request)
	if err != nil {
		return SystemLogResult{}, err
	}
	defer cancel()
	var text string
	err = seam.call(opCtx, func(ctx context.Context, opts []device.RequestOptions) error {
		var callErr error
		text, callErr = seam.client.GetSystemLog(ctx, logType, MaxSystemLogBytes+1, opts...)
		return callErr
	})
	if err != nil {
		return SystemLogResult{}, mapConfigError(err)
	}
	truncated := len(text) > MaxSystemLogBytes
	if truncated {
		text = text[:MaxSystemLogBytes]
		for len(text) > 0 && !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	return SystemLogResult{Type: logType, Text: sanitizeDeviceText(text), Truncated: truncated}, nil
}

// sanitizeDeviceText keeps printable text, newlines, and tabs from untrusted
// device output.
func sanitizeDeviceText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == utf8.RuneError {
			return -1
		}
		return r
	}, value)
}
