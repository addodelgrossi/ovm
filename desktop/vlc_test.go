package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	core "github.com/addodelgrossi/ovm/internal/app"
)

func TestOpenInVLCUsesOnlyBackendResolvedSelection(t *testing.T) {
	manager := newFakeManager()
	manager.resolveFn = func(_ context.Context, selection core.StreamSelection) (string, error) {
		if selection.DeviceID != "dev_camera" || selection.ProfileToken != "profile_main" || selection.Transport != core.StreamTransportTCP {
			t.Fatalf("ResolveStreamURI() selection = %+v", selection)
		}
		return "rtsp://192.0.2.10:554/main", nil
	}
	launcher := &fakeVLCLauncher{available: true}
	host := newTestApp(t, manager, func(dependencies *AppDependencies) {
		dependencies.VLC = launcher
	})

	response := host.OpenInVLC(OpenInVLCRequest{
		DeviceID: "dev_camera", ProfileToken: "profile_main", Transport: "tcp",
	})
	if response.Error != nil || response.Result == nil || !response.Result.Completed {
		t.Fatalf("OpenInVLC() response = %+v", response)
	}
	calls, transport, uri := launcher.snapshot()
	if calls != 1 || transport != core.StreamTransportTCP || uri != "rtsp://192.0.2.10:554/main" {
		t.Fatalf("launcher calls/transport/URI = %d/%q/%q", calls, transport, uri)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("json.Marshal(response): %v", err)
	}
	if strings.Contains(string(encoded), uri) {
		t.Fatalf("OpenInVLC response exposed resolved URI: %s", encoded)
	}
}

func TestOpenInVLCIsUnavailableInDemoEvenWhenLauncherExists(t *testing.T) {
	manager := newFakeManager()
	launcher := &fakeVLCLauncher{available: true}
	host := newTestApp(t, manager, func(dependencies *AppDependencies) {
		dependencies.Demo = true
		dependencies.VLC = launcher
	})

	bootstrap := host.Bootstrap()
	if bootstrap.Result == nil || !bootstrap.Result.Capabilities.Demo || bootstrap.Result.Capabilities.VLCAvailable || bootstrap.Result.Capabilities.RTSPPlaybackAvailable {
		t.Fatalf("demo capabilities = %+v", bootstrap.Result)
	}
	response := host.OpenInVLC(OpenInVLCRequest{
		DeviceID: "dev_camera", ProfileToken: "profile_main", Transport: "udp",
	})
	if response.Error == nil || response.Error.Code != "vlc_unavailable" {
		t.Fatalf("OpenInVLC() demo response = %+v", response)
	}
	if manager.callCount("resolve") != 0 {
		t.Fatal("demo OpenInVLC reached the Manager")
	}
	if calls, _, _ := launcher.snapshot(); calls != 0 {
		t.Fatalf("demo launcher calls = %d, want 0", calls)
	}
}

func TestProductionVLCLauncherBuildsFixedArgumentsWithoutShell(t *testing.T) {
	starter := &recordingProcessStarter{}
	launcher := &productionVLCLauncher{path: "/allowlisted/vlc", starter: starter}
	if err := launcher.Launch(context.Background(), core.StreamTransportTCP, "rtsp://camera.example:554/main"); err != nil {
		t.Fatalf("Launch(TCP) error = %v", err)
	}
	if starter.executable != "/allowlisted/vlc" {
		t.Fatalf("executable = %q", starter.executable)
	}
	wantTCP := []string{"--rtsp-tcp", "--", "rtsp://camera.example:554/main"}
	if !reflect.DeepEqual(starter.arguments, wantTCP) {
		t.Fatalf("TCP arguments = %#v, want %#v", starter.arguments, wantTCP)
	}

	if err := launcher.Launch(context.Background(), core.StreamTransportUDP, "rtsps://camera.example/live"); err != nil {
		t.Fatalf("Launch(UDP) error = %v", err)
	}
	wantUDP := []string{"--", "rtsps://camera.example/live"}
	if !reflect.DeepEqual(starter.arguments, wantUDP) {
		t.Fatalf("UDP arguments = %#v, want %#v", starter.arguments, wantUDP)
	}
}

func TestProductionVLCLauncherRejectsUnsafeURIAndRedactsProcessFailure(t *testing.T) {
	starter := &recordingProcessStarter{err: errors.New("backend-path-and-uri-diagnostic")}
	launcher := &productionVLCLauncher{path: "/allowlisted/vlc", starter: starter}

	for _, unsafe := range []string{
		"https://camera.example/live",
		"rtsp://user:password@camera.example/live",
		"rtsp://camera.example/live?channel=%0Aoption",
		"rtsp://camera.example/live#fragment",
		"rtsp://camera.example/live%0Aoption",
		"--help",
	} {
		starter.mu.Lock()
		starter.arguments = nil
		starter.err = nil
		starter.mu.Unlock()
		err := launcher.Launch(context.Background(), core.StreamTransportTCP, unsafe)
		var typed *VLCError
		if !errors.As(err, &typed) || typed.Code != "unsafe_stream_uri" || strings.Contains(typed.Message, unsafe) {
			t.Fatalf("unsafe URI %q error = %#v", unsafe, err)
		}
		starter.mu.Lock()
		arguments := append([]string(nil), starter.arguments...)
		starter.mu.Unlock()
		if len(arguments) != 0 {
			t.Fatalf("unsafe URI %q reached process starter", unsafe)
		}
	}

	starter.mu.Lock()
	starter.err = errors.New("backend-path-and-uri-diagnostic")
	starter.mu.Unlock()
	err := launcher.Launch(context.Background(), core.StreamTransportUDP, "rtsp://camera.example/live")
	var typed *VLCError
	if !errors.As(err, &typed) || typed.Code != "vlc_launch_failed" || strings.Contains(typed.Message, "backend") {
		t.Fatalf("process failure was not safely typed: %#v", err)
	}
}
