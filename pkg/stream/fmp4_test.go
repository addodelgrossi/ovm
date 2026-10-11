package stream

import (
	"bytes"
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

func newSyntheticSource(t *testing.T, config synthmedia.H264Config) *synthmedia.H264Source {
	t.Helper()
	source, err := synthmedia.NewH264Source(config)
	if err != nil {
		t.Fatalf("NewH264Source() error = %v", err)
	}
	return source
}

func TestNewH264FMP4MuxerRejectsInvalidParameterSets(t *testing.T) {
	t.Parallel()
	source := newSyntheticSource(t, synthmedia.H264Config{Width: 64, Height: 48, FPS: 10, GOP: 10})
	cases := map[string][2][]byte{
		"empty":       {nil, nil},
		"swapped":     {source.PPS(), source.SPS()},
		"corrupt sps": {{0x67, 0x42}, source.PPS()},
	}
	for name, sets := range cases {
		if _, err := NewFMP4Muxer(CodecH264, [][]byte{sets[0], sets[1]}); !errors.Is(err, ErrInvalidParameterSets) {
			t.Errorf("%s: error = %v, want ErrInvalidParameterSets", name, err)
		}
	}
}

func TestH264FMP4MuxerInitDescribesTrack(t *testing.T) {
	t.Parallel()
	source := newSyntheticSource(t, synthmedia.H264Config{Width: 320, Height: 240, FPS: 15, GOP: 15})
	muxer, err := NewFMP4Muxer(CodecH264, [][]byte{source.SPS(), source.PPS()})
	if err != nil {
		t.Fatalf("NewH264FMP4Muxer() error = %v", err)
	}
	if got := muxer.CodecString(); got != "avc1.42c01f" {
		t.Fatalf("CodecString() = %q", got)
	}
	if muxer.Width() != 320 || muxer.Height() != 240 {
		t.Fatalf("size = %dx%d", muxer.Width(), muxer.Height())
	}
	segment, err := muxer.Init()
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	var init fmp4.Init
	if err := init.Unmarshal(bytes.NewReader(segment)); err != nil {
		t.Fatalf("Init.Unmarshal() error = %v", err)
	}
	if len(init.Tracks) != 1 || init.Tracks[0].TimeScale != 90000 {
		t.Fatalf("unexpected tracks %+v", init.Tracks)
	}
	codec, ok := init.Tracks[0].Codec.(*codecs.H264)
	if !ok || !bytes.Equal(codec.SPS, source.SPS()) || !bytes.Equal(codec.PPS, source.PPS()) {
		t.Fatal("init segment does not carry the parameter sets")
	}
}

func TestH264FMP4MuxerFragmentsStartAtKeyframeWithOneFrameLatency(t *testing.T) {
	t.Parallel()
	source := newSyntheticSource(t, synthmedia.H264Config{Width: 64, Height: 48, FPS: 10, GOP: 5})
	muxer, err := NewFMP4Muxer(CodecH264, [][]byte{source.SPS(), source.PPS()})
	if err != nil {
		t.Fatalf("NewH264FMP4Muxer() error = %v", err)
	}
	// Drop the first IDR so the muxer joins mid-GOP and must wait.
	source.NextAccessUnit()
	var fragments []*Fragment
	for index := 1; index < 16; index++ {
		au, _ := source.NextAccessUnit()
		fragment, err := muxer.Write(au, time.Duration(index)*source.FrameDuration())
		if err != nil {
			t.Fatalf("Write(%d) error = %v", index, err)
		}
		if index < 6 && fragment != nil {
			t.Fatalf("frame %d produced output before the first keyframe completed", index)
		}
		if fragment != nil {
			fragments = append(fragments, fragment)
		}
	}
	// Frames 5..15 were accepted; the last one is still pending.
	if len(fragments) != 10 {
		t.Fatalf("got %d fragments, want 10", len(fragments))
	}
	for index, fragment := range fragments {
		var parts fmp4.Parts
		if err := parts.Unmarshal(fragment.Bytes); err != nil {
			t.Fatalf("fragment %d: Unmarshal() error = %v", index, err)
		}
		track := parts[0].Tracks[0]
		sample := track.Samples[0]
		if parts[0].SequenceNumber != uint32(index+1) {
			t.Fatalf("fragment %d: sequence = %d", index, parts[0].SequenceNumber)
		}
		if wantBase := uint64(5+index) * 9000; track.BaseTime != wantBase {
			t.Fatalf("fragment %d: base time = %d, want %d", index, track.BaseTime, wantBase)
		}
		if sample.Duration != 9000 {
			t.Fatalf("fragment %d: duration = %d, want 9000", index, sample.Duration)
		}
		if wantKey := index%5 == 0; sample.IsNonSyncSample == wantKey || fragment.Key != wantKey {
			t.Fatalf("fragment %d: sync = %v, key = %v, want %v", index, !sample.IsNonSyncSample, fragment.Key, wantKey)
		}
	}
}

func TestH264FMP4MuxerRejectsChangedParameterSetsAndTimestamps(t *testing.T) {
	t.Parallel()
	small := newSyntheticSource(t, synthmedia.H264Config{Width: 64, Height: 48, FPS: 10, GOP: 5})
	large := newSyntheticSource(t, synthmedia.H264Config{Width: 96, Height: 48, FPS: 10, GOP: 5})
	muxer, err := NewFMP4Muxer(CodecH264, [][]byte{small.SPS(), small.PPS()})
	if err != nil {
		t.Fatalf("NewH264FMP4Muxer() error = %v", err)
	}
	foreign, _ := large.NextAccessUnit()
	if _, err := muxer.Write(foreign, 0); !errors.Is(err, ErrParameterSetsChanged) {
		t.Fatalf("Write(changed SPS) error = %v, want ErrParameterSetsChanged", err)
	}

	muxer, err = NewFMP4Muxer(CodecH264, [][]byte{small.SPS(), small.PPS()})
	if err != nil {
		t.Fatalf("NewH264FMP4Muxer() error = %v", err)
	}
	first, _ := small.NextAccessUnit()
	if _, err := muxer.Write(first, time.Second); err != nil {
		t.Fatalf("Write(first) error = %v", err)
	}
	second, _ := small.NextAccessUnit()
	if _, err := muxer.Write(second, time.Second); !errors.Is(err, ErrTimestamp) {
		t.Fatalf("Write(repeated timestamp) error = %v, want ErrTimestamp", err)
	}
}

func TestDurationToTicksDoesNotOverflow(t *testing.T) {
	t.Parallel()
	if got := durationToTicks(1000 * time.Hour); got != 1000*3600*90000 {
		t.Fatalf("durationToTicks(1000h) = %d", got)
	}
	if got := durationToTicks(time.Second / 3); got != 30000 {
		t.Fatalf("durationToTicks(1/3s) = %d", got)
	}
}

// TestFFmpegDecodesMuxedStream concatenates the init segment and fragments the
// way Media Source Extensions consumes them and decodes the result with FFmpeg
// when it is installed.
func TestFFmpegDecodesMuxedStream(t *testing.T) {
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe is not installed")
	}
	t.Parallel()
	source := newSyntheticSource(t, synthmedia.H264Config{Width: 160, Height: 96, FPS: 25, GOP: 25})
	muxer, err := NewFMP4Muxer(CodecH264, [][]byte{source.SPS(), source.PPS()})
	if err != nil {
		t.Fatalf("NewH264FMP4Muxer() error = %v", err)
	}
	stream, err := muxer.Init()
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	for index := 0; index < 51; index++ {
		au, _ := source.NextAccessUnit()
		fragment, err := muxer.Write(au, time.Duration(index)*source.FrameDuration())
		if err != nil {
			t.Fatalf("Write(%d) error = %v", index, err)
		}
		if fragment != nil {
			stream = append(stream, fragment.Bytes...)
		}
	}
	path := filepath.Join(t.TempDir(), "preview.mp4")
	if err := os.WriteFile(path, stream, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	// The path is test-owned and ffprobe comes from PATH.
	output, err := exec.Command(ffprobe, "-v", "error", "-count_frames", "-select_streams", "v:0", //nolint:gosec // test-owned arguments
		"-show_entries", "stream=codec_name,width,height,nb_read_frames", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe error = %v", err)
	}
	if got := strings.TrimSpace(string(output)); got != "h264,160,96,50" {
		t.Fatalf("ffprobe = %q, want h264,160,96,50", got)
	}
}
