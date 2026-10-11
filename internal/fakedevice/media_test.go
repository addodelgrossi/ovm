package fakedevice_test

import (
	"context"
	"errors"
	"net"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// mediaCall issues one SOAP request against a fake device server and returns
// the response.
func mediaCall(t *testing.T, client *soap.Client, serverURL, action string, body string) *soap.Response {
	t.Helper()
	response, err := client.Call(context.Background(), serverURL, soap.Request{
		Action:  action,
		BodyXML: []byte(body),
	})
	if err != nil {
		t.Fatalf("Call(%s) error = %v", action, err)
	}
	return response
}

func TestMediaOperationsCoverVer10AndVer20(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		action   string
		body     string
		contains []string
	}{
		{
			name:   "Media1 GetProfiles",
			action: fakedevice.ActionMedia1GetProfiles,
			body:   `<trt:GetProfiles xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/>`,
			contains: []string{
				`GetProfilesResponse`, `token="profile_main"`, `token="profile_sub"`,
				`<tt:Name>main</tt:Name>`,
			},
		},
		{
			name:     "Media2 GetProfiles",
			action:   fakedevice.ActionMedia2GetProfiles,
			body:     `<tr2:GetProfiles xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"/>`,
			contains: []string{`GetProfilesResponse`, `token="profile_main"`, `<tr2:Name>sub</tr2:Name>`},
		},
		{
			name:     "Media1 GetVideoSources",
			action:   fakedevice.ActionMedia1GetVideoSources,
			body:     `<trt:GetVideoSources xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/>`,
			contains: []string{`GetVideoSourcesResponse`, `token="vsrc0"`, `<tt:Framerate>30</tt:Framerate>`},
		},
		{
			name:     "Media1 GetVideoEncoderConfigurations",
			action:   fakedevice.ActionMedia1GetVideoEncoderConfigurations,
			body:     `<trt:GetVideoEncoderConfigurations xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/>`,
			contains: []string{`GetVideoEncoderConfigurationsResponse`, `token="venc_main"`, `<tt:Encoding>H264</tt:Encoding>`},
		},
		{
			name:     "Media2 GetVideoEncoderConfigurations",
			action:   fakedevice.ActionMedia2GetVideoEncoderConfigurations,
			body:     `<tr2:GetVideoEncoderConfigurations xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"/>`,
			contains: []string{`GetVideoEncoderConfigurationsResponse`, `token="venc_sub"`},
		},
		{
			name:     "Media1 GetVideoEncoderConfigurationOptions",
			action:   fakedevice.ActionMedia1GetVideoEncoderConfigurationOptions,
			body:     `<trt:GetVideoEncoderConfigurationOptions xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/>`,
			contains: []string{`GetVideoEncoderConfigurationOptionsResponse`, `<tt:QualityRange>`, `<tt:Max>10</tt:Max>`},
		},
		{
			name:     "Media2 GetVideoEncoderConfigurationOptions",
			action:   fakedevice.ActionMedia2GetVideoEncoderConfigurationOptions,
			body:     `<tr2:GetVideoEncoderConfigurationOptions xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"/>`,
			contains: []string{`GetVideoEncoderConfigurationOptionsResponse`, `<tt:BitrateRange>`},
		},
		{
			name:     "Media1 GetAudioEncoderConfigurations",
			action:   fakedevice.ActionMedia1GetAudioEncoderConfigurations,
			body:     `<trt:GetAudioEncoderConfigurations xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/>`,
			contains: []string{`GetAudioEncoderConfigurationsResponse`, `token="aenc0"`, `<tt:Encoding>G711</tt:Encoding>`},
		},
		{
			name:     "Media2 GetAudioEncoderConfigurations",
			action:   fakedevice.ActionMedia2GetAudioEncoderConfigurations,
			body:     `<tr2:GetAudioEncoderConfigurations xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"/>`,
			contains: []string{`GetAudioEncoderConfigurationsResponse`, `token="aenc0"`, `<tt:Encoding>PCMU</tt:Encoding>`},
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			device := newDevice(t, fakedevice.Config{})
			server := httptest.NewServer(device)
			defer server.Close()
			client := newSOAPClient(t, nil)

			response := mediaCall(t, client, server.URL, tt.action, tt.body)
			raw := string(response.RawXML)
			for _, want := range tt.contains {
				if !strings.Contains(raw, want) {
					t.Errorf("response missing %q\ngot: %s", want, raw)
				}
			}
		})
	}
}

