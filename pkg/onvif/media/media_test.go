package media_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func TestClientConstructionValidation(t *testing.T) {
	t.Parallel()
	if _, err := media.NewClient(nil, "http://device.invalid"); err == nil {
		t.Error("NewClient(nil transport) error = nil, want error")
	}
	soapClient, err := soap.NewClient(soap.Config{})
	if err != nil {
		t.Fatalf("soap.NewClient() error = %v", err)
	}
	if _, err := media.NewClient(soapClient, ""); err == nil {
		t.Error("NewClient(empty endpoint) error = nil, want error")
	}
	client, err := media.NewClient(soapClient, "http://device.invalid/media")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.Endpoint() != "http://device.invalid/media" {
		t.Errorf("Endpoint() = %q", client.Endpoint())
	}
}

// TestOfficialMediaWSDLNamespaceAndActions pins each literal published by the
// ver10 Media WSDL, including the anomalous GetVideoSources soapAction.
func TestOfficialMediaWSDLNamespaceAndActions(t *testing.T) {
	t.Parallel()
	const ns = "http://www.onvif.org/ver10/media/wsdl"
	if media.Namespace != ns {
		t.Fatalf("Namespace = %q, want %q", media.Namespace, ns)
	}
	cases := map[string]string{
		ns + "/GetProfiles":    media.ActionGetProfiles,
		ns + "/GetStreamUri":   media.ActionGetStreamURI,
		ns + "/GetSnapshotUri": media.ActionGetSnapshotURI,
		"http://www.onvif.org/ver10/media/wsdlGetVideoSources/": media.ActionGetVideoSources,
		ns + "/GetVideoEncoderConfigurations":                   media.ActionGetVideoEncoderConfigurations,
		ns + "/GetVideoEncoderConfigurationOptions":             media.ActionGetVideoEncoderConfigurationOptions,
		ns + "/GetAudioEncoderConfigurations":                   media.ActionGetAudioEncoderConfigurations,
	}
	for want, got := range cases {
		if got != want {
			t.Errorf("action = %q, want %q", got, want)
		}
	}
}

