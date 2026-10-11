package fakedevice_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

// baseValidInventory returns an inventory whose profiles carry no encoder
// cross-references, so a test can override a single encoder slice with an
// invalid entry and reach that slice's validation without tripping an unrelated
// reference check first.
func baseValidInventory() fakedevice.MediaInventory {
	return fakedevice.MediaInventory{
		Profiles: []fakedevice.MediaProfileData{{Token: "p0", Name: "main"}},
	}
}

func TestFakeMedia2EncoderConfiguredFieldValidation(t *testing.T) {
	t.Parallel()
	valid := fakedevice.VideoEncoder2ConfigurationData{
		Token: "venc", Name: "VE", Encoding: "H264", Width: 1920, Height: 1080, Quality: 5,
		FrameRateLimit: 30, BitrateLimit: 4096, AverageBitRate: 3072, GovLength: 30, Profile: "Main", UseCount: 1,
	}

	cases := map[string]func(e *fakedevice.VideoEncoder2ConfigurationData){
		"zero fps":         func(e *fakedevice.VideoEncoder2ConfigurationData) { e.FrameRateLimit = 0 },
		"negative fps":     func(e *fakedevice.VideoEncoder2ConfigurationData) { e.FrameRateLimit = -1 },
		"zero bitrate":     func(e *fakedevice.VideoEncoder2ConfigurationData) { e.BitrateLimit = 0 },
		"negative average": func(e *fakedevice.VideoEncoder2ConfigurationData) { e.AverageBitRate = -1 },
		"zero gov":         func(e *fakedevice.VideoEncoder2ConfigurationData) { e.GovLength = 0 },
		"invalid profile":  func(e *fakedevice.VideoEncoder2ConfigurationData) { e.Profile = "Nope" },
		"empty profile":    func(e *fakedevice.VideoEncoder2ConfigurationData) { e.Profile = "" },
	}
	for name, mutate := range cases {
		mutate := mutate
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			encoder := valid
			mutate(&encoder)
			inventory := baseValidInventory()
			inventory.Media2VideoEncoders = []fakedevice.VideoEncoder2ConfigurationData{encoder}
			if _, err := fakedevice.New(fakedevice.Config{MediaInventory: inventory}); err == nil {
				t.Fatalf("expected rejection for %s", name)
			}
		})
	}

	// The fully valid encoder must be accepted.
	inventory := baseValidInventory()
	inventory.Media2VideoEncoders = []fakedevice.VideoEncoder2ConfigurationData{valid}
	if _, err := fakedevice.New(fakedevice.Config{MediaInventory: inventory}); err != nil {
		t.Fatalf("valid Media2 encoder rejected: %v", err)
	}
}

func TestFakeProfileVideoSourceConfigurationTokenValidation(t *testing.T) {
	t.Parallel()
	secret := "profile-source-config-secret"
	inventory := fakedevice.MediaInventory{
		Profiles: []fakedevice.MediaProfileData{{
			Token: "p0", Name: "main",
			VideoSourceConfigurationToken: secret + strings.Repeat("x", 64),
		}},
	}
	_, err := fakedevice.New(fakedevice.Config{MediaInventory: inventory})
	if err == nil {
		t.Fatal("oversize VideoSourceConfigurationToken accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked configuration value: %v", err)
	}
}

