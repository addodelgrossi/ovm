package stream

import (
	"bytes"
	"context"
	"errors"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
)

// H264Source yields H.264 access units with presentation timestamps.
type H264Source interface {
	// SPS and PPS describe the stream before the first access unit, for
	// example from an RTSP session description. They may be empty when the
	// stream carries parameter sets only in-band.
	SPS() []byte
	PPS() []byte
	// ReadAccessUnit blocks for the next access unit (NAL units without start
	// codes) and its presentation timestamp.
	ReadAccessUnit(ctx context.Context) ([][]byte, time.Duration, error)
}

// FragmentSink receives a fragmented MP4 stream.
type FragmentSink interface {
	// SetInit publishes a codecs parameter and initialization segment. It is
	// called again when the parameter sets change.
	SetInit(codec string, init []byte)
	// WriteFragment publishes one media fragment.
	WriteFragment(fragment []byte, key bool)
}

// PumpH264FMP4 remuxes source into sink until ctx ends or the source fails.
// A change of in-band parameter sets restarts the muxer and republishes the
// initialization segment. It returns ctx.Err() after cancellation.
func PumpH264FMP4(ctx context.Context, source H264Source, sink FragmentSink) error {
	sps, pps := source.SPS(), source.PPS()
	var muxer *H264FMP4Muxer
	start := func() error {
		next, err := NewH264FMP4Muxer(sps, pps)
		if err != nil {
			return err
		}
		init, err := next.Init()
		if err != nil {
			return err
		}
		muxer = next
		sink.SetInit(next.CodecString(), init)
		return nil
	}
	for {
		au, pts, err := source.ReadAccessUnit(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return err
		}
		if inSPS, inPPS := parameterSets(au); inSPS != nil && inPPS != nil &&
			(muxer == nil || !bytes.Equal(inSPS, sps) || !bytes.Equal(inPPS, pps)) {
			sps, pps = inSPS, inPPS
			muxer = nil
		}
		if muxer == nil {
			if len(sps) == 0 || len(pps) == 0 {
				continue
			}
			if err := start(); err != nil {
				return err
			}
		}
		fragment, err := muxer.Write(au, pts)
		if errors.Is(err, ErrTimestamp) {
			// Restart at the next keyframe instead of ending the preview.
			muxer = nil
			continue
		}
		if err != nil {
			return err
		}
		if fragment != nil {
			sink.WriteFragment(fragment.Bytes, fragment.Key)
		}
	}
}

func parameterSets(au [][]byte) (sps, pps []byte) {
	for _, nalu := range au {
		if len(nalu) == 0 {
			continue
		}
		switch h264.NALUType(nalu[0] & 0x1F) {
		case h264.NALUTypeSPS:
			sps = nalu
		case h264.NALUTypePPS:
			pps = nalu
		}
	}
	return sps, pps
}
