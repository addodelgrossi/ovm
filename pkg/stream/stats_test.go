package stream

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/synthmedia"
)

type manualClock struct{ now time.Time }

func (c *manualClock) Now() time.Time { return c.now }

func TestMeterMeasuresRateGOPAndTimeToFirstFrame(t *testing.T) {
	t.Parallel()
	source := newSyntheticSource(t, synthmedia.H264Config{Width: 160, Height: 96, FPS: 25, GOP: 25})
	clock := &manualClock{now: time.Unix(1000, 0)}
	meter := NewMeter(clock.Now)

	// Join mid-GOP: the first 5 access units precede a keyframe and are ignored.
	for i := 0; i < 5; i++ {
		source.NextAccessUnit()
	}
	clock.now = clock.now.Add(150 * time.Millisecond)
	var expectedBytes uint64
	for index := 5; index < 105; index++ {
		au, _ := source.NextAccessUnit()
		meter.Observe(CodecH264, au, time.Duration(index)*40*time.Millisecond)
		if index >= 25 {
			for _, nalu := range au {
				expectedBytes += uint64(len(nalu))
			}
		}
	}
	stats := meter.Snapshot()
	if stats.Width != 160 || stats.Height != 96 {
		t.Fatalf("size = %dx%d", stats.Width, stats.Height)
	}
	if stats.TimeToFirstFrame != 150*time.Millisecond {
		t.Fatalf("TimeToFirstFrame = %v", stats.TimeToFirstFrame)
	}
	if stats.Frames != 80 || stats.Keyframes != 4 || stats.Bytes != expectedBytes {
		t.Fatalf("frames=%d keyframes=%d bytes=%d (want 80, 4, %d)", stats.Frames, stats.Keyframes, stats.Bytes, expectedBytes)
	}
	if stats.GOPFrames != 25 || math.Abs(stats.GOPSeconds-1) > 1e-9 {
		t.Fatalf("GOP = %d frames / %.3fs, want 25 / 1s", stats.GOPFrames, stats.GOPSeconds)
	}
	if math.Abs(stats.FramesPerSecond-25) > 1e-6 {
		t.Fatalf("FramesPerSecond = %f, want 25", stats.FramesPerSecond)
	}
	if stats.BitrateKbps <= 0 {
		t.Fatalf("BitrateKbps = %f", stats.BitrateKbps)
	}
}

func TestMeterBitrateCountsPayloadOverTheWindow(t *testing.T) {
	t.Parallel()
	meter := NewMeter(nil)
	key := [][]byte{{0x65, 0, 0, 0}}
	for i := 0; i <= 10; i++ {
		// 1000-byte units at 10 fps for one second: 10 units in the span.
		unit := [][]byte{append([]byte{0x41}, make([]byte, 999)...)}
		if i == 0 {
			unit = [][]byte{append(key[0], make([]byte, 996)...)}
		}
		meter.Observe(CodecH264, unit, time.Duration(i)*100*time.Millisecond)
	}
	stats := meter.Snapshot()
	if math.Abs(stats.BitrateKbps-80) > 1e-6 || math.Abs(stats.FramesPerSecond-10) > 1e-6 {
		t.Fatalf("bitrate=%f fps=%f, want 80 kbps and 10 fps", stats.BitrateKbps, stats.FramesPerSecond)
	}
}

type lossySource struct {
	VideoSource
	lost uint64
}

func (l *lossySource) LostPackets() uint64 { return l.lost }

func TestMeteredSourceObservesReadsAndReportsLoss(t *testing.T) {
	t.Parallel()
	inner := newSyntheticSource(t, synthmedia.H264Config{Width: 64, Height: 48, FPS: 10, GOP: 5})
	scripted := &scriptedSource{codec: CodecH264, sets: inner.ParameterSets()}
	for i := 0; i < 3; i++ {
		au, _ := inner.NextAccessUnit()
		scripted.units = append(scripted.units, au)
		scripted.pts = append(scripted.pts, time.Duration(i)*100*time.Millisecond)
	}
	meter := NewMeter(nil)
	source := NewMeteredSource(&lossySource{VideoSource: scripted, lost: 7}, meter)
	for i := 0; i < 3; i++ {
		if _, _, err := source.ReadAccessUnit(context.Background()); err != nil {
			t.Fatalf("ReadAccessUnit() error = %v", err)
		}
	}
	stats := meter.Snapshot()
	if stats.Frames != 3 || stats.LostPackets != 7 {
		t.Fatalf("frames=%d lost=%d, want 3 and 7", stats.Frames, stats.LostPackets)
	}
}