func TestGetProfiles(t *testing.T) {
	t.Parallel()

	t.Run("success and request QName", func(t *testing.T) {
		t.Parallel()
		payload := `<trt:GetProfilesResponse` + trtNS + ttNS + `>` +
			`<trt:Profiles token="profile0" fixed="true"><tt:Name>Main</tt:Name></trt:Profiles>` +
			`<trt:Profiles token="profile1"><tt:Name>Sub</tt:Name></trt:Profiles>` +
			`</trt:GetProfilesResponse>`
		client, rec, closeFn := serveOK(t, payload)
		defer closeFn()
		profiles, err := client.GetProfiles(context.Background())
		if err != nil {
			t.Fatalf("GetProfiles() error = %v", err)
		}
		if len(profiles) != 2 {
			t.Fatalf("profiles = %d, want 2", len(profiles))
		}
		if profiles[0].Token != "profile0" || profiles[0].Name != "Main" || !profiles[0].Fixed || !profiles[0].HasFixed {
			t.Errorf("profile0 = %+v", profiles[0])
		}
		if profiles[1].Token != "profile1" || profiles[1].HasFixed {
			t.Errorf("profile1 = %+v", profiles[1])
		}
		if rec.action != media.ActionGetProfiles {
			t.Errorf("SOAPAction = %q, want %q", rec.action, media.ActionGetProfiles)
		}
		if !strings.Contains(rec.body, "GetProfiles") || !strings.Contains(rec.body, "http://www.onvif.org/ver10/media/wsdl") {
			t.Errorf("request body missing GetProfiles QName: %s", rec.body)
		}
	})

	t.Run("empty profile list is valid", func(t *testing.T) {
		t.Parallel()
		payload := `<trt:GetProfilesResponse` + trtNS + `></trt:GetProfilesResponse>`
		client, _, closeFn := serveOK(t, payload)
		defer closeFn()
		profiles, err := client.GetProfiles(context.Background())
		if err != nil {
			t.Fatalf("GetProfiles() error = %v", err)
		}
		if len(profiles) != 0 {
			t.Fatalf("profiles = %d, want 0", len(profiles))
		}
	})

	t.Run("prefix variation", func(t *testing.T) {
		t.Parallel()
		// A completely different, non-reserved prefix for both namespaces must
		// decode identically because matching is on namespace URI only.
		payload := `<m:GetProfilesResponse xmlns:m="http://www.onvif.org/ver10/media/wsdl" xmlns:s="http://www.onvif.org/ver10/schema">` +
			`<m:Profiles token="p"><s:Name>N</s:Name></m:Profiles></m:GetProfilesResponse>`
		client, _, closeFn := serveOK(t, payload)
		defer closeFn()
		profiles, err := client.GetProfiles(context.Background())
		if err != nil {
			t.Fatalf("GetProfiles() error = %v", err)
		}
		if len(profiles) != 1 || profiles[0].Token != "p" || profiles[0].Name != "N" {
			t.Fatalf("profiles = %+v", profiles)
		}
	})

	t.Run("SOAP 1.1 fallback", func(t *testing.T) {
		t.Parallel()
		payload := `<trt:GetProfilesResponse` + trtNS + ttNS + `><trt:Profiles token="p"><tt:Name>N</tt:Name></trt:Profiles></trt:GetProfilesResponse>`
		client, closeFn := newSOAP11RawClient(t, soap11Envelope(payload))
		defer closeFn()
		profiles, err := client.GetProfiles(context.Background())
		if err != nil {
			t.Fatalf("GetProfiles() over SOAP 1.1 error = %v", err)
		}
		if len(profiles) != 1 || profiles[0].Token != "p" {
			t.Fatalf("profiles = %+v", profiles)
		}
	})

	t.Run("fault propagates", func(t *testing.T) {
		t.Parallel()
		client, _, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusBadRequest,
			soap12Envelope(soap12Fault("env:Sender", "ter:NotAuthorized", "no")))
		defer closeFn()
		if _, err := client.GetProfiles(context.Background()); !errors.Is(err, soap.ErrNotAuthorized) {
			t.Fatalf("error = %v, want ErrNotAuthorized", err)
		}
	})

	t.Run("unsupported action propagates", func(t *testing.T) {
		t.Parallel()
		client, _, closeFn := newRawClient(t, "application/soap+xml; charset=utf-8", http.StatusBadRequest,
			soap12Envelope(soap12Fault("env:Receiver", "ter:ActionNotSupported", "no")))
		defer closeFn()
		if _, err := client.GetProfiles(context.Background()); !errors.Is(err, soap.ErrActionNotSupported) {
			t.Fatalf("error = %v, want ErrActionNotSupported", err)
		}
	})
}

