package stream

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/synthmedia"
)

func startRTSPServer(t *testing.T, config synthmedia.RTSPServerConfig) *synthmedia.RTSPServer {
	t.Helper()
	if config.Video.Width == 0 {
		config.Video = synthmedia.H264Config{Width: 64, Height: 48, FPS: 25, GOP: 10}
	}
	server, err := synthmedia.StartRTSPServer(config)
	if err != nil {
		t.Fatalf("StartRTSPServer() error = %v", err)
	}
	t.Cleanup(server.Close)
	return server
}

func TestRTSPSourceAuthenticatesAndFeedsTheMuxer(t *testing.T) {
	t.Parallel()
	server := startRTSPServer(t, synthmedia.RTSPServerConfig{
		Username:         "viewer",
		Password:         "synthetic-secret",
		SDPParameterSets: true,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	source, err := OpenRTSPH264(ctx, RTSPOptions{
		URL:      server.URL("/cam/realmonitor?channel=1&subtype=0"),
		Username: "viewer",
		Password: "synthetic-secret",
	})
	if err != nil {
		t.Fatalf("OpenRTSPH264() error = %v", err)
	}
	defer source.Close()
	if len(source.SPS()) == 0 || len(source.PPS()) == 0 {
		t.Fatal("parameter sets from the session description are missing")
	}

	sink := &recordingSink{}
	pumpCtx, stopPump := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- PumpH264FMP4(pumpCtx, source, sink) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		sink.mu.Lock()
		fragments, keys := sink.fragments, sink.keys
		sink.mu.Unlock()
		if fragments >= 15 && keys >= 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("received %d fragments (%d keys) before the deadline", fragments, keys)
		}
		time.Sleep(20 * time.Millisecond)
	}
	stopPump()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("PumpH264FMP4() error = %v", err)
	}
	if len(sink.codecs) != 1 || sink.codecs[0] != "avc1.42c01f" {
		t.Fatalf("codecs = %v", sink.codecs)
	}
}

func TestRTSPSourceStartsFromInBandParameterSets(t *testing.T) {
	t.Parallel()
	server := startRTSPServer(t, synthmedia.RTSPServerConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	source, err := OpenRTSPH264(ctx, RTSPOptions{URL: server.URL("/stream")})
	if err != nil {
		t.Fatalf("OpenRTSPH264() error = %v", err)
	}
	defer source.Close()
	sink := &recordingSink{}
	pumpCtx, stopPump := context.WithTimeout(ctx, 2*time.Second)
	defer stopPump()
	_ = PumpH264FMP4(pumpCtx, source, sink)
	if len(sink.codecs) != 1 || sink.fragments == 0 {
		t.Fatalf("pump did not start from in-band parameter sets: codecs=%v fragments=%d", sink.codecs, sink.fragments)
	}
}

func TestRTSPSourceRejectsWrongCredentialsWithoutLeakingThem(t *testing.T) {
	t.Parallel()
	server := startRTSPServer(t, synthmedia.RTSPServerConfig{Username: "viewer", Password: "synthetic-secret"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := OpenRTSPH264(ctx, RTSPOptions{URL: server.URL("/stream"), Username: "viewer", Password: "wrong-marker"})
	if !errors.Is(err, ErrRTSPAuthentication) {
		t.Fatalf("OpenRTSPH264() error = %v, want ErrRTSPAuthentication", err)
	}
	if strings.Contains(err.Error(), "wrong-marker") || strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("error leaked a secret or URL: %v", err)
	}
}

func TestRTSPSourceRejectsUnsafeURLs(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "http://camera/stream", "rtsp://user:pass@camera/stream", "rtsp://camera/stream#x", "rtsp:///stream", "rtsp://camera/\nstream"} {
		if _, err := OpenRTSPH264(context.Background(), RTSPOptions{URL: raw}); !errors.Is(err, ErrRTSPInvalidURL) {
			t.Errorf("OpenRTSPH264(%q) error = %v, want ErrRTSPInvalidURL", raw, err)
		}
	}
}

func TestRTSPSourceSetupIsCancellable(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer func() { _ = listener.Close() }()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			// Accept and stay silent so DESCRIBE never completes.
			defer func() { _ = conn.Close() }()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = OpenRTSPH264(ctx, RTSPOptions{URL: "rtsp://" + listener.Addr().String() + "/stream", Timeout: 30 * time.Second})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("OpenRTSPH264() error = %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Fatalf("cancellation took %v", elapsed)
	}
}

func TestTicksToDuration(t *testing.T) {
	t.Parallel()
	if got := ticksToDuration(90000*3600 + 45000); got != time.Hour+500*time.Millisecond {
		t.Fatalf("ticksToDuration() = %v", got)
	}
}
