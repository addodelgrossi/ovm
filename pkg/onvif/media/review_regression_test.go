package media_test

import (
	"context"
	"errors"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media"
)

func TestGetVideoSourcesUsesPublishedWSDLActionLiteral(t *testing.T) {
	t.Parallel()
	payload := `<trt:GetVideoSourcesResponse` + trtNS + ttNS + `>` +
		`<trt:VideoSources token="source"><tt:Framerate>30</tt:Framerate>` +
		`<tt:Resolution><tt:Width>640</tt:Width><tt:Height>480</tt:Height></tt:Resolution>` +
		`</trt:VideoSources></trt:GetVideoSourcesResponse>`
	client, recorder, closeFn := serveOK(t, payload)
	defer closeFn()
	if _, err := client.GetVideoSources(context.Background()); err != nil {
		t.Fatalf("GetVideoSources() error = %v", err)
	}
	const publishedAction = "http://www.onvif.org/ver10/media/wsdlGetVideoSources/"
	if recorder.action != publishedAction {
		t.Fatalf("SOAP action = %q, want exact WSDL literal %q", recorder.action, publishedAction)
	}
}

func TestMedia1VideoSourceConfigurationRequiresBounds(t *testing.T) {
	t.Parallel()
	withoutBounds := `<tt:VideoSourceConfiguration token="source-config">` +
		`<tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount><tt:SourceToken>source</tt:SourceToken>` +
		`</tt:VideoSourceConfiguration>`
	_, err := getProfiles(t, profileWithAssociations(withoutBounds))
	if !errors.Is(err, media.ErrMissingField) {
		t.Fatalf("GetProfiles() error = %v, want ErrMissingField", err)
	}
}

func TestMedia1DuplicateIdentityTokensRejected(t *testing.T) {
	t.Parallel()
	validProfile := `<trt:Profiles token="duplicate"><tt:Name>valid</tt:Name></trt:Profiles>`
	invalidProfile := `<trt:Profiles token="duplicate"></trt:Profiles>`
	validVideo := `<trt:Configurations token="duplicate"><tt:Name>valid</tt:Name><tt:UseCount>1</tt:UseCount>` +
		`<tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>640</tt:Width><tt:Height>480</tt:Height></tt:Resolution>` +
		`<tt:Quality>5</tt:Quality>` + validMedia1StreamingTail + `</trt:Configurations>`
	invalidVideo := `<trt:Configurations token="duplicate"></trt:Configurations>`
	validAudio := `<trt:Configurations token="duplicate"><tt:Name>valid</tt:Name><tt:UseCount>1</tt:UseCount>` +
		`<tt:Encoding>G711</tt:Encoding><tt:Bitrate>64</tt:Bitrate><tt:SampleRate>8</tt:SampleRate>` +
		validMedia1StreamingTail + `</trt:Configurations>`
	invalidAudio := `<trt:Configurations token="duplicate"></trt:Configurations>`
	validSource := `<trt:VideoSources token="duplicate"><tt:Framerate>30</tt:Framerate>` +
		`<tt:Resolution><tt:Width>640</tt:Width><tt:Height>480</tt:Height></tt:Resolution></trt:VideoSources>`
	invalidSource := `<trt:VideoSources token="duplicate"></trt:VideoSources>`

	cases := []struct {
		name       string
		root       string
		first      string
		second     string
		invokeCall func(*media.Client) error
	}{
		{"profiles", "GetProfilesResponse", validProfile, invalidProfile, callProfiles},
		{"physical video sources", "GetVideoSourcesResponse", validSource, invalidSource, callVideoSources},
		{"video configurations", "GetVideoEncoderConfigurationsResponse", validVideo, invalidVideo, callVideoEncoderConfigurations},
		{"audio configurations", "GetAudioEncoderConfigurationsResponse", validAudio, invalidAudio, callAudioEncoderConfigurations},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			for _, entries := range [][2]string{{testCase.first, testCase.second}, {testCase.second, testCase.first}} {
				payload := `<trt:` + testCase.root + trtNS + ttNS + `>` + entries[0] + entries[1] + `</trt:` + testCase.root + `>`
				_, err := invoke(t, payload, testCase.invokeCall)
				if !errors.Is(err, media.ErrInvalidField) {
					t.Fatalf("duplicate tokens in wire order %#v: error = %v, want ErrInvalidField", entries, err)
				}
			}
		})
	}
}
