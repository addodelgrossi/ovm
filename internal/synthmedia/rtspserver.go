package synthmedia

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v5"
	"github.com/bluenviron/gortsplib/v5/pkg/base"
	"github.com/bluenviron/gortsplib/v5/pkg/description"
	"github.com/bluenviron/gortsplib/v5/pkg/format"
	"github.com/bluenviron/gortsplib/v5/pkg/liberrors"
	"github.com/pion/rtp"
)

// RTSPServerConfig configures a loopback RTSP server for the synthetic stream.
type RTSPServerConfig struct {
	// Codec selects CodecH264 (the default, generated from Video) or CodecH265
	// (the embedded fixture; Video is ignored).
	Codec string
	Video H264Config
	// Route, when set, chooses the codec for each request from its path and
	// query; both codecs are then served. It must return CodecH264 or
	// CodecH265.
	Route func(path, query string) string
	// Username and Password, when set, are required from every reader.
	Username string
	Password string
	// SDPParameterSets advertises the parameter sets in the session description.
	// Without them a client must wait for the in-band parameter sets.
	SDPParameterSets bool
	// EnableUDP also serves RTP/RTCP over UDP on a free loopback port pair.
	EnableUDP bool
}

// RTSPServer serves synthetic H.264 and H.265 streams on any path over
// RTSP/TCP. It binds only to 127.0.0.1 and exists for tests and the desktop
// demo.
type RTSPServer struct {
	server  *gortsplib.Server
	streams map[string]*gortsplib.ServerStream
	port    int
	cancel  context.CancelFunc
	pumps   sync.WaitGroup
	once    sync.Once
}

type codecStream struct {
	generator FrameGenerator
	format    format.Format
	encoder   rtpEncoder
}

func newCodecStream(codec string, video H264Config, sdpParameterSets bool) (*codecStream, error) {
	switch codec {
	case CodecH264:
		source, err := NewH264Source(video)
		if err != nil {
			return nil, err
		}
		h264Format := &format.H264{PayloadTyp: 96, PacketizationMode: 1}
		if sdpParameterSets {
			h264Format.SPS, h264Format.PPS = source.SPS(), source.PPS()
		}
		encoder, err := h264Format.CreateEncoder()
		if err != nil {
			return nil, err
		}
		return &codecStream{generator: source, format: h264Format, encoder: encoder}, nil
	case CodecH265:
		source, err := NewH265Source()
		if err != nil {
			return nil, err
		}
		h265Format := &format.H265{PayloadTyp: 96}
		if sdpParameterSets {
			sets := source.ParameterSets()
			h265Format.VPS, h265Format.SPS, h265Format.PPS = sets[0], sets[1], sets[2]
		}
		encoder, err := h265Format.CreateEncoder()
		if err != nil {
			return nil, err
		}
		return &codecStream{generator: source, format: h265Format, encoder: encoder}, nil
	default:
		return nil, errors.New("synthmedia: unsupported RTSP server codec")
	}
}

// StartRTSPServer starts the server and its real-time frame pumps.
func StartRTSPServer(config RTSPServerConfig) (*RTSPServer, error) {
	if config.Codec == "" {
		config.Codec = CodecH264
	}
	codecList := []string{config.Codec}
	if config.Route != nil {
		codecList = []string{CodecH264, CodecH265}
	}
	sources := make(map[string]*codecStream, len(codecList))
	for _, codec := range codecList {
		source, err := newCodecStream(codec, config.Video, config.SDPParameterSets)
		if err != nil {
			return nil, err
		}
		sources[codec] = source
	}

	handler := &rtspHandler{config: config}
	server, err := startServer(handler, config.EnableUDP)
	if err != nil {
		return nil, err
	}
	instance := &RTSPServer{server: server, streams: make(map[string]*gortsplib.ServerStream, len(sources))}
	for codec, source := range sources {
		stream := &gortsplib.ServerStream{
			Server: server,
			Desc: &description.Session{Medias: []*description.Media{{
				Type:    description.MediaTypeVideo,
				Formats: []format.Format{source.format},
			}}},
		}
		if err := stream.Initialize(); err != nil {
			instance.closeStreams()
			server.Close()
			return nil, err
		}
		instance.streams[codec] = stream
	}
	handler.streams = instance.streams
	address, ok := server.NetListener().Addr().(*net.TCPAddr)
	if !ok {
		instance.closeStreams()
		server.Close()
		return nil, errors.New("synthmedia: unexpected RTSP listener address")
	}
	instance.port = address.Port
	ctx, cancel := context.WithCancel(context.Background())
	instance.cancel = cancel
	for codec, source := range sources {
		instance.pumps.Add(1)
		go instance.pump(ctx, instance.streams[codec], source)
	}
	return instance, nil
}

