package app

import (
	"context"
	"testing"

	"github.com/addodelgrossi/ovm/internal/store"
)

func seedAuthenticatedStreamDevice(t *testing.T, h *harness, endpoint string) DeviceDTO {
	t.Helper()
	device := seedDevice(t, h, endpoint)
	stored, err := h.store.Device(device.ID)
	if err != nil {
		t.Fatalf("read seeded device: %v", err)
	}
	secret, err := store.NewSecret("operator", "synthetic-password", store.AuthPolicy{})
	if err != nil {
		t.Fatalf("new secret: %v", err)
	}
	reference, err := store.NewSecretReference()
	if err != nil {
		t.Fatalf("new secret reference: %v", err)
	}
	if err := h.secrets.Put(context.Background(), reference, secret); err != nil {
		t.Fatalf("put secret: %v", err)
	}
	stored.CredentialReference = reference
	if _, err := h.repo.Upsert(stored); err != nil {
		t.Fatalf("attach secret reference: %v", err)
	}
	return device
}

func supportedStreamProfile(token, tcpURI, udpURI string) MediaProfileDTO {
	return MediaProfileDTO{
		Token:  token,
		Stream: &StreamURIDTO{TCPURI: tcpURI, UDPURI: udpURI},
		Operations: []MediaOperationStatus{
			{Operation: MediaOpStreamTCP, State: CapabilitySupported},
			{Operation: MediaOpStreamUDP, State: CapabilitySupported},
		},
	}
}

func TestResolveStreamURIReResolvesBackendProfileTransport(t *testing.T) {
	h := newHarness(t)
	device := seedAuthenticatedStreamDevice(t, h, "192.168.44.10")
	h.media.result = MediaProfilesResult{Profiles: []MediaProfileDTO{
		supportedStreamProfile(
			"profile_main",
			"rtsp://192.168.44.10:554/main",
			"rtsp://192.168.44.10:554/main-udp",
		),
	}}

	for _, test := range []struct {
		transport StreamTransport
		want      string
	}{
		{StreamTransportTCP, "rtsp://192.168.44.10:554/main"},
		{StreamTransportUDP, "rtsp://192.168.44.10:554/main-udp"},
	} {
		uri, err := h.manager.ResolveStreamURI(context.Background(), StreamSelection{
			DeviceID: device.ID, ProfileToken: "profile_main", Transport: test.transport,
		})
		if err != nil {
			t.Fatalf("ResolveStreamURI(%s): %v", test.transport, err)
		}
		if uri != test.want {
			t.Fatalf("ResolveStreamURI(%s) = %q, want %q", test.transport, uri, test.want)
		}
	}
}

func TestResolveStreamURIRejectsInvalidSelectionBeforeMediaWork(t *testing.T) {
	h := newHarness(t)
	device := seedAuthenticatedStreamDevice(t, h, "192.168.44.11")

	for _, selection := range []StreamSelection{
		{DeviceID: device.ID, ProfileToken: "", Transport: StreamTransportTCP},
		{DeviceID: device.ID, ProfileToken: "profile_main", Transport: "sctp"},
	} {
		if _, err := h.manager.ResolveStreamURI(context.Background(), selection); err == nil {
			t.Fatalf("ResolveStreamURI(%+v) succeeded, want validation error", selection)
		}
	}
	h.media.mu.Lock()
	calls := h.media.calls
	h.media.mu.Unlock()
	if calls != 0 {
		t.Fatalf("invalid stream selections made %d media calls, want 0", calls)
	}
}

func TestResolveStreamURIFailsClosedOnUnsafeOrUnsupportedDTO(t *testing.T) {
	tests := []struct {
		name    string
		profile MediaProfileDTO
	}{
		{
			name:    "userinfo",
			profile: supportedStreamProfile("profile_main", "rtsp://user:pass@192.168.44.12/main", ""),
		},
		{
			name:    "encoded control in query",
			profile: supportedStreamProfile("profile_main", "rtsp://192.168.44.12/main?x=%0A", ""),
		},
		{
			name:    "different host",
			profile: supportedStreamProfile("profile_main", "rtsp://other.invalid/main", ""),
		},
		{
			name: "operation not supported",
			profile: MediaProfileDTO{
				Token:      "profile_main",
				Stream:     &StreamURIDTO{TCPURI: "rtsp://192.168.44.12/main"},
				Operations: []MediaOperationStatus{{Operation: MediaOpStreamTCP, State: CapabilityUnsupported}},
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			h := newHarness(t)
			device := seedAuthenticatedStreamDevice(t, h, "192.168.44.12")
			h.media.result = MediaProfilesResult{Profiles: []MediaProfileDTO{test.profile}}
			if _, err := h.manager.ResolveStreamURI(context.Background(), StreamSelection{
				DeviceID: device.ID, ProfileToken: "profile_main", Transport: StreamTransportTCP,
			}); err == nil {
				t.Fatal("unsafe or unsupported stream URI succeeded")
			}
		})
	}
}
