package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	core "github.com/addodelgrossi/ovm/internal/app"
	"github.com/addodelgrossi/ovm/pkg/stream"
)

func TestPreviewControllerStreamsDemoSourceAndStops(t *testing.T) {
	t.Parallel()
	controller := newPreviewController(demoPreviewSources, []string{wailsPageOrigin})
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
	factory := func(context.Context, Manager, core.StreamSelection) (stream.H264Source, func(), error) {
		source, _, err := demoPreviewSources(context.Background(), nil, core.StreamSelection{})
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
