package fakedevice_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	media1 "github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func TestFakeGetVideoSourcesUsesPublishedWSDLActionLiteral(t *testing.T) {
	t.Parallel()
	device := newDevice(t, fakedevice.Config{})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)
	const publishedAction = "http://www.onvif.org/ver10/media/wsdlGetVideoSources/"
	body := []byte(`<trt:GetVideoSources xmlns:trt="http://www.onvif.org/ver10/media/wsdl"></trt:GetVideoSources>`)
	response, err := client.Call(context.Background(), server.URL, soap.Request{Action: publishedAction, BodyXML: body})
	if err != nil {
		t.Fatalf("published action rejected: %v", err)
	}
	if !strings.Contains(string(response.BodyXML), "GetVideoSourcesResponse") {
		t.Fatalf("unexpected response: %s", response.BodyXML)
	}

	const conventionalButWrong = "http://www.onvif.org/ver10/media/wsdl/GetVideoSources"
	if _, err := client.Call(context.Background(), server.URL, soap.Request{Action: conventionalButWrong, BodyXML: body}); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("conventional action error = %v, want ErrInvalidArgVal", err)
	}

	withChild := []byte(`<trt:GetVideoSources xmlns:trt="http://www.onvif.org/ver10/media/wsdl"><trt:Unexpected></trt:Unexpected></trt:GetVideoSources>`)
	if _, err := client.Call(context.Background(), server.URL, soap.Request{Action: publishedAction, BodyXML: withChild}); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("invalid wrapper error = %v, want ErrInvalidArgVal", err)
	}
}

