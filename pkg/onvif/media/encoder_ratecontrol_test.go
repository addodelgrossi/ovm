package media_test

import (
	"context"
	"errors"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media"
)

// videoEncoderConfig builds a GetVideoEncoderConfigurationsResponse with one
// configuration whose body (after Quality, before the multicast/session tail)
// is supplied by the caller.
func videoEncoderConfig(encoding, afterQuality string) string {
	return `<trt:GetVideoEncoderConfigurationsResponse` + trtNS + ttNS + `>` +
		`<trt:Configurations token="vec0"><tt:Name>Enc</tt:Name><tt:UseCount>1</tt:UseCount>` +
		`<tt:Encoding>` + encoding + `</tt:Encoding>` +
		`<tt:Resolution><tt:Width>1280</tt:Width><tt:Height>720</tt:Height></tt:Resolution>` +
		`<tt:Quality>5</tt:Quality>` + afterQuality + validMedia1StreamingTail +
		`</trt:Configurations></trt:GetVideoEncoderConfigurationsResponse>`
}

func getVideoEncoders(t *testing.T, payload string) ([]media.VideoEncoderConfiguration, error) {
	t.Helper()
	client, _, closeFn := serveOK(t, payload)
	defer closeFn()
	return client.GetVideoEncoderConfigurations(context.Background())
}

const media1RateControl = `<tt:RateControl><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>`

func TestMedia1EncoderRateControlAndCodecDecode(t *testing.T) {
	t.Parallel()
	codec := `<tt:H264><tt:GovLength>30</tt:GovLength><tt:H264Profile>Main</tt:H264Profile></tt:H264>`
	configs, err := getVideoEncoders(t, videoEncoderConfig("H264", media1RateControl+codec))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	c := configs[0]
	if !c.HasFrameRateLimit || c.FrameRateLimit != 30 {
		t.Errorf("FrameRateLimit = %d present=%v", c.FrameRateLimit, c.HasFrameRateLimit)
	}
	if !c.HasEncodingInterval || c.EncodingInterval != 1 {
		t.Errorf("EncodingInterval = %d present=%v", c.EncodingInterval, c.HasEncodingInterval)
	}
	if !c.HasBitrateLimit || c.BitrateLimit != 4096 {
		t.Errorf("BitrateLimit = %d present=%v", c.BitrateLimit, c.HasBitrateLimit)
	}
	if !c.HasGovLength || c.GovLength != 30 {
		t.Errorf("GovLength = %d present=%v", c.GovLength, c.HasGovLength)
	}
	if !c.HasCodecProfile || c.CodecProfile != "Main" {
		t.Errorf("CodecProfile = %q present=%v", c.CodecProfile, c.HasCodecProfile)
	}
}

func TestMedia1EncoderJPEGHasNoCodecDetail(t *testing.T) {
	t.Parallel()
	// JPEG legitimately carries RateControl but no codec GOV/profile block.
	configs, err := getVideoEncoders(t, videoEncoderConfig("JPEG", media1RateControl))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	c := configs[0]
	if !c.HasFrameRateLimit || !c.HasBitrateLimit {
		t.Errorf("expected rate control present for JPEG, got %+v", c)
	}
	if c.HasGovLength || c.HasCodecProfile {
		t.Errorf("JPEG must not expose GOV/profile, got GovLength=%v Profile=%v", c.HasGovLength, c.HasCodecProfile)
	}
}

func TestMedia1EncoderRateControlOptionalAbsence(t *testing.T) {
	t.Parallel()
	configs, err := getVideoEncoders(t, videoEncoderConfig("H264", ""))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	c := configs[0]
	if c.HasFrameRateLimit || c.HasBitrateLimit || c.HasEncodingInterval || c.HasGovLength || c.HasCodecProfile {
		t.Errorf("expected no rate-control/codec detail, got %+v", c)
	}
}

func TestMedia1EncoderRejectsInvalidRateControl(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"zero fps":         `<tt:RateControl><tt:FrameRateLimit>0</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>`,
		"negative bitrate": `<tt:RateControl><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>-1</tt:BitrateLimit></tt:RateControl>`,
		"missing bitrate":  `<tt:RateControl><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval></tt:RateControl>`,
		"out-of-order":     `<tt:RateControl><tt:BitrateLimit>4096</tt:BitrateLimit><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval></tt:RateControl>`,
		"nested abuse":     `<tt:RateControl><tt:FrameRateLimit><tt:X>1</tt:X></tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>`,
		"non-numeric fps":  `<tt:RateControl><tt:FrameRateLimit>fast</tt:FrameRateLimit><tt:EncodingInterval>1</tt:EncodingInterval><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>`,
	}
	for name, rc := range cases {
		rc := rc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := getVideoEncoders(t, videoEncoderConfig("H264", rc))
			if err == nil {
				t.Fatalf("expected error for %s", name)
			}
			if !errors.Is(err, media.ErrInvalidField) && !errors.Is(err, media.ErrMissingField) && !errors.Is(err, media.ErrUnexpectedElement) {
				t.Fatalf("got %v, want a typed decode sentinel", err)
			}
		})
	}
}

func TestMedia1EncoderRejectsInvalidCodec(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"zero gov":        media1RateControl + `<tt:H264><tt:GovLength>0</tt:GovLength><tt:H264Profile>Main</tt:H264Profile></tt:H264>`,
		"invalid profile": media1RateControl + `<tt:H264><tt:GovLength>30</tt:GovLength><tt:H264Profile>Bogus</tt:H264Profile></tt:H264>`,
		"both codecs":     media1RateControl + `<tt:MPEG4><tt:GovLength>30</tt:GovLength><tt:Mpeg4Profile>SP</tt:Mpeg4Profile></tt:MPEG4><tt:H264><tt:GovLength>30</tt:GovLength><tt:H264Profile>Main</tt:H264Profile></tt:H264>`,
	}
	for name, body := range cases {
		body := body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := getVideoEncoders(t, videoEncoderConfig("H264", body))
			if err == nil {
				t.Fatalf("expected error for %s", name)
			}
			if !errors.Is(err, media.ErrInvalidField) {
				t.Fatalf("got %v, want ErrInvalidField", err)
			}
		})
	}
}

func TestMedia1EncoderMPEG4Codec(t *testing.T) {
	t.Parallel()
	codec := `<tt:MPEG4><tt:GovLength>15</tt:GovLength><tt:Mpeg4Profile>SP</tt:Mpeg4Profile></tt:MPEG4>`
	configs, err := getVideoEncoders(t, videoEncoderConfig("MPEG4", media1RateControl+codec))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	c := configs[0]
	if !c.HasGovLength || c.GovLength != 15 || !c.HasCodecProfile || c.CodecProfile != "SP" {
		t.Errorf("mpeg4 codec detail = %+v", c)
	}
}