// startServer binds RTSP on an ephemeral loopback port. With UDP, gortsplib
// needs an explicit even RTP port and the next port for RTCP, so a free pair
// is chosen and the bind retried if another process takes it first.
func startServer(handler *rtspHandler, udp bool) (*gortsplib.Server, error) {
	attempts := 1
	if udp {
		attempts = 20
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		server := &gortsplib.Server{
			Handler:     handler,
			RTSPAddress: "127.0.0.1:0",
			ReadTimeout: 10 * time.Second,
		}
		if udp {
			port, err := freeUDPPort()
			if err != nil {
				return nil, err
			}
			rtp := port &^ 1
			server.UDPRTPAddress = net.JoinHostPort("127.0.0.1", strconv.Itoa(rtp))
			server.UDPRTCPAddress = net.JoinHostPort("127.0.0.1", strconv.Itoa(rtp+1))
		}
		if lastErr = server.Start(); lastErr == nil {
			return server, nil
		}
	}
	return nil, lastErr
}

func freeUDPPort() (int, error) {
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = conn.Close() }()
	address, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		return 0, errors.New("synthmedia: unexpected UDP address")
	}
	return address.Port, nil
}

// Port is the bound TCP port.
func (s *RTSPServer) Port() int { return s.port }

// URL returns the stream URL for path, which must begin with "/".
func (s *RTSPServer) URL(path string) string {
	return "rtsp://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(s.port)) + path
}

// Close stops the pumps and the server. It is idempotent.
func (s *RTSPServer) Close() {
	s.once.Do(func() {
		s.cancel()
		s.pumps.Wait()
		s.closeStreams()
		s.server.Close()
	})
}

func (s *RTSPServer) closeStreams() {
	for _, stream := range s.streams {
		stream.Close()
	}
}

// rtpEncoder turns access units into RTP packets.
type rtpEncoder interface {
	Encode(au [][]byte) ([]*rtp.Packet, error)
}

func (s *RTSPServer) pump(ctx context.Context, stream *gortsplib.ServerStream, source *codecStream) {
	defer s.pumps.Done()
	paced := NewPaced(source.generator)
	media := stream.Desc.Medias[0]
	for {
		au, pts, err := paced.ReadAccessUnit(ctx)
		if err != nil {
			return
		}
		packets, err := source.encoder.Encode(au)
		if err != nil {
			return
		}
		ticks := int64(pts/time.Second)*90000 + int64(pts%time.Second)*90000/int64(time.Second)
		timestamp := uint32(ticks) //nolint:gosec // RTP timestamps wrap by design
		for _, packet := range packets {
			packet.Timestamp = timestamp
			_ = stream.WritePacketRTP(media, packet)
		}
	}
}

type rtspHandler struct {
	config  RTSPServerConfig
	streams map[string]*gortsplib.ServerStream
}

func (h *rtspHandler) route(path, query string) *gortsplib.ServerStream {
	if h.config.Route != nil {
		if stream := h.streams[h.config.Route(path, query)]; stream != nil {
			return stream
		}
		return h.streams[CodecH264]
	}
	return h.streams[h.config.Codec]
}

func (h *rtspHandler) authorized(conn *gortsplib.ServerConn, request *base.Request) bool {
	if h.config.Username == "" {
		return true
	}
	return conn.VerifyCredentials(request, h.config.Username, h.config.Password)
}

// OnDescribe implements gortsplib.ServerHandlerOnDescribe.
func (h *rtspHandler) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	if !h.authorized(ctx.Conn, ctx.Request) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	return &base.Response{StatusCode: base.StatusOK}, h.route(ctx.Path, ctx.Query), nil
}

// OnSetup implements gortsplib.ServerHandlerOnSetup.
func (h *rtspHandler) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	if !h.authorized(ctx.Conn, ctx.Request) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	return &base.Response{StatusCode: base.StatusOK}, h.route(ctx.Path, ctx.Query), nil
}

// OnPlay implements gortsplib.ServerHandlerOnPlay.
func (h *rtspHandler) OnPlay(_ *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	return &base.Response{StatusCode: base.StatusOK}, nil
}