func TestFakeMedia2ProfilesUseConfiguredInventoryFiltersAndServiceTokens(t *testing.T) {
	t.Parallel()
	inventory := fakedevice.MediaInventory{
		Profiles: []fakedevice.MediaProfileData{{
			Token: "media2-profile", Name: "Media2", Fixed: true,
			VideoSourceConfigurationToken: "media2-source-config", VideoSourceToken: "source-custom",
			VideoEncoderToken: "media2-video", AudioEncoderToken: "media2-audio",
		}},
		Media1Profiles: []fakedevice.MediaProfileData{{
			Token: "media1-profile", Name: "Media1", Fixed: true,
			VideoSourceConfigurationToken: "media1-source-config", VideoSourceToken: "source-custom",
			VideoEncoderToken: "media1-video", AudioEncoderToken: "media1-audio",
		}},
		VideoSources: []fakedevice.VideoSourceData{{Token: "source-custom", Framerate: 25, Width: 1111, Height: 777}},
		VideoEncoders: []fakedevice.VideoEncoderConfigurationData{{
			Token: "media1-video", Name: "Media1Video", Encoding: "H264", Width: 800, Height: 600,
			Quality: 4, FrameRateLimit: 20, BitrateLimit: 2048, GovLength: 20, UseCount: 1,
		}},
		AudioEncoders: []fakedevice.AudioEncoderConfigurationData{{
			Token: "media1-audio", Name: "Media1Audio", Encoding: "G711", Bitrate: 64, SampleRate: 8, UseCount: 1,
		}},
		Media2VideoEncoders: []fakedevice.VideoEncoder2ConfigurationData{{
			Token: "media2-video", Name: "ConfiguredVideo", Encoding: "H265", Width: 1111, Height: 777,
			Quality: 6.25, FrameRateLimit: 23.5, BitrateLimit: 3456, AverageBitRate: 2345,
			GovLength: 47, Profile: "Main10", UseCount: 7,
		}},
		Media2AudioEncoders: []fakedevice.AudioEncoder2ConfigurationData{{
			Token: "media2-audio", Name: "ConfiguredAudio", Encoding: "OPUS", Bitrate: 96, SampleRate: 48, UseCount: 3,
		}},
	}
	device := newDevice(t, fakedevice.Config{MediaInventory: inventory})
	server := httptest.NewServer(device)
	defer server.Close()
	transport, err := soap.NewClient(soap.Config{})
	if err != nil {
		t.Fatalf("soap.NewClient() error = %v", err)
	}
	media1Client, err := media1.NewClient(transport, server.URL)
	if err != nil {
		t.Fatalf("media.NewClient() error = %v", err)
	}
	media2Client, err := media2.NewClient(transport, server.URL)
	if err != nil {
		t.Fatalf("media2.NewClient() error = %v", err)
	}
	ctx := context.Background()

	profiles1, err := media1Client.GetProfiles(ctx)
	if err != nil || len(profiles1) != 1 || profiles1[0].Token != "media1-profile" {
		t.Fatalf("Media1 profiles = (%+v, %v)", profiles1, err)
	}
	if profiles1[0].Associations.VideoEncoderConfigurationToken != "media1-video" {
		t.Fatalf("Media1 associations = %+v", profiles1[0].Associations)
	}
	media1XML := string(profiles1[0].RawXML)
	if !strings.Contains(media1XML, `<tt:Bounds x="0" y="0" width="1111" height="777"></tt:Bounds>`) {
		t.Fatalf("Media1 profile Bounds did not use its physical source dimensions: %s", media1XML)
	}

	metadataOnly, err := media2Client.GetProfiles(ctx, "", nil)
	if err != nil || len(metadataOnly) != 1 {
		t.Fatalf("metadata-only Media2 profiles = (%+v, %v)", metadataOnly, err)
	}
	if strings.Contains(string(metadataOnly[0].RawXML), "<tr2:Configurations") ||
		metadataOnly[0].Associations.HasVideoSourceConfiguration ||
		metadataOnly[0].Associations.HasVideoEncoderConfiguration ||
		metadataOnly[0].Associations.HasAudioEncoderConfiguration {
		t.Fatalf("omitted Type returned configuration information: %+v", metadataOnly[0])
	}

	// Use the WSDL literal independently of the package constant.
	profiles2, err := media2Client.GetProfiles(ctx, "", []media2.ConfigurationType{"All"})
	if err != nil || len(profiles2) != 1 || profiles2[0].Token != "media2-profile" {
		t.Fatalf("Media2 profiles = (%+v, %v)", profiles2, err)
	}
	allXML := string(profiles2[0].RawXML)
	for _, literal := range []string{
		`<tt:Bounds x="0" y="0" width="1111" height="777"></tt:Bounds>`,
		`<tt:Name>ConfiguredVideo</tt:Name>`, `<tt:UseCount>7</tt:UseCount>`, `<tt:Encoding>H265</tt:Encoding>`,
		`<tt:Width>1111</tt:Width>`, `<tt:Height>777</tt:Height>`, `<tt:FrameRateLimit>23.5</tt:FrameRateLimit>`,
		`<tt:BitrateLimit>3456</tt:BitrateLimit>`, `<tt:AverageBitRate>2345</tt:AverageBitRate>`,
		`GovLength="47"`, `Profile="Main10"`, `<tt:Quality>6.25</tt:Quality>`,
		`<tt:Name>ConfiguredAudio</tt:Name>`, `<tt:Encoding>OPUS</tt:Encoding>`, `<tt:Bitrate>96</tt:Bitrate>`, `<tt:SampleRate>48</tt:SampleRate>`,
	} {
		if !strings.Contains(allXML, literal) {
			t.Errorf("Media2 profile XML missing %q: %s", literal, allXML)
		}
	}

	videoOnly, err := media2Client.GetProfiles(ctx, "media2-profile", []media2.ConfigurationType{media2.ConfigurationVideoEncoder})
	if err != nil || len(videoOnly) != 1 {
		t.Fatalf("video-only profiles = (%+v, %v)", videoOnly, err)
	}
	videoXML := string(videoOnly[0].RawXML)
	if !strings.Contains(videoXML, "<tr2:VideoEncoder") || strings.Contains(videoXML, "<tr2:VideoSource") || strings.Contains(videoXML, "<tr2:AudioEncoder") {
		t.Fatalf("video filter produced wrong members: %s", videoXML)
	}
	audioOnly, err := media2Client.GetProfiles(ctx, "media2-profile", []media2.ConfigurationType{media2.ConfigurationAudioEncoder})
	if err != nil || len(audioOnly) != 1 {
		t.Fatalf("audio-only profiles = (%+v, %v)", audioOnly, err)
	}
	audioXML := string(audioOnly[0].RawXML)
	if !strings.Contains(audioXML, "<tr2:AudioEncoder") || strings.Contains(audioXML, "<tr2:VideoSource") || strings.Contains(audioXML, "<tr2:VideoEncoder") {
		t.Fatalf("audio filter produced wrong members: %s", audioXML)
	}
	analyticsOnly, err := media2Client.GetProfiles(ctx, "media2-profile", []media2.ConfigurationType{media2.ConfigurationAnalytics})
	if err != nil || len(analyticsOnly) != 1 || strings.Contains(string(analyticsOnly[0].RawXML), "<tr2:Configurations") {
		t.Fatalf("unconfigured analytics filter = (%+v, %v)", analyticsOnly, err)
	}
	reverseFilterOrder, err := media2Client.GetProfiles(ctx, "media2-profile", []media2.ConfigurationType{
		media2.ConfigurationAudioEncoder,
		media2.ConfigurationVideoEncoder,
	})
	if err != nil || len(reverseFilterOrder) != 1 {
		t.Fatalf("multi-type profiles = (%+v, %v)", reverseFilterOrder, err)
	}
	orderedXML := string(reverseFilterOrder[0].RawXML)
	videoIndex := strings.Index(orderedXML, "<tr2:VideoEncoder")
	audioIndex := strings.Index(orderedXML, "<tr2:AudioEncoder")
	if videoIndex < 0 || audioIndex < 0 || videoIndex > audioIndex {
		t.Fatalf("configuration output followed request order instead of schema order: %s", orderedXML)
	}

	configs, err := media2Client.GetVideoEncoderConfigurations(ctx, "media2-video", "media2-profile")
	if err != nil || len(configs) != 1 || configs[0].Encoding != "H265" || configs[0].Resolution.Width != 1111 || configs[0].GovLength != 47 {
		t.Fatalf("configured Media2 encoder = (%+v, %v)", configs, err)
	}

	if _, err := media1Client.GetSnapshotUri(ctx, "media1-profile"); err != nil {
		t.Fatalf("Media1 profile token rejected by Media1: %v", err)
	}
	if _, err := media1Client.GetSnapshotUri(ctx, "media2-profile"); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("Media2-only token accepted by Media1: %v", err)
	}
	if _, err := media2Client.GetSnapshotUri(ctx, "media2-profile"); err != nil {
		t.Fatalf("Media2 profile token rejected by Media2: %v", err)
	}
	if _, err := media2Client.GetSnapshotUri(ctx, "media1-profile"); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("Media1-only token accepted by Media2: %v", err)
	}
}
