// Package stream contains reusable RTSP, remuxing, and media-statistics
// building blocks. It does not import UI, CLI, or persistence packages.
package stream

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4/seekablebuffer"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"
)

// videoTimeScale is the 90 kHz RTP video clock, also used as the MP4 timescale
// so RTP-derived timestamps need no rescaling.
const videoTimeScale = 90000

const videoTrackID = 1

var (
	// ErrInvalidParameterSets reports parameter sets that cannot describe a track.
	ErrInvalidParameterSets = errors.New("stream: invalid video parameter sets")
	// ErrParameterSetsChanged reports in-band parameter sets that differ from
	// the initialization segment; the caller must start a new muxer.
	ErrParameterSetsChanged = errors.New("stream: video parameter sets changed")
	// ErrTimestamp reports a timestamp sequence that cannot be fragmented.
	ErrTimestamp = errors.New("stream: invalid access unit timestamp")
)

// dtsExtractor derives decode timestamps for reordered (B-frame) streams.
type dtsExtractor interface {
	Extract(au [][]byte, pts int64) (int64, error)
}

// FMP4Muxer converts timestamped H.264 or H.265 access units into a
// fragmented MP4 initialization segment and one media fragment (moof+mdat)
// per access unit. Output starts at the first random-access unit. Because an
// MP4 sample's duration is known only when the following sample arrives, each
// fragment is emitted one access unit late. It is not safe for concurrent use.
type FMP4Muxer struct {
	codec     string
	sets      [][]byte
	codecName string
	width     int
	height    int
	extractor dtsExtractor
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

// NewFMP4Muxer validates the ordered parameter sets for codec: SPS and PPS
// for H.264, VPS, SPS, and PPS for H.265.
func NewFMP4Muxer(codec string, parameterSets [][]byte) (*FMP4Muxer, error) {
	if !validParameterSets(codec, parameterSets) {
		return nil, ErrInvalidParameterSets
	}
	sets := make([][]byte, len(parameterSets))
	for i, set := range parameterSets {
		sets[i] = bytes.Clone(set)
	}
	sps := sets[len(sets)-2]
	width, height, ok := pictureSize(codec, sps)
	if !ok {
		return nil, ErrInvalidParameterSets
	}
	name, err := codecString(codec, sps)
	if err != nil {
		return nil, err
	}
	return &FMP4Muxer{codec: codec, sets: sets, codecName: name, width: width, height: height}, nil
}

// CodecString is the RFC 6381 codecs parameter for Media Source Extensions,
// for example "avc1.42c01f" or "hvc1.1.6.L93.B0".
func (m *FMP4Muxer) CodecString() string { return m.codecName }

// Width is the coded picture width after cropping.
func (m *FMP4Muxer) Width() int { return m.width }

// Height is the coded picture height after cropping.
func (m *FMP4Muxer) Height() int { return m.height }

// Init returns the initialization segment (ftyp+moov).
func (m *FMP4Muxer) Init() ([]byte, error) {
	var codec codecs.Codec = &codecs.H264{SPS: m.sets[0], PPS: m.sets[1]}
	if m.codec == CodecH265 {
		codec = &codecs.H265{VPS: m.sets[0], SPS: m.sets[1], PPS: m.sets[2]}
	}
	init := fmp4.Init{Tracks: []*fmp4.InitTrack{{
		ID:        videoTrackID,
		TimeScale: videoTimeScale,
		Codec:     codec,
	}}}
	var buffer seekablebuffer.Buffer
	if err := init.Marshal(&buffer); err != nil {
		return nil, fmt.Errorf("stream: marshal init segment: %w", err)
	}
	return buffer.Bytes(), nil
}

func (m *FMP4Muxer) newExtractor() dtsExtractor {
	if m.codec == CodecH265 {
		extractor := &h265.DTSExtractor{}
		extractor.Initialize()
		return extractor
	}
	extractor := &h264.DTSExtractor{}
	extractor.Initialize()
	return extractor
}

// Write accepts one access unit (NAL units without start codes) and its
// presentation timestamp. It returns the completed fragment of the previous
// access unit, or nil while waiting for a random-access unit or for the first
// sample's duration. Parameter sets and access unit delimiters are removed
// from samples because the initialization segment carries them.
func (m *FMP4Muxer) Write(au [][]byte, pts time.Duration) (*Fragment, error) {
	if len(au) == 0 {
		return nil, nil
	}
	random := isRandomAccess(m.codec, au)
	if m.extractor == nil {
		if !random {
			return nil, nil
		}
		m.extractor = m.newExtractor()
	}

	var samples [][]byte
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		switch kind := classifyNALU(m.codec, nalu); kind {
		case naluVPS, naluSPS, naluPPS:
			if !m.knownParameterSet(kind, nalu) {
				return nil, ErrParameterSetsChanged
			}
		case naluDelimiter:
		default:
			samples = append(samples, nalu)
		}
	}
	if len(samples) == 0 {
		return nil, nil
	}

	ticks := durationToTicks(pts)
	// The extractor needs the parameter sets in-band; RTSP often carries them
	// only in the session description.
	withSets := append(append([][]byte{}, m.sets...), samples...)
	dts, err := m.extractor.Extract(withSets, ticks)
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

func (m *FMP4Muxer) knownParameterSet(kind naluKind, nalu []byte) bool {
	index := map[naluKind]int{naluSPS: 0, naluPPS: 1}
	if m.codec == CodecH265 {
		index = map[naluKind]int{naluVPS: 0, naluSPS: 1, naluPPS: 2}
	}
	position, ok := index[kind]
	return ok && bytes.Equal(nalu, m.sets[position])
}

func (m *FMP4Muxer) fragment(sample *pendingSample, duration int64) (*Fragment, error) {
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
