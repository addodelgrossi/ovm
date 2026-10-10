package fakedevice_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func TestMediaNestedWrapperAndEnumValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		action string
		body   string
	}{
		{"Media1 StreamSetup missing Transport", fakedevice.ActionMedia1GetStreamURI, `<m:GetStreamUri xmlns:m="http://www.onvif.org/ver10/media/wsdl" xmlns:t="http://www.onvif.org/ver10/schema"><m:StreamSetup><t:Stream>RTP-Unicast</t:Stream></m:StreamSetup><m:ProfileToken>profile_main</m:ProfileToken></m:GetStreamUri>`},
		{"Media1 StreamSetup invalid stream enum", fakedevice.ActionMedia1GetStreamURI, `<m:GetStreamUri xmlns:m="http://www.onvif.org/ver10/media/wsdl" xmlns:t="http://www.onvif.org/ver10/schema"><m:StreamSetup><t:Stream>unicast</t:Stream><t:Transport><t:Protocol>UDP</t:Protocol></t:Transport></m:StreamSetup><m:ProfileToken>profile_main</m:ProfileToken></m:GetStreamUri>`},
		{"Media1 StreamSetup duplicate protocol", fakedevice.ActionMedia1GetStreamURI, `<m:GetStreamUri xmlns:m="http://www.onvif.org/ver10/media/wsdl" xmlns:t="http://www.onvif.org/ver10/schema"><m:StreamSetup><t:Stream>RTP-Unicast</t:Stream><t:Transport><t:Protocol>UDP</t:Protocol><t:Protocol>TCP</t:Protocol></t:Transport></m:StreamSetup><m:ProfileToken>profile_main</m:ProfileToken></m:GetStreamUri>`},
		{"Media1 StreamSetup foreign Transport", fakedevice.ActionMedia1GetStreamURI, `<m:GetStreamUri xmlns:m="http://www.onvif.org/ver10/media/wsdl" xmlns:t="http://www.onvif.org/ver10/schema" xmlns:x="urn:foreign"><m:StreamSetup><t:Stream>RTP-Unicast</t:Stream><x:Transport><t:Protocol>UDP</t:Protocol></x:Transport></m:StreamSetup><m:ProfileToken>profile_main</m:ProfileToken></m:GetStreamUri>`},
		{"Media2 invalid protocol", fakedevice.ActionMedia2GetStreamURI, `<m:GetStreamUri xmlns:m="http://www.onvif.org/ver20/media/wsdl"><m:Protocol>UDP</m:Protocol><m:ProfileToken>profile_main</m:ProfileToken></m:GetStreamUri>`},
		{"Media2 mixed All profile filter", fakedevice.ActionMedia2GetProfiles, `<m:GetProfiles xmlns:m="http://www.onvif.org/ver20/media/wsdl"><m:Type>All</m:Type><m:Type>VideoEncoder</m:Type></m:GetProfiles>`},
		{"Media2 duplicate profile filter", fakedevice.ActionMedia2GetProfiles, `<m:GetProfiles xmlns:m="http://www.onvif.org/ver20/media/wsdl"><m:Type>VideoEncoder</m:Type><m:Type>VideoEncoder</m:Type></m:GetProfiles>`},
		{"Media2 configuration tokens out of order", fakedevice.ActionMedia2GetVideoEncoderConfigurations, `<m:GetVideoEncoderConfigurations xmlns:m="http://www.onvif.org/ver20/media/wsdl"><m:ProfileToken>profile_main</m:ProfileToken><m:ConfigurationToken>venc_main</m:ConfigurationToken></m:GetVideoEncoderConfigurations>`},
		{"Media2 audio unexpected child", fakedevice.ActionMedia2GetAudioEncoderConfigurations, `<m:GetAudioEncoderConfigurations xmlns:m="http://www.onvif.org/ver20/media/wsdl"><m:Unexpected/></m:GetAudioEncoderConfigurations>`},
		{"Media2 wrapper foreign attribute", fakedevice.ActionMedia2GetProfiles, `<m:GetProfiles xmlns:m="http://www.onvif.org/ver20/media/wsdl" xmlns:x="urn:foreign" x:mode="unsafe"/>`},
	}
	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			device := newDevice(t, fakedevice.Config{})
			server := newTestServer(t, device)
			client := newSOAPClient(t, nil)
			_, err := client.Call(context.Background(), server.URL, soap.Request{Action: testCase.action, BodyXML: []byte(testCase.body)})
			if !errors.Is(err, soap.ErrInvalidArgVal) {
				t.Fatalf("error = %v, want ErrInvalidArgVal", err)
			}
		})
	}
}

