package media2_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
)

// media2Profile wraps the given inner content (the tr2:Configurations element,
// if any) in a single-profile GetProfilesResponse.
func media2Profile(inner string) string {
	return `<tr2:GetProfilesResponse` + tr2NS + ttNS + `>` +
		`<tr2:Profiles token="profile_main" fixed="true"><tr2:Name>main</tr2:Name>` +
		inner +
		`</tr2:Profiles></tr2:GetProfilesResponse>`
}

const media2ConfigurationSet = `<tr2:Configurations>` +
	`<tr2:VideoSource token="vsrccfg0"><tt:Name>vs</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>vsrc0</tt:SourceToken><tt:Bounds x="0" y="0" width="1920" height="1080"></tt:Bounds></tr2:VideoSource>` +
	`<tr2:VideoEncoder token="venc_main"><tt:Name>ve</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution><tt:Quality>5</tt:Quality></tr2:VideoEncoder>` +
	`<tr2:AudioEncoder token="aenc0"><tt:Name>ae</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>PCMU</tt:Encoding><tt:Bitrate>64</tt:Bitrate><tt:SampleRate>8</tt:SampleRate></tr2:AudioEncoder>` +
	`</tr2:Configurations>`

func getMedia2Profiles(t *testing.T, payload string) ([]media2.Profile, error) {
	t.Helper()
	client, _, closeFn := serveOK(t, payload)
	defer closeFn()
	// Use the literal independently of the production constant so these
	// response tests also pin the WSDL request semantics.
	return client.GetProfiles(context.Background(), "", []media2.ConfigurationType{"All"})
}

func TestMedia2ProfileAssociationsDecode(t *testing.T) {
	t.Parallel()
	profiles, err := getMedia2Profiles(t, media2Profile(media2ConfigurationSet))
	if err != nil {
		t.Fatalf("GetProfiles() error = %v", err)
	}
	a := profiles[0].Associations
	if !a.HasVideoSourceConfiguration || a.VideoSourceConfigurationToken != "vsrccfg0" {
		t.Errorf("video source configuration token = %q present=%v", a.VideoSourceConfigurationToken, a.HasVideoSourceConfiguration)
	}
	if !a.HasVideoSource || a.VideoSourceToken != "vsrc0" {
		t.Errorf("physical source token = %q present=%v", a.VideoSourceToken, a.HasVideoSource)
	}
	if !a.HasVideoEncoderConfiguration || a.VideoEncoderConfigurationToken != "venc_main" {
		t.Errorf("video encoder token = %q present=%v", a.VideoEncoderConfigurationToken, a.HasVideoEncoderConfiguration)
	}
	if !a.HasAudioEncoderConfiguration || a.AudioEncoderConfigurationToken != "aenc0" {
		t.Errorf("audio encoder token = %q present=%v", a.AudioEncoderConfigurationToken, a.HasAudioEncoderConfiguration)
	}
}

func TestMedia2ProfileAssociationsOptionalAbsence(t *testing.T) {
	t.Parallel()
	// No tr2:Configurations at all is valid; an empty set is also valid.
	for name, inner := range map[string]string{
		"no configurations": "",
		"empty set":         `<tr2:Configurations></tr2:Configurations>`,
	} {
		inner := inner
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			profiles, err := getMedia2Profiles(t, media2Profile(inner))
			if err != nil {
				t.Fatalf("GetProfiles() error = %v", err)
			}
			a := profiles[0].Associations
			if a.HasVideoSourceConfiguration || a.HasVideoSource || a.HasVideoEncoderConfiguration || a.HasAudioEncoderConfiguration {
				t.Errorf("expected no associations, got %+v", a)
			}
		})
	}
}

func TestMedia2ProfileAssociationsAlternatePrefix(t *testing.T) {
	t.Parallel()
	payload := `<m:GetProfilesResponse xmlns:m="http://www.onvif.org/ver20/media/wsdl" xmlns:s="http://www.onvif.org/ver10/schema">` +
		`<m:Profiles token="p"><m:Name>main</m:Name>` +
		`<m:Configurations><m:VideoEncoder token="venc_main"><s:Name>v</s:Name></m:VideoEncoder></m:Configurations>` +
		`</m:Profiles></m:GetProfilesResponse>`
	profiles, err := getMedia2Profiles(t, payload)
	if err != nil {
		t.Fatalf("GetProfiles() error = %v", err)
	}
	a := profiles[0].Associations
	if !a.HasVideoEncoderConfiguration || a.VideoEncoderConfigurationToken != "venc_main" {
		t.Errorf("alternate-prefix encoder token = %q present=%v", a.VideoEncoderConfigurationToken, a.HasVideoEncoderConfiguration)
	}
}

