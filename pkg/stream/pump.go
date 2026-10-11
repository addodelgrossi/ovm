package stream

import (
	"bytes"
	"context"
	"errors"
)

// FragmentSink receives a fragmented MP4 stream.
type FragmentSink interface {
	// SetInit publishes a codecs parameter and initialization segment. It is
	// called again when the parameter sets change.
	SetInit(codec string, init []byte)
	// WriteFragment publishes one media fragment.
	WriteFragment(fragment []byte, key bool)
}

// PumpFMP4 remuxes source into sink until ctx ends or the source fails. A
// change of in-band parameter sets restarts the muxer and republishes the
// initialization segment, as does a timestamp discontinuity. It returns
// ctx.Err() after cancellation.
func PumpFMP4(ctx context.Context, source VideoSource, sink FragmentSink) error {
	codec := source.Codec()
	if codec != CodecH264 && codec != CodecH265 {
		return ErrRTSPUnsupportedCodec
	}
	sets := source.ParameterSets()
	var muxer *FMP4Muxer
	for {
		au, pts, err := source.ReadAccessUnit(ctx)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return ctxErr
			}
			return err
		}
		if inBand := inBandParameterSets(codec, au); inBand != nil && (muxer == nil || !sameSets(inBand, sets)) {
			sets = inBand
			muxer = nil
		}
		if muxer == nil {
			if !validParameterSets(codec, sets) {
				continue
			}
			next, err := NewFMP4Muxer(codec, sets)
			if err != nil {
				return err
			}
			init, err := next.Init()
			if err != nil {
				return err
			}
			muxer = next
			sink.SetInit(next.CodecString(), init)
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

func sameSets(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