// TestMediaURIOperations exercises the stream and snapshot URI builders. URI
// assertions use BodyXML, because the SOAP client intentionally redacts
// http(s) query strings in RawXML; BodyXML retains the operation payload.
func TestMediaURIOperations(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		action   string
		body     string
		contains []string
	}{
		{
			name:     "Media1 GetStreamUri",
			action:   fakedevice.ActionMedia1GetStreamURI,
			body:     `<trt:GetStreamUri xmlns:trt="http://www.onvif.org/ver10/media/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema"><trt:StreamSetup><tt:Stream>RTP-Unicast</tt:Stream><tt:Transport><tt:Protocol>UDP</tt:Protocol></tt:Transport></trt:StreamSetup><trt:ProfileToken>profile_main</trt:ProfileToken></trt:GetStreamUri>`,
			contains: []string{`GetStreamUriResponse`, `rtsp://`, `/onvif/stream?profile=profile_main`},
		},
		{
			name:     "Media2 GetStreamUri",
			action:   fakedevice.ActionMedia2GetStreamURI,
			body:     `<tr2:GetStreamUri xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><tr2:Protocol>RTSP</tr2:Protocol><tr2:ProfileToken>profile_sub</tr2:ProfileToken></tr2:GetStreamUri>`,
			contains: []string{`GetStreamUriResponse`, `rtsp://`, `/onvif/stream?profile=profile_sub`},
		},
		{
			name:     "Media1 GetSnapshotUri",
			action:   fakedevice.ActionMedia1GetSnapshotURI,
			body:     `<trt:GetSnapshotUri xmlns:trt="http://www.onvif.org/ver10/media/wsdl"><trt:ProfileToken>profile_main</trt:ProfileToken></trt:GetSnapshotUri>`,
			contains: []string{`GetSnapshotUriResponse`, `http://`, `/onvif/snapshot?profile=profile_main`},
		},
		{
			name:     "Media2 GetSnapshotUri",
			action:   fakedevice.ActionMedia2GetSnapshotURI,
			body:     `<tr2:GetSnapshotUri xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><tr2:ProfileToken>profile_sub</tr2:ProfileToken></tr2:GetSnapshotUri>`,
			contains: []string{`GetSnapshotUriResponse`, `/onvif/snapshot?profile=profile_sub`},
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			device := newDevice(t, fakedevice.Config{})
			server := httptest.NewServer(device)
			defer server.Close()
			client := newSOAPClient(t, nil)

			response := mediaCall(t, client, server.URL, tt.action, tt.body)
			body := string(response.BodyXML)
			for _, want := range tt.contains {
				if !strings.Contains(body, want) {
					t.Errorf("response missing %q\ngot: %s", want, body)
				}
			}
		})
	}
}

func TestMediaStreamURIDerivesRequestOrigin(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	// The snapshot URI must reflect the live request host (httptest's
	// 127.0.0.1:<port>), never a hard-coded host. BodyXML is used because the
	// SOAP client redacts http(s) query strings in RawXML.
	response := mediaCall(t, client, server.URL, fakedevice.ActionMedia2GetSnapshotURI,
		`<tr2:GetSnapshotUri xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><tr2:ProfileToken>profile_main</tr2:ProfileToken></tr2:GetSnapshotUri>`)

	serverHost := mustHost(t, server.URL)
	if !strings.Contains(string(response.BodyXML), "http://"+serverHost+"/onvif/snapshot") {
		t.Fatalf("snapshot URI does not derive request origin %q\ngot: %s", serverHost, response.BodyXML)
	}
	// The RTSP stream URI reuses the bare host with the RTSP default port.
	stream := mediaCall(t, client, server.URL, fakedevice.ActionMedia2GetStreamURI,
		`<tr2:GetStreamUri xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><tr2:Protocol>RTSP</tr2:Protocol><tr2:ProfileToken>profile_main</tr2:ProfileToken></tr2:GetStreamUri>`)
	bareHost := serverHost
	if host, _, err := net.SplitHostPort(serverHost); err == nil {
		bareHost = host
	}
	if !strings.Contains(string(stream.BodyXML), "rtsp://"+bareHost+":554/onvif/stream") {
		t.Fatalf("stream URI does not derive request origin\ngot: %s", stream.BodyXML)
	}
}

