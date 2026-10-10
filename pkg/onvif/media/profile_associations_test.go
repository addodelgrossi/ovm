package media_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media"
)

// profileWithAssociations builds a GetProfilesResponse carrying a single
// profile whose embedded configurations supply the typed association tokens.
// The prefix arguments let a test exercise alternate namespace prefixes while
// keeping the resolved namespaces identical.
func profileWithAssociations(inner string) string {
	return `<trt:GetProfilesResponse` + trtNS + ttNS + `>` +
		`<trt:Profiles token="profile_main" fixed="true">` +
		`<tt:Name>main</tt:Name>` + inner +
		`</trt:Profiles></trt:GetProfilesResponse>`
}

const media1Associations = `<tt:VideoSourceConfiguration token="vsrccfg0">` +
	`<tt:Name>vsc</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>vsrc0</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"></tt:Bounds></tt:VideoSourceConfiguration>` +
	`<tt:VideoEncoderConfiguration token="venc_main">` +
	`<tt:Name>vec</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>H264</tt:Encoding>` +
	`<tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution>` +
	`<tt:Quality>5</tt:Quality></tt:VideoEncoderConfiguration>` +
	`<tt:AudioEncoderConfiguration token="aenc0">` +
	`<tt:Name>aec</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>G711</tt:Encoding>` +
	`<tt:Bitrate>64</tt:Bitrate><tt:SampleRate>8</tt:SampleRate></tt:AudioEncoderConfiguration>`

func getProfiles(t *testing.T, payload string) ([]media.Profile, error) {
	t.Helper()
	client, _, closeFn := serveOK(t, payload)
	defer closeFn()
	return client.GetProfiles(context.Background())
}

func TestMedia1ProfileAssociationsDecode(t *testing.T) {
	t.Parallel()
	profiles, err := getProfiles(t, profileWithAssociations(media1Associations))
	if err != nil {
		t.Fatalf("GetProfiles() error = %v", err)
	}
	if len(profiles) != 1 {
		t.Fatalf("got %d profiles, want 1", len(profiles))
	}
	a := profiles[0].Associations
	if !a.HasVideoSourceConfiguration || a.VideoSourceConfigurationToken != "vsrccfg0" {
		t.Errorf("video source configuration token = %q present=%v", a.VideoSourceConfigurationToken, a.HasVideoSourceConfiguration)
	}
	if !a.HasVideoSource || a.VideoSourceToken != "vsrc0" {
		t.Errorf("physical video source token = %q present=%v", a.VideoSourceToken, a.HasVideoSource)
	}
	if !a.HasVideoEncoderConfiguration || a.VideoEncoderConfigurationToken != "venc_main" {
		t.Errorf("video encoder configuration token = %q present=%v", a.VideoEncoderConfigurationToken, a.HasVideoEncoderConfiguration)
	}
	if !a.HasAudioEncoderConfiguration || a.AudioEncoderConfigurationToken != "aenc0" {
		t.Errorf("audio encoder configuration token = %q present=%v", a.AudioEncoderConfigurationToken, a.HasAudioEncoderConfiguration)
	}
}

func TestMedia1ProfileAssociationsOptionalAbsence(t *testing.T) {
	t.Parallel()
	// An identity-only profile (just Name) is valid: every configuration member
	// is optional, so all presence flags must be false and no error raised.
	profiles, err := getProfiles(t, profileWithAssociations(""))
	if err != nil {
		t.Fatalf("GetProfiles() error = %v", err)
	}
	a := profiles[0].Associations
	if a.HasVideoSourceConfiguration || a.HasVideoSource || a.HasVideoEncoderConfiguration || a.HasAudioEncoderConfiguration {
		t.Errorf("expected no associations, got %+v", a)
	}
}

func TestMedia1ProfileAssociationsAlternatePrefix(t *testing.T) {
	t.Parallel()
	// The decoder matches on {namespace}local, so a different prefix bound to
	// the same namespace must decode identically.
	payload := `<m:GetProfilesResponse xmlns:m="http://www.onvif.org/ver10/media/wsdl" xmlns:s="http://www.onvif.org/ver10/schema">` +
		`<m:Profiles token="profile_main"><s:Name>main</s:Name>` +
		`<s:VideoSourceConfiguration token="vsrccfg0"><s:Name>v</s:Name><s:UseCount>1</s:UseCount><s:SourceToken>vsrc0</s:SourceToken><s:Bounds x="0" y="0" width="1" height="1"></s:Bounds></s:VideoSourceConfiguration>` +
		`</m:Profiles></m:GetProfilesResponse>`
	profiles, err := getProfiles(t, payload)
	if err != nil {
		t.Fatalf("GetProfiles() error = %v", err)
	}
	a := profiles[0].Associations
	if !a.HasVideoSource || a.VideoSourceToken != "vsrc0" {
		t.Errorf("alternate-prefix source token = %q present=%v", a.VideoSourceToken, a.HasVideoSource)
	}
}

