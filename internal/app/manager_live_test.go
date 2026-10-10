package app

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/pkg/stream"
)

type fakeLiveSource struct{ closed bool }

func (f *fakeLiveSource) Codec() string           { return stream.CodecH264 }
func (f *fakeLiveSource) ParameterSets() [][]byte { return nil }
func (f *fakeLiveSource) ReadAccessUnit(ctx context.Context) ([][]byte, time.Duration, error) {
	<-ctx.Done()
	return nil, 0, ctx.Err()
}
func (f *fakeLiveSource) Close() { f.closed = true }

func TestOpenLiveStreamUsesResolvedURIAndStoredCredentials(t *testing.T) {
	h := newHarness(t)
	device := seedAuthenticatedStreamDevice(t, h, "192.168.44.20")
	h.media.result = MediaProfilesResult{Profiles: []MediaProfileDTO{
		supportedStreamProfile("profile_main",
			"rtsp://192.168.44.20/cam/realmonitor?channel=1&subtype=0",
			"rtsp://192.168.44.20/cam/realmonitor?channel=1&subtype=0&udp=1"),
	}}
	var received []stream.RTSPOptions
	h.manager.liveStreams = func(_ context.Context, options stream.RTSPOptions) (stream.LiveSource, error) {
		received = append(received, options)
		return &fakeLiveSource{}, nil
	}

	for _, transport := range []StreamTransport{StreamTransportTCP, StreamTransportUDP} {
		source, err := h.manager.OpenLiveStream(context.Background(), StreamSelection{
			DeviceID: device.ID, ProfileToken: "profile_main", Transport: transport,
		})
		if err != nil || source == nil {
			t.Fatalf("OpenLiveStream(%s) error = %v", transport, err)
		}
	}
	if len(received) != 2 {
		t.Fatalf("opener called %d times", len(received))
	}
	tcp, udp := received[0], received[1]
	if tcp.URL != "rtsp://192.168.44.20/cam/realmonitor?channel=1&subtype=0" || tcp.Transport != stream.TransportTCP {
		t.Fatalf("TCP options = %+v", tcp)
	}
	if !strings.HasSuffix(udp.URL, "&udp=1") || udp.Transport != stream.TransportUDP {
		t.Fatalf("UDP options = %+v", udp)
	}
	if tcp.Username != "operator" || tcp.Password != "synthetic-password" {
		t.Fatal("stored credentials were not supplied to the stream opener")
	}
}

func TestOpenLiveStreamMapsFailuresToSafeErrors(t *testing.T) {
	h := newHarness(t)
	device := seedAuthenticatedStreamDevice(t, h, "192.168.44.21")
	h.media.result = MediaProfilesResult{Profiles: []MediaProfileDTO{
		supportedStreamProfile("profile_main", "rtsp://192.168.44.21/main", ""),
	}}
	selection := StreamSelection{DeviceID: device.ID, ProfileToken: "profile_main", Transport: StreamTransportTCP}

	if _, err := h.manager.OpenLiveStream(context.Background(), selection); err == nil {
		t.Fatal("OpenLiveStream() without an opener succeeded")
	}
	cases := map[error]ErrorCategory{
		stream.ErrRTSPAuthentication:   CategoryAuthentication,
		stream.ErrRTSPNetwork:          CategoryNetwork,
		stream.ErrRTSPUnsupportedCodec: CategoryProtocol,
		stream.ErrRTSPNotFound:         CategoryProtocol,
		context.Canceled:               CategoryCancelled,
	}
	for cause, want := range cases {
		h.manager.liveStreams = func(context.Context, stream.RTSPOptions) (stream.LiveSource, error) {
			return nil, cause
		}
		_, err := h.manager.OpenLiveStream(context.Background(), selection)
		var managed *ManagerError
		if !errors.As(err, &managed) || managed.Category != want {
			t.Errorf("%v: error = %v, want category %s", cause, err, want)
			continue
		}
		if strings.Contains(managed.Message, "synthetic-password") || strings.Contains(managed.Message, "192.168.44.21") {
			t.Errorf("%v: message leaked a secret or URL: %q", cause, managed.Message)
		}
	}
}

type fakeSnapshotFeeds struct {
	request SnapshotFeedRequest
	err     error
}

func (f *fakeSnapshotFeeds) OpenSnapshotFeed(_ context.Context, request SnapshotFeedRequest) (SnapshotFeed, error) {
	f.request = request
	if f.err != nil {
		return nil, f.err
	}
	return nil, nil
}

func TestOpenSnapshotFeedUsesDeviceEndpointAndStoredCredentials(t *testing.T) {
	h := newHarness(t)
	device := seedAuthenticatedStreamDevice(t, h, "192.168.44.30")
	if _, err := h.manager.OpenSnapshotFeed(context.Background(), StreamSelection{DeviceID: device.ID, ProfileToken: "profile_main"}); err == nil {
		t.Fatal("OpenSnapshotFeed() without a use case succeeded")
	}
	feeds := &fakeSnapshotFeeds{}
	h.manager.snapshotFeeds = feeds
	if _, err := h.manager.OpenSnapshotFeed(context.Background(), StreamSelection{DeviceID: device.ID, ProfileToken: "profile_main"}); err != nil {
		t.Fatalf("OpenSnapshotFeed() error = %v", err)
	}
	if feeds.request.ProfileToken != "profile_main" || !strings.Contains(feeds.request.Host, "192.168.44.30") {
		t.Fatalf("request = %+v", feeds.request)
	}
	if feeds.request.Credentials.Username() != "operator" {
		t.Fatal("stored credentials were not supplied")
	}
	if _, err := h.manager.OpenSnapshotFeed(context.Background(), StreamSelection{DeviceID: device.ID, ProfileToken: ""}); err == nil {
		t.Fatal("OpenSnapshotFeed() accepted an empty profile token")
	}
	feeds.err = newError(CategoryAuthentication, "authentication failed", nil)
	_, err := h.manager.OpenSnapshotFeed(context.Background(), StreamSelection{DeviceID: device.ID, ProfileToken: "profile_main"})
	var managed *ManagerError
	if !errors.As(err, &managed) || managed.Category != CategoryAuthentication {
		t.Fatalf("error = %v, want authentication", err)
	}
}
