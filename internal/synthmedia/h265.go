package synthmedia

import (
	_ "embed"
	"errors"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
)

// h265Pattern is the synthetic color-bar pattern of the H.264 source, encoded
// once as H.265 Main (320x240, 15 fps, IDR every 15 frames, no B-frames, AUD
// and in-band VPS/SPS/PPS before each IDR). See testdata/README.md.
//
//go:embed testdata/pattern-320x240-15fps.h265
var h265Pattern []byte

const h265PatternFPS = 15

// H265Source loops the embedded H.265 fixture endlessly. It is not safe for
// concurrent use.
type H265Source struct {
	units [][][]byte
	sets  [][]byte
	next  int
}

// NewH265Source parses the embedded fixture into access units.
func NewH265Source() (*H265Source, error) {
	source := &H265Source{}
	var current [][]byte
	var vps, sps, pps []byte
	for _, nalu := range splitAnnexB(h265Pattern) {
		if len(nalu) < 2 {
			continue
		}
		switch h265.NALUType((nalu[0] >> 1) & 0x3F) {
		case h265.NALUType_AUD_NUT:
			if len(current) > 0 {
				source.units = append(source.units, current)
			}
			current = nil
			continue
		case h265.NALUType_VPS_NUT:
			vps = nalu
		case h265.NALUType_SPS_NUT:
			sps = nalu
		case h265.NALUType_PPS_NUT:
			pps = nalu
		}
		current = append(current, nalu)
	}
	if len(current) > 0 {
		source.units = append(source.units, current)
	}
	if vps == nil || sps == nil || pps == nil || len(source.units) == 0 || !h265.IsRandomAccess(source.units[0]) {
		return nil, errors.New("synthmedia: the H.265 fixture is malformed")
	}
	source.sets = [][]byte{vps, sps, pps}
	return source, nil
}

// Codec returns CodecH265.
func (s *H265Source) Codec() string { return CodecH265 }

// ParameterSets returns the VPS, SPS, and PPS.
func (s *H265Source) ParameterSets() [][]byte {
	out := make([][]byte, len(s.sets))
	for i, set := range s.sets {
		out[i] = append([]byte(nil), set...)
	}
	return out
}

// FrameDuration is the fixture's frame interval.
func (s *H265Source) FrameDuration() time.Duration { return time.Second / h265PatternFPS }

// Frames is the number of access units in one loop.
func (s *H265Source) Frames() int { return len(s.units) }

// NextAccessUnit returns the next access unit and whether it is random access.
func (s *H265Source) NextAccessUnit() ([][]byte, bool) {
	unit := s.units[s.next]
	s.next = (s.next + 1) % len(s.units)
	return unit, h265.IsRandomAccess(unit)
}

// splitAnnexB splits a byte stream on 3- and 4-byte start codes.
func splitAnnexB(data []byte) [][]byte {
	var nalus [][]byte
	start := -1
	for i := 0; i+2 < len(data); {
		if data[i] == 0 && data[i+1] == 0 && data[i+2] == 1 {
			if start >= 0 {
				end := i
				if end > start && data[end-1] == 0 {
					end--
				}
				nalus = append(nalus, data[start:end])
			}
			i += 3
			start = i
			continue
		}
		i++
	}
	if start >= 0 && start < len(data) {
		nalus = append(nalus, data[start:])
	}
	return nalus
}
