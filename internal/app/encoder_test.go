package app

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

func encoderFakeHost(t *testing.T, behavior fakedevice.EncoderSetBehavior, withMedia2 bool) (*fakedevice.Device, string) {
	t.Helper()
	return encoderFakeHostWith(t, behavior, withMedia2, 0)
}

func encoderFakeHostWith(t *testing.T, behavior fakedevice.EncoderSetBehavior, withMedia2 bool, ceiling int) (*fakedevice.Device, string) {
	t.Helper()
	srv := newUnstartedFake(t, func(base string) fakedevice.Config {
		services := []fakedevice.ServiceData{
			{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
			{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: base + "/onvif/media_service", VersionMajor: 25, VersionMinor: 6},
		}
		if withMedia2 {
			services = append(services, fakedevice.ServiceData{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6})
		}
		return fakedevice.Config{
			HTTPAuth:             fakedevice.HTTPAuthDigest,
			DigestAlgorithm:      "SHA-256",
			RequireUsernameToken: true,
			Username:             "admin",
			Password:             "encoder-secret",
			Inventory:            fakedevice.Inventory{Services: services},
			MediaInventory: fakedevice.MediaInventory{
				EncoderSetBehavior:    behavior,
				EncoderBitrateCeiling: ceiling,
				Media2EncoderOptions: []fakedevice.Media2EncoderOptionData{
					{Encoding: "H264", Profiles: []string{"Main", "High"}, FrameRates: []float64{30, 25, 15},
						Resolutions: []fakedevice.ResolutionData{{Width: 1920, Height: 1080}, {Width: 1280, Height: 720}},
						QualityMax:  10, BitrateMin: 256, BitrateMax: 8192, GovLengthMin: 1, GovLengthMax: 120},
					{Encoding: "H265", Profiles: []string{"Main"}, FrameRates: []float64{30, 25},
						Resolutions: []fakedevice.ResolutionData{{Width: 1920, Height: 1080}},
						QualityMax:  10, BitrateMin: 256, BitrateMax: 8192, GovLengthMin: 1, GovLengthMax: 120},
				},
			},
		}
	})
	return srv.device, srv.host
}

func encoderRequest(t *testing.T, host string) EncoderRequest {
	t.Helper()
	creds, err := NewCredentials("admin", "encoder-secret", false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	return EncoderRequest{Host: host, Credentials: creds, ProfileToken: "profile_main", Timeout: 10 * time.Second}
}

func fastEncoder() *productionMedia {
	return &productionMedia{readbackPolicy: &readbackPolicy{attempts: 2, delay: 10 * time.Millisecond}}
}

func h264Change() EncoderChange {
	return EncoderChange{Encoding: "H264", Width: 1280, Height: 720, FrameRate: 25, BitrateKbps: 2048, GOPLength: 50, CodecProfile: "High"}
}

func TestReadEncoderReturnsCurrentSettingsAndOptions(t *testing.T) {
	t.Parallel()
	_, host := encoderFakeHost(t, fakedevice.EncoderSetApply, true)
	view, err := fastEncoder().ReadEncoder(context.Background(), encoderRequest(t, host))
	if err != nil {
		t.Fatalf("ReadEncoder() error = %v", err)
	}
	if view.Current.Source != MediaServiceMedia2 || view.Current.Encoding != "H265" || view.Current.Width != 1920 || view.Current.ConfigurationToken != "venc_main" {
		t.Fatalf("current = %+v", view.Current)
	}
	if len(view.Codecs) != 2 || view.Codecs[0].Encoding != "H264" || !slices.Equal(view.Codecs[0].FrameRates, []float64{30, 25, 15}) || view.Codecs[0].GOPMax != 120 {
		t.Fatalf("codecs = %+v", view.Codecs)
	}
}

func TestApplyEncoderReportsExactAdjustedAndNotApplied(t *testing.T) {
	t.Parallel()
	_, host := encoderFakeHost(t, fakedevice.EncoderSetApply, true)
	result, err := fastEncoder().ApplyEncoder(context.Background(), encoderRequest(t, host), h264Change())
	if err != nil {
		t.Fatalf("ApplyEncoder() error = %v", err)
	}
	if result.Report.Outcome != OutcomeExact || result.After.Encoding != "H264" || result.Before.Encoding != "H265" || len(result.Report.Changes) == 0 {
		t.Fatalf("exact result = %+v", result)
	}

	_, ignoring := encoderFakeHost(t, fakedevice.EncoderSetIgnore, true)
	result, err = fastEncoder().ApplyEncoder(context.Background(), encoderRequest(t, ignoring), h264Change())
	if err != nil || result.Report.Outcome != OutcomeNotApplied {
		t.Fatalf("ignoring device: result = %+v, error = %v", result.Report, err)
	}

	_, clamping := encoderFakeHostWith(t, fakedevice.EncoderSetClamp, true, 3000)
	change := h264Change()
	change.BitrateKbps = 6000
	result, err = fastEncoder().ApplyEncoder(context.Background(), encoderRequest(t, clamping), change)
	if err != nil || result.Report.Outcome != OutcomeAdjusted || result.After.BitrateKbps != 3000 {
		t.Fatalf("clamping device: %+v, %v", result, err)
	}
	if index := slices.IndexFunc(result.Report.Changes, func(c FieldChange) bool { return c.Field == "bitrate_kbps" }); index < 0 ||
		result.Report.Changes[index].Requested != "6000" || result.Report.Changes[index].After != "3000" {
		t.Fatalf("bitrate change = %+v", result.Report.Changes)
	}
}

func TestApplyEncoderValidatesAgainstOptionsBeforeSetting(t *testing.T) {
	t.Parallel()
	device, host := encoderFakeHost(t, fakedevice.EncoderSetApply, true)
	for name, mutate := range map[string]func(*EncoderChange){
		"codec":      func(c *EncoderChange) { c.Encoding = "MJPEG" },
		"resolution": func(c *EncoderChange) { c.Width = 800 },
		"frame rate": func(c *EncoderChange) { c.FrameRate = 24 },
		"bitrate":    func(c *EncoderChange) { c.BitrateKbps = 9000 },
		"gop":        func(c *EncoderChange) { c.GOPLength = 500 },
		"profile":    func(c *EncoderChange) { c.CodecProfile = "Baseline" },
	} {
		change := h264Change()
		mutate(&change)
		_, err := fastEncoder().ApplyEncoder(context.Background(), encoderRequest(t, host), change)
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Category != CategoryUsage {
			t.Errorf("%s: error = %v, want usage", name, err)
		}
	}
	for _, record := range device.Requests() {
		if record.Action == fakedevice.ActionMedia2SetVideoEncoderConfiguration {
			t.Fatal("an invalid change reached the camera")
		}
	}
}

func TestApplyEncoderFallsBackToMedia1(t *testing.T) {
	t.Parallel()
	_, host := encoderFakeHost(t, fakedevice.EncoderSetApply, false)
	view, err := fastEncoder().ReadEncoder(context.Background(), encoderRequest(t, host))
	if err != nil || view.Current.Source != MediaServiceMedia1 {
		t.Fatalf("ReadEncoder() = %+v, %v", view.Current, err)
	}
	option := view.Codecs[0]
	change := EncoderChange{Encoding: option.Encoding, Width: option.Resolutions[0].Width, Height: option.Resolutions[0].Height,
		FrameRate: 20, BitrateKbps: 1500, GOPLength: 40, CodecProfile: "Main"}
	result, err := fastEncoder().ApplyEncoder(context.Background(), encoderRequest(t, host), change)
	if err != nil || result.Report.Outcome != OutcomeExact || result.After.BitrateKbps != 1500 {
		t.Fatalf("Media1 apply = %+v, %v", result, err)
	}
}

func TestCompareMutationClassifiesOutcomes(t *testing.T) {
	t.Parallel()
	values := func(codec, bitrate string) fieldValues {
		fields := newFieldValues()
		fields.set("codec", codec)
		fields.set("bitrate", bitrate)
		return fields
	}
	before := values("H265", "4096")
	requested := values("H264", "9000")
	cases := map[MutationOutcome]fieldValues{
		OutcomeExact:      values("H264", "9000"),
		OutcomeAdjusted:   values("H264", "8192"),
		OutcomeNotApplied: values("H265", "4096"),
	}
	for want, after := range cases {
		if got := compareMutation(before, requested, after); got.Outcome != want {
			t.Errorf("after %v: outcome = %s, want %s", after.values, got.Outcome, want)
		}
	}
	if got := compareMutation(before, before, before); got.Outcome != OutcomeExact || len(got.Changes) != 0 {
		t.Fatalf("no-op change = %+v", got)
	}
}
