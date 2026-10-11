package app

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// newResampleSeam builds an authDeviceSeam backed by an injected session wired
// for deterministic resample testing: credentials are present (so the retry
// path is active) and the clock read is driven by an injected counter instead
// of a device or network. The seam is a thin delegate; the offset/resample
// state it exercises lives in the shared session.
func newResampleSeam(clockCall func(ctx context.Context) (device.SystemDateAndTime, error)) *authDeviceSeam {
	s := newTestSession(&Credentials{username: "u", password: "p"}, auth.ClockSampler{Attempts: 1, Now: time.Now}, clockCall)
	return &authDeviceSeam{session: s}
}

// notAuthorizedThenOK returns an invoke function that fails the first attempt of
// every operation with a genuine SOAP NotAuthorized fault and succeeds on the
// second. It records the number of attempts per logical operation key so a test
// can assert exactly one retry per operation.
func notAuthorizedThenOK() (func(key string) func(ctx context.Context, opts []device.RequestOptions) error, func() map[string]int) {
	var mu sync.Mutex
	attempts := map[string]int{}
	factory := func(key string) func(ctx context.Context, opts []device.RequestOptions) error {
		return func(ctx context.Context, _ []device.RequestOptions) error {
			mu.Lock()
			attempts[key]++
			n := attempts[key]
			mu.Unlock()
			if n == 1 {
				return soap.ErrNotAuthorized
			}
			return nil
		}
	}
	snapshot := func() map[string]int {
		mu.Lock()
		defer mu.Unlock()
		out := make(map[string]int, len(attempts))
		for k, v := range attempts {
			out[k] = v
		}
		return out
	}
	return factory, snapshot
}

// TestNotAuthorizedCausesExactlyOneSharedResampleAndOneRetryPerOp proves the
// real retry path (not sync.Once in isolation): a genuine SOAP NotAuthorized on
// the first attempt of each concurrent operation triggers exactly one shared
// clock resample for the whole use case, under a bounded child deadline, and
// exactly one retry per operation that then succeeds.
func TestNotAuthorizedCausesExactlyOneSharedResampleAndOneRetryPerOp(t *testing.T) {
	var resamples int32
	var resampleHadDeadline int32

	clockCall := func(ctx context.Context) (device.SystemDateAndTime, error) {
		atomic.AddInt32(&resamples, 1)
		// The resample must run under a bounded child deadline, never the raw
		// parent, so the negotiation/resample cannot consume the whole budget.
		if _, ok := ctx.Deadline(); ok {
			atomic.AddInt32(&resampleHadDeadline, 1)
		}
		return device.SystemDateAndTime{UTC: time.Now().UTC(), UTCValid: true}, nil
	}

	seam := newResampleSeam(clockCall)

	parent, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	factory, snapshot := notAuthorizedThenOK()
	const ops = 7
	var wg sync.WaitGroup
	errs := make([]error, ops)
	for i := 0; i < ops; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			key := strconv.Itoa(idx)
			errs[idx] = seam.call(parent, factory(key))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("operation %d returned %v, want success after one retry", i, err)
		}
	}

	// Exactly one shared resample across all concurrent operations.
	if got := atomic.LoadInt32(&resamples); got != 1 {
		t.Fatalf("shared resample ran %d times, want exactly 1", got)
	}
	// That single resample ran under a bounded child deadline.
	if got := atomic.LoadInt32(&resampleHadDeadline); got != 1 {
		t.Fatalf("resample ran under a bounded deadline %d times, want 1", got)
	}
	// Exactly one retry per operation: two attempts each (first fails, retry OK).
	for key, n := range snapshot() {
		if n != 2 {
			t.Fatalf("operation %q made %d attempts, want 2 (one failure + one retry)", key, n)
		}
	}
}

// TestParentCancellationDuringResamplePreventsRetry proves that when the
// parent context is cancelled while the shared resample runs, cancellation
// wins and the retry callback is never entered.
func TestParentCancellationDuringResamplePreventsRetry(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())

	var resamples int32
	clockCall := func(ctx context.Context) (device.SystemDateAndTime, error) {
		atomic.AddInt32(&resamples, 1)
		// Cancel the parent mid-resample, simulating an interrupt arriving while
		// the shared resample is in flight.
		cancel()
		return device.SystemDateAndTime{}, context.Canceled
	}

	seam := newResampleSeam(clockCall)

	// The invoke rejects with NotAuthorized on the first attempt. A retry would
	// ignore the context and succeed, so the session itself must suppress it.
	var attempts int32
	invoke := func(context.Context, []device.RequestOptions) error {
		if atomic.AddInt32(&attempts, 1) == 1 {
			return soap.ErrNotAuthorized
		}
		return nil
	}

	err := seam.call(parent, invoke)
	if err == nil {
		t.Fatal("expected cancellation to prevent the retry")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled (cancellation wins)", err)
	}
	// The resample seam was entered exactly once.
	if got := atomic.LoadInt32(&resamples); got != 1 {
		t.Fatalf("resample entered %d times, want exactly 1", got)
	}
	// The callback ran only for the original NotAuthorized attempt.
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("made %d attempts, want 1 (retry suppressed)", got)
	}
}
