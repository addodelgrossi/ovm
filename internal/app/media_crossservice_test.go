package app

import (
	"context"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

// TestMedia2FallbackUsesMedia1ProfileAssociations proves Finding 1's fix: when
// the root GetProfiles is answered by Media2 but an individual video/audio
// encoder operation falls back to Media1, the fallback resolves the CORRESPON-
// DING Media1 profile and matches the Media1 configuration list against that
// profile's OWN Media1 association tokens — never the Media2-derived tokens.
// The Media1-only video source lookup likewise uses the Media1 profile source.
//
// The fake is configured so the single profile token "profile_main" maps to
// DIFFERENT tokens per service:
//
//	Media2: video venc2_main, audio aenc2, source vsrc2
//	Media1: video venc1_main, audio aenc1, source vsrc1
//
// Only Media2 GetVideoEncoderConfigurations and GetAudioEncoderConfigurations
// are forced to ActionNotSupported, so GetProfiles (and the Media2 snapshot/
// stream ops) stay on Media2 while the two encoder ops fall back. If the code
// reused the Media2 tokens, the Media1 list (which only contains venc1_main /
// aenc1) would not match and the operations would report unknown; the fix
// matches the Media1 tokens and produces complete data.
func TestMedia2FallbackUsesMedia1ProfileAssociations(t *testing.T) {
	t.Parallel()

	inv := crossServiceMediaInventory()
	setup := newUnstartedFake(t, func(base string) fakedevice.Config {
		return fakedevice.Config{
			Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
				{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
				{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: base + "/onvif/media_service", VersionMajor: 25, VersionMinor: 6},
				{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6},
			}},
			MediaInventory: inv,
		}
	})

	// Force ONLY the two Media2 encoder reads to be unsupported so GetProfiles
	// remains answered by Media2 and the encoder ops fall back to Media1.
	setup.device.SetUnsupported(fakedevice.ActionMedia2GetVideoEncoderConfigurations, true)
	setup.device.SetUnsupported(fakedevice.ActionMedia2GetAudioEncoderConfigurations, true)

	result, err := NewProductionMediaProfiles().Profiles(context.Background(), MediaProfilesRequest{Host: setup.host})
	if err != nil {
		t.Fatalf("Profiles() error = %v", err)
	}
	if len(result.Profiles) != 1 {
		t.Fatalf("profiles = %d, want 1", len(result.Profiles))
	}
	p := result.Profiles[0]
	if p.Source != MediaServiceMedia2 {
		t.Fatalf("profile source = %q, want media2 (GetProfiles answered by Media2)", p.Source)
	}

	// Video encoder: fell back to Media1 and matched the Media1 token.
	if p.Video == nil {
		t.Fatal("video encoder was not resolved; the Media1 fallback did not match")
	}
	if p.Video.ConfigToken != "venc1_main" {
		t.Fatalf("video config token = %q, want venc1_main (Media1 association)", p.Video.ConfigToken)
	}
	videoStatus := opStatus(t, p, MediaOpVideoEncoder)
	if videoStatus.State != CapabilitySupported || videoStatus.Source != MediaServiceMedia1 || !videoStatus.FellBack {
		t.Fatalf("video status = %+v, want supported/media1/fellBack", videoStatus)
	}

	// Audio encoder: fell back to Media1 and matched the Media1 token.
	if p.Audio == nil {
		t.Fatal("audio encoder was not resolved; the Media1 fallback did not match")
	}
	if p.Audio.ConfigToken != "aenc1" {
		t.Fatalf("audio config token = %q, want aenc1 (Media1 association)", p.Audio.ConfigToken)
	}
	audioStatus := opStatus(t, p, MediaOpAudioEncoder)
	if audioStatus.State != CapabilitySupported || audioStatus.Source != MediaServiceMedia1 || !audioStatus.FellBack {
		t.Fatalf("audio status = %+v, want supported/media1/fellBack", audioStatus)
	}

	// Video source: Media1-only, matched via the Media1 profile source token.
	sourceStatus := opStatus(t, p, MediaOpVideoSource)
	if sourceStatus.State != CapabilitySupported || sourceStatus.Source != MediaServiceMedia1 {
		t.Fatalf("video source status = %+v, want supported/media1", sourceStatus)
	}
}

