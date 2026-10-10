package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	core "github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/internal/synthmedia"
	"github.com/addodelgrossi/ovm/pkg/stream"
)

func syntheticPreviewSources(context.Context, Manager, core.StreamSelection) (stream.VideoSource, func(), error) {
	source, err := synthmedia.NewH264Source(synthmedia.H264Config{Width: 64, Height: 48, FPS: 25, GOP: 25})
	if err != nil {
		return nil, nil, err
	}
	return synthmedia.NewPaced(source), nil, nil
}

type closingLiveSource struct {
	stream.VideoSource
	closed chan struct{}
}

func (c *closingLiveSource) Close() { close(c.closed) }

func TestManagerPreviewSourcesOpensThroughManagerAndReleasesWithClose(t *testing.T) {
	t.Parallel()
	inner, _, err := syntheticPreviewSources(context.Background(), nil, core.StreamSelection{})
	if err != nil {
		t.Fatalf("synthetic source error = %v", err)
	}
	live := &closingLiveSource{VideoSource: inner, closed: make(chan struct{})}
	var got core.StreamSelection
	manager := newFakeManager()
	manager.liveFn = func(_ context.Context, selection core.StreamSelection) (stream.LiveSource, error) {
		got = selection
		return live, nil
	}
	selection := core.StreamSelection{DeviceID: "device", ProfileToken: "profile", Transport: core.StreamTransportTCP}
	source, release, err := managerPreviewSources(context.Background(), manager, selection)
	if err != nil || source == nil || release == nil {
		t.Fatalf("managerPreviewSources() source=%v release=%t error=%v", source, release != nil, err)
	}
	if got != selection {
		t.Fatalf("selection = %+v, want %+v", got, selection)
	}
	release()
	select {
	case <-live.closed:
	default:
		t.Fatal("release did not close the live source")
	}
}

