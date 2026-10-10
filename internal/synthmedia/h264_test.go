package synthmedia

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
)

func TestBitWriterExpGolomb(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		write func(*bitWriter)
		want  string
	}{
		{"ue0", func(w *bitWriter) { w.writeUE(0) }, "1"},
		{"ue1", func(w *bitWriter) { w.writeUE(1) }, "010"},
		{"ue2", func(w *bitWriter) { w.writeUE(2) }, "011"},
		{"ue7", func(w *bitWriter) { w.writeUE(7) }, "0001000"},
		{"se1", func(w *bitWriter) { w.writeSE(1) }, "010"},
		{"se-1", func(w *bitWriter) { w.writeSE(-1) }, "011"},
		{"se0", func(w *bitWriter) { w.writeSE(0) }, "1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var w bitWriter
			tc.write(&w)
			if got := bitString(w); got != tc.want {
				t.Fatalf("bits = %s, want %s", got, tc.want)
			}
		})
	}
}

func bitString(w bitWriter) string {
	out := make([]byte, 0, w.nbits)
	for i := 0; i < w.nbits; i++ {
		if w.buf[i/8]&(0x80>>(i%8)) != 0 {
			out = append(out, '1')
		} else {
			out = append(out, '0')
		}
	}
	return string(out)
}

func TestNALUnitInsertsEmulationPrevention(t *testing.T) {
	t.Parallel()
	got := nalUnit(3, naluTypeIDR, []byte{0, 0, 1, 0, 0, 0, 0, 0, 3, 0xAA})
	want := []byte{0x65, 0, 0, 3, 1, 0, 0, 3, 0, 0, 3, 0, 3, 0xAA}
	if !bytes.Equal(got, want) {
		t.Fatalf("nalUnit = % x, want % x", got, want)
	}
}

func TestNewH264SourceRejectsInvalidConfig(t *testing.T) {
	t.Parallel()
	valid := H264Config{Width: 320, Height: 240, FPS: 15, GOP: 15}
	mutations := map[string]func(*H264Config){
		"unaligned width":  func(c *H264Config) { c.Width = 330 },
		"tiny height":      func(c *H264Config) { c.Height = 16 },
		"oversized width":  func(c *H264Config) { c.Width = 1936 },
		"zero fps":         func(c *H264Config) { c.FPS = 0 },
		"excessive fps":    func(c *H264Config) { c.FPS = 61 },
		"zero gop":         func(c *H264Config) { c.GOP = 0 },
		"excessive gop":    func(c *H264Config) { c.GOP = 601 },
		"oversized height": func(c *H264Config) { c.Height = 1104 },
	}
	for name, mutate := range mutations {
		config := valid
		mutate(&config)
		if _, err := NewH264Source(config); !errors.Is(err, ErrInvalidConfig) {
			t.Errorf("%s: error = %v, want ErrInvalidConfig", name, err)
		}
	}
}

func TestParameterSetsDescribeConfiguredStream(t *testing.T) {
	t.Parallel()
	source, err := NewH264Source(H264Config{Width: 640, Height: 352, FPS: 25, GOP: 50})
	if err != nil {
		t.Fatalf("NewH264Source() error = %v", err)
	}
	var sps h264.SPS
	if err := sps.Unmarshal(source.SPS()); err != nil {
		t.Fatalf("SPS.Unmarshal() error = %v", err)
	}
	if sps.Width() != 640 || sps.Height() != 352 {
		t.Fatalf("SPS size = %dx%d, want 640x352", sps.Width(), sps.Height())
	}
	if sps.ProfileIdc != 66 {
		t.Fatalf("profile_idc = %d, want 66", sps.ProfileIdc)
	}
	if source.FrameDuration() != 40*time.Millisecond {
		t.Fatalf("FrameDuration() = %v, want 40ms", source.FrameDuration())
	}
	if got := h264.NALUType(source.PPS()[0] & 0x1F); got != h264.NALUTypePPS {
		t.Fatalf("PPS NAL type = %v", got)
	}
}

func TestAccessUnitsFollowGOPCadence(t *testing.T) {
	t.Parallel()
	source, err := NewH264Source(H264Config{Width: 64, Height: 48, FPS: 10, GOP: 4})
	if err != nil {
		t.Fatalf("NewH264Source() error = %v", err)
	}
	for index := 0; index < 12; index++ {
		au, idr := source.NextAccessUnit()
		wantIDR := index%4 == 0
		if idr != wantIDR || h264.IsRandomAccess(au) != wantIDR {
			t.Fatalf("frame %d: idr = %v, random access = %v, want %v", index, idr, h264.IsRandomAccess(au), wantIDR)
		}
		if wantIDR {
			if len(au) != 3 || h264.NALUType(au[0][0]&0x1F) != h264.NALUTypeSPS || h264.NALUType(au[1][0]&0x1F) != h264.NALUTypePPS {
				t.Fatalf("frame %d: IDR access unit must carry SPS and PPS in-band", index)
			}
			continue
		}
		if len(au) != 1 || h264.NALUType(au[0][0]&0x1F) != h264.NALUTypeNonIDR {
			t.Fatalf("frame %d: want one non-IDR slice", index)
		}
	}
}

// TestFFmpegDecodesExactPicturesLosslessly decodes the stream with FFmpeg when
// it is installed. I_PCM coding is lossless, so every decoded picture must
// equal the model exactly, which proves both syntax and reference handling.
func TestFFmpegDecodesExactPicturesLosslessly(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg is not installed")
	}
	t.Parallel()
	config := H264Config{Width: 96, Height: 64, FPS: 10, GOP: 7}
	source, err := NewH264Source(config)
	if err != nil {
		t.Fatalf("NewH264Source() error = %v", err)
	}
	const frames = 23
	var stream []byte
	for i := 0; i < frames; i++ {
		au, _ := source.NextAccessUnit()
		for _, nalu := range au {
			stream = append(stream, 0, 0, 0, 1)
			stream = append(stream, nalu...)
		}
	}
	input := filepath.Join(t.TempDir(), "synthetic.h264")
	if err := os.WriteFile(input, stream, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	// The input path is test-owned and ffmpeg comes from PATH.
	command := exec.Command(ffmpeg, "-v", "error", "-err_detect", "explode", "-i", input, "-f", "rawvideo", "-pix_fmt", "yuv420p", "-") //nolint:gosec // test-owned arguments
	decoded, err := command.Output()
	if err != nil {
		t.Fatalf("ffmpeg decode error = %v", err)
	}
	frameSize := config.Width * config.Height * 3 / 2
	if len(decoded) != frames*frameSize {
		t.Fatalf("decoded %d bytes, want %d frames of %d", len(decoded), frames, frameSize)
	}
	for index := 0; index < frames; index++ {
		want := expectedPicture(source, index)
		got := decoded[index*frameSize : (index+1)*frameSize]
		if !bytes.Equal(got, want) {
			t.Fatalf("decoded picture %d differs from the synthetic model", index)
		}
	}
}

func expectedPicture(s *H264Source, index int) []byte {
	width, height := s.config.Width, s.config.Height
	column := s.blockColumn(index)
	picture := make([]byte, 0, width*height*3/2)
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			picture = append(picture, s.pixel(s.inBlock(x/16, y/16, column), x)[0])
		}
	}
	for plane := 1; plane <= 2; plane++ {
		for y := 0; y < height/2; y++ {
			for x := 0; x < width/2; x++ {
				picture = append(picture, s.pixel(s.inBlock(x/8, y/8, column), x*2)[plane])
			}
		}
	}
	return picture
}
