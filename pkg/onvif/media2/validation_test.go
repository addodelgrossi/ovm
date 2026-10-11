package media2_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func callProfiles(c *media2.Client) error {
	_, err := c.GetProfiles(context.Background(), "", nil)
	return err
}

func callVideoEncoderConfigurations(c *media2.Client) error {
	_, err := c.GetVideoEncoderConfigurations(context.Background(), "", "")
	return err
}

func callAudioEncoderConfigurations(c *media2.Client) error {
	_, err := c.GetAudioEncoderConfigurations(context.Background(), "", "")
	return err
}

func callVideoEncoderOptions(c *media2.Client) error {
	_, err := c.GetVideoEncoderConfigurationOptions(context.Background(), "", "")
	return err
}

func callStreamURI(c *media2.Client) error {
	_, err := c.GetStreamUri(context.Background(), media2.StreamRtspUnicast, "p")
	return err
}

func callSnapshotURI(c *media2.Client) error {
	_, err := c.GetSnapshotUri(context.Background(), "p")
	return err
}

func TestMalformedResponses(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		payload string
		call    func(*media2.Client) error
		want    error
	}{
		{
			name:    "wrong root element",
			payload: `<tr2:GetProfiles` + tr2NS + `/>`,
			call:    callProfiles,
			want:    media2.ErrUnexpectedElement,
		},
		{
			name:    "foreign namespace root",
			payload: `<x:GetProfilesResponse xmlns:x="urn:not-onvif"/>`,
			call:    callProfiles,
			want:    media2.ErrUnexpectedElement,
		},
		{
			name:    "ver10 namespace rejected for ver20 operation",
			payload: `<trt:GetProfilesResponse xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/>`,
			call:    callProfiles,
			want:    media2.ErrUnexpectedElement,
		},
		{
			name:    "profile missing name",
			payload: `<tr2:GetProfilesResponse` + tr2NS + `><tr2:Profiles token="p"></tr2:Profiles></tr2:GetProfilesResponse>`,
			call:    callProfiles,
			want:    media2.ErrMissingField,
		},
		{
			name:    "profile missing token",
			payload: `<tr2:GetProfilesResponse` + tr2NS + `><tr2:Profiles><tr2:Name>N</tr2:Name></tr2:Profiles></tr2:GetProfilesResponse>`,
			call:    callProfiles,
			want:    media2.ErrMissingField,
		},
		{
			name:    "duplicate name",
			payload: `<tr2:GetProfilesResponse` + tr2NS + `><tr2:Profiles token="p"><tr2:Name>A</tr2:Name><tr2:Name>B</tr2:Name></tr2:Profiles></tr2:GetProfilesResponse>`,
			call:    callProfiles,
			want:    media2.ErrInvalidField,
		},
		{
			name:    "stream uri missing Uri",
			payload: `<tr2:GetStreamUriResponse` + tr2NS + `/>`,
			call:    callStreamURI,
			want:    media2.ErrMissingField,
		},
		{
			name:    "stream uri empty Uri",
			payload: `<tr2:GetStreamUriResponse` + tr2NS + `><tr2:Uri></tr2:Uri></tr2:GetStreamUriResponse>`,
			call:    callStreamURI,
			want:    media2.ErrInvalidField,
		},
		{
			name:    "stream uri duplicate Uri",
			payload: `<tr2:GetStreamUriResponse` + tr2NS + `><tr2:Uri>a</tr2:Uri><tr2:Uri>b</tr2:Uri></tr2:GetStreamUriResponse>`,
			call:    callStreamURI,
			want:    media2.ErrInvalidField,
		},
		{
			name:    "snapshot uri missing Uri",
			payload: `<tr2:GetSnapshotUriResponse` + tr2NS + `/>`,
			call:    callSnapshotURI,
			want:    media2.ErrMissingField,
		},
		{
			name:    "video encoder missing encoding",
			payload: `<tr2:GetVideoEncoderConfigurationsResponse` + tr2NS + ttNS + `><tr2:Configurations token="c"><tt:Name>N</tt:Name></tr2:Configurations></tr2:GetVideoEncoderConfigurationsResponse>`,
			call:    callVideoEncoderConfigurations,
			want:    media2.ErrMissingField,
		},
		{
			name:    "video encoder missing name",
			payload: `<tr2:GetVideoEncoderConfigurationsResponse` + tr2NS + ttNS + `><tr2:Configurations token="c"><tt:Encoding>H264</tt:Encoding></tr2:Configurations></tr2:GetVideoEncoderConfigurationsResponse>`,
			call:    callVideoEncoderConfigurations,
			want:    media2.ErrMissingField,
		},
		{
			name:    "audio encoder invalid samplerate",
			payload: `<tr2:GetAudioEncoderConfigurationsResponse` + tr2NS + ttNS + `><tr2:Configurations token="c"><tt:Name>N</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>AAC</tt:Encoding><tt:Bitrate>64</tt:Bitrate><tt:SampleRate>x</tt:SampleRate></tr2:Configurations></tr2:GetAudioEncoderConfigurationsResponse>`,
			call:    callAudioEncoderConfigurations,
			want:    media2.ErrInvalidField,
		},
		{
			name:    "options missing Options",
			payload: `<tr2:GetVideoEncoderConfigurationOptionsResponse` + tr2NS + `/>`,
			call:    callVideoEncoderOptions,
			want:    media2.ErrMissingField,
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

func TestDTDRejected(t *testing.T) {
	t.Parallel()
	payload := `<!DOCTYPE x><tr2:GetProfilesResponse` + tr2NS + `/>`
	_, err := invoke(t, payload, callProfiles)
	if err == nil {
		t.Fatal("DTD payload accepted, want rejection")
	}
}

func TestResponseErrorsAreSecretSafe(t *testing.T) {
	t.Parallel()
	secret := "super-secret-marker"
	payload := `<tr2:GetProfilesResponse` + tr2NS + `><tr2:Profiles token="p"><tr2:Name>A</tr2:Name><tr2:Name>` + secret + `</tr2:Name></tr2:Profiles></tr2:GetProfilesResponse>`
	_, err := invoke(t, payload, callProfiles)
	if err == nil {
		t.Fatal("expected duplicate-name rejection")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("error leaked response text: %v", err)
	}
}

func TestUnsafeInputTokens(t *testing.T) {
	t.Parallel()

	t.Run("special characters escaped not injected", func(t *testing.T) {
		t.Parallel()
		payload := `<tr2:GetSnapshotUriResponse` + tr2NS + `><tr2:Uri>http://x/s</tr2:Uri></tr2:GetSnapshotUriResponse>`
		client, rec, closeFn := serveOK(t, payload)
		defer closeFn()
		//nolint:gosec // This adversarial XML token is test data, not a credential.
		token := `a&b<c>"d'</tr2:ProfileToken><tr2:Injected/>`
		if _, err := client.GetSnapshotUri(context.Background(), token); err != nil {
			t.Fatalf("GetSnapshotUri() error = %v", err)
		}
		if strings.Contains(rec.body, "<tr2:Injected/>") {
			t.Fatalf("token broke out of its element: %s", rec.body)
		}
		if !strings.Contains(rec.body, "&amp;") || !strings.Contains(rec.body, "&lt;") || !strings.Contains(rec.body, "&gt;") {
			t.Fatalf("special characters not escaped: %s", rec.body)
		}
	})

	t.Run("control character rejected", func(t *testing.T) {
		t.Parallel()
		soapClient, _ := soap.NewClient(soap.Config{})
		client, _ := media2.NewClient(soapClient, "http://device.invalid/media2")
		if _, err := client.GetSnapshotUri(context.Background(), "bad\x1ftoken"); !errors.Is(err, media2.ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("oversize token rejected without echo", func(t *testing.T) {
		t.Parallel()
		soapClient, _ := soap.NewClient(soap.Config{})
		client, _ := media2.NewClient(soapClient, "http://device.invalid/media2")
		oversize := strings.Repeat("z", 65)
		_, err := client.GetStreamUri(context.Background(), media2.StreamRtspUnicast, oversize)
		if !errors.Is(err, media2.ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
		if strings.Contains(err.Error(), oversize) {
			t.Fatalf("error echoed the rejected token: %v", err)
		}
	})
}

func TestXMLComplexityBounds(t *testing.T) {
	t.Parallel()
	var builder strings.Builder
	builder.WriteString(`<tr2:GetProfilesResponse` + tr2NS + `>`)
	const depth = 20000
	for i := 0; i < depth; i++ {
		builder.WriteString(`<tr2:Nest>`)
	}
	for i := 0; i < depth; i++ {
		builder.WriteString(`</tr2:Nest>`)
	}
	builder.WriteString(`</tr2:GetProfilesResponse>`)
	_, err := invoke(t, builder.String(), callProfiles)
	if err == nil {
		t.Fatal("deeply nested payload accepted, want rejection")
	}
}

func TestPreservedMedia2XMLIsCredentialRedacted(t *testing.T) {
	t.Parallel()
	sensitiveMarker := strings.Join([]string{"synthetic", "extension", "marker"}, "-")
	payload := `<tr2:GetProfilesResponse` + tr2NS + ` xmlns:v="urn:synthetic">` +
		`<tr2:Profiles token="p"><tr2:Name>N</tr2:Name>` +
		`<tr2:Configurations><v:Password>` + sensitiveMarker + `</v:Password></tr2:Configurations></tr2:Profiles>` +
		`</tr2:GetProfilesResponse>`
	client, _, closeFn := serveOK(t, payload)
	defer closeFn()
	profiles, err := client.GetProfiles(context.Background(), "", []media2.ConfigurationType{"All"})
	if err != nil {
		t.Fatalf("GetProfiles() error = %v", err)
	}
	text := string(profiles[0].RawXML)
	if strings.Contains(text, sensitiveMarker) {
		t.Fatalf("preserved XML retained sensitive marker: %s", text)
	}
	if !strings.Contains(text, "REDACTED") {
		t.Fatalf("preserved XML has no redaction marker: %s", text)
	}
}
