package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

const (
	defaultClockAttempts       = 3
	wholeSecondQuantizationGap = 500 * time.Millisecond
)

// ClockSample describes the best camera clock observation. Uncertainty is half
// the request round-trip time plus half a second for ONVIF whole-second time.
type ClockSample struct {
	Offset          time.Duration
	Uncertainty     time.Duration
	RoundTripTime   time.Duration
	RequestMidpoint time.Time
	CameraTime      time.Time
}

// ClockSampler samples a camera clock and retains the successful observation
// with the minimum round-trip time.
type ClockSampler struct {
	Attempts int
	Now      func() time.Time
}

// Sample invokes query with the caller's context for every attempt.
func (s ClockSampler) Sample(ctx context.Context, query func(context.Context) (time.Time, error)) (ClockSample, error) {
	if query == nil {
		return ClockSample{}, errors.New("camera clock query is required")
	}
	attempts := s.Attempts
	if attempts == 0 {
		attempts = defaultClockAttempts
	}
	if attempts < 1 {
		return ClockSample{}, errors.New("camera clock sample count must be positive")
	}
	now := s.Now
	if now == nil {
		now = time.Now
	}

	var (
		best       ClockSample
		haveSample bool
		lastErr    error
	)
	for range attempts {
		if err := ctx.Err(); err != nil {
			return ClockSample{}, err
		}
		started := now()
		cameraTime, err := query(ctx)
		finished := now()
		if err != nil {
			if contextErr := ctx.Err(); contextErr != nil {
				return ClockSample{}, contextErr
			}
			lastErr = err
			continue
		}
		roundTripTime := finished.Sub(started)
		if roundTripTime < 0 {
			lastErr = errors.New("local clock moved backwards during camera clock sample")
			continue
		}
		midpoint := started.Add(roundTripTime / 2)
		// ONVIF exposes an integer second. Treat the serialized value as the
		// start of that represented second and use its midpoint as the estimate.
		estimatedCameraTime := cameraTime.Add(wholeSecondQuantizationGap)
		candidate := ClockSample{
			Offset:          estimatedCameraTime.Sub(midpoint),
			Uncertainty:     roundTripTime/2 + wholeSecondQuantizationGap,
			RoundTripTime:   roundTripTime,
			RequestMidpoint: midpoint,
			CameraTime:      cameraTime,
		}
		if !haveSample || candidate.RoundTripTime < best.RoundTripTime {
			best = candidate
			haveSample = true
		}
	}
	if haveSample {
		return best, nil
	}
	if lastErr == nil {
		lastErr = errors.New("camera clock query produced no sample")
	}
	return ClockSample{}, fmt.Errorf("sample camera clock: %w", lastErr)
}
