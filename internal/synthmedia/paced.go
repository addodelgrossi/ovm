package synthmedia

import (
	"context"
	"time"
)

// PacedH264 releases the synthetic stream in real time, as a camera would.
type PacedH264 struct {
	source *H264Source
	start  time.Time
	frames int
	now    func() time.Time
}

// NewPacedH264 starts the real-time clock at the first read.
func NewPacedH264(source *H264Source) *PacedH264 {
	return &PacedH264{source: source, now: time.Now}
}

// SPS returns the sequence parameter set.
func (p *PacedH264) SPS() []byte { return p.source.SPS() }

// PPS returns the picture parameter set.
func (p *PacedH264) PPS() []byte { return p.source.PPS() }

// ReadAccessUnit waits until the next frame is due and returns it with its
// presentation timestamp relative to the first frame.
func (p *PacedH264) ReadAccessUnit(ctx context.Context) ([][]byte, time.Duration, error) {
	if p.frames == 0 {
		p.start = p.now()
	}
	pts := time.Duration(p.frames) * p.source.FrameDuration()
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
	au, _ := p.source.NextAccessUnit()
	return au, pts, nil
}
