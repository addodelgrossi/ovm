package app

import (
	"context"
	"errors"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
)

func imagingFakeHost(t *testing.T, ignoreIrCut, advertise bool) string {
	t.Helper()
	srv := newUnstartedFake(t, func(base string) fakedevice.Config {
		services := []fakedevice.ServiceData{
			{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
			{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6},
		}
		if advertise {
			services = append(services, fakedevice.ServiceData{Namespace: imagingWSDLNamespace, XAddr: base + "/onvif/imaging_service", VersionMajor: 25, VersionMinor: 6})
		}
		return fakedevice.Config{
			HTTPAuth: fakedevice.HTTPAuthDigest, DigestAlgorithm: "SHA-256", RequireUsernameToken: true,
			Username: "admin", Password: "encoder-secret",
			Inventory: fakedevice.Inventory{Services: services},
			Imaging:   fakedevice.ImagingInventory{IgnoreIrCut: ignoreIrCut},
		}
	})
	return srv.host
}

func TestImagingReadsAndAppliesIrCutWithReadback(t *testing.T) {
	t.Parallel()
	host := imagingFakeHost(t, false, true)
	request := encoderRequest(t, host)
	view, err := fastEncoder().ReadImaging(context.Background(), request)
	if err != nil {
		t.Fatalf("ReadImaging() error = %v", err)
	}
	if view.VideoSourceToken != "vsrc0" || view.Settings.IrCutFilter != "AUTO" || *view.Settings.Brightness != 50 ||
		view.Options.Brightness.Max != 100 || len(view.Options.IrCutModes) != 3 {
		t.Fatalf("view = %+v", view)
	}
	brightness := 70.0
	result, err := fastEncoder().ApplyImaging(context.Background(), request, ImagingSettingsDTO{IrCutFilter: "ON", Brightness: &brightness})
	if err != nil || result.Report.Outcome != OutcomeExact || result.After.IrCutFilter != "ON" || *result.After.Brightness != 70 {
		t.Fatalf("ApplyImaging() = %+v, %v", result, err)
	}
	if len(result.Report.Changes) != 2 || result.Report.Changes[0].Field != "ir_cut_filter" || result.Report.Changes[0].Before != "AUTO" {
		t.Fatalf("changes = %+v", result.Report.Changes)
	}
}

func TestImagingReportsIgnoredIrCutAndValidatesInput(t *testing.T) {
	t.Parallel()
	host := imagingFakeHost(t, true, true)
	request := encoderRequest(t, host)
	result, err := fastEncoder().ApplyImaging(context.Background(), request, ImagingSettingsDTO{IrCutFilter: "OFF"})
	if err != nil || result.Report.Outcome != OutcomeNotApplied {
		t.Fatalf("ignoring camera: %+v, %v", result.Report, err)
	}
	tooBright := 150.0
	for name, change := range map[string]ImagingSettingsDTO{
		"range":   {Brightness: &tooBright},
		"mode":    {IrCutFilter: "DUSK"},
		"nothing": {},
	} {
		_, err := fastEncoder().ApplyImaging(context.Background(), request, change)
		var appErr *Error
		if !errors.As(err, &appErr) || appErr.Category != CategoryUsage {
			t.Errorf("%s: error = %v", name, err)
		}
	}
}

func TestImagingRequiresTheAdvertisedService(t *testing.T) {
	t.Parallel()
	host := imagingFakeHost(t, false, false)
	_, err := fastEncoder().ReadImaging(context.Background(), encoderRequest(t, host))
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Category != CategoryProtocol {
		t.Fatalf("missing service error = %v", err)
	}
}
