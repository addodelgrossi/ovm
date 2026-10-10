// Package stream contains reusable RTSP, remuxing, and media-statistics
// building blocks. It does not import UI, CLI, or persistence packages.
package stream

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
)

// videoTimeScale is the 90 kHz RTP video clock, also used as the MP4 timescale
// so RTP-derived timestamps need no rescaling.
const videoTimeScale = 90000

const videoTrackID = 1

var (
	// ErrInvalidParameterSets reports an SPS/PPS pair that cannot describe a track.
	ErrInvalidParameterSets = errors.New("stream: invalid H.264 parameter sets")
	// ErrParameterSetsChanged reports an in-band SPS or PPS that differs from the
	// initialization segment; the caller must start a new muxer and init segment.
	ErrParameterSetsChanged = errors.New("stream: H.264 parameter sets changed")
	// ErrTimestamp reports a timestamp sequence that cannot be fragmented.
	ErrTimestamp = errors.New("stream: invalid access unit timestamp")
)

// H264FMP4Muxer converts timestamped H.264 access units into a fragmented MP4
// initialization segment and one media fragment (moof+mdat) per access unit.
// Output starts at the first random-access unit. Because an MP4 sample's
// duration is known only when the following sample arrives, each fragment is
// emitted one access unit late. It is not safe for concurrent use.
type H264FMP4Muxer struct {
	sps       []byte
	pps       []byte
	parsed    h264.SPS
	extractor *h264.DTSExtractor
	sequence  uint32
	pending   *pendingSample
	// bias shifts timestamps so the first decode timestamp is not negative,
	// which reordered streams can otherwise produce.
	bias    int64
	started bool
}

// Fragment is one encoded moof+mdat pair.
type Fragment struct {
	// Bytes is the encoded fragment.
	Bytes []byte
	// Key reports that the fragment's sample is a random-access point.
	Key bool
}

type pendingSample struct {
	dts     int64
	pts     int64
	payload []byte
	key     bool
}

// NewH264FMP4Muxer validates the parameter sets that describe the track.
func NewH264FMP4Muxer(sps, pps []byte) (*H264FMP4Muxer, error) {
	if len(sps) < 4 || len(pps) < 1 ||
		h264.NALUType(sps[0]&0x1F) != h264.NALUTypeSPS || h264.NALUType(pps[0]&0x1F) != h264.NALUTypePPS {
		return nil, ErrInvalidParameterSets
	}
	muxer := &H264FMP4Muxer{sps: bytes.Clone(sps), pps: bytes.Clone(pps)}
	if err := muxer.parsed.Unmarshal(muxer.sps); err != nil {
		return nil, ErrInvalidParameterSets
	}
	return muxer, nil
}

// CodecString is the RFC 6381 codecs parameter for Media Source Extensions,
// for example "avc1.42c01f".
func (m *H264FMP4Muxer) CodecString() string {
	return fmt.Sprintf("avc1.%02x%02x%02x", m.sps[1], m.sps[2], m.sps[3])
}

// Width is the coded picture width after cropping.
func (m *H264FMP4Muxer) Width() int { return m.parsed.Width() }

// Height is the coded picture height after cropping.
func (m *H264FMP4Muxer) Height() int { return m.parsed.Height() }

// Init returns the initialization segment (ftyp+moov).
func (m *H264FMP4Muxer) Init() ([]byte, error) {
	init := fmp4.Init{Tracks: []*fmp4.InitTrack{{
		ID:        videoTrackID,
		TimeScale: videoTimeScale,
		Codec:     &codecs.H264{SPS: m.sps, PPS: m.pps},
	}}}
	var buffer seekablebuffer.Buffer
	if err := init.Marshal(&buffer); err != nil {
		return nil, fmt.Errorf("stream: marshal init segment: %w", err)
	}
	return buffer.Bytes(), nil
}

// Write accepts one access unit (NAL units without start codes) and its
// presentation timestamp. It returns the completed fragment of the previous
// access unit, or nil while waiting for a random-access unit or for the first
// sample's duration. Parameter sets and access unit delimiters are removed
// from samples because the initialization segment carries them.
func (m *H264FMP4Muxer) Write(au [][]byte, pts time.Duration) (*Fragment, error) {
	if len(au) == 0 {
		return nil, nil
	}
	random := h264.IsRandomAccess(au)
	if m.extractor == nil {
		if !random {
			return nil, nil
		}
		m.extractor = &h264.DTSExtractor{}
		m.extractor.Initialize()
	}

	var samples [][]byte
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		switch h264.NALUType(nalu[0] & 0x1F) {
		case h264.NALUTypeSPS:
			if !bytes.Equal(nalu, m.sps) {
				return nil, ErrParameterSetsChanged
			}
		case h264.NALUTypePPS:
			if !bytes.Equal(nalu, m.pps) {
				return nil, ErrParameterSetsChanged
			}
		case h264.NALUTypeAccessUnitDelimiter:
		default:
			samples = append(samples, nalu)
		}
	}
	if len(samples) == 0 {
		return nil, nil
	}

	ticks := durationToTicks(pts)
	// The extractor needs the SPS in-band; RTSP often carries it only in SDP.
	dts, err := m.extractor.Extract(append([][]byte{m.sps, m.pps}, samples...), ticks)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTimestamp, err)
	}
	if !m.started {
		m.started = true
		if dts < 0 {
			m.bias = -dts
		}
	}
	dts += m.bias
	ticks += m.bias
	payload, err := h264.AVCC(samples).Marshal()
	if err != nil {
		return nil, fmt.Errorf("stream: encode sample: %w", err)
	}
	next := &pendingSample{dts: dts, pts: ticks, payload: payload, key: random}

	previous := m.pending
	m.pending = next
	if previous == nil {
		return nil, nil
	}
	if next.dts <= previous.dts {
		return nil, ErrTimestamp
	}
	return m.fragment(previous, next.dts-previous.dts)
}

func (m *H264FMP4Muxer) fragment(sample *pendingSample, duration int64) (*Fragment, error) {
	offset := sample.pts - sample.dts
	if sample.dts < 0 || duration > int64(^uint32(0)) || offset > 1<<31-1 || offset < -(1<<31) {
		return nil, ErrTimestamp
	}
	m.sequence++
	part := fmp4.Part{
		SequenceNumber: m.sequence,
		Tracks: []*fmp4.PartTrack{{
			ID:       videoTrackID,
			BaseTime: uint64(sample.dts),
			Samples: []*fmp4.Sample{{
				Duration:        uint32(duration), //nolint:gosec // range checked above
				PTSOffset:       int32(offset),    //nolint:gosec // range checked above
				IsNonSyncSample: !sample.key,
				Payload:         sample.payload,
			}},
		}},
	}
	var buffer seekablebuffer.Buffer
	if err := part.Marshal(&buffer); err != nil {
		return nil, fmt.Errorf("stream: marshal fragment: %w", err)
	}
	return &Fragment{Bytes: buffer.Bytes(), Key: sample.key}, nil
}

// durationToTicks rounds to the nearest tick without overflowing for
// long-running sessions.
func durationToTicks(value time.Duration) int64 {
	seconds := int64(value / time.Second)
	remainder := int64(value % time.Second)
	return seconds*videoTimeScale + (remainder*videoTimeScale+int64(time.Second)/2)/int64(time.Second)
}
