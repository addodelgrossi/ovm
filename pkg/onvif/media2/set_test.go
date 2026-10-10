package media2_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func sampleEncoderConfiguration() media2.VideoEncoderConfiguration {
	return media2.VideoEncoderConfiguration{
		Token: "venc_main", Name: "Main <stream>", UseCount: 1, HasUseCount: true,
		Encoding: "H264", Resolution: media2.VideoResolution{Width: 1920, Height: 1080}, HasResolution: true,
		Quality: 4.5, HasQuality: true,
		FrameRateLimit: 25, HasFrameRateLimit: true, BitrateLimit: 4096, HasBitrateLimit: true,
		ConstantBitRate: true, HasConstantBitRate: true,
		GovLength: 50, HasGovLength: true, Profile: "Main", HasProfile: true,
		Multicast: &media2.Multicast{AddressType: "IPv4", Address: "239.0.0.1", Port: 5000, TTL: 2},
	}
}

func TestSetVideoEncoderConfigurationSerializesTheTypedConfiguration(t *testing.T) {
	t.Parallel()
	rec, err := invoke(t, `<tr2:SetVideoEncoderConfigurationResponse`+tr2NS+`/>`, func(client *media2.Client) error {
		return client.SetVideoEncoderConfiguration(context.Background(), sampleEncoderConfiguration())
	})
	if err != nil {
		t.Fatalf("SetVideoEncoderConfiguration() error = %v", err)
	}
	if rec.action != media2.ActionSetVideoEncoderConfiguration {
		t.Fatalf("action = %q", rec.action)
	}
	for _, want := range []string{
		`<tr2:Configuration token="venc_main" GovLength="50" Profile="Main">`,
		`<tt:Name>Main &lt;stream&gt;</tt:Name>`,
		`<tt:Encoding>H264</tt:Encoding>`,
		`<tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution>`,
		`<tt:RateControl ConstantBitRate="true"><tt:FrameRateLimit>25</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>`,
		`<tt:Multicast><tt:Address><tt:Type>IPv4</tt:Type><tt:IPv4Address>239.0.0.1</tt:IPv4Address></tt:Address><tt:Port>5000</tt:Port><tt:TTL>2</tt:TTL><tt:AutoStart>false</tt:AutoStart></tt:Multicast>`,
		`<tt:Quality>4.5</tt:Quality></tr2:Configuration>`,
	} {
		if !strings.Contains(rec.body, want) {
			t.Errorf("request body missing %s\n%s", want, rec.body)
		}
	}
}

func TestSetVideoEncoderConfigurationRejectsInvalidInputBeforeTheNetwork(t *testing.T) {
	t.Parallel()
	mutations := map[string]func(*media2.VideoEncoderConfiguration){
		"empty token":     func(c *media2.VideoEncoderConfiguration) { c.Token = "" },
		"control name":    func(c *media2.VideoEncoderConfiguration) { c.Name = "a\x01b" },
		"long name":       func(c *media2.VideoEncoderConfiguration) { c.Name = strings.Repeat("n", 65) },
		"empty encoding":  func(c *media2.VideoEncoderConfiguration) { c.Encoding = "" },
		"zero width":      func(c *media2.VideoEncoderConfiguration) { c.Resolution.Width = 0 },
		"negative fps":    func(c *media2.VideoEncoderConfiguration) { c.FrameRateLimit = -1 },
		"half rate":       func(c *media2.VideoEncoderConfiguration) { c.HasBitrateLimit = false },
		"bad profile":     func(c *media2.VideoEncoderConfiguration) { c.Profile = "Turbo" },
		"bad gov":         func(c *media2.VideoEncoderConfiguration) { c.GovLength = 0 },
		"bad multicast":   func(c *media2.VideoEncoderConfiguration) { c.Multicast.Address = "not-an-ip" },
		"mismatched ipv6": func(c *media2.VideoEncoderConfiguration) { c.Multicast.Address = "ff02::1" },
	}
	for name, mutate := range mutations {
		config := sampleEncoderConfiguration()
		multicast := *config.Multicast
		config.Multicast = &multicast
		mutate(&config)
		rec, err := invoke(t, `<tr2:SetVideoEncoderConfigurationResponse`+tr2NS+`/>`, func(client *media2.Client) error {
			return client.SetVideoEncoderConfiguration(context.Background(), config)
		})
		if !errors.Is(err, media2.ErrInvalidInput) || rec.body != "" {
			t.Errorf("%s: error = %v, request sent = %v", name, err, rec.body != "")
		}
	}
}

