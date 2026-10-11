package media2_test

import (
	"context"
	"errors"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
)

// videoEncoder2Config builds a GetVideoEncoderConfigurationsResponse with one
// configuration. attrs is appended to the Configurations start tag (for the
// unqualified GovLength/Profile attributes) and afterResolution is inserted
// between Resolution and Quality (for the RateControl block).
func videoEncoder2Config(attrs, afterResolution string) string {
	return `<tr2:GetVideoEncoderConfigurationsResponse` + tr2NS + ttNS + `>` +
		`<tr2:Configurations token="vec0"` + attrs + `>` +
		`<tt:Name>Enc</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>H264</tt:Encoding>` +
		`<tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution>` +
		afterResolution +
		`<tt:Quality>5</tt:Quality></tr2:Configurations></tr2:GetVideoEncoderConfigurationsResponse>`
}

func getMedia2Encoders(t *testing.T, payload string) ([]media2.VideoEncoderConfiguration, error) {
	t.Helper()
	client, _, closeFn := serveOK(t, payload)
	defer closeFn()
	return client.GetVideoEncoderConfigurations(context.Background(), "", "")
}

const media2RateControl = `<tt:RateControl><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit><tt:AverageBitRate>3072</tt:AverageBitRate></tt:RateControl>`

func TestMedia2EncoderRateControlAndAttributesDecode(t *testing.T) {
	t.Parallel()
	configs, err := getMedia2Encoders(t, videoEncoder2Config(` GovLength="30" Profile="Main"`, media2RateControl))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	c := configs[0]
	if !c.HasFrameRateLimit || c.FrameRateLimit != 30 {
		t.Errorf("FrameRateLimit = %v present=%v", c.FrameRateLimit, c.HasFrameRateLimit)
	}
	if !c.HasBitrateLimit || c.BitrateLimit != 4096 {
		t.Errorf("BitrateLimit = %d present=%v", c.BitrateLimit, c.HasBitrateLimit)
	}
	if !c.HasAverageBitRate || c.AverageBitRate != 3072 {
		t.Errorf("AverageBitRate = %d present=%v", c.AverageBitRate, c.HasAverageBitRate)
	}
	if !c.HasGovLength || c.GovLength != 30 {
		t.Errorf("GovLength = %d present=%v", c.GovLength, c.HasGovLength)
	}
	if !c.HasProfile || c.Profile != "Main" {
		t.Errorf("Profile = %q present=%v", c.Profile, c.HasProfile)
	}
}

func TestMedia2EncoderRateControlFloatFrameRate(t *testing.T) {
	t.Parallel()
	rc := `<tt:RateControl><tt:FrameRateLimit>29.97</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>`
	configs, err := getMedia2Encoders(t, videoEncoder2Config("", rc))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	c := configs[0]
	if !c.HasFrameRateLimit || c.FrameRateLimit != 29.97 {
		t.Errorf("float FrameRateLimit = %v present=%v", c.FrameRateLimit, c.HasFrameRateLimit)
	}
	if c.HasAverageBitRate {
		t.Errorf("AverageBitRate must be absent, got %d", c.AverageBitRate)
	}
}

func TestMedia2EncoderOptionalAbsence(t *testing.T) {
	t.Parallel()
	configs, err := getMedia2Encoders(t, videoEncoder2Config("", ""))
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	c := configs[0]
	if c.HasFrameRateLimit || c.HasBitrateLimit || c.HasAverageBitRate || c.HasGovLength || c.HasProfile {
		t.Errorf("expected no configured rate-control/attributes, got %+v", c)
	}
}

func TestMedia2EncoderRejectsInvalidRateControl(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"zero fps":         `<tt:RateControl><tt:FrameRateLimit>0</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>`,
		"zero bitrate":     `<tt:RateControl><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:BitrateLimit>0</tt:BitrateLimit></tt:RateControl>`,
		"negative average": `<tt:RateControl><tt:FrameRateLimit>30</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit><tt:AverageBitRate>-1</tt:AverageBitRate></tt:RateControl>`,
		"missing fps":      `<tt:RateControl><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>`,
		"infinite fps":     `<tt:RateControl><tt:FrameRateLimit>+Inf</tt:FrameRateLimit><tt:BitrateLimit>4096</tt:BitrateLimit></tt:RateControl>`,
		"out-of-order":     `<tt:RateControl><tt:BitrateLimit>4096</tt:BitrateLimit><tt:FrameRateLimit>30</tt:FrameRateLimit></tt:RateControl>`,
	}
	for name, rc := range cases {
		rc := rc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := getMedia2Encoders(t, videoEncoder2Config("", rc))
			if err == nil {
				t.Fatalf("expected error for %s", name)
			}
			if !errors.Is(err, media2.ErrInvalidField) && !errors.Is(err, media2.ErrMissingField) && !errors.Is(err, media2.ErrUnexpectedElement) {
				t.Fatalf("got %v, want a typed decode sentinel", err)
			}
		})
	}
}

func TestMedia2EncoderRejectsInvalidAttributes(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"zero gov":        ` GovLength="0"`,
		"non-numeric gov": ` GovLength="many"`,
		"invalid profile": ` Profile="Bogus"`,
	}
	for name, attrs := range cases {
		attrs := attrs
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := getMedia2Encoders(t, videoEncoder2Config(attrs, media2RateControl))
			if !errors.Is(err, media2.ErrInvalidField) {
				t.Fatalf("got %v, want ErrInvalidField", err)
			}
		})
	}
}

func TestMedia2EncoderQualifiedAttributeDoesNotSatisfyUnqualified(t *testing.T) {
	t.Parallel()
	// A lookalike qualified attribute (bound to the tt namespace) must not be
	// read as the unqualified GovLength/Profile field. The configuration then
	// simply carries no configured GovLength/Profile.
	payload := `<tr2:GetVideoEncoderConfigurationsResponse` + tr2NS + ttNS + `>` +
		`<tr2:Configurations token="vec0" tt:GovLength="30" tt:Profile="Main">` +
		`<tt:Name>Enc</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>H264</tt:Encoding>` +
		`<tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution>` +
		`<tt:Quality>5</tt:Quality></tr2:Configurations></tr2:GetVideoEncoderConfigurationsResponse>`
	configs, err := getMedia2Encoders(t, payload)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	c := configs[0]
	if c.HasGovLength || c.HasProfile {
		t.Errorf("qualified lookalike attributes must not satisfy unqualified fields, got GovLength=%v Profile=%v", c.HasGovLength, c.HasProfile)
	}
}
