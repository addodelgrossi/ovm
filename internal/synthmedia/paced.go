package synthmedia

import (
	"context"
	"time"
)

// Codec names match pkg/stream's VideoSource codecs.
const (
	CodecH264 = "h264"
	CodecH265 = "h265"
)

// FrameGenerator produces an endless sequence of access units.
type FrameGenerator interface {
	Codec() string
	ParameterSets() [][]byte
	FrameDuration() time.Duration
	NextAccessUnit() ([][]byte, bool)
}

// Paced releases a generator's stream in real time, as a camera would. It
// implements pkg/stream's VideoSource.
type Paced struct {
	generator FrameGenerator
	start     time.Time
	frames    int
	now       func() time.Time
}

// NewPaced starts the real-time clock at the first read.
func NewPaced(generator FrameGenerator) *Paced {
	return &Paced{generator: generator, now: time.Now}
}

// Codec returns the generator's codec.
func (p *Paced) Codec() string { return p.generator.Codec() }

// ParameterSets returns the generator's parameter sets.
func (p *Paced) ParameterSets() [][]byte { return p.generator.ParameterSets() }

// ReadAccessUnit waits until the next frame is due and returns it with its
// presentation timestamp relative to the first frame.
func (p *Paced) ReadAccessUnit(ctx context.Context) ([][]byte, time.Duration, error) {
	if p.frames == 0 {
		p.start = p.now()
	}
	pts := time.Duration(p.frames) * p.generator.FrameDuration()
	if wait := p.start.Add(pts).Sub(p.now()); wait > 0 {
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, 0, ctx.Err()
		case <-timer.C:
		}
	} else if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	p.frames++
	au, _ := p.generator.NextAccessUnit()
	return au, pts, nil
}
