package app

import (
	"context"
	"testing"
	"time"
)

func probeOK(ttff int64) ProbeResult {
	return ProbeResult{ProfileToken: "main", Transport: "tcp", Complete: true, Keyframes: 3, FramesPerSecond: 15, BitrateKbps: 1000, TimeToFirstFrameMillis: ttff}
}

func TestRunStabilitySummarizesCycles(t *testing.T) {
	t.Parallel()
	results := []ProbeResult{probeOK(300), probeOK(100), {ProfileToken: "main", Transport: "tcp", LostPackets: 4}, probeOK(200)}
	errs := []error{nil, nil, newError(CategoryNetwork, "the stream could not be reached", nil), nil}
	call := 0
	var seen []int
	report, err := RunStability(context.Background(), StabilityOptions{Cycles: 4, CycleDuration: time.Second},
		func(context.Context) (ProbeResult, error) {
			call++
			return results[call-1], errs[call-1]
		}, func(cycle StabilityCycle) { seen = append(seen, cycle.Index) })
	if err != nil {
		t.Fatal(err)
	}
	if report.Verdict != StabilityFail || report.Passed != 3 || report.Failed != 1 || report.CyclesCompleted != 4 || len(seen) != 4 {
		t.Fatalf("report = %+v", report)
	}
	if report.MinTTFFMillis != 100 || report.MedianTTFFMillis != 200 || report.MaxTTFFMillis != 300 || report.MeanFPS != 15 || report.LostPackets != 4 {
		t.Fatalf("summary = %+v", report)
	}
	if report.Cycles[2].Error != "the stream could not be reached" || report.Cycles[2].Passed {
		t.Fatalf("failed cycle = %+v", report.Cycles[2])
	}
}

func TestRunStabilityCancellationIsIncomplete(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	report, err := RunStability(ctx, StabilityOptions{Cycles: 5, CycleDuration: time.Second},
		func(context.Context) (ProbeResult, error) { return probeOK(100), nil },
		func(cycle StabilityCycle) {
			if cycle.Index == 2 {
				cancel()
			}
		})
	if err != nil || report.Verdict != StabilityIncomplete || report.CyclesCompleted != 2 {
		t.Fatalf("report = %+v, %v", report, err)
	}
}

func TestValidateStabilityOptions(t *testing.T) {
	t.Parallel()
	for _, options := range []StabilityOptions{
		{Cycles: 0, CycleDuration: time.Second},
		{Cycles: MaxStabilityCycles + 1, CycleDuration: time.Second},
		{Cycles: 1, CycleDuration: time.Millisecond},
		{Cycles: 1, CycleDuration: time.Second, Pause: -time.Second},
	} {
		if ValidateStabilityOptions(options) == nil {
			t.Errorf("options %+v were accepted", options)
		}
	}
}
