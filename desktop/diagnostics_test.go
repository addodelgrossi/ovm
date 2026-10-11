package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	core "github.com/addodelgrossi/ovm/internal/app"
)

func diagnosticsHost(t *testing.T, manager *fakeManager) *App {
	t.Helper()
	host, err := NewApp(AppDependencies{Manager: manager})
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	host.reportDir = func() (string, error) { return directory, nil }
	t.Cleanup(func() { _ = host.close(context.Background()) })
	return host
}

func eventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestEventFeedPollsAndStops(t *testing.T) {
	t.Parallel()
	subscription := &fakeSubscription{batches: [][]core.EventDTO{
		{{Topic: "VideoSource/MotionAlarm"}}, {{Topic: "Device/Trigger/DigitalInput"}, {Topic: "VideoSource/MotionAlarm"}},
	}}
	manager := newFakeManager()
	manager.subscribeFn = func(context.Context, string, string) (core.EventSubscription, error) { return subscription, nil }
	host := diagnosticsHost(t, manager)

	started := host.StartEvents(StartEventsRequest{DeviceID: "device-1", TopicFilter: "tns1:VideoSource//."})
	if started.Error != nil {
		t.Fatalf("StartEvents() error = %+v", started.Error)
	}
	feedID := started.Result.FeedID
	var all []EventRecord
	eventually(t, func() bool {
		poll := host.PollEvents(EventFeedRequest{FeedID: feedID})
		all = poll.Result.Events
		return len(all) == 3
	})
	if all[0].ID != 1 || all[2].ID != 3 || all[1].Topic != "Device/Trigger/DigitalInput" || all[0].ReceivedAt == "" {
		t.Fatalf("events = %+v", all)
	}
	if newer := host.PollEvents(EventFeedRequest{FeedID: feedID, AfterID: 2}); len(newer.Result.Events) != 1 {
		t.Fatalf("events after 2 = %+v", newer.Result.Events)
	}
	if stopped := host.StopEvents(EventFeedRequest{FeedID: feedID}); stopped.Error != nil {
		t.Fatalf("StopEvents() error = %+v", stopped.Error)
	}
	eventually(t, func() bool { return subscription.closeCount() == 1 })
	if poll := host.PollEvents(EventFeedRequest{FeedID: feedID}); poll.Error == nil || poll.Error.Code != "feed_not_found" {
		t.Fatalf("poll after stop = %+v", poll)
	}
	if poll := host.PollEvents(EventFeedRequest{FeedID: "../x"}); poll.Error == nil || poll.Error.Code != "invalid_feed_id" {
		t.Fatalf("invalid feed id = %+v", poll)
	}
}

func TestStabilityJobFeedsTheReport(t *testing.T) {
	t.Parallel()
	manager := newFakeManager()
	manager.stabilityFn = func(_ context.Context, selection core.StreamSelection, options core.StabilityOptions, progress func(core.StabilityCycle)) (core.StabilityReport, error) {
		if selection.ProfileToken != "main" || options.Cycles != 2 || options.CycleDuration != 5*time.Second {
			t.Errorf("selection = %+v, options = %+v", selection, options)
		}
		progress(core.StabilityCycle{Index: 1, Passed: true})
		progress(core.StabilityCycle{Index: 2, Passed: true})
		return core.StabilityReport{Verdict: core.StabilityPass, CyclesCompleted: 2, Passed: 2}, nil
	}
	host := diagnosticsHost(t, manager)
	if invalid := host.StartStability(StartStabilityRequest{DeviceID: "device-1", ProfileToken: "main", Transport: "tcp", Cycles: 0, CycleSeconds: 5}); invalid.Error == nil {
		t.Fatal("zero cycles were accepted")
	}
	started := host.StartStability(StartStabilityRequest{DeviceID: "device-1", ProfileToken: "main", Transport: "tcp", Cycles: 2, CycleSeconds: 5})
	if started.Error != nil {
		t.Fatalf("StartStability() error = %+v", started.Error)
	}
	var status StabilityJobResponse
	eventually(t, func() bool {
		status = host.StabilityStatus(StabilityJobRequest{JobID: started.Result.JobID})
		return status.Result.State == jobCompleted
	})
	if len(status.Result.Cycles) != 2 || status.Result.Report.Verdict != core.StabilityPass {
		t.Fatalf("status = %+v", status.Result)
	}

	if clock := host.ClockCheck(ClockCheckRequest{DeviceID: "device-1", ProfileToken: "main"}); clock.Error != nil {
		t.Fatalf("ClockCheck() error = %+v", clock.Error)
	}
	export := host.ExportReport(ExportReportRequest{DeviceID: "device-1"})
	if export.Error != nil {
		t.Fatalf("ExportReport() error = %+v", export.Error)
	}
	manager.mu.Lock()
	options := manager.lastReport
	manager.mu.Unlock()
	if options.Stability == nil || options.Clock == nil || !strings.HasPrefix(options.Generator, "ovm ") {
		t.Fatalf("report options = %+v", options)
	}
	for _, path := range []string{export.Result.MarkdownPath, export.Result.JSONPath} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 || !strings.HasPrefix(filepath.Base(path), "ovm-report-FakeCam1-") {
			t.Fatalf("report file %s: %v, %v", path, info, err)
		}
	}
	if !strings.Contains(export.Result.Markdown, "# Device report: Fake/Cam 1") {
		t.Fatalf("markdown = %q", export.Result.Markdown)
	}
}

func TestRevealReportOnlyAcceptsSavedReports(t *testing.T) {
	t.Parallel()
	host := diagnosticsHost(t, newFakeManager())
	starter := &recordingStarter{}
	host.files = starter
	directory, _ := host.reportDir()
	saved := filepath.Join(directory, "ovm-report-cam-20261010-120000.md")
	if err := os.WriteFile(saved, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "relative.md", filepath.Join(directory, "other.md"), filepath.Join(directory, "ovm-report-x.sh"), "/etc/passwd"} {
		if got := host.RevealReport(RevealReportRequest{Path: path}); got.Error == nil {
			t.Errorf("RevealReport(%q) was accepted", path)
		}
	}
	if got := host.RevealReport(RevealReportRequest{Path: saved}); got.Error != nil {
		t.Fatalf("RevealReport(saved) error = %+v", got.Error)
	}
}
