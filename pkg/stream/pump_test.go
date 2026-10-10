package stream

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/synthmedia"
)

type scriptedSource struct {
	sps, pps []byte
	units    [][][]byte
	pts      []time.Duration
	index    int
	err      error
}

func (s *scriptedSource) SPS() []byte { return s.sps }
func (s *scriptedSource) PPS() []byte { return s.pps }

func (s *scriptedSource) ReadAccessUnit(ctx context.Context) ([][]byte, time.Duration, error) {
	if s.index == len(s.units) {
		if s.err != nil {
			return nil, 0, s.err
		}
		<-ctx.Done()
		return nil, 0, ctx.Err()
	}
	s.index++
	return s.units[s.index-1], s.pts[s.index-1], nil
}

type recordingSink struct {
	mu        sync.Mutex
	codecs    []string
	fragments int
	keys      int
}

func (r *recordingSink) SetInit(codec string, _ []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.codecs = append(r.codecs, codec)
}

func (r *recordingSink) WriteFragment(_ []byte, key bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.fragments++
	if key {
		r.keys++
	}
}

func scripted(t *testing.T, configs ...synthmedia.H264Config) *scriptedSource {
	t.Helper()
	script := &scriptedSource{}
	frame := 0
	for _, config := range configs {
		source := newSyntheticSource(t, config)
		for i := 0; i < config.GOP; i++ {
			au, _ := source.NextAccessUnit()
			script.units = append(script.units, au)
			script.pts = append(script.pts, time.Duration(frame)*source.FrameDuration())
			frame++
		}
	}
	return script
}

func TestPumpRepublishesInitWhenParameterSetsChange(t *testing.T) {
	t.Parallel()
	source := scripted(t,
		synthmedia.H264Config{Width: 64, Height: 48, FPS: 10, GOP: 4},
		synthmedia.H264Config{Width: 96, Height: 48, FPS: 10, GOP: 4},
	)
	source.err = errors.New("source ended")
	sink := &recordingSink{}
	err := PumpH264FMP4(context.Background(), source, sink)
	if err == nil || err.Error() != "source ended" {
		t.Fatalf("PumpH264FMP4() error = %v, want source error", err)
	}
	if len(sink.codecs) != 2 {
		t.Fatalf("SetInit called %d times, want 2", len(sink.codecs))
	}
	// Each muxer holds back its last access unit.
	if sink.fragments != 6 || sink.keys != 2 {
		t.Fatalf("fragments = %d keys = %d, want 6 and 2", sink.fragments, sink.keys)
	}
}

func TestPumpReturnsContextErrorOnCancellation(t *testing.T) {
	t.Parallel()
	source := scripted(t, synthmedia.H264Config{Width: 64, Height: 48, FPS: 10, GOP: 3})
	sink := &recordingSink{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- PumpH264FMP4(ctx, source, sink) }()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("PumpH264FMP4() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pump did not stop after cancellation")
	}
}

func TestPumpWaitsForInBandParameterSets(t *testing.T) {
	t.Parallel()
	source := scripted(t, synthmedia.H264Config{Width: 64, Height: 48, FPS: 10, GOP: 3})
	// Hide the out-of-band parameter sets and drop the first IDR so nothing can
	// start until a later access unit carries them in-band.
	source.units = source.units[1:]
	source.pts = source.pts[1:]
	source.err = errors.New("eof")
	sink := &recordingSink{}
	_ = PumpH264FMP4(context.Background(), source, sink)
	if len(sink.codecs) != 0 || sink.fragments != 0 {
		t.Fatalf("pump published without parameter sets: %+v", sink)
	}
}
