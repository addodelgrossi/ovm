package app

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/addodelgrossi/ovm/internal/fakedevice"
	"github.com/addodelgrossi/ovm/pkg/onvif/auth"
)

// TestMediaBasicPolicyRefusalAndOptIn proves the shared session enforces the
// same Basic policy as inventory for the media flow: a Basic-only camera is
// refused unless the caller explicitly opts in to Basic (over HTTP here).
func TestMediaBasicPolicyRefusalAndOptIn(t *testing.T) {
	secret := "media-basic-secret" //nolint:gosec // synthetic test credential, not a real secret.
	setup := newUnstartedFake(t, func(base string) fakedevice.Config {
		return fakedevice.Config{
			HTTPAuth: fakedevice.HTTPAuthBasic,
			Username: "admin",
			Password: secret,
			Inventory: fakedevice.Inventory{Services: []fakedevice.ServiceData{
				{Namespace: "http://www.onvif.org/ver10/device/wsdl", XAddr: base + "/onvif/device_service", VersionMajor: 25, VersionMinor: 6},
				{Namespace: "http://www.onvif.org/ver10/media/wsdl", XAddr: base + "/onvif/media_service", VersionMajor: 25, VersionMinor: 6},
				{Namespace: "http://www.onvif.org/ver20/media/wsdl", XAddr: base + "/onvif/media2_service", VersionMajor: 25, VersionMinor: 6},
			}},
		}
	})

	// Without Basic opt-in, the Digest-only transport cannot answer the Basic
	// challenge, so GetServices fails and the use case returns an error.
	refuseCreds, err := NewCredentials("admin", secret, false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	if _, err := NewProductionMediaProfiles().Profiles(context.Background(), MediaProfilesRequest{
		Host: setup.host, Credentials: refuseCreds, Timeout: 10 * time.Second,
	}); err == nil {
		t.Fatal("Basic-only camera accepted without Basic opt-in; want refusal")
	}

	// With explicit Basic-over-HTTP opt-in, the flow succeeds.
	optInCreds, err := NewCredentialsWithPolicy("admin", secret, Policy{AllowBasic: true, AllowBasicOverHTTP: true})
	if err != nil {
		t.Fatalf("NewCredentialsWithPolicy: %v", err)
	}
	result, err := NewProductionMediaProfiles().Profiles(context.Background(), MediaProfilesRequest{
		Host: setup.host, Credentials: optInCreds, Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("Basic opt-in flow failed: %v", err)
	}
	if len(result.Profiles) == 0 {
		t.Fatal("expected profiles with Basic opt-in")
	}
}

// TestFreshUsernameTokenPerAttempt proves the shared session produces a fresh
// WS-Security UsernameToken (new nonce/created) on every logical attempt, so no
// token is ever reused across operations or retries.
func TestFreshUsernameTokenPerAttempt(t *testing.T) {
	creds, err := NewCredentials("admin", "pw", false)
	if err != nil {
		t.Fatalf("NewCredentials: %v", err)
	}
	gen := auth.UsernameTokenGenerator{}
	first, err := freshUsernameTokenHeader(gen, creds, 0)
	if err != nil {
		t.Fatalf("first token: %v", err)
	}
	second, err := freshUsernameTokenHeader(gen, creds, 0)
	if err != nil {
		t.Fatalf("second token: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Fatal("two logical attempts produced identical UsernameToken headers; want fresh nonce/created each time")
	}
}