func TestGetStreamUriVariants(t *testing.T) {
	t.Parallel()
	okPayload := `<trt:GetStreamUriResponse` + trtNS + ttNS + `><trt:MediaUri>` +
		`<tt:Uri>rtsp://device.invalid/stream</tt:Uri>` +
		`<tt:InvalidAfterConnect>false</tt:InvalidAfterConnect>` +
		`<tt:InvalidAfterReboot>false</tt:InvalidAfterReboot>` +
		`<tt:Timeout>PT30S</tt:Timeout>` +
		`</trt:MediaUri></trt:GetStreamUriResponse>`

	variants := []struct {
		name  string
		setup media.StreamSetup
	}{
		{"unicast-udp", media.StreamSetup{Stream: media.StreamRTPUnicast, Protocol: media.TransportUDP}},
		{"unicast-rtsp-tcp", media.StreamSetup{Stream: media.StreamRTPUnicast, Protocol: media.TransportRTSP}},
		{"unicast-tcp", media.StreamSetup{Stream: media.StreamRTPUnicast, Protocol: media.TransportTCP}},
		{"multicast-udp", media.StreamSetup{Stream: media.StreamRTPMulticast, Protocol: media.TransportUDP}},
	}
	for _, variant := range variants {
		variant := variant
		t.Run(variant.name, func(t *testing.T) {
			t.Parallel()
			client, rec, closeFn := serveOK(t, okPayload)
			defer closeFn()
			uri, err := client.GetStreamUri(context.Background(), "profile0", variant.setup)
			if err != nil {
				t.Fatalf("GetStreamUri() error = %v", err)
			}
			if uri.URI != "rtsp://device.invalid/stream" {
				t.Errorf("URI = %q", uri.URI)
			}
			if !uri.HasInvalidAfterConnect || uri.InvalidAfterConnect {
				t.Errorf("InvalidAfterConnect = %v (has=%v)", uri.InvalidAfterConnect, uri.HasInvalidAfterConnect)
			}
			if uri.Timeout != "PT30S" {
				t.Errorf("Timeout = %q", uri.Timeout)
			}
			if rec.action != media.ActionGetStreamURI {
				t.Errorf("SOAPAction = %q", rec.action)
			}
			if !strings.Contains(rec.body, "<tt:Stream>"+string(variant.setup.Stream)+"</tt:Stream>") {
				t.Errorf("request body missing stream type: %s", rec.body)
			}
			if !strings.Contains(rec.body, "<tt:Protocol>"+string(variant.setup.Protocol)+"</tt:Protocol>") {
				t.Errorf("request body missing protocol: %s", rec.body)
			}
			if !strings.Contains(rec.body, "<trt:ProfileToken>profile0</trt:ProfileToken>") {
				t.Errorf("request body missing profile token: %s", rec.body)
			}
		})
	}

	t.Run("invalid stream enum rejected before transport", func(t *testing.T) {
		t.Parallel()
		soapClient, _ := soap.NewClient(soap.Config{})
		client, _ := media.NewClient(soapClient, "http://device.invalid/media")
		_, err := client.GetStreamUri(context.Background(), "profile0", media.StreamSetup{Stream: "bogus", Protocol: media.TransportUDP})
		if !errors.Is(err, media.ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("invalid protocol enum rejected before transport", func(t *testing.T) {
		t.Parallel()
		soapClient, _ := soap.NewClient(soap.Config{})
		client, _ := media.NewClient(soapClient, "http://device.invalid/media")
		_, err := client.GetStreamUri(context.Background(), "profile0", media.StreamSetup{Stream: media.StreamRTPUnicast, Protocol: "bogus"})
		if !errors.Is(err, media.ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
	})
}

func TestGetSnapshotUri(t *testing.T) {
	t.Parallel()
	payload := `<trt:GetSnapshotUriResponse` + trtNS + ttNS + `><trt:MediaUri>` +
		`<tt:Uri>http://device.invalid/snapshot.jpg</tt:Uri>` +
		`<tt:InvalidAfterConnect>false</tt:InvalidAfterConnect><tt:InvalidAfterReboot>false</tt:InvalidAfterReboot><tt:Timeout>PT0S</tt:Timeout>` +
		`</trt:MediaUri></trt:GetSnapshotUriResponse>`
	client, rec, closeFn := serveOK(t, payload)
	defer closeFn()
	uri, err := client.GetSnapshotUri(context.Background(), "profile0")
	if err != nil {
		t.Fatalf("GetSnapshotUri() error = %v", err)
	}
	if uri.URI != "http://device.invalid/snapshot.jpg" {
		t.Errorf("URI = %q", uri.URI)
	}
	if rec.action != media.ActionGetSnapshotURI {
		t.Errorf("SOAPAction = %q", rec.action)
	}
	if !strings.Contains(rec.body, "GetSnapshotUri") {
		t.Errorf("request body missing GetSnapshotUri: %s", rec.body)
	}
}

func TestGetVideoSources(t *testing.T) {
	t.Parallel()
	payload := `<trt:GetVideoSourcesResponse` + trtNS + ttNS + `>` +
		`<trt:VideoSources token="vs0"><tt:Framerate>30</tt:Framerate>` +
		`<tt:Resolution><tt:Width>1920</tt:Width><tt:Height>1080</tt:Height></tt:Resolution></trt:VideoSources>` +
		`</trt:GetVideoSourcesResponse>`
	client, rec, closeFn := serveOK(t, payload)
	defer closeFn()
	sources, err := client.GetVideoSources(context.Background())
	if err != nil {
		t.Fatalf("GetVideoSources() error = %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("sources = %d, want 1", len(sources))
	}
	src := sources[0]
	if src.Token != "vs0" || !src.HasFramerate || src.Framerate != 30 {
		t.Errorf("source = %+v", src)
	}
	if !src.HasResolution || src.Resolution.Width != 1920 || src.Resolution.Height != 1080 {
		t.Errorf("resolution = %+v", src.Resolution)
	}
	if rec.action != media.ActionGetVideoSources {
		t.Errorf("SOAPAction = %q", rec.action)
	}
}

func TestGetVideoEncoderConfigurations(t *testing.T) {
	t.Parallel()
	payload := `<trt:GetVideoEncoderConfigurationsResponse` + trtNS + ttNS + `>` +
		`<trt:Configurations token="vec0"><tt:Name>Enc</tt:Name><tt:UseCount>2</tt:UseCount>` +
		`<tt:Encoding>H264</tt:Encoding>` +
		`<tt:Resolution><tt:Width>1280</tt:Width><tt:Height>720</tt:Height></tt:Resolution>` +
		`<tt:Quality>5</tt:Quality>` + validMedia1StreamingTail + `</trt:Configurations></trt:GetVideoEncoderConfigurationsResponse>`
	client, rec, closeFn := serveOK(t, payload)
	defer closeFn()
	configs, err := client.GetVideoEncoderConfigurations(context.Background())
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("configs = %d, want 1", len(configs))
	}
	c := configs[0]
	if c.Token != "vec0" || c.Name != "Enc" || c.UseCount != 2 || c.Encoding != "H264" {
		t.Errorf("config = %+v", c)
	}
	if !c.HasResolution || c.Resolution.Width != 1280 || c.Resolution.Height != 720 {
		t.Errorf("resolution = %+v", c.Resolution)
	}
	if !c.HasQuality || c.Quality != 5 {
		t.Errorf("quality = %v (has=%v)", c.Quality, c.HasQuality)
	}
	if rec.action != media.ActionGetVideoEncoderConfigurations {
		t.Errorf("SOAPAction = %q", rec.action)
	}
}

func TestGetVideoEncoderConfigurationOptions(t *testing.T) {
	t.Parallel()
	payload := `<trt:GetVideoEncoderConfigurationOptionsResponse` + trtNS + ttNS + `>` +
		`<trt:Options><tt:QualityRange><tt:Min>1</tt:Min><tt:Max>10</tt:Max></tt:QualityRange></trt:Options>` +
		`</trt:GetVideoEncoderConfigurationOptionsResponse>`

	t.Run("no tokens omits elements", func(t *testing.T) {
		t.Parallel()
		client, rec, closeFn := serveOK(t, payload)
		defer closeFn()
		options, err := client.GetVideoEncoderConfigurationOptions(context.Background(), "", "")
		if err != nil {
			t.Fatalf("error = %v", err)
		}
		if len(options.RawXML) == 0 {
			t.Error("Options RawXML is empty")
		}
		if strings.Contains(rec.body, "ConfigurationToken") || strings.Contains(rec.body, "ProfileToken") {
			t.Errorf("request body should omit tokens: %s", rec.body)
		}
	})

	t.Run("tokens included in schema order", func(t *testing.T) {
		t.Parallel()
		client, rec, closeFn := serveOK(t, payload)
		defer closeFn()
		if _, err := client.GetVideoEncoderConfigurationOptions(context.Background(), "cfg0", "profile0"); err != nil {
			t.Fatalf("error = %v", err)
		}
		cfgIdx := strings.Index(rec.body, "<trt:ConfigurationToken>cfg0</trt:ConfigurationToken>")
		profIdx := strings.Index(rec.body, "<trt:ProfileToken>profile0</trt:ProfileToken>")
		if cfgIdx < 0 || profIdx < 0 || cfgIdx > profIdx {
			t.Errorf("tokens missing or out of order: %s", rec.body)
		}
	})
}

func TestGetAudioEncoderConfigurations(t *testing.T) {
	t.Parallel()
	payload := `<trt:GetAudioEncoderConfigurationsResponse` + trtNS + ttNS + `>` +
		`<trt:Configurations token="aec0"><tt:Name>Audio</tt:Name><tt:UseCount>1</tt:UseCount>` +
		`<tt:Encoding>G711</tt:Encoding><tt:Bitrate>64</tt:Bitrate><tt:SampleRate>8</tt:SampleRate>` +
		validMedia1StreamingTail + `</trt:Configurations></trt:GetAudioEncoderConfigurationsResponse>`
	client, rec, closeFn := serveOK(t, payload)
	defer closeFn()
	configs, err := client.GetAudioEncoderConfigurations(context.Background())
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("configs = %d", len(configs))
	}
	c := configs[0]
	if c.Token != "aec0" || c.Encoding != "G711" || !c.HasBitrate || c.Bitrate != 64 || !c.HasSampleRate || c.SampleRate != 8 {
		t.Errorf("config = %+v", c)
	}
	if rec.action != media.ActionGetAudioEncoderConfigurations {
		t.Errorf("SOAPAction = %q", rec.action)
	}
}
