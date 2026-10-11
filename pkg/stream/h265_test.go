package stream

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/formats/fmp4"
	"github.com/bluenviron/mediacommon/v2/pkg/formats/mp4/codecs"

	"github.com/addodelgrossi/ovm/internal/synthmedia"
)

func newH265Source(t *testing.T) *synthmedia.H265Source {
	t.Helper()
	source, err := synthmedia.NewH265Source()
	if err != nil {
		t.Fatalf("NewH265Source() error = %v", err)
	}
	return source
}

func TestCodecStringFollowsRFC6381(t *testing.T) {
	t.Parallel()
	h265Source := newH265Source(t)
	got, err := codecString(CodecH265, h265Source.ParameterSets()[1])
	if err != nil {
		t.Fatalf("codecString(h265) error = %v", err)
	}
	// Main profile (compatibility flags 1 and 2), Main tier level 2.0, and
	// progressive + frame-only constraint flags.
	if got != "hvc1.1.6.L60.90" {
		t.Fatalf("codecString(h265) = %q, want hvc1.1.6.L60.90", got)
	}
	h264Source := newSyntheticSource(t, synthmedia.H264Config{Width: 64, Height: 48, FPS: 10, GOP: 10})
	if got, _ := codecString(CodecH264, h264Source.SPS()); got != "avc1.42c01f" {
		t.Fatalf("codecString(h264) = %q", got)
	}
	if _, err := codecString(CodecH265, []byte{0x42, 0x01, 0x01}); !errors.Is(err, ErrInvalidParameterSets) {
		t.Fatalf("short H.265 SPS error = %v", err)
	}
}

func TestFMP4MuxerPackagesH265(t *testing.T) {
	t.Parallel()
	source := newH265Source(t)
	if _, err := NewFMP4Muxer(CodecH265, source.ParameterSets()[1:]); !errors.Is(err, ErrInvalidParameterSets) {
		t.Fatalf("missing VPS error = %v", err)
	}
	muxer, err := NewFMP4Muxer(CodecH265, source.ParameterSets())
	if err != nil {
		t.Fatalf("NewFMP4Muxer() error = %v", err)
	}
	if muxer.Width() != 320 || muxer.Height() != 240 {
		t.Fatalf("size = %dx%d", muxer.Width(), muxer.Height())
	}
	init, err := muxer.Init()
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	var parsed fmp4.Init
	if err := parsed.Unmarshal(bytes.NewReader(init)); err != nil {
		t.Fatalf("Init.Unmarshal() error = %v", err)
	}
	if _, ok := parsed.Tracks[0].Codec.(*codecs.H265); !ok {
		t.Fatalf("init codec = %T, want H265", parsed.Tracks[0].Codec)
	}

	stream := init
	keys := 0
	frames := source.Frames()
	for index := 0; index <= frames; index++ {
		au, _ := source.NextAccessUnit()
		fragment, err := muxer.Write(au, time.Duration(index)*source.FrameDuration())
		if err != nil {
			t.Fatalf("Write(%d) error = %v", index, err)
		}
		if fragment != nil {
			stream = append(stream, fragment.Bytes...)
			if fragment.Key {
				keys++
			}
		}
	}
	if keys != 2 {
		t.Fatalf("key fragments = %d, want 2", keys)
	}

	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		return
	}
	path := filepath.Join(t.TempDir(), "preview-h265.mp4")
	if err := os.WriteFile(path, stream, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	// The path is test-owned and ffprobe comes from PATH.
	output, err := exec.Command(ffprobe, "-v", "error", "-count_frames", "-select_streams", "v:0", //nolint:gosec // test-owned arguments
		"-show_entries", "stream=codec_name,width,height,nb_read_frames", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe error = %v", err)
	}
	if got := strings.TrimSpace(string(output)); got != "hevc,320,240,30" {
		t.Fatalf("ffprobe = %q, want hevc,320,240,30", got)
	}
}

func TestRTSPSourcePlaysH265(t *testing.T) {
	t.Parallel()
	server := startRTSPServer(t, synthmedia.RTSPServerConfig{Codec: synthmedia.CodecH265, SDPParameterSets: true})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	source, err := OpenRTSP(ctx, RTSPOptions{URL: server.URL("/h265")})
	if err != nil {
		t.Fatalf("OpenRTSP() error = %v", err)
	}
	defer source.Close()
	if source.Codec() != CodecH265 || len(source.ParameterSets()) != 3 {
		t.Fatalf("codec = %q, sets = %d", source.Codec(), len(source.ParameterSets()))
	}
	sink := &recordingSink{}
	meter := NewMeter(nil)
	pumpCtx, stop := context.WithTimeout(ctx, 2500*time.Millisecond)
	defer stop()
	_ = PumpFMP4(pumpCtx, NewMeteredSource(source, meter), sink)
	if len(sink.codecs) != 1 || sink.codecs[0] != "hvc1.1.6.L60.90" || sink.keys == 0 {
		t.Fatalf("codecs = %v keys = %d", sink.codecs, sink.keys)
	}
	if stats := meter.Snapshot(); stats.Width != 320 || stats.Height != 240 || stats.Keyframes == 0 {
		t.Fatalf("stats = %+v", stats)
	}
}
