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

// newResampleSeam builds an authDeviceSeam wired for deterministic resample
// testing: credentials are present (so the retry path is active) and the clock
// read is driven by an injected counter instead of a device or network.
func newResampleSeam(clockCall func(ctx context.Context) (device.SystemDateAndTime, error)) *authDeviceSeam {
	return &authDeviceSeam{
		creds:     &Credentials{username: "u", password: "p"},
		generator: auth.UsernameTokenGenerator{},
		sampler:   auth.ClockSampler{Attempts: 1, Now: time.Now},
		clock:     time.Now,
		clockCall: clockCall,
	}
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

// TestParentCancellationDuringResampleWinsNoSuccessfulRetry proves that when the
// parent context is cancelled while the shared resample runs, cancellation wins:
// the resample seam observes the cancelled context and the fresh-token retry
// does not succeed. The retry attempt still happens (bounded to one), but it
// returns the cancellation rather than a success.
func TestParentCancellationDuringResampleWinsNoSuccessfulRetry(t *testing.T) {
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

	// The invoke always rejects with NotAuthorized on the first attempt; on the
	// retry it honours the context, so a cancelled parent yields no success.
	var attempts int32
	invoke := func(ctx context.Context, _ []device.RequestOptions) error {
		if atomic.AddInt32(&attempts, 1) == 1 {
			return soap.ErrNotAuthorized
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}

	err := seam.call(parent, invoke)
	if err == nil {
		t.Fatal("expected the retry to fail because the parent was cancelled during resample")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("retry error = %v, want context.Canceled (cancellation wins)", err)
	}
	// The resample seam was entered exactly once.
	if got := atomic.LoadInt32(&resamples); got != 1 {
		t.Fatalf("resample entered %d times, want exactly 1", got)
	}
	// Exactly two attempts total: the first failure plus one bounded retry.
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("made %d attempts, want 2 (first failure + one retry)", got)
	}
}
