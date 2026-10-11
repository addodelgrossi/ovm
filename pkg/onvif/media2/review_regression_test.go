package media2_test

import (
	"errors"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
)

func TestMedia2ConfigurationSetUsesPublishedWSDLSequence(t *testing.T) {
	t.Parallel()
	inOrder := `<tr2:Configurations>` +
		`<tr2:VideoSource token="vs"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="640" height="480"></tt:Bounds></tr2:VideoSource>` +
		`<tr2:AudioSource token="as"></tr2:AudioSource>` +
		`<tr2:VideoEncoder token="ve"></tr2:VideoEncoder>` +
		`<tr2:AudioEncoder token="ae"></tr2:AudioEncoder>` +
		`<tr2:Analytics token="analytics"></tr2:Analytics>` +
		`<tr2:PTZ token="ptz"></tr2:PTZ>` +
		`<tr2:Metadata token="metadata"></tr2:Metadata>` +
		`<tr2:AudioOutput token="output"></tr2:AudioOutput>` +
		`<tr2:AudioDecoder token="decoder"></tr2:AudioDecoder>` +
		`<tr2:Receiver token="receiver"></tr2:Receiver>` +
		`</tr2:Configurations>`
	if _, err := getMedia2Profiles(t, media2Profile(inOrder)); err != nil {
		t.Fatalf("published ConfigurationSet order rejected: %v", err)
	}

	ptzBeforeAnalytics := `<tr2:Configurations><tr2:PTZ token="ptz"></tr2:PTZ><tr2:Analytics token="analytics"></tr2:Analytics></tr2:Configurations>`
	if _, err := getMedia2Profiles(t, media2Profile(ptzBeforeAnalytics)); err == nil {
		t.Fatal("PTZ before Analytics was accepted")
	}

	videoSourceModeAsMember := `<tr2:Configurations><tr2:VideoSourceMode token="mode"></tr2:VideoSourceMode><tr2:Analytics token="analytics"></tr2:Analytics></tr2:Configurations>`
	if _, err := getMedia2Profiles(t, media2Profile(videoSourceModeAsMember)); err == nil {
		t.Fatal("VideoSourceMode was accepted as a declared ConfigurationSet member")
	}
}

func TestMedia2ConstantBitRateIsOptionalUnqualifiedAttribute(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name  string
		value string
		want  bool
	}{
		{"true", "true", true},
		{"false", "false", false},
		{"numeric true", "1", true},
		{"numeric false", "0", false},
	} {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			rateControl := `<tt:RateControl ConstantBitRate="` + testCase.value + `"><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>`
			configs, err := getMedia2Encoders(t, videoEncoder2Config("", rateControl))
			if err != nil {
				t.Fatalf("decode error = %v", err)
			}
			if !configs[0].HasConstantBitRate || configs[0].ConstantBitRate != testCase.want {
				t.Fatalf("ConstantBitRate = %v present=%v, want %v present", configs[0].ConstantBitRate, configs[0].HasConstantBitRate, testCase.want)
			}
		})
	}

	invalid := `<tt:RateControl ConstantBitRate="sometimes"><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>`
	if _, err := getMedia2Encoders(t, videoEncoder2Config("", invalid)); !errors.Is(err, media2.ErrInvalidField) {
		t.Fatalf("invalid ConstantBitRate error = %v, want ErrInvalidField", err)
	}

	qualified := `<tt:RateControl tt:ConstantBitRate="true"><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>`
	configs, err := getMedia2Encoders(t, videoEncoder2Config("", qualified))
	if err != nil {
		t.Fatalf("qualified lookalike decode error = %v", err)
	}
	if configs[0].HasConstantBitRate {
		t.Fatal("qualified lookalike attribute satisfied unqualified ConstantBitRate")
	}

	child := `<tt:RateControl><tt:ConstantBitRate>true</tt:ConstantBitRate><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>`
	if _, err := getMedia2Encoders(t, videoEncoder2Config("", child)); err == nil {
		t.Fatal("ConstantBitRate child was accepted in the XSD attribute position")
	}
}

func TestMedia2DuplicateProfileAndConfigurationTokensRejected(t *testing.T) {
	t.Parallel()
	validProfile := `<tr2:Profiles token="duplicate"><tr2:Name>valid</tr2:Name></tr2:Profiles>`
	invalidProfile := `<tr2:Profiles token="duplicate"></tr2:Profiles>`
	validVideo := `<tr2:Configurations token="duplicate"><tt:Name>valid</tt:Name><tt:UseCount>1</tt:UseCount>` +
		`<tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>640</tt:Width><tt:Height>480</tt:Height></tt:Resolution><tt:Quality>5</tt:Quality></tr2:Configurations>`
	invalidVideo := `<tr2:Configurations token="duplicate"></tr2:Configurations>`
	validAudio := `<tr2:Configurations token="duplicate"><tt:Name>valid</tt:Name><tt:UseCount>1</tt:UseCount>` +
		`<tt:Encoding>PCMU</tt:Encoding><tt:Bitrate>64</tt:Bitrate><tt:SampleRate>8</tt:SampleRate></tr2:Configurations>`
	invalidAudio := `<tr2:Configurations token="duplicate"></tr2:Configurations>`

	cases := []struct {
		name       string
		root       string
		first      string
		second     string
		invokeCall func(*media2.Client) error
	}{
		{"profiles", "GetProfilesResponse", validProfile, invalidProfile, callProfiles},
		{"video configurations", "GetVideoEncoderConfigurationsResponse", validVideo, invalidVideo, callVideoEncoderConfigurations},
		{"audio configurations", "GetAudioEncoderConfigurationsResponse", validAudio, invalidAudio, callAudioEncoderConfigurations},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			for _, entries := range [][2]string{{testCase.first, testCase.second}, {testCase.second, testCase.first}} {
				payload := `<tr2:` + testCase.root + tr2NS + ttNS + `>` + entries[0] + entries[1] + `</tr2:` + testCase.root + `>`
				_, err := invoke(t, payload, testCase.invokeCall)
				if !errors.Is(err, media2.ErrInvalidField) {
					t.Fatalf("duplicate tokens in wire order %#v: error = %v, want ErrInvalidField", entries, err)
				}
			}
		})
	}
}
