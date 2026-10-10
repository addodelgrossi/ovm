package stream

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/synthmedia"
)

// closableScript is a scripted LiveSource that ends with err after its units.
type closableScript struct {
	scriptedSource
	closed bool
	lost   uint64
}

func (c *closableScript) Close()              { c.closed = true }
func (c *closableScript) LostPackets() uint64 { return c.lost }

func scriptFrom(t *testing.T, frames int, skip int, ptsStart time.Duration, end error) *closableScript {
	t.Helper()
	source := newSyntheticSource(t, synthmedia.H264Config{Width: 64, Height: 48, FPS: 25, GOP: 5})
	script := &closableScript{scriptedSource: scriptedSource{sps: source.SPS(), pps: source.PPS(), err: end}}
	for i := 0; i < skip; i++ {
		source.NextAccessUnit()
	}
	for i := 0; i < frames; i++ {
		au, _ := source.NextAccessUnit()
		script.units = append(script.units, au)
		script.pts = append(script.pts, ptsStart+time.Duration(i)*40*time.Millisecond)
	}
	return script
}

type recordingSleeper struct {
	mu     sync.Mutex
	delays []time.Duration
}

func (r *recordingSleeper) sleep(ctx context.Context, delay time.Duration) error {
	r.mu.Lock()
	r.delays = append(r.delays, delay)
	r.mu.Unlock()
	return ctx.Err()
}

func TestReconnectingSourceResumesAtKeyframeWithContinuousTimestamps(t *testing.T) {
	t.Parallel()
	dropped := errors.New("connection reset")
	first := scriptFrom(t, 4, 0, 0, dropped)
	first.lost = 2
	// The second session starts mid-GOP (2 P frames) with an unrelated clock.
	second := scriptFrom(t, 6, 3, 90*time.Second, dropped)
	second.lost = 1
	sessions := []LiveSource{first, second}
	failures := []error{ErrRTSPNetwork, ErrRTSPNetwork}
	opens := 0
	opener := func(context.Context) (LiveSource, error) {
		if opens == 0 {
			opens++
			return sessions[0], nil
		}
		if len(failures) > 0 {
			failure := failures[0]
			failures = failures[1:]
			return nil, failure
		}
		opens++
		return sessions[opens-1], nil
	}
	sleeper := &recordingSleeper{}
	source, err := OpenReconnecting(context.Background(), opener, ReconnectOptions{
		MinBackoff: time.Second, MaxBackoff: 3 * time.Second, Sleep: sleeper.sleep,
	})
	if err != nil {
		t.Fatalf("OpenReconnecting() error = %v", err)
	}
	var timestamps []time.Duration
	for i := 0; i < 8; i++ {
		_, pts, err := source.ReadAccessUnit(context.Background())
		if err != nil {
			t.Fatalf("ReadAccessUnit(%d) error = %v", i, err)
		}
		timestamps = append(timestamps, pts)
	}
	want := []time.Duration{0, 40, 80, 120, 160, 200, 240, 280}
	for i := range want {
		if timestamps[i] != want[i]*time.Millisecond {
			t.Fatalf("timestamps = %v, want %v ms", timestamps, want)
		}
	}
	if !first.closed {
		t.Fatal("the failed session was not closed")
	}
	if source.Reconnects() != 1 {
		t.Fatalf("Reconnects() = %d, want 1", source.Reconnects())
	}
	if got := source.LostPackets(); got != 3 {
		t.Fatalf("LostPackets() = %d, want 3 across sessions", got)
	}
	if len(sleeper.delays) != 3 || sleeper.delays[0] != time.Second || sleeper.delays[1] != 2*time.Second || sleeper.delays[2] != 3*time.Second {
		t.Fatalf("backoff delays = %v, want 1s, 2s, 3s", sleeper.delays)
	}
}

func TestReconnectingSourceStopsOnAuthenticationFailure(t *testing.T) {
	t.Parallel()
	first := scriptFrom(t, 1, 0, 0, errors.New("reset"))
	opens := 0
	opener := func(context.Context) (LiveSource, error) {
		opens++
		if opens == 1 {
			return first, nil
		}
		return nil, ErrRTSPAuthentication
	}
	source, err := OpenReconnecting(context.Background(), opener, ReconnectOptions{Sleep: (&recordingSleeper{}).sleep})
	if err != nil {
		t.Fatalf("OpenReconnecting() error = %v", err)
	}
	if _, _, err := source.ReadAccessUnit(context.Background()); err != nil {
		t.Fatalf("first read error = %v", err)
	}
	if _, _, err := source.ReadAccessUnit(context.Background()); !errors.Is(err, ErrRTSPAuthentication) {
		t.Fatalf("read after failure error = %v, want ErrRTSPAuthentication", err)
	}
}

func TestReconnectingSourceHonorsCancellationAndClose(t *testing.T) {
	t.Parallel()
	first := scriptFrom(t, 0, 0, 0, errors.New("reset"))
	opener := func(context.Context) (LiveSource, error) {
		if first != nil {
			source := first
			first = nil
			return source, nil
		}
		return nil, ErrRTSPNetwork
	}
	source, err := OpenReconnecting(context.Background(), opener, ReconnectOptions{MinBackoff: time.Hour})
	if err != nil {
		t.Fatalf("OpenReconnecting() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, _, err := source.ReadAccessUnit(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ReadAccessUnit() error = %v, want deadline during backoff", err)
	}
	source.Close()
	if _, _, err := source.ReadAccessUnit(context.Background()); !errors.Is(err, ErrRTSPClosed) {
		t.Fatalf("ReadAccessUnit() after Close error = %v, want ErrRTSPClosed", err)
	}
}

func TestOpenReconnectingReturnsFirstOpenError(t *testing.T) {
	t.Parallel()
	_, err := OpenReconnecting(context.Background(), func(context.Context) (LiveSource, error) {
		return nil, ErrRTSPNotFound
	}, ReconnectOptions{})
	if !errors.Is(err, ErrRTSPNotFound) {
		t.Fatalf("OpenReconnecting() error = %v", err)
	}
}

func TestMeterReportsReconnects(t *testing.T) {
	t.Parallel()
	source, err := OpenReconnecting(context.Background(), func(context.Context) (LiveSource, error) {
		return scriptFrom(t, 1, 0, 0, nil), nil
	}, ReconnectOptions{})
	if err != nil {
		t.Fatalf("OpenReconnecting() error = %v", err)
	}
	source.reconnects = 2
	meter := NewMeter(nil)
	NewMeteredSource(source, meter)
	if got := meter.Snapshot().Reconnects; got != 2 {
		t.Fatalf("Reconnects = %d, want 2", got)
	}
}
