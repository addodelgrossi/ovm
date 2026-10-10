package stream

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
)

// LiveSource is an H264Source that owns a connection.
type LiveSource interface {
	H264Source
	Close()
}

// SourceOpener opens a fresh live source. It is called again after failures.
type SourceOpener func(ctx context.Context) (LiveSource, error)

// ReconnectOptions bounds the retry schedule.
type ReconnectOptions struct {
	// MinBackoff and MaxBackoff bound the doubling delay between attempts.
	// They default to 1s and 10s.
	MinBackoff time.Duration
	MaxBackoff time.Duration
	// Sleep waits between attempts; tests replace it. It must return early
	// with ctx.Err() when ctx ends.
	Sleep func(ctx context.Context, delay time.Duration) error
}

// defaultFrameGap separates the last frame before a reconnect from the first
// frame after it when no frame interval has been observed yet.
const defaultFrameGap = 40 * time.Millisecond

// ReconnectingSource keeps a live stream running across connection failures.
// After a reconnect it skips to the next random-access unit and shifts
// timestamps so the output stays continuous and monotonic, letting the muxer
// and viewers continue without restarting. Authentication and invalid-URL
// failures are permanent and end the stream.
type ReconnectingSource struct {
	opener  SourceOpener
	options ReconnectOptions

	mu          sync.Mutex
	current     LiveSource
	sps, pps    []byte
	reconnects  uint64
	lostClosed  uint64
	needKey     bool
	haveOutput  bool
	lastOutput  time.Duration
	lastGap     time.Duration
	offset      time.Duration
	pendingBase bool
	closed      bool
}

// OpenReconnecting opens the first session immediately so configuration
// errors surface to the caller, then reconnects on later failures.
func OpenReconnecting(ctx context.Context, opener SourceOpener, options ReconnectOptions) (*ReconnectingSource, error) {
	if options.MinBackoff <= 0 {
		options.MinBackoff = time.Second
	}
	if options.MaxBackoff < options.MinBackoff {
		options.MaxBackoff = 10 * time.Second
	}
	if options.Sleep == nil {
		options.Sleep = sleepContext
	}
	first, err := opener(ctx)
	if err != nil {
		return nil, err
	}
	return &ReconnectingSource{
		opener:  opener,
		options: options,
		current: first,
		sps:     first.SPS(),
		pps:     first.PPS(),
	}, nil
}

// SPS returns the first session's sequence parameter set.
func (s *ReconnectingSource) SPS() []byte { return s.sps }

// PPS returns the first session's picture parameter set.
func (s *ReconnectingSource) PPS() []byte { return s.pps }

// Reconnects reports how many times the stream was re-established.
func (s *ReconnectingSource) Reconnects() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reconnects
}

// LostPackets sums RTP loss across sessions when sources report it.
func (s *ReconnectingSource) LostPackets() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	total := s.lostClosed
	if loss, ok := s.current.(interface{ LostPackets() uint64 }); ok && s.current != nil {
		total += loss.LostPackets()
	}
	return total
}

// ReadAccessUnit returns the next access unit, reconnecting as needed.
func (s *ReconnectingSource) ReadAccessUnit(ctx context.Context) ([][]byte, time.Duration, error) {
	for {
		source, err := s.ensureSource(ctx)
		if err != nil {
			return nil, 0, err
		}
		au, pts, err := source.ReadAccessUnit(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return nil, 0, ctxErr
			}
			s.drop(source)
			continue
		}
		if out, ok := s.mapTimestamp(au, pts); ok {
			return au, out, nil
		}
	}
}

// Close ends the current session. It is idempotent.
func (s *ReconnectingSource) Close() {
	s.mu.Lock()
	current := s.current
	s.current = nil
	s.closed = true
	s.mu.Unlock()
	if current != nil {
		current.Close()
	}
}

func (s *ReconnectingSource) ensureSource(ctx context.Context) (LiveSource, error) {
	s.mu.Lock()
	current, closed := s.current, s.closed
	s.mu.Unlock()
	if closed {
		return nil, ErrRTSPClosed
	}
	if current != nil {
		return current, nil
	}
	delay := s.options.MinBackoff
	for {
		if err := s.options.Sleep(ctx, delay); err != nil {
			return nil, err
		}
		next, err := s.opener(ctx)
		if err == nil {
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				next.Close()
				return nil, ErrRTSPClosed
			}
			s.current = next
			s.reconnects++
			s.needKey = true
			s.pendingBase = true
			s.mu.Unlock()
			return next, nil
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		if errors.Is(err, ErrRTSPAuthentication) || errors.Is(err, ErrRTSPInvalidURL) {
			return nil, err
		}
		delay *= 2
		if delay > s.options.MaxBackoff {
			delay = s.options.MaxBackoff
		}
	}
}

func (s *ReconnectingSource) drop(source LiveSource) {
	s.mu.Lock()
	if s.current == source {
		if loss, ok := source.(interface{ LostPackets() uint64 }); ok {
			s.lostClosed += loss.LostPackets()
		}
		s.current = nil
	}
	s.mu.Unlock()
	source.Close()
}

// mapTimestamp shifts timestamps after a reconnect so they continue from the
// last output, and suppresses access units until a random-access point.
func (s *ReconnectingSource) mapTimestamp(au [][]byte, pts time.Duration) (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.needKey {
		if !h264.IsRandomAccess(au) {
			return 0, false
		}
		s.needKey = false
	}
	if s.pendingBase {
		s.pendingBase = false
		gap := s.lastGap
		if gap <= 0 {
			gap = defaultFrameGap
		}
		if s.haveOutput {
			s.offset = s.lastOutput + gap - pts
		} else {
			s.offset = 0
		}
	}
	out := pts + s.offset
	if s.haveOutput && out > s.lastOutput {
		s.lastGap = out - s.lastOutput
	}
	s.lastOutput = out
	s.haveOutput = true
	return out, true
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