// TestMedia2FallbackMismatchedMedia1ProfileIsSafeUnknown proves that when the
// Media2 profile has no corresponding Media1 profile (same token), the Media1
// encoder fallback reports a safe unknown rather than silently matching a
// cross-version token. The Media1 GetProfiles lists a DIFFERENT token.
func TestMedia2FallbackMismatchedMedia1ProfileIsSafeUnknown(t *testing.T) {
	t.Parallel()

	inv := crossServiceMediaInventory()
	// Media1 profile keeps a different token than Media2 so no Media1 profile
	// matches "profile_main". Media2 is the profiles source.
	inv.Media1Profiles = []fakedevice.MediaProfileData{
		//nolint:gosec // G101 false positive: these are synthetic ONVIF reference tokens, not credentials.
		{Token: "profile_media1_only", Name: "m1only", VideoSourceConfigurationToken: "vsrccfg1", VideoSourceToken: "vsrc1", VideoEncoderToken: "venc1_main", AudioEncoderToken: "aenc1"},
	}
	setup := newUnstartedFake(t, func(base string) fakedevice.Config {
		return fakedevice.Config{
			Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
				{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
				{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: base + "/onvif/media_service", VersionMajor: 25, VersionMinor: 6},
				{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6},
			}},
			MediaInventory: inv,
		}
	})
	setup.device.SetUnsupported(fakedevice.ActionMedia2GetVideoEncoderConfigurations, true)

	result, err := NewProductionMediaProfiles().Profiles(context.Background(), MediaProfilesRequest{Host: setup.host})
	if err != nil {
		t.Fatalf("Profiles() error = %v", err)
	}
	// Find the Media2 profile (profile_main).
	var p MediaProfileDTO
	for _, candidate := range result.Profiles {
		if candidate.Token == "profile_main" {
			p = candidate
		}
	}
	if p.Token != "profile_main" {
		t.Fatal("profile_main not present in result")
	}
	videoStatus := opStatus(t, p, MediaOpVideoEncoder)
	if videoStatus.State != CapabilityUnknown {
		t.Fatalf("video status state = %q, want unknown (no matching Media1 profile)", videoStatus.State)
	}
	if p.Video != nil {
		t.Fatalf("video DTO should be nil for an unmatched cross-version token, got %+v", p.Video)
	}
}

// crossServiceMediaInventory builds a MediaInventory where profile_main maps to
// different Media1 and Media2 association tokens. All tokens are synthetic
// reference tokens, never credentials.
func crossServiceMediaInventory() fakedevice.MediaInventory {
	return fakedevice.MediaInventory{
		Profiles: []fakedevice.MediaProfileData{
			//nolint:gosec // G101 false positive: synthetic ONVIF reference tokens, not credentials.
			{
				Token: "profile_main", Name: "main", Fixed: true,
				// Media2 associations (shared fields are used by Media2).
				VideoSourceConfigurationToken: "vsrccfg2", VideoSourceToken: "vsrc2",
				VideoEncoderToken: "venc2_main", AudioEncoderToken: "aenc2",
				// Media1-specific associations (distinct tokens).
				Media1VideoSourceConfigurationToken: "vsrccfg1", Media1VideoSourceToken: "vsrc1",
				Media1VideoEncoderToken: "venc1_main", Media1AudioEncoderToken: "aenc1",
			},
		},
		// Media1 profile list used by the fallback: same profile token, Media1
		// associations.
		Media1Profiles: []fakedevice.MediaProfileData{
			//nolint:gosec // G101 false positive: synthetic ONVIF reference tokens, not credentials.
			{Token: "profile_main", Name: "main", VideoSourceConfigurationToken: "vsrccfg1", VideoSourceToken: "vsrc1", VideoEncoderToken: "venc1_main", AudioEncoderToken: "aenc1"},
		},
		VideoSources: []fakedevice.VideoSourceData{
			{Token: "vsrc1", Framerate: 30, Width: 1920, Height: 1080},
			{Token: "vsrc2", Framerate: 30, Width: 1920, Height: 1080},
		},
		VideoEncoders: []fakedevice.VideoEncoderConfigurationData{
			//nolint:gosec // G101 false positive: synthetic ONVIF reference tokens, not credentials.
			{Token: "venc1_main", Name: "VideoEncoderM1", Encoding: "H264", Width: 1920, Height: 1080, Quality: 5, FrameRateLimit: 30, BitrateLimit: 4096, GovLength: 30, UseCount: 1},
		},
		AudioEncoders: []fakedevice.AudioEncoderConfigurationData{
			{Token: "aenc1", Name: "AudioEncoderM1", Encoding: "G711", Bitrate: 64, SampleRate: 8, UseCount: 1},
		},
		Media2VideoEncoders: []fakedevice.VideoEncoder2ConfigurationData{
			//nolint:gosec // G101 false positive: synthetic ONVIF reference tokens, not credentials.
			{Token: "venc2_main", Name: "VideoEncoderM2", Encoding: "H265", Width: 1920, Height: 1080, Quality: 5.5, FrameRateLimit: 30, BitrateLimit: 4096, AverageBitRate: 3072, GovLength: 30, Profile: "Main", UseCount: 1},
		},
		Media2AudioEncoders: []fakedevice.AudioEncoder2ConfigurationData{
			{Token: "aenc2", Name: "AudioEncoderM2", Encoding: "PCMU", Bitrate: 64, SampleRate: 8, UseCount: 1},
		},
	}
}
