package media_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// callProfiles and friends adapt each operation to a uniform signature so the
// malformed-response matrix can drive them all.
func callProfiles(c *media.Client) error {
	_, err := c.GetProfiles(context.Background())
	return err
}

func callVideoSources(c *media.Client) error {
	_, err := c.GetVideoSources(context.Background())
	return err
}

func callVideoEncoderConfigurations(c *media.Client) error {
	_, err := c.GetVideoEncoderConfigurations(context.Background())
	return err
}

func callAudioEncoderConfigurations(c *media.Client) error {
	_, err := c.GetAudioEncoderConfigurations(context.Background())
	return err
}

func callStreamURI(c *media.Client) error {
	_, err := c.GetStreamUri(context.Background(), "p", media.StreamSetup{Stream: media.StreamRTPUnicast, Protocol: media.TransportUDP})
	return err
}

func callSnapshotURI(c *media.Client) error {
	_, err := c.GetSnapshotUri(context.Background(), "p")
	return err
}

func callVideoEncoderOptions(c *media.Client) error {
	_, err := c.GetVideoEncoderConfigurationOptions(context.Background(), "", "")
	return err
}

// TestMalformedResponses drives a matrix of structurally broken payloads and
// asserts each is rejected with the right typed sentinel. The invoke helper
// serves each payload over the real SOAP transport.
func TestMalformedResponses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		payload string
		call    func(*media.Client) error
		want    error
	}{
		{
			name:    "wrong root element",
			payload: `<trt:GetProfiles` + trtNS + `/>`,
			call:    callProfiles,
			want:    media.ErrUnexpectedElement,
		},
		{
			name:    "foreign namespace root",
			payload: `<x:GetProfilesResponse xmlns:x="urn:not-onvif"/>`,
			call:    callProfiles,
			want:    media.ErrUnexpectedElement,
		},
		{
			name:    "profile missing name",
			payload: `<trt:GetProfilesResponse` + trtNS + ttNS + `><trt:Profiles token="p"></trt:Profiles></trt:GetProfilesResponse>`,
			call:    callProfiles,
			want:    media.ErrMissingField,
		},
		{
			name:    "profile missing token attribute",
			payload: `<trt:GetProfilesResponse` + trtNS + ttNS + `><trt:Profiles><tt:Name>N</tt:Name></trt:Profiles></trt:GetProfilesResponse>`,
			call:    callProfiles,
			want:    media.ErrMissingField,
		},
		{
			name:    "duplicate name in profile",
			payload: `<trt:GetProfilesResponse` + trtNS + ttNS + `><trt:Profiles token="p"><tt:Name>A</tt:Name><tt:Name>B</tt:Name></trt:Profiles></trt:GetProfilesResponse>`,
			call:    callProfiles,
			want:    media.ErrInvalidField,
		},
		{
			name:    "stream uri missing MediaUri",
			payload: `<trt:GetStreamUriResponse` + trtNS + `/>`,
			call:    callStreamURI,
			want:    media.ErrMissingField,
		},
		{
			name: "stream uri empty Uri",
			payload: `<trt:GetStreamUriResponse` + trtNS + ttNS + `><trt:MediaUri><tt:Uri></tt:Uri>` +
				`<tt:InvalidAfterConnect>false</tt:InvalidAfterConnect><tt:InvalidAfterReboot>false</tt:InvalidAfterReboot><tt:Timeout>PT0S</tt:Timeout>` +
				`</trt:MediaUri></trt:GetStreamUriResponse>`,
			call: callStreamURI,
			want: media.ErrInvalidField,
		},
		{
			name:    "stream uri duplicate MediaUri",
			payload: `<trt:GetStreamUriResponse` + trtNS + ttNS + `><trt:MediaUri><tt:Uri>a</tt:Uri></trt:MediaUri><trt:MediaUri><tt:Uri>b</tt:Uri></trt:MediaUri></trt:GetStreamUriResponse>`,
			call:    callStreamURI,
			want:    media.ErrInvalidField,
		},
		{
			name:    "snapshot uri missing MediaUri",
			payload: `<trt:GetSnapshotUriResponse` + trtNS + `/>`,
			call:    callSnapshotURI,
			want:    media.ErrMissingField,
		},
		{
			name:    "video source missing token",
			payload: `<trt:GetVideoSourcesResponse` + trtNS + ttNS + `><trt:VideoSources><tt:Framerate>30</tt:Framerate></trt:VideoSources></trt:GetVideoSourcesResponse>`,
			call:    callVideoSources,
			want:    media.ErrMissingField,
		},
		{
			name: "video source invalid framerate",
			payload: `<trt:GetVideoSourcesResponse` + trtNS + ttNS + `><trt:VideoSources token="v"><tt:Framerate>abc</tt:Framerate>` +
				`<tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution></trt:VideoSources></trt:GetVideoSourcesResponse>`,
			call: callVideoSources,
			want: media.ErrInvalidField,
		},
		{
			name:    "video encoder missing encoding",
			payload: `<trt:GetVideoEncoderConfigurationsResponse` + trtNS + ttNS + `><trt:Configurations token="c"><tt:Name>N</tt:Name><tt:UseCount>1</tt:UseCount></trt:Configurations></trt:GetVideoEncoderConfigurationsResponse>`,
			call:    callVideoEncoderConfigurations,
			want:    media.ErrMissingField,
		},
		{
			name: "video encoder invalid usecount",
			payload: `<trt:GetVideoEncoderConfigurationsResponse` + trtNS + ttNS + `><trt:Configurations token="c"><tt:Name>N</tt:Name><tt:UseCount>x</tt:UseCount>` +
				`<tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution><tt:Quality>5</tt:Quality>` +
				validMedia1StreamingTail + `</trt:Configurations></trt:GetVideoEncoderConfigurationsResponse>`,
			call: callVideoEncoderConfigurations,
			want: media.ErrInvalidField,
		},
		{
			name:    "video encoder resolution missing height",
			payload: `<trt:GetVideoEncoderConfigurationsResponse` + trtNS + ttNS + `><trt:Configurations token="c"><tt:Name>N</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>H264</tt:Encoding><tt:Resolution><tt:Width>1</tt:Width></tt:Resolution></trt:Configurations></trt:GetVideoEncoderConfigurationsResponse>`,
			call:    callVideoEncoderConfigurations,
			want:    media.ErrMissingField,
		},
		{
			name: "audio encoder invalid bitrate",
			payload: `<trt:GetAudioEncoderConfigurationsResponse` + trtNS + ttNS + `><trt:Configurations token="c"><tt:Name>N</tt:Name><tt:UseCount>1</tt:UseCount>` +
				`<tt:Encoding>G711</tt:Encoding><tt:Bitrate>x</tt:Bitrate><tt:SampleRate>8</tt:SampleRate>` + validMedia1StreamingTail +
				`</trt:Configurations></trt:GetAudioEncoderConfigurationsResponse>`,
			call: callAudioEncoderConfigurations,
			want: media.ErrInvalidField,
		},
		{
			name:    "options missing Options",
			payload: `<trt:GetVideoEncoderConfigurationOptionsResponse` + trtNS + `/>`,
			call:    callVideoEncoderOptions,
			want:    media.ErrMissingField,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := invoke(t, tc.payload, tc.call)
			if !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestDTDRejected asserts a document type declaration in the operation payload
// is rejected as malformed and never processed.
func TestDTDRejected(t *testing.T) {
	t.Parallel()
	payload := `<!DOCTYPE x><trt:GetProfilesResponse` + trtNS + `/>`
	_, err := invoke(t, payload, callProfiles)
	// The soap envelope parser rejects a DTD before the media decoder runs, so
	// the error is a soap-level invalid XML / DTD error. Either way it must not
	// succeed.
	if err == nil {
		t.Fatal("DTD payload accepted, want rejection")
	}
}

// TestResponseErrorsAreSecretSafe asserts no decode error echoes response body
// text, which could carry credentials.
func TestResponseErrorsAreSecretSafe(t *testing.T) {
	t.Parallel()
	secret := "super-secret-marker"
	payload := `<trt:GetProfilesResponse` + trtNS + ttNS + `><trt:Profiles token="p"><tt:Name>A</tt:Name><tt:Name>` + secret + `</tt:Name></trt:Profiles></trt:GetProfilesResponse>`
	_, err := invoke(t, payload, callProfiles)
	if err == nil {
		t.Fatal("expected duplicate-name rejection")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked response text: %v", err)
	}
}

// TestUnsafeInputTokens asserts the escape-after-validate contract: valid XML
// special characters are escaped (not rejected, not injected), while control
// characters and oversize tokens are rejected without echoing the value.
func TestUnsafeInputTokens(t *testing.T) {
	t.Parallel()

	t.Run("special characters are escaped not injected", func(t *testing.T) {
		t.Parallel()
		payload := `<trt:GetSnapshotUriResponse` + trtNS + ttNS + `><trt:MediaUri><tt:Uri>http://x/s</tt:Uri>` +
			`<tt:InvalidAfterConnect>false</tt:InvalidAfterConnect><tt:InvalidAfterReboot>false</tt:InvalidAfterReboot><tt:Timeout>PT0S</tt:Timeout>` +
			`</trt:MediaUri></trt:GetSnapshotUriResponse>`
		client, rec, closeFn := serveOK(t, payload)
		defer closeFn()
		//nolint:gosec // This adversarial XML token is test data, not a credential.
		token := `a&b<c>"d'</trt:ProfileToken><trt:Injected/>`
		if _, err := client.GetSnapshotUri(context.Background(), token); err != nil {
			t.Fatalf("GetSnapshotUri() error = %v", err)
		}
		if strings.Contains(rec.body, "<trt:Injected/>") {
			t.Fatalf("token broke out of its element: %s", rec.body)
		}
		if !strings.Contains(rec.body, "&amp;") || !strings.Contains(rec.body, "&lt;") || !strings.Contains(rec.body, "&gt;") {
			t.Fatalf("special characters not escaped: %s", rec.body)
		}
	})

	t.Run("control character rejected", func(t *testing.T) {
		t.Parallel()
		soapClient, _ := soap.NewClient(soap.Config{})
		client, _ := media.NewClient(soapClient, "http://device.invalid/media")
		if _, err := client.GetSnapshotUri(context.Background(), "bad\x01token"); !errors.Is(err, media.ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("empty token rejected", func(t *testing.T) {
		t.Parallel()
		soapClient, _ := soap.NewClient(soap.Config{})
		client, _ := media.NewClient(soapClient, "http://device.invalid/media")
		if _, err := client.GetSnapshotUri(context.Background(), ""); !errors.Is(err, media.ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("oversize token rejected without echo", func(t *testing.T) {
		t.Parallel()
		soapClient, _ := soap.NewClient(soap.Config{})
		client, _ := media.NewClient(soapClient, "http://device.invalid/media")
		oversize := strings.Repeat("z", 65)
		_, err := client.GetSnapshotUri(context.Background(), oversize)
		if !errors.Is(err, media.ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
		if strings.Contains(err.Error(), oversize) {
			t.Fatalf("error echoed the rejected token: %v", err)
		}
	})
}

// TestXMLComplexityBounds asserts a deeply nested operation payload is rejected
// rather than consuming unbounded stack while building the element tree.
func TestXMLComplexityBounds(t *testing.T) {
	t.Parallel()
	var builder strings.Builder
	builder.WriteString(`<trt:GetProfilesResponse` + trtNS + `>`)
	const depth = 20000
	for i := 0; i < depth; i++ {
		builder.WriteString(`<trt:Nest>`)
	}
	for i := 0; i < depth; i++ {
		builder.WriteString(`</trt:Nest>`)
	}
	builder.WriteString(`</trt:GetProfilesResponse>`)
	_, err := invoke(t, builder.String(), callProfiles)
	if err == nil {
		t.Fatal("deeply nested payload accepted, want rejection")
	}
}
