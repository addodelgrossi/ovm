package stream

import (
	"context"
	"sync"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
)

// statsWindow is the sliding window for frame rate and bitrate.
const statsWindow = 3 * time.Second

// Stats is a point-in-time measurement of a live stream. Bitrate counts the
// encoded video payload (NAL units), not RTP or transport overhead. Rates use
// presentation timestamps, so they reflect the camera's clock rather than
// network jitter.
type Stats struct {
	Width  int `json:"width"`
	Height int `json:"height"`
	// FramesPerSecond and BitrateKbps cover the last few seconds.
	FramesPerSecond float64 `json:"frames_per_second"`
	BitrateKbps     float64 `json:"bitrate_kbps"`
	// GOPFrames and GOPSeconds describe the last complete group of pictures;
	// they are zero until two keyframes have been seen.
	GOPFrames  int     `json:"gop_frames"`
	GOPSeconds float64 `json:"gop_seconds"`
	// TimeToFirstFrame is the delay from Start to the first keyframe.
	TimeToFirstFrame time.Duration `json:"time_to_first_frame_ns"`
	Frames           uint64        `json:"frames"`
	Keyframes        uint64        `json:"keyframes"`
	Bytes            uint64        `json:"bytes"`
	// LostPackets is reported by sources that can detect RTP loss.
	LostPackets uint64 `json:"lost_packets"`
	// Elapsed is the wall time since Start.
	Elapsed time.Duration `json:"elapsed_ns"`
}

type windowSample struct {
	pts   time.Duration
	bytes int
}

// Meter measures access units. It is safe for concurrent use.
type Meter struct {
	now func() time.Time

	mu          sync.Mutex
	started     time.Time
	firstKey    time.Duration
	haveKey     bool
	window      []windowSample
	frames      uint64
	keyframes   uint64
	bytes       uint64
	sinceKey    int
	lastKeyPTS  time.Duration
	gopFrames   int
	gopDuration time.Duration
	width       int
	height      int
	lossSource  interface{ LostPackets() uint64 }
}

// NewMeter starts measuring now. A nil clock uses time.Now.
func NewMeter(now func() time.Time) *Meter {
	if now == nil {
		now = time.Now
	}
	return &Meter{now: now, started: now()}
}

// Observe records one access unit with its presentation timestamp.
func (m *Meter) Observe(au [][]byte, pts time.Duration) {
	size := 0
	var sps []byte
	for _, nalu := range au {
		size += len(nalu)
		if len(nalu) > 0 && h264.NALUType(nalu[0]&0x1F) == h264.NALUTypeSPS {
			sps = nalu
		}
	}
	key := h264.IsRandomAccess(au)

	m.mu.Lock()
	defer m.mu.Unlock()
	if sps != nil {
		var parsed h264.SPS
		if parsed.Unmarshal(sps) == nil {
			m.width, m.height = parsed.Width(), parsed.Height()
		}
	}
	if key && !m.haveKey {
		m.haveKey = true
		m.firstKey = m.now().Sub(m.started)
	}
	if !m.haveKey {
		return
	}
	m.frames++
	m.bytes += uint64(size) //nolint:gosec // size is a non-negative length
	if key {
		if m.keyframes > 0 {
			m.gopFrames = m.sinceKey
			m.gopDuration = pts - m.lastKeyPTS
		}
		m.keyframes++
		m.sinceKey = 0
		m.lastKeyPTS = pts
	}
	m.sinceKey++

	if n := len(m.window); n > 0 && pts < m.window[n-1].pts {
		// A timestamp discontinuity restarts the rate window.
		m.window = m.window[:0]
	}
	m.window = append(m.window, windowSample{pts: pts, bytes: size})
	cut := 0
	for cut < len(m.window)-1 && pts-m.window[cut].pts > statsWindow {
		cut++
	}
	m.window = m.window[cut:]
}

// Snapshot returns the current measurements.
func (m *Meter) Snapshot() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	stats := Stats{
		Width:            m.width,
		Height:           m.height,
		GOPFrames:        m.gopFrames,
		GOPSeconds:       m.gopDuration.Seconds(),
		TimeToFirstFrame: m.firstKey,
		Frames:           m.frames,
		Keyframes:        m.keyframes,
		Bytes:            m.bytes,
		Elapsed:          m.now().Sub(m.started),
	}
	if m.lossSource != nil {
		stats.LostPackets = m.lossSource.LostPackets()
	}
	if n := len(m.window); n >= 2 {
		span := m.window[n-1].pts - m.window[0].pts
		if span > 0 {
			// The last sample's payload is outside the measured span.
			bytes := 0
			for _, sample := range m.window[:n-1] {
				bytes += sample.bytes
			}
			stats.FramesPerSecond = float64(n-1) / span.Seconds()
			stats.BitrateKbps = float64(bytes) * 8 / 1000 / span.Seconds()
		}
	}
	return stats
}

// MeteredSource measures every access unit read from an H264Source. When the
// wrapped source reports RTP loss (LostPackets), the meter includes it.
type MeteredSource struct {
	H264Source
	meter *Meter
}

// NewMeteredSource wraps source with meter.
func NewMeteredSource(source H264Source, meter *Meter) *MeteredSource {
	if loss, ok := source.(interface{ LostPackets() uint64 }); ok {
		meter.mu.Lock()
		meter.lossSource = loss
		meter.mu.Unlock()
	}
	return &MeteredSource{H264Source: source, meter: meter}
}

// ReadAccessUnit reads from the wrapped source and records the access unit.
func (s *MeteredSource) ReadAccessUnit(ctx context.Context) ([][]byte, time.Duration, error) {
	au, pts, err := s.H264Source.ReadAccessUnit(ctx)
	if err == nil {
		s.meter.Observe(au, pts)
	}
	return au, pts, err
}
