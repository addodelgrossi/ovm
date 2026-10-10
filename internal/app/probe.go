package app

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/addodelgrossi/ovm/pkg/stream"
)

// Probe duration bounds.
const (
	DefaultProbeDuration = 30 * time.Second
	MaxProbeDuration     = 10 * time.Minute
)

// ProbeRequest parameterizes a timed stream measurement.
type ProbeRequest struct {
	// Host is the camera endpoint, the same-origin anchor.
	Host string
	// Credentials authenticate ONVIF and RTSP ephemerally.
	Credentials *Credentials
	// ProfileToken selects the profile; empty selects the lexicographically
	// smallest token whose stream is supported for the transport.
	ProfileToken string
	// Transport is StreamTransportTCP (default) or StreamTransportUDP.
	Transport StreamTransport
	// Duration is the measurement window (1s to MaxProbeDuration).
	Duration time.Duration
	// Timeout bounds profile resolution and connection setup.
	Timeout time.Duration
}

// ProbeResult is a credential-free measurement. Bitrate counts encoded video
// payload only. Complete is false when the probe ended before Duration.
type ProbeResult struct {
	ProfileToken           string  `json:"profile_token"`
	Transport              string  `json:"transport"`
	Codec                  string  `json:"codec"`
	Width                  int     `json:"width"`
	Height                 int     `json:"height"`
	FramesPerSecond        float64 `json:"frames_per_second"`
	BitrateKbps            float64 `json:"bitrate_kbps"`
	GOPFrames              int     `json:"gop_frames"`
	GOPSeconds             float64 `json:"gop_seconds"`
	TimeToFirstFrameMillis int64   `json:"time_to_first_frame_ms"`
	Frames                 uint64  `json:"frames"`
	Keyframes              uint64  `json:"keyframes"`
	LostPackets            uint64  `json:"lost_packets"`
	DurationMillis         int64   `json:"duration_ms"`
	Complete               bool    `json:"complete"`
}

// ProbeUseCase measures one camera stream for a fixed duration.
type ProbeUseCase interface {
	Probe(ctx context.Context, request ProbeRequest) (ProbeResult, error)
}

type probeUseCase struct {
	media MediaProfilesUseCase
	open  LiveStreamOpener
	now   func() time.Time
}

// NewProductionProbe composes the production profile resolution and RTSP
// client.
func NewProductionProbe() ProbeUseCase {
	return &probeUseCase{media: NewProductionMediaProfiles(), open: NewProductionLiveStreams(), now: time.Now}
}

// Probe resolves the profile's stream, connects, and measures it. Time to
// first frame includes connecting. An interrupted probe returns the partial
// measurement together with its error.
func (p *probeUseCase) Probe(ctx context.Context, request ProbeRequest) (ProbeResult, error) {
	if request.ProfileToken != "" {
		if err := ValidateSnapshotProfileToken(request.ProfileToken); err != nil {
			return ProbeResult{}, err
		}
	}
	transport := request.Transport
	if transport == "" {
		transport = StreamTransportTCP
	}
	if transport != StreamTransportTCP && transport != StreamTransportUDP {
		return ProbeResult{}, usageError("the stream transport is not supported")
	}
	if request.Duration < time.Second || request.Duration > MaxProbeDuration {
		return ProbeResult{}, usageError("the probe duration must be between 1s and 10m")
	}

	profiles, err := p.media.Profiles(ctx, MediaProfilesRequest{Host: request.Host, Credentials: request.Credentials, Timeout: request.Timeout})
	if err != nil {
		return ProbeResult{}, err
	}
	profile, err := probeProfile(profiles, request.ProfileToken, transport)
	if err != nil {
		return ProbeResult{}, err
	}
	uri := profile.Stream.TCPURI
	options := stream.RTSPOptions{URL: uri, Transport: stream.TransportTCP, Timeout: request.Timeout}
	if transport == StreamTransportUDP {
		options.URL, options.Transport = profile.Stream.UDPURI, stream.TransportUDP
	}
	if request.Credentials != nil {
		options.Username, options.Password = request.Credentials.username, request.Credentials.password
	}

	result := ProbeResult{ProfileToken: profile.Token, Transport: string(transport)}
	meter := stream.NewMeter(p.now)
	started := p.now()
	source, openErr := p.open(ctx, options)
	options.Password = ""
	if openErr != nil {
		return result, probeError(openErr)
	}
	defer source.Close()
	result.Codec = source.Codec()
	metered := stream.NewMeteredSource(source, meter)

	window, cancel := context.WithTimeout(ctx, request.Duration)
	defer cancel()
	var readErr error
	for {
		if _, _, err := metered.ReadAccessUnit(window); err != nil {
			readErr = err
			break
		}
	}
	fillProbeResult(&result, meter.Snapshot(), p.now().Sub(started))

	switch {
	case ctx.Err() != nil:
		return result, mapContextError(ctx.Err())
	case errors.Is(readErr, context.DeadlineExceeded) && window.Err() != nil:
		result.Complete = true
	default:
		return result, probeError(readErr)
	}
	if result.Keyframes == 0 {
		result.Complete = false
		return result, newError(CategoryProtocol, "no decodable video was received during the probe", nil)
	}
	return result, nil
}

