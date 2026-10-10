package imaging_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/imaging"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

type recorded struct{ body string }

func serve(t *testing.T, status int, payload string) (*imaging.Client, *recorded) {
	t.Helper()
	rec := &recorded{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec.body = string(raw)
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `<?xml version="1.0"?><env:Envelope xmlns:env="http://www.w3.org/2003/05/soap-envelope"><env:Body>`+payload+`</env:Body></env:Envelope>`)
	}))
	t.Cleanup(server.Close)
	transport, err := soap.NewClient(soap.Config{})
	if err != nil {
		t.Fatalf("soap.NewClient() error = %v", err)
	}
	client, err := imaging.NewClient(transport, server.URL)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	return client, rec
}

const settingsPayload = `<i:GetImagingSettingsResponse xmlns:i="http://www.onvif.org/ver20/imaging/wsdl" xmlns:s="http://www.onvif.org/ver10/schema">` +
	`<i:ImagingSettings><s:Brightness>55</s:Brightness><s:ColorSaturation>40.5</s:ColorSaturation><s:Contrast>60</s:Contrast>` +
	`<s:Exposure><s:Mode>AUTO</s:Mode></s:Exposure><s:IrCutFilter>AUTO</s:IrCutFilter><s:Sharpness>30</s:Sharpness>` +
	`</i:ImagingSettings></i:GetImagingSettingsResponse>`

func TestGetImagingSettingsDecodesWithAnyPrefix(t *testing.T) {
	t.Parallel()
	client, rec := serve(t, http.StatusOK, settingsPayload)
	settings, err := client.GetImagingSettings(context.Background(), "vsrc<0>")
	if err != nil {
		t.Fatalf("GetImagingSettings() error = %v", err)
	}
	if *settings.Brightness != 55 || *settings.ColorSaturation != 40.5 || *settings.Contrast != 60 || *settings.Sharpness != 30 || *settings.IrCutFilter != imaging.IrCutAuto {
		t.Fatalf("settings = %+v", settings)
	}
	if !strings.Contains(rec.body, `<timg:VideoSourceToken>vsrc&lt;0&gt;</timg:VideoSourceToken>`) {
		t.Fatalf("request did not escape the token: %s", rec.body)
	}
}

func TestGetOptionsDecodesRangesAndModes(t *testing.T) {
	t.Parallel()
	client, _ := serve(t, http.StatusOK, `<timg:GetOptionsResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">`+
		`<timg:ImagingOptions><tt:Brightness><tt:Min>0</tt:Min><tt:Max>100</tt:Max></tt:Brightness>`+
		`<tt:IrCutFilterModes>ON</tt:IrCutFilterModes><tt:IrCutFilterModes>OFF</tt:IrCutFilterModes><tt:IrCutFilterModes>AUTO</tt:IrCutFilterModes>`+
		`<tt:Sharpness><tt:Min>0</tt:Min><tt:Max>15</tt:Max></tt:Sharpness></timg:ImagingOptions></timg:GetOptionsResponse>`)
	options, err := client.GetOptions(context.Background(), "vsrc0")
	if err != nil {
		t.Fatalf("GetOptions() error = %v", err)
	}
	if options.Brightness.Max != 100 || options.Sharpness.Max != 15 || options.Contrast != nil || len(options.IrCutFilterModes) != 3 {
		t.Fatalf("options = %+v", options)
	}
}

