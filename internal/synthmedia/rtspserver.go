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
)

// RTSPServerConfig configures a loopback RTSP server for the synthetic stream.
type RTSPServerConfig struct {
	Video H264Config
	// Username and Password, when set, are required from every reader.
	Username string
	Password string
	// SDPParameterSets advertises the SPS and PPS in the session description.
	// Without them a client must wait for the in-band parameter sets.
	SDPParameterSets bool
}

// RTSPServer serves the synthetic H.264 stream on any path over RTSP/TCP. It
// binds only to 127.0.0.1 and exists for tests and the desktop demo.
type RTSPServer struct {
	server *gortsplib.Server
	stream *gortsplib.ServerStream
	config RTSPServerConfig
	port   int
	cancel context.CancelFunc
	done   chan struct{}
	once   sync.Once
}

// StartRTSPServer starts the server and its real-time frame pump.
func StartRTSPServer(config RTSPServerConfig) (*RTSPServer, error) {
	source, err := NewH264Source(config.Video)
	if err != nil {
		return nil, err
	}
	videoFormat := &format.H264{PayloadTyp: 96, PacketizationMode: 1}
	if config.SDPParameterSets {
		videoFormat.SPS = source.SPS()
		videoFormat.PPS = source.PPS()
	}
	handler := &rtspHandler{config: config}
	server := &gortsplib.Server{
		Handler:     handler,
		RTSPAddress: "127.0.0.1:0",
		ReadTimeout: 10 * time.Second,
	}
	if err := server.Start(); err != nil {
		return nil, err
	}
	stream := &gortsplib.ServerStream{
		Server: server,
		Desc: &description.Session{Medias: []*description.Media{{
			Type:    description.MediaTypeVideo,
			Formats: []format.Format{videoFormat},
		}}},
	}
	if err := stream.Initialize(); err != nil {
		server.Close()
		return nil, err
	}
	handler.stream = stream
	address, ok := server.NetListener().Addr().(*net.TCPAddr)
	if !ok {
		stream.Close()
		server.Close()
		return nil, errors.New("synthmedia: unexpected RTSP listener address")
	}
	ctx, cancel := context.WithCancel(context.Background())
	instance := &RTSPServer{
		server: server,
		stream: stream,
		config: config,
		port:   address.Port,
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go instance.pump(ctx, source, videoFormat)
	return instance, nil
}

// Port is the bound TCP port.
func (s *RTSPServer) Port() int { return s.port }

// URL returns the stream URL for path, which must begin with "/".
func (s *RTSPServer) URL(path string) string {
	return "rtsp://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(s.port)) + path
}

// Close stops the pump and the server. It is idempotent.
func (s *RTSPServer) Close() {
	s.once.Do(func() {
		s.cancel()
		<-s.done
		s.stream.Close()
		s.server.Close()
	})
}

func (s *RTSPServer) pump(ctx context.Context, source *H264Source, videoFormat *format.H264) {
	defer close(s.done)
	encoder, err := videoFormat.CreateEncoder()
	if err != nil {
		return
	}
	paced := NewPacedH264(source)
	media := s.stream.Desc.Medias[0]
	for {
		au, pts, err := paced.ReadAccessUnit(ctx)
		if err != nil {
			return
		}
		packets, err := encoder.Encode(au)
		if err != nil {
			return
		}
		ticks := int64(pts/time.Second)*90000 + int64(pts%time.Second)*90000/int64(time.Second)
		timestamp := uint32(ticks) //nolint:gosec // RTP timestamps wrap by design
		for _, packet := range packets {
			packet.Timestamp = timestamp
			_ = s.stream.WritePacketRTP(media, packet)
		}
	}
}

type rtspHandler struct {
	config RTSPServerConfig
	stream *gortsplib.ServerStream
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
	return &base.Response{StatusCode: base.StatusOK}, h.stream, nil
}

// OnSetup implements gortsplib.ServerHandlerOnSetup.
func (h *rtspHandler) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	if !h.authorized(ctx.Conn, ctx.Request) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	return &base.Response{StatusCode: base.StatusOK}, h.stream, nil
}

// OnPlay implements gortsplib.ServerHandlerOnPlay.
func (h *rtspHandler) OnPlay(_ *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	return &base.Response{StatusCode: base.StatusOK}, nil
}
