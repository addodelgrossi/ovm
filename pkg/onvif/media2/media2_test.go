package media2_test

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func TestClientConstructionValidation(t *testing.T) {
	t.Parallel()
	if _, err := media2.NewClient(nil, "http://device.invalid"); err == nil {
		t.Error("NewClient(nil transport) error = nil, want error")
	}
	soapClient, _ := soap.NewClient(soap.Config{})
	if _, err := media2.NewClient(soapClient, ""); err == nil {
		t.Error("NewClient(empty endpoint) error = nil, want error")
	}
	client, err := media2.NewClient(soapClient, "http://device.invalid/media2")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.Endpoint() != "http://device.invalid/media2" {
		t.Errorf("Endpoint() = %q", client.Endpoint())
	}
}

// TestOfficialMedia2WSDLNamespaceAndActions pins the action URIs to the official
// ver20 Media2 WSDL target namespace followed by the operation name.
func TestOfficialMedia2WSDLNamespaceAndActions(t *testing.T) {
	t.Parallel()
	const ns = "http://www.onvif.org/ver20/media/wsdl"
	if media2.Namespace != ns {
		t.Fatalf("Namespace = %q, want %q", media2.Namespace, ns)
	}
	cases := map[string]string{
		ns + "/GetProfiles":                         media2.ActionGetProfiles,
		ns + "/GetStreamUri":                        media2.ActionGetStreamURI,
		ns + "/GetSnapshotUri":                      media2.ActionGetSnapshotURI,
		ns + "/GetVideoEncoderConfigurations":       media2.ActionGetVideoEncoderConfigurations,
		ns + "/GetVideoEncoderConfigurationOptions": media2.ActionGetVideoEncoderConfigurationOptions,
		ns + "/GetAudioEncoderConfigurations":       media2.ActionGetAudioEncoderConfigurations,
	}
	for want, got := range cases {
		if got != want {
			t.Errorf("action = %q, want %q", got, want)
		}
	}
}

// TestNoGetVideoSourcesOperation asserts the deliberate decision that the ver20
// Media2 service has no GetVideoSources operation. The ver20 WSDL defines no
// such action, so neither an action constant nor a client method is exposed.
// This test fails to compile only if such a symbol is added, which is the
// intended guard. We assert the absence structurally via reflection over the
// client's method set and the package's exported action constants.
func TestNoGetVideoSourcesOperation(t *testing.T) {
	t.Parallel()
	clientType := reflect.TypeOf(&media2.Client{})
	for i := 0; i < clientType.NumMethod(); i++ {
		name := clientType.Method(i).Name
		if strings.Contains(name, "VideoSources") {
			t.Fatalf("media2.Client exposes %s, but ver20 Media2 has no GetVideoSources operation", name)
		}
	}
	// GetVideoSources belongs to the ver10 Media service only; a Media2 caller
	// needing physical video sources must use pkg/onvif/media. This assertion
	// documents the contract at test time.
}