func TestMedia1ProfileAssociationsForeignNamespaceRejected(t *testing.T) {
	t.Parallel()
	// A VideoSourceConfiguration lookalike in a foreign namespace must not be
	// accepted as the schema member, and because it is an unexpected element in
	// the strict profile sequence the decode must fail.
	payload := `<trt:GetProfilesResponse` + trtNS + ttNS + ` xmlns:x="urn:evil">` +
		`<trt:Profiles token="p"><tt:Name>main</tt:Name>` +
		`<x:VideoSourceConfiguration token="vsrccfg0"><x:SourceToken>vsrc0</x:SourceToken></x:VideoSourceConfiguration>` +
		`</trt:Profiles></trt:GetProfilesResponse>`
	_, err := getProfiles(t, payload)
	if err == nil {
		t.Fatal("expected error for foreign-namespace configuration lookalike")
	}
}

func TestMedia1ProfileAssociationsDuplicateRejected(t *testing.T) {
	t.Parallel()
	inner := `<tt:VideoSourceConfiguration token="a"><tt:Name>v</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>s</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"></tt:Bounds></tt:VideoSourceConfiguration>` +
		`<tt:VideoSourceConfiguration token="b"><tt:Name>v</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>s</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"></tt:Bounds></tt:VideoSourceConfiguration>`
	_, err := getProfiles(t, profileWithAssociations(inner))
	if err == nil {
		t.Fatal("expected error for duplicate VideoSourceConfiguration")
	}
}

func TestMedia1ProfileAssociationsOutOfOrderRejected(t *testing.T) {
	t.Parallel()
	// VideoEncoderConfiguration before VideoSourceConfiguration violates the
	// tt:Profile sequence order.
	inner := `<tt:VideoEncoderConfiguration token="venc_main"><tt:Name>v</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>1</tt:Width><tt:Height>1</tt:Height></tt:Resolution><tt:Quality>5</tt:Quality></tt:VideoEncoderConfiguration>` +
		`<tt:VideoSourceConfiguration token="vsrccfg0"><tt:Name>v</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>vsrc0</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"></tt:Bounds></tt:VideoSourceConfiguration>`
	_, err := getProfiles(t, profileWithAssociations(inner))
	if err == nil {
		t.Fatal("expected error for out-of-order configuration members")
	}
}

func TestMedia1ProfileAssociationsMissingTokenRejected(t *testing.T) {
	t.Parallel()
	inner := `<tt:VideoSourceConfiguration><tt:Name>v</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>vsrc0</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"></tt:Bounds></tt:VideoSourceConfiguration>`
	_, err := getProfiles(t, profileWithAssociations(inner))
	if !errors.Is(err, media.ErrMissingField) {
		t.Fatalf("got %v, want ErrMissingField", err)
	}
}

func TestMedia1ProfileAssociationsSecretSafeError(t *testing.T) {
	t.Parallel()
	// An over-length SourceToken is invalid per the reference-token bound but is
	// still well-formed XML, so it reaches the field validator. The error must
	// name the field and never echo the offending value.
	oversized := strings.Repeat("x", 200)
	inner := `<tt:VideoSourceConfiguration token="vsrccfg0"><tt:Name>v</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>` + oversized + `</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"></tt:Bounds></tt:VideoSourceConfiguration>`
	_, err := getProfiles(t, profileWithAssociations(inner))
	if !errors.Is(err, media.ErrInvalidField) {
		t.Fatalf("got %v, want ErrInvalidField", err)
	}
	if strings.Contains(err.Error(), oversized) {
		t.Errorf("error leaked offending value: %v", err)
	}
	if !strings.Contains(err.Error(), "SourceToken") {
		t.Errorf("error should name the SourceToken field, got %v", err)
	}
}

