package fakedevice_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	media1 "github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func encoderFake(t *testing.T, behavior fakedevice.EncoderSetBehavior) (*media1.Client, *media2.Client) {
	t.Helper()
	device, err := fakedevice.New(fakedevice.Config{MediaInventory: fakedevice.MediaInventory{
		EncoderSetBehavior: behavior,
		Media2EncoderOptions: []fakedevice.Media2EncoderOptionData{
			{Encoding: "H264", Profiles: []string{"Main", "High"}, FrameRates: []float64{30, 25, 15},
				Resolutions: []fakedevice.ResolutionData{{1920, 1080}, {1280, 720}, {640, 360}},
				QualityMax:  10, BitrateMin: 256, BitrateMax: 8192, GovLengthMin: 1, GovLengthMax: 120},
			{Encoding: "H265", Profiles: []string{"Main"}, FrameRates: []float64{30, 25},
				Resolutions: []fakedevice.ResolutionData{{1920, 1080}},
				QualityMax:  10, BitrateMin: 256, BitrateMax: 8192, GovLengthMin: 1, GovLengthMax: 120},
		},
	}})
	if err != nil {
		t.Fatalf("fakedevice.New() error = %v", err)
	}
	server := httptest.NewServer(device)
	t.Cleanup(server.Close)
	transport, err := soap.NewClient(soap.Config{})
	if err != nil {
		t.Fatalf("soap.NewClient() error = %v", err)
	}
	first, err := media1.NewClient(transport, server.URL)
	if err != nil {
		t.Fatalf("media1.NewClient() error = %v", err)
	}
	second, err := media2.NewClient(transport, server.URL)
	if err != nil {
		t.Fatalf("media2.NewClient() error = %v", err)
	}
	return first, second
}

func mainEncoder(t *testing.T, client *media2.Client) media2.VideoEncoderConfiguration {
	t.Helper()
	configs, err := client.GetVideoEncoderConfigurations(context.Background(), "venc_main", "")
	if err != nil || len(configs) != 1 {
		t.Fatalf("GetVideoEncoderConfigurations() = %d, %v", len(configs), err)
	}
	return configs[0]
}

func TestFakeAppliesValidMedia2EncoderChanges(t *testing.T) {
	t.Parallel()
	_, client := encoderFake(t, fakedevice.EncoderSetApply)
	options, err := client.GetVideoEncoderConfigurationOptions(context.Background(), "venc_main", "")
	if err != nil || len(options.Options) != 2 || options.Options[0].FrameRatesSupported[1] != 25 {
		t.Fatalf("options = %+v, %v", options.Options, err)
	}
	config := mainEncoder(t, client)
	config.Encoding, config.Resolution = "H264", media2.VideoResolution{Width: 1280, Height: 720}
	config.FrameRateLimit, config.BitrateLimit, config.GovLength, config.Profile = 25, 2048, 50, "High"
	if err := client.SetVideoEncoderConfiguration(context.Background(), config); err != nil {
		t.Fatalf("SetVideoEncoderConfiguration() error = %v", err)
	}
	got := mainEncoder(t, client)
	if got.Encoding != "H264" || got.Resolution.Width != 1280 || got.FrameRateLimit != 25 || got.BitrateLimit != 2048 || got.GovLength != 50 || got.Profile != "High" {
		t.Fatalf("readback = %+v", got)
	}
}

func TestFakeRejectsClampsOrIgnoresEncoderChanges(t *testing.T) {
	t.Parallel()
	_, strict := encoderFake(t, fakedevice.EncoderSetApply)
	config := mainEncoder(t, strict)
	config.BitrateLimit = 20000
	if err := strict.SetVideoEncoderConfiguration(context.Background(), config); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("out-of-range bitrate error = %v", err)
	}
	config = mainEncoder(t, strict)
	config.Resolution = media2.VideoResolution{Width: 1280, Height: 720}
	if err := strict.SetVideoEncoderConfiguration(context.Background(), config); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("unsupported H.265 resolution error = %v", err)
	}

	_, clamping := encoderFake(t, fakedevice.EncoderSetClamp)
	config = mainEncoder(t, clamping)
	config.BitrateLimit = 20000
	if err := clamping.SetVideoEncoderConfiguration(context.Background(), config); err != nil {
		t.Fatalf("clamping Set error = %v", err)
	}
	if got := mainEncoder(t, clamping).BitrateLimit; got != 8192 {
		t.Fatalf("clamped bitrate = %d, want 8192", got)
	}

	_, ignoring := encoderFake(t, fakedevice.EncoderSetIgnore)
	config = mainEncoder(t, ignoring)
	before := config.BitrateLimit
	config.BitrateLimit = 2000
	if err := ignoring.SetVideoEncoderConfiguration(context.Background(), config); err != nil {
		t.Fatalf("ignoring Set error = %v", err)
	}
	if got := mainEncoder(t, ignoring).BitrateLimit; got != before {
		t.Fatalf("ignoring device changed bitrate to %d", got)
	}
}

func TestFakeAppliesMedia1EncoderChanges(t *testing.T) {
	t.Parallel()
	client, _ := encoderFake(t, fakedevice.EncoderSetApply)
	configs, err := client.GetVideoEncoderConfigurations(context.Background())
	if err != nil || len(configs) == 0 {
		t.Fatalf("GetVideoEncoderConfigurations() = %v", err)
	}
	config := configs[0]
	config.BitrateLimit, config.FrameRateLimit, config.HasFrameRateLimit, config.HasBitrateLimit = 1500, 20, true, true
	if err := client.SetVideoEncoderConfiguration(context.Background(), config, true); err != nil {
		t.Fatalf("SetVideoEncoderConfiguration() error = %v", err)
	}
	updated, err := client.GetVideoEncoderConfigurations(context.Background())
	if err != nil || updated[0].BitrateLimit != 1500 || updated[0].FrameRateLimit != 20 {
		t.Fatalf("readback = %+v, %v", updated[0], err)
	}
}