func fillProbeResult(result *ProbeResult, stats stream.Stats, elapsed time.Duration) {
	result.Width, result.Height = stats.Width, stats.Height
	result.FramesPerSecond = stats.FramesPerSecond
	result.BitrateKbps = stats.BitrateKbps
	result.GOPFrames, result.GOPSeconds = stats.GOPFrames, stats.GOPSeconds
	result.TimeToFirstFrameMillis = stats.TimeToFirstFrame.Milliseconds()
	result.Frames, result.Keyframes = stats.Frames, stats.Keyframes
	result.LostPackets = stats.LostPackets
	result.DurationMillis = elapsed.Milliseconds()
}

// probeProfile picks the requested profile, or the smallest token whose
// stream is supported for the transport.
func probeProfile(profiles MediaProfilesResult, token string, transport StreamTransport) (MediaProfileDTO, error) {
	operation := MediaOpStreamTCP
	if transport == StreamTransportUDP {
		operation = MediaOpStreamUDP
	}
	usable := func(profile MediaProfileDTO) bool {
		if profile.Stream == nil || !profileOperationSupported(profile, operation) {
			return false
		}
		if transport == StreamTransportUDP {
			return profile.Stream.UDPURI != ""
		}
		return profile.Stream.TCPURI != ""
	}
	candidates := append([]MediaProfileDTO(nil), profiles.Profiles...)
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].Token < candidates[j].Token })
	for _, profile := range candidates {
		if token != "" && profile.Token != token {
			continue
		}
		if usable(profile) {
			return profile, nil
		}
		if token != "" {
			return MediaProfileDTO{}, newError(CategoryProtocol, "the requested stream transport is unavailable", nil)
		}
	}
	if token != "" {
		return MediaProfileDTO{}, usageError("the media profile was not found")
	}
	return MediaProfileDTO{}, newError(CategoryProtocol, "the camera reports no usable stream", nil)
}

// probeError maps stream failures onto safe application errors.
func probeError(err error) error {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return mapContextError(err)
	case errors.Is(err, stream.ErrRTSPAuthentication):
		return newError(CategoryAuthentication, "the camera rejected the stream credentials", nil)
	case errors.Is(err, stream.ErrRTSPNetwork):
		return newError(CategoryNetwork, "the camera stream could not be reached", nil)
	case errors.Is(err, stream.ErrRTSPUnsupportedCodec):
		return newError(CategoryProtocol, "the stream has no supported H.264 or H.265 video track", nil)
	case errors.Is(err, stream.ErrRTSPNotFound):
		return newError(CategoryProtocol, "the camera does not serve the selected stream", nil)
	default:
		return newError(CategoryProtocol, "the camera stream failed", nil)
	}
}
