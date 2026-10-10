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

// TestFakeMediaProfileAssociationsE2E proves that the synthetic device emits
// profile association tokens and configured encoder detail that the public wire
// clients decode into the typed fields, for both the main and sub profiles.
func TestFakeMediaProfileAssociationsE2E(t *testing.T) {
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

	// ----- Media1 -----
	media1Client, err := media1.NewClient(transport, server.URL)
	if err != nil {
		t.Fatalf("media.NewClient() error = %v", err)
	}
	profiles1, err := media1Client.GetProfiles(ctx)
	if err != nil {
		t.Fatalf("Media1 GetProfiles() error = %v", err)
	}
	byToken1 := map[string]media1.Profile{}
	for _, p := range profiles1 {
		byToken1[p.Token] = p
	}
	main1, ok := byToken1["profile_main"]
	if !ok {
		t.Fatalf("missing profile_main in %+v", profiles1)
	}
	sub1 := byToken1["profile_sub"]
	for name, p := range map[string]media1.Profile{"main": main1, "sub": sub1} {
		a := p.Associations
		if !a.HasVideoSourceConfiguration || a.VideoSourceConfigurationToken != "vsrccfg0" {
			t.Errorf("%s: video source config token = %q present=%v", name, a.VideoSourceConfigurationToken, a.HasVideoSourceConfiguration)
		}
		if !a.HasVideoSource || a.VideoSourceToken != "vsrc0" {
			t.Errorf("%s: physical source token = %q present=%v", name, a.VideoSourceToken, a.HasVideoSource)
		}
		if !a.HasAudioEncoderConfiguration || a.AudioEncoderConfigurationToken != "aenc0" {
			t.Errorf("%s: audio encoder token = %q present=%v", name, a.AudioEncoderConfigurationToken, a.HasAudioEncoderConfiguration)
		}
	}
	if main1.Associations.VideoEncoderConfigurationToken != "venc_main" {
		t.Errorf("main video encoder token = %q", main1.Associations.VideoEncoderConfigurationToken)
	}
	if sub1.Associations.VideoEncoderConfigurationToken != "venc_sub" {
		t.Errorf("sub video encoder token = %q", sub1.Associations.VideoEncoderConfigurationToken)
	}

	// Configured rate-control/codec detail on the dedicated configuration op.
	configs1, err := media1Client.GetVideoEncoderConfigurations(ctx)
	if err != nil {
		t.Fatalf("Media1 GetVideoEncoderConfigurations() error = %v", err)
	}
	byToken1Config := map[string]media1.VideoEncoderConfiguration{}
	for _, c := range configs1 {
		byToken1Config[c.Token] = c
	}
	mainCfg1 := byToken1Config["venc_main"]
	if !mainCfg1.HasFrameRateLimit || mainCfg1.FrameRateLimit != 30 {
		t.Errorf("main cfg FrameRateLimit = %d present=%v", mainCfg1.FrameRateLimit, mainCfg1.HasFrameRateLimit)
	}
	if !mainCfg1.HasBitrateLimit || mainCfg1.BitrateLimit != 4096 {
		t.Errorf("main cfg BitrateLimit = %d present=%v", mainCfg1.BitrateLimit, mainCfg1.HasBitrateLimit)
	}
	if !mainCfg1.HasGovLength || mainCfg1.GovLength != 30 {
		t.Errorf("main cfg GovLength = %d present=%v", mainCfg1.GovLength, mainCfg1.HasGovLength)
	}
	if !mainCfg1.HasCodecProfile || mainCfg1.CodecProfile != "Main" {
		t.Errorf("main cfg CodecProfile = %q present=%v", mainCfg1.CodecProfile, mainCfg1.HasCodecProfile)
	}

	// ----- Media2 -----
	media2Client, err := media2.NewClient(transport, server.URL)
	if err != nil {
		t.Fatalf("media2.NewClient() error = %v", err)
	}
	// This test consumes associations, so request them explicitly. The literal
	// keeps the WSDL contract pinned independently of production constants.
	profiles2, err := media2Client.GetProfiles(ctx, "", []media2.ConfigurationType{"All"})
	if err != nil {
		t.Fatalf("Media2 GetProfiles() error = %v", err)
	}
	byToken2 := map[string]media2.Profile{}
	for _, p := range profiles2 {
		byToken2[p.Token] = p
	}
	main2 := byToken2["profile_main"]
	sub2 := byToken2["profile_sub"]
	if main2.Associations.VideoEncoderConfigurationToken != "venc_main" || !main2.Associations.HasVideoSource || main2.Associations.VideoSourceToken != "vsrc0" {
		t.Errorf("Media2 main associations = %+v", main2.Associations)
	}
	if sub2.Associations.VideoEncoderConfigurationToken != "venc_sub" || sub2.Associations.AudioEncoderConfigurationToken != "aenc0" {
		t.Errorf("Media2 sub associations = %+v", sub2.Associations)
	}

	// Configured rate-control + unqualified attributes on the dedicated op.
	mainConfigs2, err := media2Client.GetVideoEncoderConfigurations(ctx, "venc_main", "")
	if err != nil || len(mainConfigs2) != 1 {
		t.Fatalf("Media2 GetVideoEncoderConfigurations(venc_main) = (%+v, %v)", mainConfigs2, err)
	}
	m := mainConfigs2[0]
	if !m.HasFrameRateLimit || m.FrameRateLimit != 30 {
		t.Errorf("Media2 main FrameRateLimit = %v present=%v", m.FrameRateLimit, m.HasFrameRateLimit)
	}
	if !m.HasBitrateLimit || m.BitrateLimit != 4096 {
		t.Errorf("Media2 main BitrateLimit = %d present=%v", m.BitrateLimit, m.HasBitrateLimit)
	}
	if !m.HasAverageBitRate || m.AverageBitRate != 3072 {
		t.Errorf("Media2 main AverageBitRate = %d present=%v", m.AverageBitRate, m.HasAverageBitRate)
	}
	if !m.HasGovLength || m.GovLength != 30 {
		t.Errorf("Media2 main GovLength = %d present=%v", m.GovLength, m.HasGovLength)
	}
	if !m.HasProfile || m.Profile != "Main" {
		t.Errorf("Media2 main Profile = %q present=%v", m.Profile, m.HasProfile)
	}

	subConfigs2, err := media2Client.GetVideoEncoderConfigurations(ctx, "venc_sub", "")
	if err != nil || len(subConfigs2) != 1 {
		t.Fatalf("Media2 GetVideoEncoderConfigurations(venc_sub) = (%+v, %v)", subConfigs2, err)
	}
	s := subConfigs2[0]
	if !s.HasFrameRateLimit || s.FrameRateLimit != 15 || !s.HasGovLength || s.GovLength != 15 || !s.HasProfile || s.Profile != "High" {
		t.Errorf("Media2 sub configured detail = %+v", s)
	}
}
