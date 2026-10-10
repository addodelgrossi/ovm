package stream

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/format/rtph264"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
	"github.com/bluenviron/mediacommon/v2/pkg/codecs/h264"
	"github.com/pion/rtp"
)

// Transport selects how RTP is carried.
type Transport string

const (
	// TransportTCP interleaves RTP in the RTSP TCP connection (the default).
	TransportTCP Transport = "tcp"
	// TransportUDP carries RTP over UDP.
	TransportUDP Transport = "udp"
)

const (
	defaultRTSPTimeout = 10 * time.Second
	// accessUnitQueue bounds decoded access units waiting for the consumer.
	// When it overflows the source drops until the next keyframe rather than
	// blocking the RTSP reader.
	accessUnitQueue = 128
)

// Typed RTSP failures. Their messages never contain the stream URI or
// credentials, so callers may surface them directly.
var (
	ErrRTSPInvalidURL       = errors.New("stream: the RTSP URL is not valid")
	ErrRTSPAuthentication   = errors.New("stream: the camera rejected the RTSP credentials")
	ErrRTSPNotFound         = errors.New("stream: the camera does not serve this RTSP stream")
	ErrRTSPUnsupportedCodec = errors.New("stream: the RTSP stream has no supported video track")
	ErrRTSPProtocol         = errors.New("stream: the RTSP session failed")
	ErrRTSPNetwork          = errors.New("stream: the RTSP connection failed")
	ErrRTSPClosed           = errors.New("stream: the RTSP session ended")
)

// RTSPOptions configures an RTSP play session.
type RTSPOptions struct {
	// URL is an rtsp:// or rtsps:// URL without userinfo. Its path and query
	// are used exactly as given.
	URL      string
	Username string
	Password string
	// Transport defaults to TCP.
	Transport Transport
	// Timeout bounds connection setup and each read; it defaults to 10s.
	Timeout time.Duration
}

type accessUnit struct {
	nalus [][]byte
	pts   time.Duration
}

// RTSPH264Source plays the first H.264 track of an RTSP stream and implements
// H264Source. Close must be called to release the connection.
type RTSPH264Source struct {
	client *gortsplib.Client
	sps    []byte
	pps    []byte
	units  chan accessUnit
	done   chan struct{}

	lost *lossCounter

	mu        sync.Mutex
	err       error
	dropping  bool
	closeOnce sync.Once
}

// lossCounter is registered before the client starts so the callback never
// races with setup.
type lossCounter struct {
	mu   sync.Mutex
	lost uint64
}

func (l *lossCounter) add(lost uint64) {
	l.mu.Lock()
	l.lost += lost
	l.mu.Unlock()
}

// OpenRTSPH264 connects, describes, sets up the first H.264 video track, and
// starts playing. Cancelling ctx aborts setup; after it returns, the session
// lives until Close or a transport failure.
func OpenRTSPH264(ctx context.Context, options RTSPOptions) (*RTSPH264Source, error) {
	target, err := parseRTSPURL(options)
	if err != nil {
		return nil, err
	}
	timeout := options.Timeout
	if timeout <= 0 {
		timeout = defaultRTSPTimeout
	}
	protocol := gortsplib.ProtocolTCP
	if options.Transport == TransportUDP {
		protocol = gortsplib.ProtocolUDP
	}
	dialer := &net.Dialer{Timeout: timeout}
	client := &gortsplib.Client{
		Scheme:       target.Scheme,
		Host:         target.Host,
		Protocol:     &protocol,
		ReadTimeout:  timeout,
		WriteTimeout: timeout,
		DialContext:  dialer.DialContext,
		UserAgent:    "ovm",
	}
	loss := &lossCounter{}
	client.OnPacketsLost = loss.add
	if err := client.Start(); err != nil {
		return nil, ErrRTSPNetwork
	}

	// Setup calls block without a context; closing the client unblocks them.
	setupDone := make(chan struct{})
	defer close(setupDone)
	go func() {
		select {
		case <-ctx.Done():
			client.Close()
		case <-setupDone:
		}
	}()

	source, err := setupH264(client, target, loss)
	if err != nil {
		client.Close()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}
	return source, nil
}

