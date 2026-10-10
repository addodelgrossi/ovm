package app

import (
	"context"
	"errors"
	"testing"
)

type fakeEncoderUseCase struct {
	request EncoderRequest
	change  EncoderChange
	err     error
}

func (f *fakeEncoderUseCase) ReadEncoder(_ context.Context, request EncoderRequest) (EncoderView, error) {
	f.request = request
	return EncoderView{ProfileToken: request.ProfileToken}, f.err
}

func (f *fakeEncoderUseCase) ApplyEncoder(_ context.Context, request EncoderRequest, change EncoderChange) (EncoderApplyResult, error) {
	f.request, f.change = request, change
	return EncoderApplyResult{ProfileToken: request.ProfileToken, Report: MutationReport{Outcome: OutcomeExact}}, f.err
}

func TestManagerEncoderUsesStoredDeviceAndCredentials(t *testing.T) {
	h := newHarness(t)
	device := seedAuthenticatedStreamDevice(t, h, "192.168.44.50")
	if _, err := h.manager.ReadEncoder(context.Background(), device.ID, "profile_main"); err == nil {
		t.Fatal("ReadEncoder() without a use case succeeded")
	}
	encoder := &fakeEncoderUseCase{}
	h.manager.encoder = encoder
	if _, err := h.manager.ReadEncoder(context.Background(), device.ID, "profile_main"); err != nil {
		t.Fatalf("ReadEncoder() error = %v", err)
	}
	if encoder.request.Credentials.Username() != "operator" || encoder.request.ProfileToken != "profile_main" {
		t.Fatalf("request = %+v", encoder.request)
	}
	change := EncoderChange{Encoding: "H264", Width: 1280, Height: 720, FrameRate: 25, BitrateKbps: 2048}
	if result, err := h.manager.ApplyEncoder(context.Background(), device.ID, "profile_main", change); err != nil || result.Report.Outcome != OutcomeExact || encoder.change != change {
		t.Fatalf("ApplyEncoder() = %+v, %v", result, err)
	}
	encoder.err = newError(CategoryUsage, "the camera rejected the encoder settings", nil)
	_, err := h.manager.ApplyEncoder(context.Background(), device.ID, "profile_main", change)
	var managed *ManagerError
	if !errors.As(err, &managed) || managed.Category != CategoryUsage || managed.Message != "the camera rejected the encoder settings" {
		t.Fatalf("error = %v", err)
	}
	if _, err := h.manager.ReadEncoder(context.Background(), "missing-device", "profile_main"); err == nil {
		t.Fatal("unknown device accepted")
	}
}
