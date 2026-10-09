package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestClockSamplerUsesRequestMidpointAndMinimumRTT(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	times := []time.Time{
		base, base.Add(200 * time.Millisecond),
		base.Add(time.Second), base.Add(time.Second + 40*time.Millisecond),
		base.Add(2 * time.Second), base.Add(2*time.Second + 100*time.Millisecond),
	}
	nowIndex := 0
	cameraTimes := []time.Time{
		base.Add(3*time.Second + 100*time.Millisecond),
		base.Add(4*time.Second + 20*time.Millisecond),
		base.Add(5*time.Second + 50*time.Millisecond),
	}
	cameraIndex := 0

	sampler := ClockSampler{
		Attempts: 3,
		Now: func() time.Time {
			value := times[nowIndex]
			nowIndex++
			return value
		},
	}
	sample, err := sampler.Sample(context.Background(), func(context.Context) (time.Time, error) {
		value := cameraTimes[cameraIndex]
		cameraIndex++
		return value, nil
	})
	if err != nil {
		t.Fatalf("Sample() error = %v", err)
	}
	if sample.RoundTripTime != 40*time.Millisecond {
		t.Errorf("RoundTripTime = %s, want 40ms", sample.RoundTripTime)
	}
	if sample.RequestMidpoint != base.Add(time.Second+20*time.Millisecond) {
		t.Errorf("RequestMidpoint = %s", sample.RequestMidpoint)
	}
	if sample.Offset != 3500*time.Millisecond {
		t.Errorf("Offset = %s, want represented-second midpoint offset", sample.Offset)
	}
	if sample.Uncertainty != 520*time.Millisecond {
		t.Errorf("Uncertainty = %s, want half RTT plus whole-second quantization", sample.Uncertainty)
	}
}

func TestClockSamplerHonorsContextAndRetainsBestSuccessfulSample(t *testing.T) {
	t.Parallel()

	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := (ClockSampler{Attempts: 2}).Sample(ctx, func(context.Context) (time.Time, error) {
			t.Fatal("query called after cancellation")
			return time.Time{}, nil
		})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Sample() error = %v, want context.Canceled", err)
		}
	})

	t.Run("partial success", func(t *testing.T) {
		attempt := 0
		queryErr := errors.New("synthetic query failure")
		sample, err := (ClockSampler{Attempts: 2}).Sample(context.Background(), func(context.Context) (time.Time, error) {
			attempt++
			if attempt == 1 {
				return time.Time{}, queryErr
			}
			return time.Now().UTC(), nil
		})
		if err != nil {
			t.Fatalf("Sample() error = %v", err)
		}
		if sample.RoundTripTime < 0 {
			t.Errorf("RoundTripTime = %s", sample.RoundTripTime)
		}
	})
}
