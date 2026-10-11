package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/store"
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

func TestManagerRotatesStoredSecretAfterVerifiedPasswordChange(t *testing.T) {
	_, host := deviceFakeHost(t, nil)
	h := newHarness(t)
	device := seedDevice(t, h, host)
	stored, err := h.store.Device(device.ID)
	if err != nil {
		t.Fatalf("read seeded device: %v", err)
	}
	secret, err := store.NewSecret("admin", "encoder-secret", store.AuthPolicy{})
	if err != nil {
		t.Fatalf("new secret: %v", err)
	}
	reference, err := store.NewSecretReference()
	if err != nil {
		t.Fatalf("reference: %v", err)
	}
	if err := h.secrets.Put(context.Background(), reference, secret); err != nil {
		t.Fatalf("put secret: %v", err)
	}
	stored.CredentialReference = reference
	if _, err := h.repo.Upsert(stored); err != nil {
		t.Fatalf("attach secret: %v", err)
	}
	h.manager.users = &productionMedia{readbackPolicy: &readbackPolicy{attempts: 2, delay: 10 * time.Millisecond}}

	result, err := h.manager.SetUser(context.Background(), device.ID, UserChange{Username: "admin", Password: "rotated-secret-9", Level: "Administrator"})
	if err != nil || !result.SignInVerified {
		t.Fatalf("SetUser() = %+v, %v", result, err)
	}
	if _, err := h.manager.ReadUsers(context.Background(), device.ID); err != nil {
		t.Fatalf("ReadUsers() with the rotated stored secret error = %v", err)
	}
	updated, err := h.store.Device(device.ID)
	if err != nil || updated.CredentialReference == reference {
		t.Fatalf("credential reference was not rotated: %v", err)
	}
	if _, err := h.secrets.Get(context.Background(), reference); err == nil {
		t.Fatal("the previous secret was not removed")
	}
}