func TestPreviewControllerStreamsDemoSourceAndStops(t *testing.T) {
	t.Parallel()
	controller := newPreviewController(syntheticPreviewSources, []string{wailsPageOrigin})
	root, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	selection := core.StreamSelection{DeviceID: "device", ProfileToken: "profile", Transport: core.StreamTransportTCP}

	result, err := controller.start(context.Background(), root, nil, selection)
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if !validPreviewSessionID(result.SessionID) {
		t.Fatalf("session ID %q is not valid", result.SessionID)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, result.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+result.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	defer func() { _ = response.Body.Close() }()
	if got := response.Header.Get("X-OVM-Codec"); got != "avc1.42c01f" {
		t.Fatalf("codec = %q", got)
	}
	header := make([]byte, 8)
	if _, err := io.ReadFull(response.Body, header); err != nil {
		t.Fatalf("ReadFull() error = %v", err)
	}
	if string(header[4:8]) != "ftyp" {
		t.Fatalf("stream does not start with an init segment: % x", header)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		stats, ok := controller.stats(result.SessionID)
		if !ok {
			t.Fatal("stats() did not find the active preview")
		}
		if stats.Frames >= 10 && stats.FramesPerSecond > 0 {
			if stats.Width != 64 || stats.Height != 48 || stats.TimeToFirstFrame <= 0 {
				t.Fatalf("unexpected stats %+v", stats)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stats did not advance: %+v", stats)
		}
		time.Sleep(20 * time.Millisecond)
	}

	if !controller.stop(result.SessionID) {
		t.Fatal("stop() did not find the active preview")
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatalf("body did not end cleanly: %v", err)
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	if err := controller.close(closeCtx); err != nil {
		t.Fatalf("close() error = %v", err)
	}
	if _, err := controller.start(context.Background(), root, nil, selection); err == nil {
		t.Fatal("start() after close succeeded")
	}
}

func TestPreviewControllerEnforcesLimitAndReleasesSources(t *testing.T) {
	t.Parallel()
	released := make(chan struct{}, maxConcurrentPreviews+1)
	factory := func(context.Context, Manager, core.StreamSelection) (stream.VideoSource, func(), error) {
		source, _, err := syntheticPreviewSources(context.Background(), nil, core.StreamSelection{})
		return source, func() { released <- struct{}{} }, err
	}
	controller := newPreviewController(factory, nil)
	root := context.Background()
	for i := 0; i < maxConcurrentPreviews; i++ {
		if _, err := controller.start(root, root, nil, core.StreamSelection{}); err != nil {
			t.Fatalf("start(%d) error = %v", i, err)
		}
	}
	if _, err := controller.start(root, root, nil, core.StreamSelection{}); !errors.Is(err, errPreviewLimit) {
		t.Fatalf("fifth start() error = %v, want errPreviewLimit", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := controller.close(ctx); err != nil {
		t.Fatalf("close() error = %v", err)
	}
	if len(released) != maxConcurrentPreviews {
		t.Fatalf("released %d sources, want %d", len(released), maxConcurrentPreviews)
	}
}

func TestStopPreviewValidatesSessionID(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "short", "0123456789ABCDEF0123456789ABCDEF", "0123456789abcdef0123456789abcdeg"} {
		if validPreviewSessionID(value) {
			t.Errorf("validPreviewSessionID(%q) = true", value)
		}
	}
}

type staticFeed struct{ image []byte }

func (f staticFeed) Fetch(context.Context) ([]byte, error) { return f.image, nil }

func TestPreviewControllerServesSnapshotFallback(t *testing.T) {
	t.Parallel()
	manager := newFakeManager()
	var selected core.StreamSelection
	manager.snapshotFeedFn = func(_ context.Context, selection core.StreamSelection) (core.SnapshotFeed, error) {
		selected = selection
		return staticFeed{image: []byte{0xFF, 0xD8, 0xFF, 0xD9}}, nil
	}
	controller := newPreviewController(syntheticPreviewSources, []string{wailsPageOrigin})
	selection := core.StreamSelection{DeviceID: "device", ProfileToken: "profile", Transport: core.StreamTransportTCP}
	result, err := controller.startSnapshot(context.Background(), manager, selection)
	if err != nil {
		t.Fatalf("startSnapshot() error = %v", err)
	}
	if selected != selection || !validPreviewSessionID(result.SessionID) {
		t.Fatalf("selection = %+v, session = %q", selected, result.SessionID)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, result.URL, nil)
	if err != nil {
		t.Fatalf("NewRequest() error = %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+result.Token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("Do() error = %v", err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || len(body) != 4 {
		t.Fatalf("status = %d, body = % x", response.StatusCode, body)
	}
	if !controller.stop(result.SessionID) {
		t.Fatal("stop() did not find the snapshot session")
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer closeCancel()
	if err := controller.close(closeCtx); err != nil {
		t.Fatalf("close() error = %v", err)
	}
}

func TestPreviewControllerRecordsKeyframeAlignedClips(t *testing.T) {
	t.Parallel()
	controller := newPreviewController(syntheticPreviewSources, []string{wailsPageOrigin})
	directory := t.TempDir()
	controller.clipDir = func() (string, error) { return directory, nil }
	root, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()
	// A profile token with path characters must not escape the clip folder.
	unsafeProfile := core.StreamSelection{DeviceID: "device", ProfileToken: "main/../token", Transport: core.StreamTransportTCP} //nolint:gosec // synthetic ONVIF profile token, not a credential
	result, err := controller.start(context.Background(), root, nil, unsafeProfile)
	if err != nil {
		t.Fatalf("start() error = %v", err)
	}
	if _, err := controller.stopRecording(result.SessionID); !errors.Is(err, errRecordingInactive) {
		t.Fatalf("stopRecording() before start error = %v", err)
	}
	path, err := controller.startRecording(result.SessionID, time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("startRecording() error = %v", err)
	}
	if filepath.Dir(path) != directory || filepath.Base(path) != "ovm-maintoken-20261010-120000.000.mp4" {
		t.Fatalf("clip path = %q", path)
	}
	if _, err := controller.startRecording(result.SessionID, time.Now()); !errors.Is(err, errRecordingActive) {
		t.Fatalf("second startRecording() error = %v", err)
	}
	// GOP 25 at 25 fps: wait past a keyframe so the clip is not empty.
	time.Sleep(1300 * time.Millisecond)
	clip, err := controller.stopRecording(result.SessionID)
	if err != nil {
		t.Fatalf("stopRecording() error = %v", err)
	}
	info, err := os.Stat(clip.Path)
	if err != nil || info.Size() != clip.Bytes || clip.Bytes == 0 || clip.Interrupted {
		t.Fatalf("clip = %+v, stat = %v, %v", clip, info, err)
	}
	if _, err := controller.startRecording(strings.Repeat("0", 32), time.Now()); !errors.Is(err, errPreviewNotFound) {
		t.Fatalf("startRecording(unknown) error = %v", err)
	}
	closeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := controller.close(closeCtx); err != nil {
		t.Fatalf("close() error = %v", err)
	}
}
