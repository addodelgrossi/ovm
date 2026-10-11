package app

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
	"github.com/addodelgrossi/ovm/pkg/onvif/device"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func TestInvokeWithAuthPreEndedContextSkipsCallback(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
		want error
	}{
		{
			name: "cancelled",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, func() {}
			},
			want: context.Canceled,
		},
		{
			name: "deadline",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
			},
			want: context.DeadlineExceeded,
		},
	}
	for _, test := range cases {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := test.ctx()
			defer cancel()
			session := newTestSession(
				&Credentials{username: "u", password: "p"},
				auth.ClockSampler{Attempts: 1, Now: time.Now},
				func(context.Context) (device.SystemDateAndTime, error) {
					t.Fatal("pre-ended context entered clock resampling")
					return device.SystemDateAndTime{}, nil
				},
			)
			var callbacks int32
			err := session.invokeWithAuth(ctx, func(context.Context, []byte) error {
				atomic.AddInt32(&callbacks, 1)
				return nil
			})
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
			if got := atomic.LoadInt32(&callbacks); got != 0 {
				t.Fatalf("callbacks = %d, want 0", got)
			}
		})
	}
}

func TestInvokeWithAuthLifecycleErrorJoinedWithNotAuthorizedSkipsRetry(t *testing.T) {
	t.Parallel()

	var callbacks int32
	var resamples int32
	session := newTestSession(
		&Credentials{username: "u", password: "p"},
		auth.ClockSampler{Attempts: 1, Now: time.Now},
		func(context.Context) (device.SystemDateAndTime, error) {
			atomic.AddInt32(&resamples, 1)
			return device.SystemDateAndTime{UTC: time.Now(), UTCValid: true}, nil
		},
	)
	err := session.invokeWithAuth(context.Background(), func(context.Context, []byte) error {
		atomic.AddInt32(&callbacks, 1)
		return errors.Join(soap.ErrNotAuthorized, context.Canceled)
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := atomic.LoadInt32(&callbacks); got != 1 {
		t.Fatalf("callbacks = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&resamples); got != 0 {
		t.Fatalf("resamples = %d, want 0", got)
	}
}

type manualLifecycleContext struct {
	context.Context
	done chan struct{}
	once sync.Once
	mu   sync.RWMutex
	err  error
}

func newManualLifecycleContext() *manualLifecycleContext {
	return &manualLifecycleContext{Context: context.Background(), done: make(chan struct{})}
}

func (ctx *manualLifecycleContext) Done() <-chan struct{} { return ctx.done }

func (ctx *manualLifecycleContext) Err() error {
	ctx.mu.RLock()
	defer ctx.mu.RUnlock()
	return ctx.err
}

func (ctx *manualLifecycleContext) end(err error) {
	ctx.mu.Lock()
	ctx.err = err
	ctx.mu.Unlock()
	ctx.once.Do(func() { close(ctx.done) })
}

func TestInvokeWithAuthEndedParentAfterNotAuthorizedSkipsRetry(t *testing.T) {
	t.Parallel()

	for _, lifecycleErr := range []error{context.Canceled, context.DeadlineExceeded} {
		lifecycleErr := lifecycleErr
		t.Run(lifecycleErr.Error(), func(t *testing.T) {
			t.Parallel()
			var callbacks int32
			var resamples int32
			ctx := newManualLifecycleContext()
			session := newTestSession(
				&Credentials{username: "u", password: "p"},
				auth.ClockSampler{Attempts: 1, Now: time.Now},
				func(context.Context) (device.SystemDateAndTime, error) {
					atomic.AddInt32(&resamples, 1)
					return device.SystemDateAndTime{UTC: time.Now(), UTCValid: true}, nil
				},
			)
			err := session.invokeWithAuth(ctx, func(context.Context, []byte) error {
				atomic.AddInt32(&callbacks, 1)
				ctx.end(lifecycleErr)
				return soap.ErrNotAuthorized
			})
			if !errors.Is(err, lifecycleErr) {
				t.Fatalf("error = %v, want %v", err, lifecycleErr)
			}
			if got := atomic.LoadInt32(&callbacks); got != 1 {
				t.Fatalf("callbacks = %d, want 1", got)
			}
			if got := atomic.LoadInt32(&resamples); got != 0 {
				t.Fatalf("resamples = %d, want 0", got)
			}
		})
	}
}

type lifecycleRoundTripFunc func(*http.Request) (*http.Response, error)

func (function lifecycleRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

// TestInvokeWithAuthCancellationRaceStopsCallbackAndWireRetry counts both the
// logical callback and the transport boundary. Cancellation that races with a
// NotAuthorized response must leave each count at one.
func TestInvokeWithAuthCancellationRaceStopsCallbackAndWireRetry(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var callbacks int32
	var wireRequests int32
	var resamples int32
	client := &http.Client{Transport: lifecycleRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		atomic.AddInt32(&wireRequests, 1)
		cancel()
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       http.NoBody,
			Request:    request,
		}, nil
	})}
	session := newTestSession(
		&Credentials{username: "u", password: "p"},
		auth.ClockSampler{Attempts: 1, Now: time.Now},
		func(context.Context) (device.SystemDateAndTime, error) {
			atomic.AddInt32(&resamples, 1)
			return device.SystemDateAndTime{UTC: time.Now(), UTCValid: true}, nil
		},
	)

	err := session.invokeWithAuth(ctx, func(ctx context.Context, _ []byte) error {
		atomic.AddInt32(&callbacks, 1)
		request, requestErr := http.NewRequestWithContext(ctx, http.MethodPost, "http://camera.invalid/onvif", nil)
		if requestErr != nil {
			return requestErr
		}
		response, wireErr := client.Do(request)
		if response != nil {
			_ = response.Body.Close()
		}
		if wireErr != nil {
			return errors.Join(soap.ErrNotAuthorized, wireErr)
		}
		return soap.ErrNotAuthorized
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if got := atomic.LoadInt32(&callbacks); got != 1 {
		t.Fatalf("callbacks = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&wireRequests); got != 1 {
		t.Fatalf("wire requests = %d, want 1", got)
	}
	if got := atomic.LoadInt32(&resamples); got != 0 {
		t.Fatalf("resamples = %d, want 0", got)
	}
}