func TestMedia2ProfileAssociationsForeignNamespaceNotAccepted(t *testing.T) {
	t.Parallel()
	// The ConfigurationSet schema terminates in an xs:any extension point, so a
	// foreign-namespace element is tolerated as an extension. The security
	// property under test is that a foreign-namespace lookalike of
	// tr2:VideoEncoder is NEVER accepted as the typed association: the decoder
	// reads only {tr2}VideoEncoder, so the association stays absent.
	inner := `<tr2:Configurations xmlns:x="urn:evil"><x:VideoEncoder token="venc_main"></x:VideoEncoder></tr2:Configurations>`
	profiles, err := getMedia2Profiles(t, media2Profile(inner))
	if err != nil {
		t.Fatalf("GetProfiles() error = %v", err)
	}
	if profiles[0].Associations.HasVideoEncoderConfiguration {
		t.Fatal("foreign-namespace lookalike must not satisfy the VideoEncoder association")
	}
}

func TestMedia2ProfileAssociationsForeignBeforeDeclaredRejected(t *testing.T) {
	t.Parallel()
	// A foreign-namespace element appearing BEFORE a declared member breaks the
	// sequence: once an extension begins, a subsequent declared member is a
	// cardinality violation and must be rejected.
	inner := `<tr2:Configurations xmlns:x="urn:evil">` +
		`<x:Something/>` +
		`<tr2:VideoEncoder token="venc_main"><tt:Name>v</tt:Name></tr2:VideoEncoder>` +
		`</tr2:Configurations>`
	_, err := getMedia2Profiles(t, media2Profile(inner))
	if err == nil {
		t.Fatal("expected error for declared member following an extension")
	}
}

func TestMedia2ProfileAssociationsOutOfOrderRejected(t *testing.T) {
	t.Parallel()
	// AudioEncoder before VideoEncoder violates the ConfigurationSet order.
	inner := `<tr2:Configurations>` +
		`<tr2:AudioEncoder token="aenc0"><tt:Name>a</tt:Name></tr2:AudioEncoder>` +
		`<tr2:VideoEncoder token="venc_main"><tt:Name>v</tt:Name></tr2:VideoEncoder>` +
		`</tr2:Configurations>`
	_, err := getMedia2Profiles(t, media2Profile(inner))
	if err == nil {
		t.Fatal("expected error for out-of-order ConfigurationSet members")
	}
}

func TestMedia2ProfileAssociationsDuplicateRejected(t *testing.T) {
	t.Parallel()
	inner := `<tr2:Configurations>` +
		`<tr2:VideoEncoder token="a"><tt:Name>v</tt:Name></tr2:VideoEncoder>` +
		`<tr2:VideoEncoder token="b"><tt:Name>v</tt:Name></tr2:VideoEncoder>` +
		`</tr2:Configurations>`
	_, err := getMedia2Profiles(t, media2Profile(inner))
	if err == nil {
		t.Fatal("expected error for duplicate tr2:VideoEncoder")
	}
}

func TestMedia2ProfileAssociationsMissingTokenRejected(t *testing.T) {
	t.Parallel()
	inner := `<tr2:Configurations><tr2:VideoEncoder><tt:Name>v</tt:Name></tr2:VideoEncoder></tr2:Configurations>`
	_, err := getMedia2Profiles(t, media2Profile(inner))
	if !errors.Is(err, media2.ErrMissingField) {
		t.Fatalf("got %v, want ErrMissingField", err)
	}
}

func TestMedia2ProfileAssociationsSecretSafeError(t *testing.T) {
	t.Parallel()
	oversized := strings.Repeat("x", 200)
	inner := `<tr2:Configurations><tr2:VideoSource token="vsrccfg0"><tt:Name>v</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>` + oversized + `</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"></tt:Bounds></tr2:VideoSource></tr2:Configurations>`
	_, err := getMedia2Profiles(t, media2Profile(inner))
	if !errors.Is(err, media2.ErrInvalidField) {
		t.Fatalf("got %v, want ErrInvalidField", err)
	}
	if strings.Contains(err.Error(), oversized) {
		t.Errorf("error leaked offending value: %v", err)
	}
}

