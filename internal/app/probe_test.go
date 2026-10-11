package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/synthmedia"
	"github.com/addodelgrossi/ovm/pkg/stream"
)

type probeMedia struct {
	result MediaProfilesResult
	err    error
}

func (p probeMedia) Profiles(context.Context, MediaProfilesRequest) (MediaProfilesResult, error) {
	return p.result, p.err
}

type pacedLive struct {
	*synthmedia.Paced
	limit int
	reads int
}

func (p *pacedLive) ReadAccessUnit(ctx context.Context) ([][]byte, time.Duration, error) {
	if p.limit > 0 && p.reads >= p.limit {
		return nil, 0, stream.ErrRTSPClosed
	}
	p.reads++
	return p.Paced.ReadAccessUnit(ctx)
}

func (p *pacedLive) Close() {}

func syntheticLive(t *testing.T, limit int) *pacedLive {
	t.Helper()
	source, err := synthmedia.NewH264Source(synthmedia.H264Config{Width: 64, Height: 48, FPS: 25, GOP: 5})
	if err != nil {
		t.Fatalf("NewH264Source() error = %v", err)
	}
	return &pacedLive{Paced: synthmedia.NewPaced(source), limit: limit}
}

func probeProfiles() MediaProfilesResult {
	return MediaProfilesResult{Profiles: []MediaProfileDTO{
		supportedStreamProfile("profile_sub", "rtsp://cam/sub", "rtsp://cam/sub-udp"),
		supportedStreamProfile("profile_main", "rtsp://cam/main?channel=1", "rtsp://cam/main-udp"),
	}}
}

func TestProbeMeasuresTheDefaultProfileForTheDuration(t *testing.T) {
	t.Parallel()
	creds, err := NewCredentials("operator", "probe-secret", false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	var options stream.RTSPOptions
	probe := &probeUseCase{
		media: probeMedia{result: probeProfiles()},
		open: func(_ context.Context, received stream.RTSPOptions) (stream.LiveSource, error) {
			options = received
			return syntheticLive(t, 0), nil
		},
		now: time.Now,
	}
	result, err := probe.Probe(context.Background(), ProbeRequest{Host: "cam", Credentials: creds, Duration: time.Second})
	if err != nil {
		t.Fatalf("Probe() error = %v", err)
	}
	if options.URL != "rtsp://cam/main?channel=1" || options.Username != "operator" || options.Password != "probe-secret" || options.Transport != stream.TransportTCP {
		t.Fatalf("opener options = %+v", options)
	}
	if result.ProfileToken != "profile_main" || result.Codec != stream.CodecH264 || result.Transport != "tcp" || !result.Complete {
		t.Fatalf("result = %+v", result)
	}
	if result.Width != 64 || result.Height != 48 || result.Keyframes < 4 || result.FramesPerSecond < 20 || result.GOPFrames != 5 {
		t.Fatalf("measurements = %+v", result)
	}
	if result.DurationMillis < 1000 {
		t.Fatalf("duration = %dms, want at least 1000", result.DurationMillis)
	}
}

func TestProbeSelectsUDPAndValidatesInput(t *testing.T) {
	t.Parallel()
	var url string
	probe := &probeUseCase{
		media: probeMedia{result: probeProfiles()},
		open: func(_ context.Context, options stream.RTSPOptions) (stream.LiveSource, error) {
			url = options.URL
			return syntheticLive(t, 0), nil
		},
		now: time.Now,
	}
	if _, err := probe.Probe(context.Background(), ProbeRequest{Host: "cam", ProfileToken: "profile_sub", Transport: StreamTransportUDP, Duration: time.Second}); err != nil {
		t.Fatalf("Probe(udp) error = %v", err)
	}
	if url != "rtsp://cam/sub-udp" {
		t.Fatalf("UDP URL = %q", url)
	}
	for name, request := range map[string]ProbeRequest{
		"short duration": {Host: "cam", Duration: 500 * time.Millisecond},
		"long duration":  {Host: "cam", Duration: 11 * time.Minute},
		"bad transport":  {Host: "cam", Duration: time.Second, Transport: "sctp"},
		"bad token":      {Host: "cam", Duration: time.Second, ProfileToken: "bad\x01token"},
		"missing token":  {Host: "cam", Duration: time.Second, ProfileToken: "absent"},
	} {
		_, err := probe.Probe(context.Background(), request)
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Category != CategoryUsage {
			t.Errorf("%s: error = %v, want usage", name, err)
		}
	}
}

func TestProbeReportsPartialResultsAndSafeFailures(t *testing.T) {
	t.Parallel()
	ended := &probeUseCase{
		media: probeMedia{result: probeProfiles()},
		open: func(context.Context, stream.RTSPOptions) (stream.LiveSource, error) {
			return syntheticLive(t, 12), nil
		},
		now: time.Now,
	}
	result, err := ended.Probe(context.Background(), ProbeRequest{Host: "cam", Duration: 5 * time.Second})
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Category != CategoryProtocol || result.Complete || result.Frames != 12 {
		t.Fatalf("early end: result = %+v, error = %v", result, err)
	}

	rejected := &probeUseCase{
		media: probeMedia{result: probeProfiles()},
		open: func(context.Context, stream.RTSPOptions) (stream.LiveSource, error) {
			return nil, stream.ErrRTSPAuthentication
		},
		now: time.Now,
	}
	if _, err := rejected.Probe(context.Background(), ProbeRequest{Host: "cam", Duration: time.Second}); !errors.As(err, &appErr) || appErr.Category != CategoryAuthentication {
		t.Fatalf("auth failure error = %v", err)
	}

	empty := &probeUseCase{media: probeMedia{result: MediaProfilesResult{}}, now: time.Now}
	if _, err := empty.Probe(context.Background(), ProbeRequest{Host: "cam", Duration: time.Second}); !errors.As(err, &appErr) || appErr.Category != CategoryProtocol {
		t.Fatalf("no stream error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancelled := &probeUseCase{
		media: probeMedia{result: probeProfiles()},
		open: func(context.Context, stream.RTSPOptions) (stream.LiveSource, error) {
			time.AfterFunc(300*time.Millisecond, cancel)
			return syntheticLive(t, 0), nil
		},
		now: time.Now,
	}
	result, err = cancelled.Probe(ctx, ProbeRequest{Host: "cam", Duration: 5 * time.Second})
	if !errors.As(err, &appErr) || appErr.Category != CategoryCancelled || result.Frames == 0 {
		t.Fatalf("cancelled: result = %+v, error = %v", result, err)
	}
}
