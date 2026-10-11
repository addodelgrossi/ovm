package fakedevice_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/pkg/onvif/imaging"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

func imagingClient(t *testing.T, config fakedevice.Config) *imaging.Client {
	t.Helper()
	device, err := fakedevice.New(config)
	if err != nil {
		t.Fatalf("fakedevice.New() error = %v", err)
	}
	server := httptest.NewServer(device)
	t.Cleanup(server.Close)
	transport, err := soap.NewClient(soap.Config{})
	if err != nil {
		t.Fatalf("soap.NewClient() error = %v", err)
	}
	client, err := imaging.NewClient(transport, server.URL)
	if err != nil {
		t.Fatalf("imaging.NewClient() error = %v", err)
	}
	return client
}

func TestFakeImagingAppliesAndReadsBackSettings(t *testing.T) {
	t.Parallel()
	client := imagingClient(t, fakedevice.Config{})
	options, err := client.GetOptions(context.Background(), "vsrc0")
	if err != nil || options.Brightness.Max != 100 || len(options.IrCutFilterModes) != 3 {
		t.Fatalf("GetOptions() = %+v, %v", options, err)
	}
	brightness := 80.0
	mode := imaging.IrCutOn
	if err := client.SetImagingSettings(context.Background(), "vsrc0", imaging.Settings{Brightness: &brightness, IrCutFilter: &mode}, true); err != nil {
		t.Fatalf("SetImagingSettings() error = %v", err)
	}
	settings, err := client.GetImagingSettings(context.Background(), "vsrc0")
	if err != nil || *settings.Brightness != 80 || *settings.IrCutFilter != imaging.IrCutOn || *settings.Contrast != 50 {
		t.Fatalf("readback = %+v, %v", settings, err)
	}
	if _, err := client.GetImagingSettings(context.Background(), "missing"); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("unknown source error = %v", err)
	}
	tooBright := 150.0
	if err := client.SetImagingSettings(context.Background(), "vsrc0", imaging.Settings{Brightness: &tooBright}, true); !errors.Is(err, soap.ErrInvalidArgVal) {
		t.Fatalf("out-of-range error = %v", err)
	}
}

func TestFakeImagingCanIgnoreIrCutChanges(t *testing.T) {
	t.Parallel()
	client := imagingClient(t, fakedevice.Config{Imaging: fakedevice.ImagingInventory{IgnoreIrCut: true}})
	mode := imaging.IrCutOff
	if err := client.SetImagingSettings(context.Background(), "vsrc0", imaging.Settings{IrCutFilter: &mode}, true); err != nil {
		t.Fatalf("SetImagingSettings() error = %v", err)
	}
	settings, err := client.GetImagingSettings(context.Background(), "vsrc0")
	if err != nil || *settings.IrCutFilter != imaging.IrCutAuto {
		t.Fatalf("ignoring device changed IR cut: %+v, %v", settings, err)
	}
}