func TestImagingRejectsMalformedResponses(t *testing.T) {
	t.Parallel()
	for name, payload := range map[string]string{
		"wrong root":       `<timg:GetOptionsResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl"/>`,
		"foreign ns":       strings.ReplaceAll(settingsPayload, "ver20/imaging", "ver99/imaging"),
		"bad mode":         strings.Replace(settingsPayload, "<s:IrCutFilter>AUTO", "<s:IrCutFilter>DUSK", 1),
		"not a number":     strings.Replace(settingsPayload, "<s:Brightness>55", "<s:Brightness>bright", 1),
		"nan":              strings.Replace(settingsPayload, "<s:Brightness>55", "<s:Brightness>NaN", 1),
		"missing settings": `<timg:GetImagingSettingsResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl"/>`,
	} {
		client, _ := serve(t, http.StatusOK, payload)
		if _, err := client.GetImagingSettings(context.Background(), "vsrc0"); !errors.Is(err, imaging.ErrInvalidResponse) {
			t.Errorf("%s: error = %v", name, err)
		}
	}
	client, _ := serve(t, http.StatusOK, `<timg:GetOptionsResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl" xmlns:tt="http://www.onvif.org/ver10/schema">`+
		`<timg:ImagingOptions><tt:Brightness><tt:Min>100</tt:Min><tt:Max>0</tt:Max></tt:Brightness></timg:ImagingOptions></timg:GetOptionsResponse>`)
	if _, err := client.GetOptions(context.Background(), "vsrc0"); !errors.Is(err, imaging.ErrInvalidResponse) {
		t.Fatalf("inverted range error = %v", err)
	}
}

func TestSetImagingSettingsSendsOnlyModeledFieldsInSchemaOrder(t *testing.T) {
	t.Parallel()
	client, rec := serve(t, http.StatusOK, `<timg:SetImagingSettingsResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl"/>`)
	brightness, sharpness := 70.0, 10.0
	mode := imaging.IrCutOn
	if err := client.SetImagingSettings(context.Background(), "vsrc0", imaging.Settings{Brightness: &brightness, IrCutFilter: &mode, Sharpness: &sharpness}, true); err != nil {
		t.Fatalf("SetImagingSettings() error = %v", err)
	}
	want := `<timg:ImagingSettings><tt:Brightness>70</tt:Brightness><tt:IrCutFilter>ON</tt:IrCutFilter><tt:Sharpness>10</tt:Sharpness></timg:ImagingSettings><timg:ForcePersistence>true</timg:ForcePersistence>`
	if !strings.Contains(rec.body, want) {
		t.Fatalf("request body = %s", rec.body)
	}
}

func TestSetImagingSettingsValidatesAndSurfacesFaults(t *testing.T) {
	t.Parallel()
	client, rec := serve(t, http.StatusOK, `<timg:SetImagingSettingsResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl"/>`)
	bad := imaging.IrCutFilterMode("DUSK")
	if err := client.SetImagingSettings(context.Background(), "vsrc0", imaging.Settings{IrCutFilter: &bad}, false); !errors.Is(err, imaging.ErrInvalidInput) || rec.body != "" {
		t.Fatalf("invalid mode: error = %v, sent = %v", err, rec.body != "")
	}
	if err := client.SetImagingSettings(context.Background(), "", imaging.Settings{}, false); !errors.Is(err, imaging.ErrInvalidInput) {
		t.Fatalf("empty token error = %v", err)
	}
	faulting, _ := serve(t, http.StatusInternalServerError, `<env:Fault xmlns:env="http://www.w3.org/2003/05/soap-envelope" xmlns:ter="http://www.onvif.org/ver10/error">`+
		`<env:Code><env:Value>env:Sender</env:Value><env:Subcode><env:Value>ter:InvalidArgVal</env:Value></env:Subcode></env:Code>`+
		`<env:Reason><env:Text xml:lang="en">SettingsInvalid</env:Text></env:Reason></env:Fault>`)
	mode := imaging.IrCutOff
	if err := faulting.SetImagingSettings(context.Background(), "vsrc0", imaging.Settings{IrCutFilter: &mode}, false); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("fault error = %v", err)
	}
	unexpected, _ := serve(t, http.StatusOK, `<timg:SetImagingSettingsResponse xmlns:timg="http://www.onvif.org/ver20/imaging/wsdl"><x/></timg:SetImagingSettingsResponse>`)
	if err := unexpected.SetImagingSettings(context.Background(), "vsrc0", imaging.Settings{IrCutFilter: &mode}, false); !errors.Is(err, imaging.ErrInvalidResponse) {
		t.Fatalf("non-empty response error = %v", err)
	}
}
