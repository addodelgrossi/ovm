package app

import (
	"context"
	"errors"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/discovery"
)

type stubDiscoverer struct {
	result discovery.Result
	err    error
}

func (s stubDiscoverer) Discover(context.Context) (discovery.Result, error) {
	return s.result, s.err
}

func newStubDiscovery(stub stubDiscoverer) *productionDiscovery {
	return &productionDiscovery{factory: func(DiscoveryRequest) discoverer { return stub }}
}

func TestDiscoveryNormalizesDevices(t *testing.T) {
	stub := stubDiscoverer{result: discovery.Result{
		Devices: []discovery.Device{
			{
				EndpointReference: "urn:uuid:a",
				XAddrs:            []string{"http://10.0.0.2/onvif/device_service"},
				Scopes:            []string{"onvif://www.onvif.org/type/video_encoder"},
				Types:             []string{"tds:Device"},
				MetadataVersion:   "1",
			},
		},
	}}
	result, err := newStubDiscovery(stub).Discover(context.Background(), DiscoveryRequest{})
	if err != nil {
		t.Fatalf("Discover error = %v", err)
	}
	if len(result.Devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(result.Devices))
	}
	device := result.Devices[0]
	if device.EndpointReference != "urn:uuid:a" || device.Addresses[0] != "http://10.0.0.2/onvif/device_service" {
		t.Fatalf("unexpected device normalization: %+v", device)
	}
	if result.Diagnostic != nil {
		t.Fatalf("diagnostic must be nil when devices were found, got %+v", result.Diagnostic)
	}
}

func TestDiscoveryEmptyRoundSurfacesDiagnostic(t *testing.T) {
	stub := stubDiscoverer{result: discovery.Result{
		Diagnostic: discovery.Diagnostic{
			Category: discovery.DiagnosticNoResponse,
			Detail:   "no device responded within the timeout",
		},
	}}
	result, err := newStubDiscovery(stub).Discover(context.Background(), DiscoveryRequest{})
	if err != nil {
		t.Fatalf("Discover error = %v", err)
	}
	if len(result.Devices) != 0 {
		t.Fatalf("expected no devices, got %d", len(result.Devices))
	}
	if result.Diagnostic == nil || result.Diagnostic.Category != string(discovery.DiagnosticNoResponse) {
		t.Fatalf("expected no_response diagnostic, got %+v", result.Diagnostic)
	}
}

func TestDiscoveryStartErrorIsNetworkCategory(t *testing.T) {
	stub := stubDiscoverer{err: errors.New("no usable interface")}
	_, err := newStubDiscovery(stub).Discover(context.Background(), DiscoveryRequest{})
	if err == nil {
		t.Fatal("expected a start error")
	}
	if CategoryOf(err) != CategoryNetwork {
		t.Fatalf("category = %s, want network", CategoryOf(err))
	}
}

func TestDiscoveryCancellationBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := newStubDiscovery(stubDiscoverer{}).Discover(ctx, DiscoveryRequest{})
	if CategoryOf(err) != CategoryCancelled {
		t.Fatalf("category = %s, want cancelled", CategoryOf(err))
	}
}

func TestDiscoverySocketErrorPromotedToNetwork(t *testing.T) {
	stub := stubDiscoverer{result: discovery.Result{
		Diagnostic: discovery.Diagnostic{
			Category: discovery.DiagnosticSocketError,
			Detail:   "the socket operation failed",
		},
	}}
	result, err := newStubDiscovery(stub).Discover(context.Background(), DiscoveryRequest{})
	if err == nil {
		t.Fatal("expected a network error for a concrete socket failure")
	}
	if CategoryOf(err) != CategoryNetwork {
		t.Fatalf("category = %s, want network", CategoryOf(err))
	}
	// The diagnostic is preserved so a caller can present it before mapping the
	// exit code.
	if result.Diagnostic == nil || result.Diagnostic.Category != string(discovery.DiagnosticSocketError) {
		t.Fatalf("expected the socket_error diagnostic to be preserved, got %+v", result.Diagnostic)
	}
}

func TestDiscoveryPermissionRestrictionPromotedToNetwork(t *testing.T) {
	stub := stubDiscoverer{result: discovery.Result{
		Diagnostic: discovery.Diagnostic{
			Category: discovery.DiagnosticPossiblePermissionRestriction,
			Detail:   "the operating system denied the socket operation",
		},
	}}
	result, err := newStubDiscovery(stub).Discover(context.Background(), DiscoveryRequest{})
	if err == nil {
		t.Fatal("expected a network error for a permission restriction")
	}
	if CategoryOf(err) != CategoryNetwork {
		t.Fatalf("category = %s, want network", CategoryOf(err))
	}
	if result.Diagnostic == nil || result.Diagnostic.Category != string(discovery.DiagnosticPossiblePermissionRestriction) {
		t.Fatalf("expected the permission diagnostic to be preserved, got %+v", result.Diagnostic)
	}
}

func TestDiscoveryNoResponseRemainsSuccessfulEmpty(t *testing.T) {
	stub := stubDiscoverer{result: discovery.Result{
		Diagnostic: discovery.Diagnostic{Category: discovery.DiagnosticNoResponse, Detail: "no device responded"},
	}}
	result, err := newStubDiscovery(stub).Discover(context.Background(), DiscoveryRequest{})
	if err != nil {
		t.Fatalf("no_response must remain a successful empty round, got %v", err)
	}
	if len(result.Devices) != 0 {
		t.Fatalf("expected no devices, got %d", len(result.Devices))
	}
	if result.Diagnostic == nil || result.Diagnostic.Category != string(discovery.DiagnosticNoResponse) {
		t.Fatalf("expected the no_response diagnostic, got %+v", result.Diagnostic)
	}
}