func validIntegerMediaInventory() fakedevice.MediaInventory {
	return fakedevice.MediaInventory{
		Profiles:     []fakedevice.MediaProfileData{{Token: "p0", Name: "main"}},
		VideoSources: []fakedevice.VideoSourceData{{Token: "source", Framerate: 30, Width: 1920, Height: 1080}},
		VideoEncoders: []fakedevice.VideoEncoderConfigurationData{{
			Token: "video1", Name: "Video1", Encoding: "H264", Width: 1920, Height: 1080,
			Quality: 5, FrameRateLimit: 30, BitrateLimit: 4096, GovLength: 30, UseCount: 1,
		}},
		VideoEncoderOptions: fakedevice.VideoEncoderOptionsData{
			Encoding: "H264", QualityMin: 0, QualityMax: 10,
			WidthMin: 1, WidthMax: 1920, HeightMin: 1, HeightMax: 1080,
			FrameRateMin: 1, FrameRateMax: 30, BitrateMin: 1, BitrateMax: 4096,
			GovLengthMin: 1, GovLengthMax: 60,
		},
		AudioEncoders: []fakedevice.AudioEncoderConfigurationData{{
			Token: "audio1", Name: "Audio1", Encoding: "G711", Bitrate: 64, SampleRate: 8, UseCount: 1,
		}},
		Media2VideoEncoders: []fakedevice.VideoEncoder2ConfigurationData{{
			Token: "video2", Name: "Video2", Encoding: "H265", Width: 1920, Height: 1080,
			Quality: 5, FrameRateLimit: 30, BitrateLimit: 4096, AverageBitRate: 3072,
			GovLength: 30, Profile: "Main", UseCount: 1,
		}},
		Media2VideoEncoderOptions: fakedevice.VideoEncoder2OptionsData{
			Encoding: "H265", QualityMin: 0, QualityMax: 10,
			WidthMin: 1, WidthMax: 1920, HeightMin: 1, HeightMax: 1080,
			BitrateMin: 1, BitrateMax: 4096, GovLengthMin: 1, GovLengthMax: 60,
		},
		Media2AudioEncoders: []fakedevice.AudioEncoder2ConfigurationData{{
			Token: "audio2", Name: "Audio2", Encoding: "PCMU", Bitrate: 64, SampleRate: 8, UseCount: 1,
		}},
	}
}

func validIntegerFakeConfig() fakedevice.Config {
	return fakedevice.Config{
		Inventory: fakedevice.Inventory{
			Services: []fakedevice.ServiceData{{
				Namespace: "urn:synthetic", XAddr: "http://device.invalid/service", VersionMajor: 1, VersionMinor: 0,
			}},
			NetworkInterfaces: []fakedevice.NetworkInterfaceData{{
				Token: "eth0", Enabled: true, Name: "eth0", HwAddress: "00:00:5E:00:53:01",
				MTU: 1500, IPv4Enabled: true, IPv4Address: "203.0.113.10", IPv4Prefix: 24,
			}},
		},
		MediaInventory: validIntegerMediaInventory(),
	}
}

func TestFakeConfigAcceptsXSIntBoundaries(t *testing.T) {
	t.Parallel()
	const (
		minimum = -2147483648
		maximum = 2147483647
	)
	config := validIntegerFakeConfig()
	config.Inventory.Services[0].VersionMajor = minimum
	config.Inventory.Services[0].VersionMinor = maximum
	config.Inventory.NetworkInterfaces[0].MTU = maximum
	config.Inventory.NetworkInterfaces[0].IPv4Prefix = 32

	mediaInventory := &config.MediaInventory
	mediaInventory.VideoSources[0].Width = maximum
	mediaInventory.VideoSources[0].Height = maximum
	video1 := &mediaInventory.VideoEncoders[0]
	video1.Width, video1.Height = maximum, maximum
	video1.FrameRateLimit, video1.BitrateLimit = maximum, maximum
	video1.GovLength, video1.UseCount = maximum, maximum
	audio1 := &mediaInventory.AudioEncoders[0]
	audio1.Bitrate, audio1.SampleRate, audio1.UseCount = maximum, maximum, maximum
	options1 := &mediaInventory.VideoEncoderOptions
	options1.QualityMax = maximum
	options1.WidthMax, options1.HeightMax = maximum, maximum
	options1.FrameRateMax, options1.BitrateMax, options1.GovLengthMax = maximum, maximum, maximum
	video2 := &mediaInventory.Media2VideoEncoders[0]
	video2.Width, video2.Height = maximum, maximum
	video2.BitrateLimit, video2.AverageBitRate = maximum, maximum
	video2.GovLength, video2.UseCount = maximum, maximum
	audio2 := &mediaInventory.Media2AudioEncoders[0]
	audio2.Bitrate, audio2.SampleRate, audio2.UseCount = maximum, maximum, maximum
	options2 := &mediaInventory.Media2VideoEncoderOptions
	options2.WidthMax, options2.HeightMax = maximum, maximum
	options2.BitrateMax, options2.GovLengthMax = maximum, maximum

	if _, err := fakedevice.New(config); err != nil {
		t.Fatalf("valid xs:int boundaries rejected: %v", err)
	}
}

