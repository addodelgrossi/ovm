package cli

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

const diagnosticsPassword = "cli-diagnostics-secret"

func diagnosticsFake(t *testing.T) cliFakeSetup {
	t.Helper()
	return newCLIFake(t, func(base string) fakedevice.Config {
		config := cliFakeConfig(base, true)
		config.Username, config.Password, config.RequireUsernameToken = "admin", diagnosticsPassword, true
		config.ClockSkew = 30 * time.Second
		config.Inventory.Services = append(config.Inventory.Services,
			fakedevice.ServiceData{Namespace: "http://www.onvif.org/ver10/events/wsdl", XAddr: base + "/onvif/events_service", VersionMajor: 25, VersionMinor: 6})
		return config
	})
}

func diagnosticsDependencies() Dependencies {
	deps := testDependencies()
	deps.Diagnostics = app.NewProductionHostDiagnostics()
	deps.LookupEnv = func(name string) (string, bool) { return diagnosticsPassword, name == "OVM_PASS" }
	return deps
}

func decodeDiagnostic(t *testing.T, stdout string) diagnosticEnvelope {
	t.Helper()
	var envelope diagnosticEnvelope
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatalf("stdout is not one JSON envelope: %v\n%s", err, stdout)
	}
	return envelope
}

func TestReportCommandIsCredentialFree(t *testing.T) {
	t.Parallel()
	setup := diagnosticsFake(t)
	for _, args := range [][]string{
		{"report", setup.host, "-u", "admin", "--password-env", "OVM_PASS"},
		{"report", setup.host, "-u", "admin", "--password-env", "OVM_PASS", "--format", "json"},
		{"report", setup.host, "-u", "admin", "--password-env", "OVM_PASS", "--json"},
	} {
		stdout, stderr, err := runCommand(diagnosticsDependencies(), args...)
		if err != nil {
			t.Fatalf("%v: error = %v, stderr = %s", args, err, stderr)
		}
		if strings.Contains(stdout+stderr, diagnosticsPassword) {
			t.Fatalf("%v: output contains the password", args)
		}
		if !strings.Contains(stdout, "FakeCam 1000") || !strings.Contains(stdout, "profile_main") {
			t.Fatalf("%v: report lacks device data:\n%s", args, stdout)
		}
	}
	stdout, _, _ := runCommand(diagnosticsDependencies(), "report", setup.host, "-u", "admin", "--password-env", "OVM_PASS", "--json")
	if envelope := decodeDiagnostic(t, stdout); envelope.Kind != kindReport || envelope.Error != nil {
		t.Fatalf("envelope = %+v", envelope)
	}
}

func TestClockCommandReportsSkew(t *testing.T) {
	t.Parallel()
	setup := diagnosticsFake(t)
	stdout, _, err := runCommand(diagnosticsDependencies(), "clock", setup.host, "-u", "admin", "--password-env", "OVM_PASS", "--no-rtcp", "--json")
	if err != nil {
		t.Fatalf("clock error = %v", err)
	}
	envelope := decodeDiagnostic(t, stdout)
	result := envelope.Result.(map[string]any)
	if envelope.Kind != kindClockCheck || result["verdict"] != app.ClockOutOfSync || result["rtcp_unavailable"] != "no stream was selected" {
		t.Fatalf("envelope = %+v", envelope)
	}
	human, _, err := runCommand(diagnosticsDependencies(), "clock", setup.host, "-u", "admin", "--password-env", "OVM_PASS", "--no-rtcp")
	if err != nil || !strings.Contains(human, "verdict: out_of_sync") || !strings.Contains(human, "onvif offset: +") {
		t.Fatalf("human output = %q, %v", human, err)
	}
}

func TestEventsCommandPrintsPublishedEvents(t *testing.T) {
	t.Parallel()
	setup := diagnosticsFake(t)
	go func() {
		for range 20 {
			time.Sleep(100 * time.Millisecond)
			setup.device.PublishEvent(fakedevice.Event{Topic: "VideoSource/MotionAlarm", Data: []fakedevice.EventItem{{Name: "State", Value: "true"}}})
		}
	}()
	stdout, stderr, err := runCommand(diagnosticsDependencies(), "events", setup.host, "-u", "admin", "--password-env", "OVM_PASS", "--duration", "2s")
	if err != nil {
		t.Fatalf("events error = %v, stderr = %s", err, stderr)
	}
	if !strings.Contains(stdout, "VideoSource/MotionAlarm Changed State=true") {
		t.Fatalf("events output = %q", stdout)
	}
	if setup.device.ActiveSubscriptions() != 0 {
		t.Fatal("the subscription was not removed")
	}
}

func TestStabilityCommandFailsOnAFailedCycle(t *testing.T) {
	t.Parallel()
	deps := diagnosticsDependencies()
	deps.Probe = &stubProbe{result: app.ProbeResult{ProfileToken: "profile_main", Transport: "tcp"}}
	stdout, stderr, err := runCommand(deps, "stability", "http://192.0.2.1/onvif/device_service", "--cycles", "2", "--duration", "1s", "--pause", "0s", "--json")
	if err == nil {
		t.Fatal("a failed stability test exited successfully")
	}
	envelope := decodeDiagnostic(t, stdout)
	if envelope.Kind != kindStability || envelope.Result.(map[string]any)["verdict"] != app.StabilityFail || !strings.Contains(stderr, "cycle 2/2: FAIL") {
		t.Fatalf("envelope = %+v, stderr = %s", envelope, stderr)
	}

	deps.Probe = &stubProbe{result: sampleProbeResult()}
	human, _, err := runCommand(deps, "stability", "http://192.0.2.1/onvif/device_service", "--cycles", "2", "--duration", "1s", "--pause", "0s")
	if err != nil || !strings.Contains(human, "verdict: pass") {
		t.Fatalf("passing run = %q, %v", human, err)
	}
	if _, _, err := runCommand(deps, "stability", "http://192.0.2.1/onvif/device_service", "--cycles", "0"); err == nil {
		t.Fatal("zero cycles were accepted")
	}
}
