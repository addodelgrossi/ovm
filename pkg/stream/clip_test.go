package stream

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/synthmedia"
)

func feedClip(t *testing.T, sink FragmentSink, frames, skip int) {
	t.Helper()
	source := newSyntheticSource(t, synthmedia.H264Config{Width: 160, Height: 96, FPS: 25, GOP: 10})
	muxer, err := NewFMP4Muxer(CodecH264, source.ParameterSets())
	if err != nil {
		t.Fatalf("NewFMP4Muxer() error = %v", err)
	}
	init, err := muxer.Init()
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	sink.SetInit(muxer.CodecString(), init)
	for index := 0; index < frames; index++ {
		au, _ := source.NextAccessUnit()
		fragment, err := muxer.Write(au, time.Duration(index)*source.FrameDuration())
		if err != nil {
			t.Fatalf("Write(%d) error = %v", index, err)
		}
		if fragment != nil && index > skip {
			sink.WriteFragment(fragment.Bytes, fragment.Key)
		}
	}
}

func TestClipRecorderPublishesKeyframeAlignedClipAtomically(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "clips", "camera.mp4")
	recorder, err := NewClipRecorder(path, 0)
	if err != nil {
		t.Fatalf("NewClipRecorder() error = %v", err)
	}
	// Fragments before the first delivered keyframe (frame 10) are skipped.
	feedClip(t, recorder, 41, 3)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the clip was visible before Finish")
	}
	size, err := recorder.Finish()
	if err != nil {
		t.Fatalf("Finish() error = %v", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() != size || info.Mode().Perm() != 0o600 {
		t.Fatalf("published clip = %v, %v", info, err)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".clip-*"))
	if len(leftovers) != 0 {
		t.Fatalf("temporary files remain: %v", leftovers)
	}
	if ffprobe, err := exec.LookPath("ffprobe"); err == nil {
		// The path is test-owned and ffprobe comes from PATH.
		output, err := exec.Command(ffprobe, "-v", "error", "-count_frames", "-select_streams", "v:0", //nolint:gosec // test-owned arguments
			"-show_entries", "stream=codec_name,nb_read_frames", "-of", "csv=p=0", path).Output()
		if err != nil {
			t.Fatalf("ffprobe error = %v", err)
		}
		// Frames 10..39 were delivered; frame 40 is still pending in the muxer.
		if got := strings.TrimSpace(string(output)); got != "h264,30" {
			t.Fatalf("ffprobe = %q, want h264,30", got)
		}
	}
	if _, err := recorder.Finish(); err == nil {
		t.Fatal("a second Finish() succeeded")
	}
}

func TestClipRecorderRefusesExistingDestinationAndEmptyClips(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	existing := filepath.Join(directory, "existing.mp4")
	if err := os.WriteFile(existing, []byte("keep"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if _, err := NewClipRecorder(existing, 0); !errors.Is(err, ErrClipExists) {
		t.Fatalf("NewClipRecorder(existing) error = %v", err)
	}
	empty, err := NewClipRecorder(filepath.Join(directory, "empty.mp4"), 0)
	if err != nil {
		t.Fatalf("NewClipRecorder() error = %v", err)
	}
	if _, err := empty.Finish(); !errors.Is(err, ErrClipEmpty) {
		t.Fatalf("Finish() without keyframe error = %v, want ErrClipEmpty", err)
	}
	if entries, _ := os.ReadDir(directory); len(entries) != 1 {
		t.Fatalf("directory has %d entries, want only the existing file", len(entries))
	}
}

func TestClipRecorderStopsAtSizeLimitAndCodecChange(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	limited, err := NewClipRecorder(filepath.Join(directory, "limited.mp4"), 4096)
	if err != nil {
		t.Fatalf("NewClipRecorder() error = %v", err)
	}
	feedClip(t, limited, 60, -1)
	size, err := limited.Finish()
	if !errors.Is(err, ErrClipInterrupted) || size == 0 || size > 4096 {
		t.Fatalf("Finish() = %d, %v; want a published clip at or under 4096 bytes", size, err)
	}

	changed, err := NewClipRecorder(filepath.Join(directory, "changed.mp4"), 0)
	if err != nil {
		t.Fatalf("NewClipRecorder() error = %v", err)
	}
	feedClip(t, changed, 20, -1)
	before := changed.Bytes()
	feedClip(t, changed, 20, -1)
	if changed.Bytes() != before {
		t.Fatal("the recorder kept writing after a new initialization segment")
	}
	if _, err := changed.Finish(); !errors.Is(err, ErrClipInterrupted) {
		t.Fatalf("Finish() after codec change error = %v, want ErrClipInterrupted", err)
	}
}