func TestMediaUnknownReferencesAreTypedFaults(t *testing.T) {
	t.Parallel()
	device := newDevice(t, fakedevice.Config{})
	server := newTestServer(t, device)
	client := newSOAPClient(t, nil)
	for _, request := range []soap.Request{
		{Action: fakedevice.ActionMedia2GetProfiles, BodyXML: []byte(`<m:GetProfiles xmlns:m="http://www.onvif.org/ver20/media/wsdl"><m:Token>missing</m:Token></m:GetProfiles>`)},
		{Action: fakedevice.ActionMedia2GetProfiles, BodyXML: []byte(`<m:GetProfiles xmlns:m="http://www.onvif.org/ver20/media/wsdl"><m:Token> profile_main </m:Token></m:GetProfiles>`)},
		{Action: fakedevice.ActionMedia1GetSnapshotURI, BodyXML: []byte(`<m:GetSnapshotUri xmlns:m="http://www.onvif.org/ver10/media/wsdl"><m:ProfileToken>missing</m:ProfileToken></m:GetSnapshotUri>`)},
		{Action: fakedevice.ActionMedia2GetAudioEncoderConfigurations, BodyXML: []byte(`<m:GetAudioEncoderConfigurations xmlns:m="http://www.onvif.org/ver20/media/wsdl"><m:ConfigurationToken>missing</m:ConfigurationToken></m:GetAudioEncoderConfigurations>`)},
	} {
		if _, err := client.Call(context.Background(), server.URL, request); !errors.Is(err, soap.ErrInvalidArgVal) {
			t.Fatalf("error = %v, want ErrInvalidArgVal", err)
		}
	}
}

func TestMediaRequestDTDAndComplexityBounds(t *testing.T) {
	t.Parallel()
	device := newDevice(t, fakedevice.Config{})
	server := newTestServer(t, device)
	client := newSOAPClient(t, nil)

	cases := []struct {
		name string
		body string
		want error
	}{
		{"DTD", `<!DOCTYPE x><m:GetProfiles xmlns:m="http://www.onvif.org/ver10/media/wsdl"/>`, soap.ErrDTDNotAllowed},
		{"depth", `<m:GetProfiles xmlns:m="http://www.onvif.org/ver10/media/wsdl">` + strings.Repeat(`<m:N>`, 100) + strings.Repeat(`</m:N>`, 100) + `</m:GetProfiles>`, soap.ErrInvalidArgVal},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := client.Call(context.Background(), server.URL, soap.Request{Action: fakedevice.ActionMedia1GetProfiles, BodyXML: []byte(testCase.body)})
			if !errors.Is(err, testCase.want) {
				t.Fatalf("error = %v, want %v", err, testCase.want)
			}
		})
	}
}

