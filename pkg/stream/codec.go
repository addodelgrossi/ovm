package stream

import (
	"context"
	"fmt"
	"math/bits"
	"strings"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h265"
)

// Supported video codecs, as returned by VideoSource.Codec.
const (
	CodecH264 = "h264"
	CodecH265 = "h265"
)

// VideoSource yields access units of one H.264 or H.265 video track.
type VideoSource interface {
	// Codec is CodecH264 or CodecH265.
	Codec() string
	// ParameterSets describe the stream before the first access unit, for
	// example from an RTSP session description: SPS and PPS for H.264, VPS,
	// SPS, and PPS for H.265. It may be empty when the stream carries them
	// only in-band.
	ParameterSets() [][]byte
	// ReadAccessUnit blocks for the next access unit (NAL units without start
	// codes) and its presentation timestamp.
	ReadAccessUnit(ctx context.Context) ([][]byte, time.Duration, error)
}

// LiveSource is a VideoSource that owns a connection.
type LiveSource interface {
	VideoSource
	Close()
}

type naluKind int

const (
	naluOther naluKind = iota
	naluVPS
	naluSPS
	naluPPS
	naluDelimiter
)

func classifyNALU(codec string, nalu []byte) naluKind {
	if len(nalu) == 0 {
		return naluOther
	}
	if codec == CodecH265 {
		switch h265.NALUType((nalu[0] >> 1) & 0x3F) {
		case h265.NALUType_VPS_NUT:
			return naluVPS
		case h265.NALUType_SPS_NUT:
			return naluSPS
		case h265.NALUType_PPS_NUT:
			return naluPPS
		case h265.NALUType_AUD_NUT:
			return naluDelimiter
		}
		return naluOther
	}
	switch h264.NALUType(nalu[0] & 0x1F) {
	case h264.NALUTypeSPS:
		return naluSPS
	case h264.NALUTypePPS:
		return naluPPS
	case h264.NALUTypeAccessUnitDelimiter:
		return naluDelimiter
	}
	return naluOther
}

func isRandomAccess(codec string, au [][]byte) bool {
	if codec == CodecH265 {
		return h265.IsRandomAccess(au)
	}
	return h264.IsRandomAccess(au)
}

// inBandParameterSets returns a complete, ordered parameter-set list carried
// by au, or nil when the access unit does not carry all of them.
func inBandParameterSets(codec string, au [][]byte) [][]byte {
	var vps, sps, pps []byte
	for _, nalu := range au {
		switch classifyNALU(codec, nalu) {
		case naluVPS:
			vps = nalu
		case naluSPS:
			sps = nalu
		case naluPPS:
			pps = nalu
		}
	}
	if sps == nil || pps == nil {
		return nil
	}
	if codec == CodecH265 {
		if vps == nil {
			return nil
		}
		return [][]byte{vps, sps, pps}
	}
	return [][]byte{sps, pps}
}

// validParameterSets checks the count and NAL types for codec.
func validParameterSets(codec string, sets [][]byte) bool {
	want := []naluKind{naluSPS, naluPPS}
	if codec == CodecH265 {
		want = []naluKind{naluVPS, naluSPS, naluPPS}
	} else if codec != CodecH264 {
		return false
	}
	if len(sets) != len(want) {
		return false
	}
	for i, kind := range want {
		if classifyNALU(codec, sets[i]) != kind {
			return false
		}
	}
	return true
}

// pictureSize parses the SPS for the coded size after cropping.
func pictureSize(codec string, sps []byte) (int, int, bool) {
	if codec == CodecH265 {
		var parsed h265.SPS
		if parsed.Unmarshal(sps) != nil {
			return 0, 0, false
		}
		return parsed.Width(), parsed.Height(), true
	}
	var parsed h264.SPS
	if parsed.Unmarshal(sps) != nil {
		return 0, 0, false
	}
	return parsed.Width(), parsed.Height(), true
}

// codecString builds the RFC 6381 codecs parameter used by Media Source
// Extensions: "avc1.PPCCLL" for H.264 and the ISO/IEC 14496-15 Annex E form
// "hvc1.P.C.TL.B" for H.265.
func codecString(codec string, sps []byte) (string, error) {
	if codec == CodecH264 {
		if len(sps) < 4 {
			return "", ErrInvalidParameterSets
		}
		return fmt.Sprintf("avc1.%02x%02x%02x", sps[1], sps[2], sps[3]), nil
	}
	// After the two-byte NAL header, the SPS RBSP begins with one byte of
	// VPS ID / sub-layer fields, then the byte-aligned general
	// profile_tier_level: 1 byte space/tier/profile, 4 bytes compatibility
	// flags, 6 bytes constraint flags, and 1 byte level.
	if len(sps) < 2 {
		return "", ErrInvalidParameterSets
	}
	rbsp := h264.EmulationPreventionRemove(sps[2:])
	if len(rbsp) < 13 {
		return "", ErrInvalidParameterSets
	}
	profileSpace := rbsp[1] >> 6
	tier := "L"
	if rbsp[1]&0x20 != 0 {
		tier = "H"
	}
	profile := rbsp[1] & 0x1F
	compatibility := bits.Reverse32(uint32(rbsp[2])<<24 | uint32(rbsp[3])<<16 | uint32(rbsp[4])<<8 | uint32(rbsp[5]))
	constraints := rbsp[6:12]
	last := len(constraints)
	for last > 0 && constraints[last-1] == 0 {
		last--
	}
	var builder strings.Builder
	builder.WriteString("hvc1.")
	if profileSpace > 0 {
		builder.WriteByte("ABC"[profileSpace-1])
	}
	fmt.Fprintf(&builder, "%d.%X.%s%d", profile, compatibility, tier, rbsp[12])
	for _, constraint := range constraints[:last] {
		fmt.Fprintf(&builder, ".%X", constraint)
	}
	return builder.String(), nil
}