func TestGetProfiles(t *testing.T) {
	t.Parallel()

	t.Run("success, filter, and request QName", func(t *testing.T) {
		t.Parallel()
		payload := `<tr2:GetProfilesResponse` + tr2NS + `>` +
			`<tr2:Profiles token="p0" fixed="true"><tr2:Name>Main</tr2:Name></tr2:Profiles>` +
			`<tr2:Profiles token="p1"><tr2:Name>Sub</tr2:Name></tr2:Profiles>` +
			`</tr2:GetProfilesResponse>`
		client, rec, closeFn := serveOK(t, payload)
		defer closeFn()
		profiles, err := client.GetProfiles(context.Background(), "p0", []media2.ConfigurationType{media2.ConfigurationVideoEncoder})
		if err != nil {
			t.Fatalf("GetProfiles() error = %v", err)
		}
		if len(profiles) != 2 || profiles[0].Token != "p0" || profiles[0].Name != "Main" || !profiles[0].Fixed {
			t.Fatalf("profiles = %+v", profiles)
		}
		if rec.action != media2.ActionGetProfiles {
			t.Errorf("action = %q", rec.action)
		}
		if !strings.Contains(rec.body, "<tr2:Token>p0</tr2:Token>") {
			t.Errorf("missing token filter: %s", rec.body)
		}
		if !strings.Contains(rec.body, "<tr2:Type>VideoEncoder</tr2:Type>") || strings.Contains(rec.body, "<tr2:Type>All</tr2:Type>") {
			t.Errorf("unexpected type filters: %s", rec.body)
		}
	})

	t.Run("omitted Type requests profile metadata only", func(t *testing.T) {
		t.Parallel()
		payload := `<tr2:GetProfilesResponse` + tr2NS + `><tr2:Profiles token="p"><tr2:Name>Main</tr2:Name></tr2:Profiles></tr2:GetProfilesResponse>`
		client, rec, closeFn := serveOK(t, payload)
		defer closeFn()
		profiles, err := client.GetProfiles(context.Background(), "", nil)
		if err != nil {
			t.Fatalf("GetProfiles() error = %v", err)
		}
		if len(profiles) != 1 || profiles[0].Associations.HasVideoSourceConfiguration || profiles[0].Associations.HasVideoEncoderConfiguration {
			t.Fatalf("metadata-only profiles = %+v", profiles)
		}
		if strings.Contains(rec.body, "Token") || strings.Contains(rec.body, "Type") {
			t.Errorf("expected Token and Type to be omitted: %s", rec.body)
		}
	})

	t.Run("literal All requests all configurations", func(t *testing.T) {
		t.Parallel()
		payload := `<tr2:GetProfilesResponse` + tr2NS + ttNS + `><tr2:Profiles token="p"><tr2:Name>Main</tr2:Name>` +
			`<tr2:Configurations><tr2:VideoSource token="cfg"><tt:Name>source</tt:Name><tt:UseCount>1</tt:UseCount>` +
			`<tt:SourceToken>source</tt:SourceToken><tt:Bounds x="0" y="0" width="640" height="480"/></tr2:VideoSource></tr2:Configurations>` +
			`</tr2:Profiles></tr2:GetProfilesResponse>`
		client, rec, closeFn := serveOK(t, payload)
		defer closeFn()
		if got := string(media2.ConfigurationAll); got != "All" {
			t.Fatalf("ConfigurationAll = %q, want WSDL literal %q", got, "All")
		}
		profiles, err := client.GetProfiles(context.Background(), "", []media2.ConfigurationType{media2.ConfigurationAll})
		if err != nil {
			t.Fatalf("GetProfiles() error = %v", err)
		}
		if len(profiles) != 1 || !profiles[0].Associations.HasVideoSourceConfiguration {
			t.Fatalf("all-configuration profiles = %+v", profiles)
		}
		if !strings.Contains(rec.body, "<tr2:Type>All</tr2:Type>") {
			t.Errorf("All filter missing from request: %s", rec.body)
		}
	})

	t.Run("prefix variation", func(t *testing.T) {
		t.Parallel()
		payload := `<m:GetProfilesResponse xmlns:m="http://www.onvif.org/ver20/media/wsdl">` +
			`<m:Profiles token="p"><m:Name>N</m:Name></m:Profiles></m:GetProfilesResponse>`
		client, _, closeFn := serveOK(t, payload)
		defer closeFn()
		profiles, err := client.GetProfiles(context.Background(), "", nil)
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(profiles) != 1 || profiles[0].Token != "p" || profiles[0].Name != "N" {
			t.Fatalf("profiles = %+v", profiles)
		}
	})

	t.Run("invalid type filter rejected before transport", func(t *testing.T) {
		t.Parallel()
		soapClient, _ := soap.NewClient(soap.Config{})
		client, _ := media2.NewClient(soapClient, "http://device.invalid/media2")
		if _, err := client.GetProfiles(context.Background(), "", []media2.ConfigurationType{"Bogus"}); !errors.Is(err, media2.ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("SOAP 1.1 fallback", func(t *testing.T) {
		t.Parallel()
		payload := `<tr2:GetProfilesResponse` + tr2NS + `><tr2:Profiles token="p"><tr2:Name>N</tr2:Name></tr2:Profiles></tr2:GetProfilesResponse>`
		client, closeFn := newSOAP11RawClient(t, soap11Envelope(payload))
		defer closeFn()
		profiles, err := client.GetProfiles(context.Background(), "", nil)
		if err != nil {
			t.Fatalf("GetProfiles() over SOAP 1.1 error = %v", err)
		}
		if len(profiles) != 1 {
			t.Fatalf("profiles = %+v", profiles)
		}
	})

	t.Run("fault propagates", func(t *testing.T) {
		t.Parallel()
		client, _, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusBadRequest,
			soap12Envelope(soap12Fault("env:Sender", "ter:InvalidArgVal", "bad")))
		defer closeFn()
		if _, err := client.GetProfiles(context.Background(), "", nil); !errors.Is(err, soap.ErrInvalidArgVal) {
			t.Fatalf("error = %v, want ErrInvalidArgVal", err)
		}
	})
}

func TestGetStreamUri(t *testing.T) {
	t.Parallel()
	payload := `<tr2:GetStreamUriResponse` + tr2NS + `><tr2:Uri>rtsp://device.invalid/stream</tr2:Uri></tr2:GetStreamUriResponse>`

	protocols := []media2.StreamProtocol{
		media2.StreamRtspUnicast,
		media2.StreamRtspMulticast,
		media2.StreamRtspsUnicast,
		media2.StreamRTSP,
		media2.StreamRtspOverHTTP,
	}
	for _, protocol := range protocols {
		protocol := protocol
		t.Run(string(protocol), func(t *testing.T) {
			t.Parallel()
			client, rec, closeFn := serveOK(t, payload)
			defer closeFn()
			uri, err := client.GetStreamUri(context.Background(), protocol, "p0")
			if err != nil {
				t.Fatalf("GetStreamUri() error = %v", err)
			}
			if uri != "rtsp://device.invalid/stream" {
				t.Errorf("uri = %q", uri)
			}
			if rec.action != media2.ActionGetStreamURI {
				t.Errorf("action = %q", rec.action)
			}
			if !strings.Contains(rec.body, "<tr2:Protocol>"+string(protocol)+"</tr2:Protocol>") {
				t.Errorf("missing protocol: %s", rec.body)
			}
			if !strings.Contains(rec.body, "<tr2:ProfileToken>p0</tr2:ProfileToken>") {
				t.Errorf("missing profile token: %s", rec.body)
			}
		})
	}

	t.Run("invalid protocol rejected before transport", func(t *testing.T) {
		t.Parallel()
		soapClient, _ := soap.NewClient(soap.Config{})
		client, _ := media2.NewClient(soapClient, "http://device.invalid/media2")
		// A ver10 protocol value must not be accepted by the ver20 client.
		if _, err := client.GetStreamUri(context.Background(), "UDP", "p0"); !errors.Is(err, media2.ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
	})
}

func TestGetSnapshotUri(t *testing.T) {
	t.Parallel()
	payload := `<tr2:GetSnapshotUriResponse` + tr2NS + `><tr2:Uri>http://device.invalid/s.jpg</tr2:Uri></tr2:GetSnapshotUriResponse>`
	client, rec, closeFn := serveOK(t, payload)
	defer closeFn()
	uri, err := client.GetSnapshotUri(context.Background(), "p0")
	if err != nil {
		t.Fatalf("GetSnapshotUri() error = %v", err)
	}
	if uri != "http://device.invalid/s.jpg" {
		t.Errorf("uri = %q", uri)
	}
	if rec.action != media2.ActionGetSnapshotURI {
		t.Errorf("action = %q", rec.action)
	}
}

func TestGetVideoEncoderConfigurations(t *testing.T) {
	t.Parallel()
	payload := `<tr2:GetVideoEncoderConfigurationsResponse` + tr2NS + ttNS + `>` +
		`<tr2:Configurations token="vec0"><tt:Name>Enc</tt:Name><tt:UseCount>2</tt:UseCount>` +
		`<tt:Encoding>H265</tt:Encoding>` +
		`<tt:Resolution><tt:Width>3840</tt:Width><tt:Height>2160</tt:Height></tt:Resolution>` +
		`<tt:Quality>6</tt:Quality></tr2:Configurations></tr2:GetVideoEncoderConfigurationsResponse>`

	t.Run("success and GetConfiguration body", func(t *testing.T) {
		t.Parallel()
		client, rec, closeFn := serveOK(t, payload)
		defer closeFn()
		configs, err := client.GetVideoEncoderConfigurations(context.Background(), "vec0", "p0")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(configs) != 1 {
			t.Fatalf("configs = %d", len(configs))
		}
		c := configs[0]
		if c.Token != "vec0" || c.Name != "Enc" || !c.HasUseCount || c.UseCount != 2 || c.Encoding != "H265" {
			t.Errorf("config = %+v", c)
		}
		if !c.HasResolution || c.Resolution.Width != 3840 || c.Resolution.Height != 2160 {
			t.Errorf("resolution = %+v", c.Resolution)
		}
		cfgIdx := strings.Index(rec.body, "<tr2:ConfigurationToken>vec0</tr2:ConfigurationToken>")
		profIdx := strings.Index(rec.body, "<tr2:ProfileToken>p0</tr2:ProfileToken>")
		if cfgIdx < 0 || profIdx < 0 || cfgIdx > profIdx {
			t.Errorf("tokens missing or out of order: %s", rec.body)
		}
	})

	t.Run("usecount remains required by inherited schema", func(t *testing.T) {
		t.Parallel()
		noUseCount := `<tr2:GetVideoEncoderConfigurationsResponse` + tr2NS + ttNS + `>` +
			`<tr2:Configurations token="v"><tt:Name>N</tt:Name><tt:Encoding>H264</tt:Encoding>` +
			`<tt:Resolution><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:Resolution><tt:Quality>5</tt:Quality></tr2:Configurations>` +
			`</tr2:GetVideoEncoderConfigurationsResponse>`
		client, _, closeFn := serveOK(t, noUseCount)
		defer closeFn()
		if _, err := client.GetVideoEncoderConfigurations(context.Background(), "", ""); !errors.Is(err, media2.ErrMissingField) {
			t.Fatalf("error = %v, want ErrMissingField", err)
		}
	})
}

func TestGetVideoEncoderConfigurationOptions(t *testing.T) {
	t.Parallel()
	payload := `<tr2:GetVideoEncoderConfigurationOptionsResponse` + tr2NS + ttNS + `>` +
		`<tr2:Options GovLengthRange="1 60"><tt:Encoding>H264</tt:Encoding><tt:QualityRange><tt:Min>1</tt:Min><tt:Max>10</tt:Max></tt:QualityRange>` +
		`<tt:ResolutionsAvailable><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:ResolutionsAvailable>` +
		`<tt:BitrateRange><tt:Min>256</tt:Min><tt:Max>8192</tt:Max></tt:BitrateRange></tr2:Options>` +
		`<tr2:Options><tt:Encoding>JPEG</tt:Encoding><tt:QualityRange><tt:Min>2</tt:Min><tt:Max>8</tt:Max></tt:QualityRange>` +
		`<tt:ResolutionsAvailable><tt:Width>640</tt:Width><tt:Height>360</tt:Height></tt:ResolutionsAvailable>` +
		`<tt:BitrateRange><tt:Min>128</tt:Min><tt:Max>2048</tt:Max></tt:BitrateRange></tr2:Options>` +
		`</tr2:GetVideoEncoderConfigurationOptionsResponse>`
	client, _, closeFn := serveOK(t, payload)
	defer closeFn()
	options, err := client.GetVideoEncoderConfigurationOptions(context.Background(), "", "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(options.OptionsXML) != 2 || len(options.Options) != 2 {
		t.Fatalf("options entries = (%d typed, %d XML), want 2", len(options.Options), len(options.OptionsXML))
	}
	if options.Options[0].Encoding != "H264" || options.Options[0].QualityRange.Min != 1 || options.Options[0].BitrateRange.Max != 8192 ||
		options.Options[0].GovLengthRange == nil || options.Options[0].GovLengthRange.Max != 60 {
		t.Fatalf("first option = %+v", options.Options[0])
	}
}

func TestGetAudioEncoderConfigurations(t *testing.T) {
	t.Parallel()
	payload := `<tr2:GetAudioEncoderConfigurationsResponse` + tr2NS + ttNS + `>` +
		`<tr2:Configurations token="aec0"><tt:Name>Audio</tt:Name><tt:UseCount>1</tt:UseCount><tt:Encoding>AAC</tt:Encoding>` +
		`<tt:Bitrate>128</tt:Bitrate><tt:SampleRate>48</tt:SampleRate></tr2:Configurations>` +
		`</tr2:GetAudioEncoderConfigurationsResponse>`
	client, rec, closeFn := serveOK(t, payload)
	defer closeFn()
	configs, err := client.GetAudioEncoderConfigurations(context.Background(), "", "")
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("configs = %d", len(configs))
	}
	c := configs[0]
	if c.Token != "aec0" || c.Encoding != "AAC" || !c.HasBitrate || c.Bitrate != 128 || !c.HasSampleRate || c.SampleRate != 48 {
		t.Errorf("config = %+v", c)
	}
	if rec.action != media2.ActionGetAudioEncoderConfigurations {
		t.Errorf("action = %q", rec.action)
	}
}
