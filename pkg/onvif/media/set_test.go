package media_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func sampleMedia1Encoder() media.VideoEncoderConfiguration {
	return media.VideoEncoderConfiguration{ //nolint:gosec // synthetic ONVIF configuration token, not a credential
		Token: "venc_main", Name: "main", UseCount: 1, Encoding: media.VideoEncodingH264,
		Resolution: media.VideoResolution{Width: 1280, Height: 720}, HasResolution: true,
		Quality: 5, HasQuality: true,
		FrameRateLimit: 25, HasFrameRateLimit: true, EncodingInterval: 1, HasEncodingInterval: true,
		BitrateLimit: 2048, HasBitrateLimit: true,
		GovLength: 50, HasGovLength: true, CodecProfile: "Main", HasCodecProfile: true,
		Multicast:      media.Multicast{AddressType: "IPv4", Address: "0.0.0.0", Port: 0, TTL: 1},
		SessionTimeout: "PT60S",
	}
}

func TestMedia1SetVideoEncoderConfigurationSerializesRequiredFields(t *testing.T) {
	t.Parallel()
	rec, err := invoke(t, `<trt:SetVideoEncoderConfigurationResponse`+trtNS+`/>`, func(client *media.Client) error {
		return client.SetVideoEncoderConfiguration(context.Background(), sampleMedia1Encoder(), true)
	})
	if err != nil {
		t.Fatalf("SetVideoEncoderConfiguration() error = %v", err)
	}
	if rec.action != media.ActionSetVideoEncoderConfiguration {
		t.Fatalf("action = %q", rec.action)
	}
	for _, want := range []string{
		`<trt:Configuration token="venc_main"><tt:Name>main</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>H264</tt:Encoding>`,
		`<tt:Quality>5</tt:Quality><tt:RateControl><tt:FrameRateLimit>25</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>2048</tt:BitrateLimit></tt:RateControl>`,
		`<tt:H264><tt:GovLength>50</tt:GovLength><tt:H264Profile>Main</tt:H264Profile></tt:H264>`,
		`<tt:Multicast><tt:Address><tt:Type>IPv4</tt:Type><tt:IPv4Address>0.0.0.0</tt:IPv4Address></tt:Address>`,
		`<tt:SessionTimeout>PT60S</tt:SessionTimeout></trt:Configuration><trt:ForcePersistence>true</trt:ForcePersistence>`,
	} {
		if !strings.Contains(rec.body, want) {
			t.Errorf("request body missing %s\n%s", want, rec.body)
		}
	}
}

func TestMedia1SetVideoEncoderConfigurationValidatesAndSurfacesFaults(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*media.VideoEncoderConfiguration){
		"encoding":    func(c *media.VideoEncoderConfiguration) { c.Encoding = "H265" },
		"timeout":     func(c *media.VideoEncoderConfiguration) { c.SessionTimeout = "" },
		"multicast":   func(c *media.VideoEncoderConfiguration) { c.Multicast.Address = "" },
		"codec block": func(c *media.VideoEncoderConfiguration) { c.CodecProfile = "Turbo" },
		"rate":        func(c *media.VideoEncoderConfiguration) { c.BitrateLimit = 0 },
	} {
		config := sampleMedia1Encoder()
		mutate(&config)
		rec, err := invoke(t, `<trt:SetVideoEncoderConfigurationResponse`+trtNS+`/>`, func(client *media.Client) error {
			return client.SetVideoEncoderConfiguration(context.Background(), config, false)
		})
		if !errors.Is(err, media.ErrInvalidInput) || rec.body != "" {
			t.Errorf("%s: error = %v, request sent = %v", name, err, rec.body != "")
		}
	}
	client, _, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", 500,
		soap12Envelope(soap12Fault("env:Sender", "ter:InvalidArgVal", "ConfigModify")))
	defer closeFn()
	if err := client.SetVideoEncoderConfiguration(context.Background(), sampleMedia1Encoder(), false); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("fault error = %v", err)
	}
}

func TestMedia1EncoderConfigurationDecodesStreamingFields(t *testing.T) {
	t.Parallel()
	payload := `<trt:GetVideoEncoderConfigurationsResponse` + trtNS + ttNS + `><trt:Configurations token="venc">` +
		`<tt:Name>n</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>H264</tt:Encoding>` +
		`<tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution><tt:Quality>3</tt:Quality>` +
		validMedia1StreamingTail + `</trt:Configurations></trt:GetVideoEncoderConfigurationsResponse>`
	var configs []media.VideoEncoderConfiguration
	if _, err := invoke(t, payload, func(client *media.Client) error {
		var err error
		configs, err = client.GetVideoEncoderConfigurations(context.Background())
		return err
	}); err != nil {
		t.Fatalf("GetVideoEncoderConfigurations() error = %v", err)
	}
	if got := configs[0]; got.Multicast.AddressType != "IPv4" || got.Multicast.Address != "0.0.0.0" || got.Multicast.TTL != 1 || got.SessionTimeout != "PT60S" {
		t.Fatalf("streaming fields = %+v %q", got.Multicast, got.SessionTimeout)
	}
}