func TestSetVideoEncoderConfigurationSurfacesFaultsAndMalformedResponses(t *testing.T) {
	t.Parallel()
	client, _, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", 500,
		soap12Envelope(soap12Fault("env:Sender", "ter:InvalidArgVal", "ConfigModify")))
	defer closeFn()
	if err := client.SetVideoEncoderConfiguration(context.Background(), sampleEncoderConfiguration()); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("fault error = %v, want soap.ErrInvalidArgVal", err)
	}
	for name, payload := range map[string]string{
		"wrong root":     `<tr2:GetProfilesResponse` + tr2NS + `/>`,
		"unexpected kid": `<tr2:SetVideoEncoderConfigurationResponse` + tr2NS + `><tr2:Extra/></tr2:SetVideoEncoderConfigurationResponse>`,
	} {
		_, err := invoke(t, payload, func(client *media2.Client) error {
			return client.SetVideoEncoderConfiguration(context.Background(), sampleEncoderConfiguration())
		})
		if err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEncoderOptionsAndMulticastDecodeAttributes(t *testing.T) {
	t.Parallel()
	options := `<tr2:GetVideoEncoderConfigurationOptionsResponse` + tr2NS + ttNS + `>` +
		`<tr2:Options GovLengthRange="1 300" FrameRatesSupported="30 25 15 5" ProfilesSupported="Main High" ConstantBitRateSupported="true">` +
		`<tt:Encoding>H265</tt:Encoding><tt:QualityRange><tt:Min>0</tt:Min><tt:Max>10</tt:Max></tt:QualityRange>` +
		`<tt:ResolutionsAvailable><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:ResolutionsAvailable>` +
		`<tt:BitrateRange><tt:Min>64</tt:Min><tt:Max>16384</tt:Max></tt:BitrateRange></tr2:Options>` +
		`</tr2:GetVideoEncoderConfigurationOptionsResponse>`
	var result media2.VideoEncoderConfigurationOptions
	if _, err := invoke(t, options, func(client *media2.Client) error {
		var err error
		result, err = client.GetVideoEncoderConfigurationOptions(context.Background(), "", "")
		return err
	}); err != nil {
		t.Fatalf("GetVideoEncoderConfigurationOptions() error = %v", err)
	}
	option := result.Options[0]
	if len(option.FrameRatesSupported) != 4 || option.FrameRatesSupported[1] != 25 ||
		len(option.ProfilesSupported) != 2 || !option.ConstantBitRateSupported {
		t.Fatalf("option attributes = %+v", option)
	}
	bad := strings.Replace(options, `FrameRatesSupported="30 25 15 5"`, `FrameRatesSupported="30 abc"`, 1)
	if _, err := invoke(t, bad, func(client *media2.Client) error {
		_, err := client.GetVideoEncoderConfigurationOptions(context.Background(), "", "")
		return err
	}); !errors.Is(err, media2.ErrInvalidField) {
		t.Fatalf("malformed FrameRatesSupported error = %v", err)
	}

	configs := `<tr2:GetVideoEncoderConfigurationsResponse` + tr2NS + ttNS + `>` +
		`<tr2:Configurations token="venc"><tt:Name>n</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>H264</tt:Encoding>` +
		`<tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution>` +
		`<tt:Multicast><tt:Address><tt:Type>IPv4</tt:Type><tt:IPv4Address>239.1.1.1</tt:IPv4Address></tt:Address>` +
		`<tt:Port>6000</tt:Port><tt:TTL>3</tt:TTL><tt:AutoStart>true</tt:AutoStart></tt:Multicast>` +
		`<tt:Quality>3</tt:Quality></tr2:Configurations></tr2:GetVideoEncoderConfigurationsResponse>`
	var decoded []media2.VideoEncoderConfiguration
	if _, err := invoke(t, configs, func(client *media2.Client) error {
		var err error
		decoded, err = client.GetVideoEncoderConfigurations(context.Background(), "", "")
		return err
	}); err != nil {
		t.Fatalf("GetVideoEncoderConfigurations() error = %v", err)
	}
	if m := decoded[0].Multicast; m == nil || m.Address != "239.1.1.1" || m.Port != 6000 || m.TTL != 3 || !m.AutoStart {
		t.Fatalf("multicast = %+v", decoded[0].Multicast)
	}
}