func setupH264(client *gortsplib.Client, target *base.URL, loss *lossCounter) (*RTSPH264Source, error) {
	session, _, err := client.Describe(target)
	if err != nil {
		return nil, classifyRTSPError(err)
	}
	var h264Format *format.H264
	media := session.FindFormat(&h264Format)
	if media == nil {
		return nil, ErrRTSPUnsupportedCodec
	}
	decoder, err := h264Format.CreateDecoder()
	if err != nil {
		return nil, ErrRTSPProtocol
	}
	if _, err := client.Setup(session.BaseURL, media, 0, 0); err != nil {
		return nil, classifyRTSPError(err)
	}
	source := &RTSPH264Source{
		client: client,
		sps:    bytes.Clone(h264Format.SPS),
		pps:    bytes.Clone(h264Format.PPS),
		units:  make(chan accessUnit, accessUnitQueue),
		done:   make(chan struct{}),
		lost:   loss,
	}
	client.OnPacketRTP(media, h264Format, func(packet *rtp.Packet) {
		source.handlePacket(media, decoder, packet)
	})
	if _, err := client.Play(nil); err != nil {
		return nil, classifyRTSPError(err)
	}
	go func() {
		err := client.Wait()
		source.fail(classifyRTSPError(err))
	}()
	return source, nil
}

func (s *RTSPH264Source) handlePacket(media *description.Media, decoder *rtph264.Decoder, packet *rtp.Packet) {
	ticks, ok := s.client.PacketPTS(media, packet)
	if !ok {
		return
	}
	nalus, err := decoder.Decode(packet)
	if err != nil {
		// Incomplete or lost fragments; the next access unit resynchronizes.
		return
	}
	unit := accessUnit{nalus: nalus, pts: ticksToDuration(ticks)}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.dropping {
		if !h264.IsRandomAccess(nalus) {
			return
		}
		s.dropping = false
	}
	select {
	case s.units <- unit:
	default:
		s.dropping = true
	}
}

// SPS returns the sequence parameter set from the session description, if any.
func (s *RTSPH264Source) SPS() []byte { return s.sps }

// PPS returns the picture parameter set from the session description, if any.
func (s *RTSPH264Source) PPS() []byte { return s.pps }

// ReadAccessUnit returns the next decoded access unit.
func (s *RTSPH264Source) ReadAccessUnit(ctx context.Context) ([][]byte, time.Duration, error) {
	select {
	case unit := <-s.units:
		return unit.nalus, unit.pts, nil
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	case <-s.done:
		s.mu.Lock()
		defer s.mu.Unlock()
		return nil, 0, s.err
	}
}

// LostPackets reports RTP packets the client detected as lost.
func (s *RTSPH264Source) LostPackets() uint64 {
	s.lost.mu.Lock()
	defer s.lost.mu.Unlock()
	return s.lost.lost
}

// Close ends the session. It is idempotent.
func (s *RTSPH264Source) Close() {
	s.client.Close()
	s.fail(ErrRTSPClosed)
}

func (s *RTSPH264Source) fail(err error) {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.err = err
		s.mu.Unlock()
		close(s.done)
	})
}

func parseRTSPURL(options RTSPOptions) (*base.URL, error) {
	raw := options.URL
	if raw == "" || strings.ContainsAny(raw, "\x00\r\n") {
		return nil, ErrRTSPInvalidURL
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Fragment != "" {
		return nil, ErrRTSPInvalidURL
	}
	if parsed.Scheme != "rtsp" && parsed.Scheme != "rtsps" {
		return nil, ErrRTSPInvalidURL
	}
	if options.Username != "" {
		parsed.User = url.UserPassword(options.Username, options.Password)
	}
	target, err := base.ParseURL(parsed.String())
	if err != nil {
		return nil, ErrRTSPInvalidURL
	}
	return target, nil
}

// classifyRTSPError maps library errors, which can embed URLs, onto the typed
// set above.
func classifyRTSPError(err error) error {
	if err == nil {
		return ErrRTSPClosed
	}
	var status liberrors.ErrClientBadStatusCode
	if errors.As(err, &status) {
		switch status.Code {
		case base.StatusUnauthorized, base.StatusForbidden:
			return ErrRTSPAuthentication
		case base.StatusNotFound:
			return ErrRTSPNotFound
		default:
			return ErrRTSPProtocol
		}
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return ErrRTSPNetwork
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return ErrRTSPNetwork
	}
	if errors.As(err, new(liberrors.ErrClientTerminated)) {
		return ErrRTSPClosed
	}
	return ErrRTSPProtocol
}

func ticksToDuration(ticks int64) time.Duration {
	seconds := ticks / videoTimeScale
	remainder := ticks % videoTimeScale
	return time.Duration(seconds)*time.Second + time.Duration(remainder)*time.Second/videoTimeScale
}
