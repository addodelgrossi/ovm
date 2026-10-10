package app

import (
	"context"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/internal/synthmedia"
	"github.com/addodelgrossi/ovm/pkg/stream"
)

// TestProductionProbeMeasuresSyntheticCameraEndToEnd resolves profiles over
// authenticated ONVIF and measures the authenticated synthetic RTSP stream.
func TestProductionProbeMeasuresSyntheticCameraEndToEnd(t *testing.T) {
	t.Parallel()
	const username, password = "admin", "probe-e2e-secret" //nolint:gosec // synthetic test credential
	rtsp, err := synthmedia.StartRTSPServer(synthmedia.RTSPServerConfig{
		Video:            synthmedia.H264Config{Width: 160, Height: 96, FPS: 25, GOP: 25},
		Username:         username,
		Password:         password,
		SDPParameterSets: true,
	})
	if err != nil {
		t.Fatalf("StartRTSPServer() error = %v", err)
	}
	t.Cleanup(rtsp.Close)
	srv := newUnstartedFake(t, func(base string) fakedevice.Config {
		return fakedevice.Config{
			HTTPAuth:             fakedevice.HTTPAuthDigest,
			DigestAlgorithm:      "SHA-256",
			RequireUsernameToken: true,
			Username:             username,
			Password:             password,
			Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
				{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
				{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6},
			}},
			MediaInventory: fakedevice.MediaInventory{RTSPPort: rtsp.Port()},
		}
	})
	creds, err := NewCredentials(username, password, false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	result, err := NewProductionProbe().Probe(context.Background(), ProbeRequest{
		Host:         srv.host,
		Credentials:  creds,
		ProfileToken: "profile_sub",
		Duration:     2 * time.Second,
		Timeout:      10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Probe() error = %v (result %+v)", err, result)
	}
	if !result.Complete || result.Codec != stream.CodecH264 || result.Width != 160 || result.Height != 96 {
		t.Fatalf("result = %+v", result)
	}
	if result.FramesPerSecond < 20 || result.FramesPerSecond > 30 || result.GOPFrames != 25 || result.TimeToFirstFrameMillis <= 0 {
		t.Fatalf("measurements = %+v", result)
	}
}
