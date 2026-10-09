package app

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	"github.com/addodelgrossi/ovm/pkg/onvif/device"
)

// TestChildBudgetUsesAbsoluteCapWithoutDeadline proves that without a parent
// deadline the absolute cap is used.
func TestChildBudgetUsesAbsoluteCapWithoutDeadline(t *testing.T) {
	s := &authDeviceSeam{clock: time.Now}
	ctx, cancel := s.childBudget(context.Background(), negotiationMaxBudget, negotiationBudgetFraction)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected a child deadline")
	}
	remaining := time.Until(deadline)
	if remaining > negotiationMaxBudget+50*time.Millisecond {
		t.Fatalf("child budget %v exceeds absolute cap %v", remaining, negotiationMaxBudget)
	}
}

// TestChildBudgetUsesFractionOfRemaining proves the fraction cap applies when
// the parent deadline leaves only a small remaining budget relative to the
// absolute cap.
func TestChildBudgetUsesFractionOfRemaining(t *testing.T) {
	s := &authDeviceSeam{clock: time.Now}
	parent, parentCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer parentCancel()
	ctx, cancel := s.childBudget(parent, resampleMaxBudget, resampleBudgetFraction)
	defer cancel()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("expected a child deadline")
	}
	remaining := time.Until(deadline)
	// 10% of ~1s is ~100ms, well under the 750ms absolute cap.
	if remaining > 250*time.Millisecond {
		t.Fatalf("child budget %v should be ~10%% of the 1s parent, not the absolute cap", remaining)
	}
}

// TestResampleForRetryRunsAtMostOnce proves the shared resample executes a
// single time even when many goroutines invoke it concurrently.
func TestResampleForRetryRunsAtMostOnce(t *testing.T) {
	s := &authDeviceSeam{
		creds:   &Credentials{username: "u", password: "p"},
		sampler: auth.ClockSampler{Attempts: 1, Now: time.Now},
		clock:   time.Now,
	}
	var ran int32
	done := make(chan struct{})
	const n = 16
	for i := 0; i < n; i++ {
		go func() {
			s.resampleOnce.Do(func() { atomic.AddInt32(&ran, 1) })
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
	if got := atomic.LoadInt32(&ran); got != 1 {
		t.Fatalf("shared resample ran %d times, want exactly 1", got)
	}
}

// TestResampleHonoursExpiredParent proves that resampleForRetry returns promptly
// (and leaves offset state untouched) when the parent context is already
// cancelled, exercising the fallback path without touching the client.
func TestResampleHonoursExpiredParent(t *testing.T) {
	s := &authDeviceSeam{
		creds:   &Credentials{username: "u", password: "p"},
		sampler: auth.ClockSampler{Attempts: 1, Now: time.Now},
		clock:   time.Now,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("resampleForRetry panicked: %v", r)
			}
			close(done)
		}()
		s.resampleForRetry(ctx)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("resampleForRetry did not return on an expired parent")
	}
	s.offsetMu.Lock()
	defer s.offsetMu.Unlock()
	if s.haveOffset {
		t.Fatal("a failed resample must not set an offset")
	}
}

func TestInitialNegotiationUsesBoundedBudgetAndAttemptCount(t *testing.T) {
	var calls int32
	var latestDeadline time.Time
	seam := &authDeviceSeam{
		creds:   &Credentials{username: "u", password: "p"},
		sampler: auth.ClockSampler{Attempts: negotiationAttempts, Now: time.Now},
		clock:   time.Now,
		clockCall: func(ctx context.Context) (device.SystemDateAndTime, error) {
			atomic.AddInt32(&calls, 1)
			deadline, ok := ctx.Deadline()
			if !ok {
				t.Fatal("initial negotiation query has no child deadline")
			}
			latestDeadline = deadline
			return device.SystemDateAndTime{}, errors.New("synthetic clock failure")
		},
	}
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	started := time.Now()
	seam.negotiate(parent)
	if got := atomic.LoadInt32(&calls); got != negotiationAttempts {
		t.Fatalf("clock attempts = %d, want %d", got, negotiationAttempts)
	}
	if budget := latestDeadline.Sub(started); budget <= 0 || budget > negotiationMaxBudget+100*time.Millisecond {
		t.Fatalf("initial negotiation budget = %v, want <= %v", budget, negotiationMaxBudget)
	}
	if seam.haveOffset {
		t.Fatal("failed negotiation must not publish a clock offset")
	}
}