func TestMediaInventoryValidationIsBoundedAndSecretSafe(t *testing.T) {
	t.Parallel()
	secret := "inventory-secret-value"
	_, err := fakedevice.New(fakedevice.Config{MediaInventory: fakedevice.MediaInventory{
		Profiles: []fakedevice.MediaProfileData{{Token: secret + strings.Repeat("x", 64), Name: "main"}},
	}})
	if err == nil {
		t.Fatal("oversize media token accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("configuration error leaked media value: %v", err)
	}

	_, err = fakedevice.New(fakedevice.Config{JPEGSnapshot: []byte("not-a-jpeg-" + secret)})
	if err == nil {
		t.Fatal("invalid JPEG accepted")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("JPEG error leaked bytes: %v", err)
	}
}

// newTestServer centralizes cleanup for tests that expect fault responses.
func newTestServer(t *testing.T, handler *fakedevice.Device) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

func TestMedia1StreamSetupAcceptsDeclaredExtensionPoints(t *testing.T) {
	t.Parallel()
	device := newDevice(t, fakedevice.Config{})
	server := newTestServer(t, device)
	client := newSOAPClient(t, nil)
	body := `<m:GetStreamUri xmlns:m="http://www.onvif.org/ver10/media/wsdl" xmlns:t="http://www.onvif.org/ver10/schema" xmlns:x="urn:synthetic-extension">` +
		`<m:StreamSetup x:mode="synthetic"><t:Stream>RTP-Unicast</t:Stream><t:Transport><t:Protocol>RTSP</t:Protocol><t:Tunnel><x:Option>synthetic</x:Option></t:Tunnel></t:Transport><x:Extension/></m:StreamSetup>` +
		`<m:ProfileToken>profile_main</m:ProfileToken></m:GetStreamUri>`
	if _, err := client.Call(context.Background(), server.URL, soap.Request{Action: fakedevice.ActionMedia1GetStreamURI, BodyXML: []byte(body)}); err != nil {
		t.Fatalf("declared StreamSetup extension points were rejected: %v", err)
	}
}

func TestEveryFakeMediaOperationSupportsFaultAndUnsupportedInjection(t *testing.T) {
	t.Parallel()
	requests := []soap.Request{
		{Action: fakedevice.ActionMedia1GetProfiles, BodyXML: []byte(`<m:GetProfiles xmlns:m="http://www.onvif.org/ver10/media/wsdl"/>`)},
		{Action: fakedevice.ActionMedia1GetVideoSources, BodyXML: []byte(`<m:GetVideoSources xmlns:m="http://www.onvif.org/ver10/media/wsdl"/>`)},
		{Action: fakedevice.ActionMedia1GetVideoEncoderConfigurations, BodyXML: []byte(`<m:GetVideoEncoderConfigurations xmlns:m="http://www.onvif.org/ver10/media/wsdl"/>`)},
		{Action: fakedevice.ActionMedia1GetVideoEncoderConfigurationOptions, BodyXML: []byte(`<m:GetVideoEncoderConfigurationOptions xmlns:m="http://www.onvif.org/ver10/media/wsdl"/>`)},
		{Action: fakedevice.ActionMedia1GetAudioEncoderConfigurations, BodyXML: []byte(`<m:GetAudioEncoderConfigurations xmlns:m="http://www.onvif.org/ver10/media/wsdl"/>`)},
		{Action: fakedevice.ActionMedia1GetStreamURI, BodyXML: []byte(`<m:GetStreamUri xmlns:m="http://www.onvif.org/ver10/media/wsdl" xmlns:t="http://www.onvif.org/ver10/schema"><m:StreamSetup><t:Stream>RTP-Unicast</t:Stream><t:Transport><t:Protocol>UDP</t:Protocol></t:Transport></m:StreamSetup><m:ProfileToken>profile_main</m:ProfileToken></m:GetStreamUri>`)},
		{Action: fakedevice.ActionMedia1GetSnapshotURI, BodyXML: []byte(`<m:GetSnapshotUri xmlns:m="http://www.onvif.org/ver10/media/wsdl"><m:ProfileToken>profile_main</m:ProfileToken></m:GetSnapshotUri>`)},
		{Action: fakedevice.ActionMedia2GetProfiles, BodyXML: []byte(`<m:GetProfiles xmlns:m="http://www.onvif.org/ver20/media/wsdl"/>`)},
		{Action: fakedevice.ActionMedia2GetVideoEncoderConfigurations, BodyXML: []byte(`<m:GetVideoEncoderConfigurations xmlns:m="http://www.onvif.org/ver20/media/wsdl"/>`)},
		{Action: fakedevice.ActionMedia2GetVideoEncoderConfigurationOptions, BodyXML: []byte(`<m:GetVideoEncoderConfigurationOptions xmlns:m="http://www.onvif.org/ver20/media/wsdl"/>`)},
		{Action: fakedevice.ActionMedia2GetAudioEncoderConfigurations, BodyXML: []byte(`<m:GetAudioEncoderConfigurations xmlns:m="http://www.onvif.org/ver20/media/wsdl"/>`)},
		{Action: fakedevice.ActionMedia2GetStreamURI, BodyXML: []byte(`<m:GetStreamUri xmlns:m="http://www.onvif.org/ver20/media/wsdl"><m:Protocol>RTSP</m:Protocol><m:ProfileToken>profile_main</m:ProfileToken></m:GetStreamUri>`)},
		{Action: fakedevice.ActionMedia2GetSnapshotURI, BodyXML: []byte(`<m:GetSnapshotUri xmlns:m="http://www.onvif.org/ver20/media/wsdl"><m:ProfileToken>profile_main</m:ProfileToken></m:GetSnapshotUri>`)},
	}
	for _, request := range requests {
		request := request
		t.Run(request.Action, func(t *testing.T) {
			device := newDevice(t, fakedevice.Config{})
			server := newTestServer(t, device)
			client := newSOAPClient(t, nil)

			if err := device.SetFault(request.Action, fakedevice.FaultSpec{Kind: fakedevice.FaultInvalidArgVal, Reason: "synthetic"}); err != nil {
				t.Fatalf("SetFault() error = %v", err)
			}
			if _, err := client.Call(context.Background(), server.URL, request); !errors.Is(err, soap.ErrInvalidArgVal) {
				t.Fatalf("fault error = %v, want ErrInvalidArgVal", err)
			}
			device.ClearFault(request.Action)
			device.SetUnsupported(request.Action, true)
			if _, err := client.Call(context.Background(), server.URL, request); !errors.Is(err, soap.ErrActionNotSupported) {
				t.Fatalf("unsupported error = %v, want ErrActionNotSupported", err)
			}
		})
	}
}