func TestMediaConfigurableInventoryAndURIVariants(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{
		MediaInventory: fakedevice.MediaInventory{
			SnapshotPath: "/custom/snap.jpg",
			StreamPath:   "live/custom",
			Profiles: []fakedevice.MediaProfileData{
				{Token: "only", Name: "Only", Fixed: false},
			},
		},
	})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	profiles := mediaCall(t, client, server.URL, fakedevice.ActionMedia2GetProfiles,
		`<tr2:GetProfiles xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"/>`)
	raw := string(profiles.RawXML)
	if !strings.Contains(raw, `token="only"`) || !strings.Contains(raw, `fixed="false"`) {
		t.Errorf("configured profile not reflected: %s", raw)
	}

	snapshot := mediaCall(t, client, server.URL, fakedevice.ActionMedia2GetSnapshotURI,
		`<tr2:GetSnapshotUri xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><tr2:ProfileToken>only</tr2:ProfileToken></tr2:GetSnapshotUri>`)
	if !strings.Contains(string(snapshot.BodyXML), "/custom/snap.jpg?profile=only") {
		t.Errorf("configured snapshot path/variant not reflected: %s", snapshot.BodyXML)
	}
	stream := mediaCall(t, client, server.URL, fakedevice.ActionMedia2GetStreamURI,
		`<tr2:GetStreamUri xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><tr2:Protocol>RTSP</tr2:Protocol><tr2:ProfileToken>only</tr2:ProfileToken></tr2:GetStreamUri>`)
	if !strings.Contains(string(stream.BodyXML), ":554/live/custom?profile=only") {
		t.Errorf("configured stream path/variant not reflected: %s", stream.BodyXML)
	}
}

func TestMediaSOAP11AndPrefixVariation(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{
		SupportedSOAPVersions: []fakedevice.SOAPVersion{fakedevice.SOAP11},
		EnvelopePrefix:        "cameraEnvelope",
		OperationPrefix:       "cameraDevice",
	})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	response, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionMedia1GetProfiles,
		BodyXML: []byte(`<trt:GetProfiles xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/>`),
	})
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if response.Version != soap.Version11 {
		t.Fatalf("Version = %s, want SOAP 1.1", response.Version)
	}
	raw := string(response.RawXML)
	// The media operation prefix is fixed (trt) and must coexist with the
	// configurable envelope prefix in one document.
	if !strings.Contains(raw, "cameraEnvelope:Envelope") || !strings.Contains(raw, "trt:GetProfilesResponse") {
		t.Errorf("prefix variation not honored: %s", raw)
	}
}

func TestMediaWrapperValidationRejectsInvalidChildren(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name   string
		action string
		body   string
	}{
		{
			name:   "GetProfiles with unexpected child",
			action: fakedevice.ActionMedia2GetProfiles,
			body:   `<tr2:GetProfiles xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><tr2:Unexpected/></tr2:GetProfiles>`,
		},
		{
			name:   "GetSnapshotUri missing ProfileToken",
			action: fakedevice.ActionMedia2GetSnapshotURI,
			body:   `<tr2:GetSnapshotUri xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"/>`,
		},
		{
			name:   "GetSnapshotUri duplicate ProfileToken",
			action: fakedevice.ActionMedia2GetSnapshotURI,
			body:   `<tr2:GetSnapshotUri xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><tr2:ProfileToken>a</tr2:ProfileToken><tr2:ProfileToken>b</tr2:ProfileToken></tr2:GetSnapshotUri>`,
		},
		{
			name:   "GetSnapshotUri foreign-namespace ProfileToken",
			action: fakedevice.ActionMedia2GetSnapshotURI,
			body:   `<tr2:GetSnapshotUri xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><foreign:ProfileToken xmlns:foreign="urn:foreign">a</foreign:ProfileToken></tr2:GetSnapshotUri>`,
		},
		{
			name:   "Media1 GetStreamUri missing StreamSetup",
			action: fakedevice.ActionMedia1GetStreamURI,
			body:   `<trt:GetStreamUri xmlns:trt="http://www.onvif.org/ver10/media/wsdl"><trt:ProfileToken>profile_main</trt:ProfileToken></trt:GetStreamUri>`,
		},
		{
			name:   "Media2 GetStreamUri foreign child",
			action: fakedevice.ActionMedia2GetStreamURI,
			body:   `<tr2:GetStreamUri xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><tr2:Protocol>RTSP</tr2:Protocol><tr2:Other>x</tr2:Other></tr2:GetStreamUri>`,
		},
		{
			name:   "GetVideoSources with child",
			action: fakedevice.ActionMedia1GetVideoSources,
			body:   `<trt:GetVideoSources xmlns:trt="http://www.onvif.org/ver10/media/wsdl"><trt:Extra/></trt:GetVideoSources>`,
		},
		{
			name:   "GetVideoEncoderConfigurationOptions duplicate token",
			action: fakedevice.ActionMedia2GetVideoEncoderConfigurationOptions,
			body:   `<tr2:GetVideoEncoderConfigurationOptions xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><tr2:ProfileToken>a</tr2:ProfileToken><tr2:ProfileToken>b</tr2:ProfileToken></tr2:GetVideoEncoderConfigurationOptions>`,
		},
	} {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			device := newDevice(t, fakedevice.Config{})
			server := httptest.NewServer(device)
			defer server.Close()
			client := newSOAPClient(t, nil)

			_, err := client.Call(context.Background(), server.URL, soap.Request{Action: tt.action, BodyXML: []byte(tt.body)})
			if !errors.Is(err, soap.ErrInvalidArgVal) {
				t.Fatalf("error = %v, want ErrInvalidArgVal", err)
			}
		})
	}
}

