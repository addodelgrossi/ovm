package app

import (
	"context"
	"errors"
	"slices"
	"time"
)

// Stability test bounds. The release gate is 100 cycles of 20 seconds.
const (
	MaxStabilityCycles          = 1000
	DefaultStabilityCycles      = 100
	DefaultStabilityCycleLength = 20 * time.Second
	MaxStabilityPause           = time.Minute
)

// Stability verdicts.
const (
	StabilityPass       = "pass"
	StabilityFail       = "fail"
	StabilityIncomplete = "incomplete"
)

// StabilityOptions parameterizes a stability test.
type StabilityOptions struct {
	Cycles        int           `json:"cycles"`
	CycleDuration time.Duration `json:"-"`
	// Pause separates cycles so each one reconnects from scratch.
	Pause time.Duration `json:"-"`
}

// StabilityCycle is one connect-measure-disconnect cycle.
type StabilityCycle struct {
	Index  int          `json:"index"`
	Passed bool         `json:"passed"`
	Error  string       `json:"error,omitempty"`
	Result *ProbeResult `json:"result,omitempty"`
}

// StabilityReport summarizes a stability test. It is credential-free.
type StabilityReport struct {
	ProfileToken     string           `json:"profile_token"`
	Transport        string           `json:"transport"`
	CyclesRequested  int              `json:"cycles_requested"`
	CycleSeconds     float64          `json:"cycle_seconds"`
	CyclesCompleted  int              `json:"cycles_completed"`
	Passed           int              `json:"passed"`
	Failed           int              `json:"failed"`
	Verdict          string           `json:"verdict"`
	MinTTFFMillis    int64            `json:"min_ttff_ms"`
	MedianTTFFMillis int64            `json:"median_ttff_ms"`
	MaxTTFFMillis    int64            `json:"max_ttff_ms"`
	MeanFPS          float64          `json:"mean_fps"`
	MeanBitrateKbps  float64          `json:"mean_bitrate_kbps"`
	LostPackets      uint64           `json:"lost_packets"`
	StartedAt        string           `json:"started_at"`
	FinishedAt       string           `json:"finished_at"`
	Cycles           []StabilityCycle `json:"cycles"`
}

// ValidateStabilityOptions checks the bounds of a stability test.
func ValidateStabilityOptions(options StabilityOptions) error {
	if options.Cycles < 1 || options.Cycles > MaxStabilityCycles {
		return usageError("the number of cycles must be between 1 and 1000")
	}
	if options.CycleDuration < time.Second || options.CycleDuration > MaxProbeDuration {
		return usageError("the cycle duration must be between 1s and 10m")
	}
	if options.Pause < 0 || options.Pause > MaxStabilityPause {
		return usageError("the pause between cycles must be between 0 and 1m")
	}
	return nil
}

// RunStability runs probe once per cycle and summarizes the results. progress,
// when set, receives each cycle as it ends. Cancelling ctx stops the test and
// returns the partial report with an incomplete verdict and no error.
func RunStability(ctx context.Context, options StabilityOptions, probe func(ctx context.Context) (ProbeResult, error), progress func(StabilityCycle)) (StabilityReport, error) {
	if err := ValidateStabilityOptions(options); err != nil {
		return StabilityReport{}, err
	}
	report := StabilityReport{
		CyclesRequested: options.Cycles, CycleSeconds: options.CycleDuration.Seconds(),
		StartedAt: time.Now().UTC().Format(time.RFC3339), Cycles: []StabilityCycle{},
	}
	for index := 1; index <= options.Cycles && ctx.Err() == nil; index++ {
		if index > 1 && options.Pause > 0 {
			timer := time.NewTimer(options.Pause)
			select {
			case <-ctx.Done():
				timer.Stop()
				continue
			case <-timer.C:
			}
		}
		result, err := probe(ctx)
		if ctx.Err() != nil {
			break
		}
		cycle := StabilityCycle{Index: index}
		if result.ProfileToken != "" {
			report.ProfileToken, report.Transport = result.ProfileToken, result.Transport
			measured := result
			cycle.Result = &measured
		}
		cycle.Passed = err == nil && result.Complete && result.Keyframes > 0
		if !cycle.Passed {
			cycle.Error = stabilityErrorMessage(err)
		}
		report.Cycles = append(report.Cycles, cycle)
		if progress != nil {
			progress(cycle)
		}
	}
	summarizeStability(&report)
	report.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	return report, nil
}

func stabilityErrorMessage(err error) string {
	if err == nil {
		return "the stream ended before the cycle finished"
	}
	var appErr *Error
	if errors.As(err, &appErr) {
		return appErr.Message
	}
	var managerErr *ManagerError
	if errors.As(err, &managerErr) {
		return managerErr.Message
	}
	return "the cycle failed"
}

func summarizeStability(report *StabilityReport) {
	report.CyclesCompleted = len(report.Cycles)
	var ttff []int64
	var fps, bitrate float64
	for _, cycle := range report.Cycles {
		if cycle.Passed {
			report.Passed++
		} else {
			report.Failed++
		}
		if cycle.Result == nil {
			continue
		}
		report.LostPackets += cycle.Result.LostPackets
		if cycle.Passed {
			ttff = append(ttff, cycle.Result.TimeToFirstFrameMillis)
			fps += cycle.Result.FramesPerSecond
			bitrate += cycle.Result.BitrateKbps
		}
	}
	if len(ttff) > 0 {
		slices.Sort(ttff)
		report.MinTTFFMillis, report.MaxTTFFMillis = ttff[0], ttff[len(ttff)-1]
		report.MedianTTFFMillis = ttff[len(ttff)/2]
		report.MeanFPS = fps / float64(len(ttff))
		report.MeanBitrateKbps = bitrate / float64(len(ttff))
	}
	switch {
	case report.Failed > 0:
		report.Verdict = StabilityFail
	case report.CyclesCompleted < report.CyclesRequested:
		report.Verdict = StabilityIncomplete
	default:
		report.Verdict = StabilityPass
	}
}

// StabilityTest repeatedly probes a stored device's stream.
func (m *Manager) StabilityTest(ctx context.Context, selection StreamSelection, options StabilityOptions, progress func(StabilityCycle)) (StabilityReport, error) {
	if err := m.ensureOpen(); err != nil {
		return StabilityReport{}, err
	}
	if _, err := m.resolveDevice(selection.DeviceID); err != nil {
		return StabilityReport{}, err
	}
	report, err := RunStability(ctx, options, func(ctx context.Context) (ProbeResult, error) {
		return m.ProbeStream(ctx, selection, options.CycleDuration)
	}, progress)
	if err != nil {
		return report, managerErrorFromApp(err)
	}
	return report, nil
}