func TestMedia1VideoSourceConfigurationFullyValidated(t *testing.T) {
	t.Parallel()
	longValue := strings.Repeat("x", 65)
	valid := `<tt:VideoSourceConfiguration token="cfg"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"/></tt:VideoSourceConfiguration>`
	cases := []struct {
		name   string
		member string
		want   error
	}{
		{
			name:   "missing inherited Name",
			member: strings.Replace(valid, `<tt:Name>source</tt:Name>`, "", 1),
			want:   media.ErrMissingField,
		},
		{
			name:   "oversized inherited Name",
			member: strings.Replace(valid, `source</tt:Name>`, longValue+`</tt:Name>`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "nested inherited Name content",
			member: strings.Replace(valid, `<tt:Name>source</tt:Name>`, `<tt:Name><tt:Nested/></tt:Name>`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "attributed inherited Name",
			member: strings.Replace(valid, `<tt:Name>`, `<tt:Name invalid="true">`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "missing inherited UseCount",
			member: strings.Replace(valid, `<tt:UseCount>1</tt:UseCount>`, "", 1),
			want:   media.ErrMissingField,
		},
		{
			name:   "malformed inherited UseCount",
			member: strings.Replace(valid, `<tt:UseCount>1</tt:UseCount>`, `<tt:UseCount>many</tt:UseCount>`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "nested inherited UseCount content",
			member: strings.Replace(valid, `<tt:UseCount>1</tt:UseCount>`, `<tt:UseCount><tt:Nested/></tt:UseCount>`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "negative inherited UseCount",
			member: strings.Replace(valid, `<tt:UseCount>1</tt:UseCount>`, `<tt:UseCount>-1</tt:UseCount>`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "inherited UseCount above xs:int",
			member: strings.Replace(valid, `<tt:UseCount>1</tt:UseCount>`, `<tt:UseCount>2147483648</tt:UseCount>`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "missing inherited token",
			member: strings.Replace(valid, ` token="cfg"`, "", 1),
			want:   media.ErrMissingField,
		},
		{
			name:   "qualified token lookalike",
			member: strings.Replace(valid, ` token="cfg"`, ` tt:token="cfg"`, 1),
			want:   media.ErrMissingField,
		},
		{
			name:   "oversized inherited token",
			member: strings.Replace(valid, `token="cfg"`, `token="`+longValue+`"`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "missing SourceToken",
			member: strings.Replace(valid, `<tt:SourceToken>source</tt:SourceToken>`, "", 1),
			want:   media.ErrMissingField,
		},
		{
			name:   "nested SourceToken content",
			member: strings.Replace(valid, `<tt:SourceToken>source</tt:SourceToken>`, `<tt:SourceToken><tt:Nested/></tt:SourceToken>`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "attributed SourceToken",
			member: strings.Replace(valid, `<tt:SourceToken>`, `<tt:SourceToken invalid="true">`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name: "core fields out of sequence",
			member: `<tt:VideoSourceConfiguration token="cfg"><tt:UseCount>1</tt:UseCount>` +
				`<tt:Name>source</tt:Name><tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"/></tt:VideoSourceConfiguration>`,
			want: media.ErrMissingField,
		},
		{
			name:   "missing Bounds",
			member: strings.Replace(valid, `<tt:Bounds x="0" y="0" width="1" height="1"/>`, "", 1),
			want:   media.ErrMissingField,
		},
		{
			name:   "duplicate Bounds",
			member: strings.Replace(valid, `</tt:VideoSourceConfiguration>`, `<tt:Bounds x="0" y="0" width="1" height="1"/></tt:VideoSourceConfiguration>`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "Bounds missing x",
			member: strings.Replace(valid, ` x="0"`, "", 1),
			want:   media.ErrMissingField,
		},
		{
			name:   "Bounds missing y",
			member: strings.Replace(valid, ` y="0"`, "", 1),
			want:   media.ErrMissingField,
		},
		{
			name:   "Bounds missing width",
			member: strings.Replace(valid, ` width="1"`, "", 1),
			want:   media.ErrMissingField,
		},
		{
			name:   "Bounds missing height",
			member: strings.Replace(valid, ` height="1"`, "", 1),
			want:   media.ErrMissingField,
		},
		{
			name:   "malformed Bounds attribute",
			member: strings.Replace(valid, `width="1"`, `width="wide"`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "Bounds attribute above xs:int",
			member: strings.Replace(valid, `width="1"`, `width="2147483648"`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "Bounds attribute below xs:int",
			member: strings.Replace(valid, `x="0"`, `x="-2147483649"`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "qualified Bounds attribute",
			member: strings.Replace(valid, `x="0"`, `tt:x="0"`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "undeclared Bounds attribute",
			member: strings.Replace(valid, `height="1"`, `height="1" depth="1"`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "nested Bounds content",
			member: strings.Replace(valid, `height="1"/>`, `height="1"><tt:Nested/></tt:Bounds>`, 1),
			want:   media.ErrInvalidField,
		},
		{
			name:   "text Bounds content",
			member: strings.Replace(valid, `height="1"/>`, `height="1">text</tt:Bounds>`, 1),
			want:   media.ErrInvalidField,
		},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			profiles, err := getProfiles(t, profileWithAssociations(testCase.member))
			if !errors.Is(err, testCase.want) {
				t.Fatalf("GetProfiles() error = %v, want %v", err, testCase.want)
			}
			if len(profiles) != 0 {
				t.Fatalf("invalid VideoSourceConfiguration exposed profiles: %+v", profiles)
			}
		})
	}
}

func TestMedia1VideoSourceConfigurationXSIntBoundaries(t *testing.T) {
	t.Parallel()
	member := `<tt:VideoSourceConfiguration token="cfg"><tt:Name></tt:Name><tt:UseCount>2147483647</tt:UseCount>` +
		`<tt:SourceToken>source</tt:SourceToken><tt:Bounds x="-2147483648" y="2147483647" width="-2147483648" height="2147483647"/>` +
		`</tt:VideoSourceConfiguration>`
	profiles, err := getProfiles(t, profileWithAssociations(member))
	if err != nil {
		t.Fatalf("GetProfiles() rejected valid xs:int boundaries: %v", err)
	}
	if len(profiles) != 1 || !profiles[0].Associations.HasVideoSource || profiles[0].Associations.VideoSourceToken != "source" {
		t.Fatalf("boundary profile associations = %+v", profiles)
	}
}

func TestMedia1VideoSourceConfigurationExtensionOrder(t *testing.T) {
	t.Parallel()
	prefix := `<tt:VideoSourceConfiguration token="cfg"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken>`
	bounds := `<tt:Bounds x="0" y="0" width="1" height="1"/>`
	suffix := `</tt:VideoSourceConfiguration>`
	cases := []struct {
		name    string
		member  string
		wantErr bool
	}{
		{name: "no extensions", member: prefix + bounds + suffix},
		{name: "foreign extensions", member: prefix + bounds + `<v:One xmlns:v="urn:vendor"/><w:Two xmlns:w="urn:vendor-two"/>` + suffix},
		{name: "foreign extensions before terminal tt Extension", member: prefix + bounds + `<v:One xmlns:v="urn:vendor"/><tt:Extension/>` + suffix},
		{name: "terminal tt Extension", member: prefix + bounds + `<tt:Extension/>` + suffix},
		{name: "foreign extension before Bounds", member: prefix + `<v:One xmlns:v="urn:vendor"/>` + bounds + suffix, wantErr: true},
		{name: "unqualified extension", member: prefix + bounds + `<Extension/>` + suffix, wantErr: true},
		{name: "unknown tt extension", member: prefix + bounds + `<tt:Vendor/>` + suffix, wantErr: true},
		{name: "duplicate tt Extension", member: prefix + bounds + `<tt:Extension/><tt:Extension/>` + suffix, wantErr: true},
		{name: "foreign extension after tt Extension", member: prefix + bounds + `<tt:Extension/><v:One xmlns:v="urn:vendor"/>` + suffix, wantErr: true},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			profiles, err := getProfiles(t, profileWithAssociations(testCase.member))
			if testCase.wantErr {
				if err == nil {
					t.Fatal("schema-invalid extension order was accepted")
				}
				if len(profiles) != 0 {
					t.Fatalf("invalid extension order exposed profiles: %+v", profiles)
				}
				return
			}
			if err != nil {
				t.Fatalf("valid extension order rejected: %v", err)
			}
			if len(profiles) != 1 {
				t.Fatalf("profiles = %d, want 1", len(profiles))
			}
		})
	}
}

func TestMedia1VideoSourceConfigurationDeclaredExtensionValidation(t *testing.T) {
	t.Parallel()
	prefix := `<tt:VideoSourceConfiguration token="cfg"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount>` +
		`<tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="1" height="1"/>`
	suffix := `</tt:VideoSourceConfiguration>`
	cases := []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "empty extension", content: `<tt:Extension/>`},
		{name: "rotate off", content: `<tt:Extension><tt:Rotate><tt:Mode>OFF</tt:Mode></tt:Rotate></tt:Extension>`},
		{name: "rotate degree boundaries", content: `<tt:Extension><tt:Rotate Mirror="true"><tt:Mode>ON</tt:Mode><tt:Degree>-2147483648</tt:Degree></tt:Rotate></tt:Extension>`},
		{name: "rotate positive boundary", content: `<tt:Extension><tt:Rotate Mirror="0"><tt:Mode>AUTO</tt:Mode><tt:Degree>2147483647</tt:Degree><tt:Extension><tt:Future/></tt:Extension></tt:Rotate></tt:Extension>`},
		{name: "nested target extension", content: `<tt:Extension><tt:Extension><tt:Future/></tt:Extension></tt:Extension>`},
		{name: "valid scene orientation", content: `<tt:Extension><tt:Extension><tt:SceneOrientation><tt:Mode>AUTO</tt:Mode></tt:SceneOrientation></tt:Extension></tt:Extension>`},
		{name: "valid lens description", content: `<tt:Extension><tt:Extension><tt:LensDescription FocalLength="1.2"><tt:Offset x="0" y="0"/><tt:Projection><tt:Angle>1</tt:Angle><tt:Radius>2</tt:Radius></tt:Projection><tt:XFactor>1</tt:XFactor></tt:LensDescription></tt:Extension></tt:Extension>`},
		{name: "missing rotate mode", content: `<tt:Extension><tt:Rotate><tt:Degree>90</tt:Degree></tt:Rotate></tt:Extension>`, wantErr: true},
		{name: "invalid rotate mode", content: `<tt:Extension><tt:Rotate><tt:Mode>SIDEWAYS</tt:Mode></tt:Rotate></tt:Extension>`, wantErr: true},
		{name: "whitespace rotate mode", content: `<tt:Extension><tt:Rotate><tt:Mode> ON </tt:Mode></tt:Rotate></tt:Extension>`, wantErr: true},
		{name: "degree positive overflow", content: `<tt:Extension><tt:Rotate><tt:Mode>ON</tt:Mode><tt:Degree>2147483648</tt:Degree></tt:Rotate></tt:Extension>`, wantErr: true},
		{name: "degree negative overflow", content: `<tt:Extension><tt:Rotate><tt:Mode>ON</tt:Mode><tt:Degree>-2147483649</tt:Degree></tt:Rotate></tt:Extension>`, wantErr: true},
		{name: "invalid mirror", content: `<tt:Extension><tt:Rotate Mirror="yes"><tt:Mode>ON</tt:Mode></tt:Rotate></tt:Extension>`, wantErr: true},
		{name: "duplicate rotate", content: `<tt:Extension><tt:Rotate><tt:Mode>OFF</tt:Mode></tt:Rotate><tt:Rotate><tt:Mode>ON</tt:Mode></tt:Rotate></tt:Extension>`, wantErr: true},
		{name: "out of order rotate children", content: `<tt:Extension><tt:Rotate><tt:Degree>90</tt:Degree><tt:Mode>ON</tt:Mode></tt:Rotate></tt:Extension>`, wantErr: true},
		{name: "unknown outer child", content: `<tt:Extension><tt:Unknown/></tt:Extension>`, wantErr: true},
		{name: "foreign nested extension child", content: `<tt:Extension><tt:Extension><v:Future xmlns:v="urn:vendor"/></tt:Extension></tt:Extension>`, wantErr: true},
		{name: "scene orientation missing mode", content: `<tt:Extension><tt:Extension><tt:SceneOrientation/></tt:Extension></tt:Extension>`, wantErr: true},
		{name: "scene orientation whitespace mode", content: `<tt:Extension><tt:Extension><tt:SceneOrientation><tt:Mode> AUTO </tt:Mode></tt:SceneOrientation></tt:Extension></tt:Extension>`, wantErr: true},
		{name: "scene orientation duplicate", content: `<tt:Extension><tt:Extension><tt:SceneOrientation><tt:Mode>AUTO</tt:Mode></tt:SceneOrientation><tt:SceneOrientation><tt:Mode>MANUAL</tt:Mode></tt:SceneOrientation></tt:Extension></tt:Extension>`, wantErr: true},
		{name: "lens after scene orientation", content: `<tt:Extension><tt:Extension><tt:SceneOrientation><tt:Mode>AUTO</tt:Mode></tt:SceneOrientation><tt:LensDescription><tt:Offset/><tt:Projection><tt:Angle>1</tt:Angle><tt:Radius>2</tt:Radius></tt:Projection><tt:XFactor>1</tt:XFactor></tt:LensDescription></tt:Extension></tt:Extension>`, wantErr: true},
		{name: "outer mixed content", content: `<tt:Extension>text</tt:Extension>`, wantErr: true},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			profiles, err := getProfiles(t, profileWithAssociations(prefix+testCase.content+suffix))
			if testCase.wantErr {
				if err == nil {
					t.Fatal("schema-invalid declared extension was accepted")
				}
				if len(profiles) != 0 {
					t.Fatalf("invalid extension exposed profiles: %+v", profiles)
				}
				return
			}
			if err != nil {
				t.Fatalf("valid declared extension rejected: %v", err)
			}
			if len(profiles) != 1 || !profiles[0].Associations.HasVideoSource {
				t.Fatalf("valid extension associations = %+v", profiles)
			}
		})
	}
}