func TestMedia2VideoSourceConfigurationFullyValidated(t *testing.T) {
	t.Parallel()
	longValue := strings.Repeat("x", 65)
	cases := []struct {
		name   string
		member string
		want   error
	}{
		{
			name:   "missing inherited Name",
			member: `<tr2:VideoSource token="cfg"><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"/></tr2:VideoSource>`,
			want:   media2.ErrMissingField,
		},
		{
			name:   "oversized inherited Name",
			member: `<tr2:VideoSource token="cfg"><tt:Name>` + longValue + `</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"/></tr2:VideoSource>`,
			want:   media2.ErrInvalidField,
		},
		{
			name:   "missing inherited UseCount",
			member: `<tr2:VideoSource token="cfg"><tt:Name>source</tt:Name><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"/></tr2:VideoSource>`,
			want:   media2.ErrMissingField,
		},
		{
			name:   "negative UseCount",
			member: `<tr2:VideoSource token="cfg"><tt:Name>source</tt:Name><tt:UseCount>-1</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"/></tr2:VideoSource>`,
			want:   media2.ErrInvalidField,
		},
		{
			name:   "UseCount outside xs:int",
			member: `<tr2:VideoSource token="cfg"><tt:Name>source</tt:Name><tt:UseCount>2147483648</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"/></tr2:VideoSource>`,
			want:   media2.ErrInvalidField,
		},
		{
			name:   "missing token attribute",
			member: `<tr2:VideoSource><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"/></tr2:VideoSource>`,
			want:   media2.ErrMissingField,
		},
		{
			name:   "oversized token attribute",
			member: `<tr2:VideoSource token="` + longValue + `"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"/></tr2:VideoSource>`,
			want:   media2.ErrInvalidField,
		},
		{
			name:   "missing SourceToken",
			member: `<tr2:VideoSource token="cfg"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:Bounds x="0" y="0" width="1" height="1"/></tr2:VideoSource>`,
			want:   media2.ErrMissingField,
		},
		{
			name:   "missing Bounds",
			member: `<tr2:VideoSource token="cfg"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken></tr2:VideoSource>`,
			want:   media2.ErrMissingField,
		},
		{
			name:   "out of sequence",
			member: `<tr2:VideoSource token="cfg"><tt:UseCount>1</tt:UseCount><tt:Name>source</tt:Name><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"/></tr2:VideoSource>`,
			want:   media2.ErrMissingField,
		},
		{
			name:   "duplicate Bounds",
			member: `<tr2:VideoSource token="cfg"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"/><tt:Bounds x="0" y="0" width="1" height="1"/></tr2:VideoSource>`,
			want:   media2.ErrInvalidField,
		},
		{
			name:   "missing Bounds attribute",
			member: `<tr2:VideoSource token="cfg"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1"/></tr2:VideoSource>`,
			want:   media2.ErrMissingField,
		},
		{
			name:   "malformed Bounds attribute",
			member: `<tr2:VideoSource token="cfg"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="wide" height="1"/></tr2:VideoSource>`,
			want:   media2.ErrInvalidField,
		},
		{
			name:   "Bounds attribute outside xs:int",
			member: `<tr2:VideoSource token="cfg"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="2147483648" height="1"/></tr2:VideoSource>`,
			want:   media2.ErrInvalidField,
		},
		{
			name:   "undeclared Bounds attribute",
			member: `<tr2:VideoSource token="cfg"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1" depth="1"/></tr2:VideoSource>`,
			want:   media2.ErrInvalidField,
		},
		{
			name:   "nested Bounds content",
			member: `<tr2:VideoSource token="cfg"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"><tt:Unexpected/></tt:Bounds></tr2:VideoSource>`,
			want:   media2.ErrInvalidField,
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			inner := `<tr2:Configurations>` + testCase.member + `</tr2:Configurations>`
			profiles, err := getMedia2Profiles(t, media2Profile(inner))
			if !errors.Is(err, testCase.want) {
				t.Fatalf("GetProfiles() error = %v, want %v", err, testCase.want)
			}
			if len(profiles) != 0 {
				t.Fatalf("invalid VideoSourceConfiguration exposed profiles: %+v", profiles)
			}
		})
	}
}