func TestFakeConfigRejectsXSIntOverflows(t *testing.T) {
	t.Parallel()
	if strconv.IntSize < 64 {
		t.Skip("host int cannot represent values outside xs:int")
	}
	above := int(int64(2147483647) + 1)
	below := int(int64(-2147483648) - 1)
	cases := []struct {
		name   string
		mutate func(*fakedevice.Config)
	}{
		{name: "service version above maximum", mutate: func(c *fakedevice.Config) { c.Inventory.Services[0].VersionMajor = above }},
		{name: "service version below minimum", mutate: func(c *fakedevice.Config) { c.Inventory.Services[0].VersionMinor = below }},
		{name: "network MTU above maximum", mutate: func(c *fakedevice.Config) { c.Inventory.NetworkInterfaces[0].MTU = above }},
		{name: "video source width above maximum", mutate: func(c *fakedevice.Config) { c.MediaInventory.VideoSources[0].Width = above }},
		{name: "Media1 video UseCount above maximum", mutate: func(c *fakedevice.Config) { c.MediaInventory.VideoEncoders[0].UseCount = above }},
		{name: "Media1 audio bitrate above maximum", mutate: func(c *fakedevice.Config) { c.MediaInventory.AudioEncoders[0].Bitrate = above }},
		{name: "Media1 option range above maximum", mutate: func(c *fakedevice.Config) { c.MediaInventory.VideoEncoderOptions.QualityMax = above }},
		{name: "Media2 average bitrate above maximum", mutate: func(c *fakedevice.Config) { c.MediaInventory.Media2VideoEncoders[0].AverageBitRate = above }},
		{name: "Media2 audio sample rate above maximum", mutate: func(c *fakedevice.Config) { c.MediaInventory.Media2AudioEncoders[0].SampleRate = above }},
		{name: "Media2 GovLengthRange above maximum", mutate: func(c *fakedevice.Config) { c.MediaInventory.Media2VideoEncoderOptions.GovLengthMax = above }},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			config := validIntegerFakeConfig()
			testCase.mutate(&config)
			if _, err := fakedevice.New(config); err == nil {
				t.Fatal("xs:int overflow was accepted")
			}
		})
	}
}

func TestFakeConfigRetainsIntegerFieldSemantics(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		mutate func(*fakedevice.Config)
	}{
		{name: "negative optional MTU", mutate: func(c *fakedevice.Config) { c.Inventory.NetworkInterfaces[0].MTU = -1 }},
		{name: "negative IPv4 prefix", mutate: func(c *fakedevice.Config) { c.Inventory.NetworkInterfaces[0].IPv4Prefix = -1 }},
		{name: "IPv4 prefix above 32", mutate: func(c *fakedevice.Config) { c.Inventory.NetworkInterfaces[0].IPv4Prefix = 33 }},
		{name: "negative Media1 UseCount", mutate: func(c *fakedevice.Config) { c.MediaInventory.VideoEncoders[0].UseCount = -1 }},
		{name: "zero Media2 bitrate", mutate: func(c *fakedevice.Config) { c.MediaInventory.Media2VideoEncoders[0].BitrateLimit = 0 }},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			config := validIntegerFakeConfig()
			testCase.mutate(&config)
			if _, err := fakedevice.New(config); err == nil {
				t.Fatal("field-specific integer constraint was not enforced")
			}
		})
	}
}