func TestMediaWrapperValidationAcceptsOptionalChildren(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	// Optional ConfigurationToken + ProfileToken on options is accepted.
	response := mediaCall(t, client, server.URL, fakedevice.ActionMedia2GetVideoEncoderConfigurationOptions,
		`<tr2:GetVideoEncoderConfigurationOptions xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"><tr2:ConfigurationToken>venc_main</tr2:ConfigurationToken><tr2:ProfileToken>profile_main</tr2:ProfileToken></tr2:GetVideoEncoderConfigurationOptions>`)
	if !strings.Contains(string(response.RawXML), "GetVideoEncoderConfigurationOptionsResponse") {
		t.Errorf("optional tokens rejected: %s", response.RawXML)
	}
}

func TestMediaForeignNamespaceOperationRejected(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	// A Media local name under a foreign namespace must not dispatch.
	_, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionMedia2GetProfiles,
		BodyXML: []byte(`<foreign:GetProfiles xmlns:foreign="urn:not-onvif-media"/>`),
	})
	if !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("foreign-namespace GetProfiles error = %v, want ErrInvalidArgVal", err)
	}
}

func TestMediaActionBodyMismatchRejected(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	// Header action says ver20 GetProfiles but the body carries ver10
	// GetVideoSources; the QName dispatch must reject the mismatch.
	_, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionMedia2GetProfiles,
		BodyXML: []byte(`<trt:GetVideoSources xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/>`),
	})
	if !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("action/body mismatch error = %v, want ErrInvalidArgVal", err)
	}
}

func TestMediaFaultAndUnsupported(t *testing.T) {
	t.Parallel()

	device := newDevice(t, fakedevice.Config{})
	server := httptest.NewServer(device)
	defer server.Close()
	client := newSOAPClient(t, nil)

	if err := device.SetFault(fakedevice.ActionMedia2GetProfiles, fakedevice.FaultSpec{Kind: fakedevice.FaultInvalidArgVal, Reason: "synthetic"}); err != nil {
		t.Fatalf("SetFault() error = %v", err)
	}
	_, err := client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionMedia2GetProfiles,
		BodyXML: []byte(`<tr2:GetProfiles xmlns:tr2="http://www.onvif.org/ver20/media/wsdl"/>`),
	})
	if !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("configured fault error = %v, want ErrInvalidArgVal", err)
	}
	device.ClearFault(fakedevice.ActionMedia2GetProfiles)

	device.SetUnsupported(fakedevice.ActionMedia1GetVideoSources, true)
	_, err = client.Call(context.Background(), server.URL, soap.Request{
		Action:  fakedevice.ActionMedia1GetVideoSources,
		BodyXML: []byte(`<trt:GetVideoSources xmlns:trt="http://www.onvif.org/ver10/media/wsdl"/>`),
	})
	if !errors.Is(err, soap.ErrActionNotSupported) {
		t.Fatalf("unsupported error = %v, want ErrActionNotSupported", err)
	}
}

// mustHost extracts host[:port] from a URL for assertions.
func mustHost(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL %q: %v", rawURL, err)
	}
	return parsed.Host
}
