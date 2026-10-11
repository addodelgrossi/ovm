package fakedevice_test

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	media1 "github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// TestPublicMediaClientsAgainstFake pins the public wire clients and synthetic
// device to one another for every operation in this milestone. Unit tests still
// exercise malformed and fault matrices independently; this catches namespace,
// action, wrapper, and required-field drift between the two package families.
func TestPublicMediaClientsAgainstFake(t *testing.T) {
	t.Parallel()

	device, err := fakedevice.New(fakedevice.Config{})
	if err != nil {
		t.Fatalf("fakedevice.New() error = %v", err)
	}
	server := httptest.NewServer(device)
	defer server.Close()
	transport, err := soap.NewClient(soap.Config{})
	if err != nil {
		t.Fatalf("soap.NewClient() error = %v", err)
	}
	ctx := context.Background()

	media1Client, err := media1.NewClient(transport, server.URL)
	if err != nil {
		t.Fatalf("media.NewClient() error = %v", err)
	}
	profiles1, err := media1Client.GetProfiles(ctx)
	if err != nil || len(profiles1) != 2 {
		t.Fatalf("Media1 GetProfiles() = (%+v, %v), want two profiles", profiles1, err)
	}
	stream1, err := media1Client.GetStreamUri(ctx, profiles1[0].Token, media1.StreamSetup{Stream: media1.StreamRTPUnicast, Protocol: media1.TransportUDP})
	if err != nil || stream1.URI == "" {
		t.Fatalf("Media1 GetStreamUri() = (%+v, %v)", stream1, err)
	}
	snapshot1, err := media1Client.GetSnapshotUri(ctx, profiles1[0].Token)
	if err != nil || snapshot1.URI == "" {
		t.Fatalf("Media1 GetSnapshotUri() = (%+v, %v)", snapshot1, err)
	}
	if sources, err := media1Client.GetVideoSources(ctx); err != nil || len(sources) != 1 {
		t.Fatalf("Media1 GetVideoSources() = (%+v, %v)", sources, err)
	}
	if configurations, err := media1Client.GetVideoEncoderConfigurations(ctx); err != nil || len(configurations) != 2 {
		t.Fatalf("Media1 GetVideoEncoderConfigurations() = (%+v, %v)", configurations, err)
	}
	if options, err := media1Client.GetVideoEncoderConfigurationOptions(ctx, "venc_main", "profile_main"); err != nil || options.H264 == nil {
		t.Fatalf("Media1 GetVideoEncoderConfigurationOptions() = (%+v, %v)", options, err)
	}
	if configurations, err := media1Client.GetAudioEncoderConfigurations(ctx); err != nil || len(configurations) != 1 {
		t.Fatalf("Media1 GetAudioEncoderConfigurations() = (%+v, %v)", configurations, err)
	}

	media2Client, err := media2.NewClient(transport, server.URL)
	if err != nil {
		t.Fatalf("media2.NewClient() error = %v", err)
	}
	profiles2, err := media2Client.GetProfiles(ctx, "profile_sub", []media2.ConfigurationType{media2.ConfigurationAll})
	if err != nil || len(profiles2) != 1 || profiles2[0].Token != "profile_sub" {
		t.Fatalf("Media2 GetProfiles() = (%+v, %v), want filtered sub profile", profiles2, err)
	}
	if stream, err := media2Client.GetStreamUri(ctx, media2.StreamRTSP, profiles2[0].Token); err != nil || stream == "" {
		t.Fatalf("Media2 GetStreamUri() = (%q, %v)", stream, err)
	}
	if snapshot, err := media2Client.GetSnapshotUri(ctx, profiles2[0].Token); err != nil || snapshot == "" {
		t.Fatalf("Media2 GetSnapshotUri() = (%q, %v)", snapshot, err)
	}
	if configurations, err := media2Client.GetVideoEncoderConfigurations(ctx, "", profiles2[0].Token); err != nil || len(configurations) != 1 || configurations[0].Encoding != "H264" {
		t.Fatalf("Media2 GetVideoEncoderConfigurations() = (%+v, %v)", configurations, err)
	}
	if mainConfigurations, err := media2Client.GetVideoEncoderConfigurations(ctx, "venc_main", ""); err != nil || len(mainConfigurations) != 1 || mainConfigurations[0].Encoding != "H265" {
		t.Fatalf("Media2 H265 configuration = (%+v, %v)", mainConfigurations, err)
	}
	if options, err := media2Client.GetVideoEncoderConfigurationOptions(ctx, "venc_sub", profiles2[0].Token); err != nil || len(options.Options) != 1 || options.Options[0].QualityRange.Min != 0.5 {
		t.Fatalf("Media2 GetVideoEncoderConfigurationOptions() = (%+v, %v)", options, err)
	}
	if configurations, err := media2Client.GetAudioEncoderConfigurations(ctx, "aenc0", profiles2[0].Token); err != nil || len(configurations) != 1 || configurations[0].Encoding != "PCMU" {
		t.Fatalf("Media2 GetAudioEncoderConfigurations() = (%+v, %v)", configurations, err)
	}
}
