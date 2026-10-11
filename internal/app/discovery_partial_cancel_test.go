package app

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/addodelgrossi/ovm/pkg/onvif/discovery"
)

// TestDiscoveryCancellationReturnsPartialDevices proves that when the
// underlying discoverer returns an aggregated-so-far Result together with a
// context.Canceled error, the adapter normalizes and returns the partial
// devices paired with a typed CategoryCancelled error. Discovered devices are
// never discarded on cancellation.
func TestDiscoveryCancellationReturnsPartialDevices(t *testing.T) {
	stub := stubDiscoverer{
		result: discovery.Result{
			Devices: []discovery.Device{
				{
					EndpointReference: "urn:uuid:partial-a",
					XAddrs:            []string{"http://10.0.0.3/onvif/device_service"},
					Scopes:            []string{"onvif://www.onvif.org/type/video_encoder"},
					Types:             []string{"tds:Device"},
					MetadataVersion:   "1",
				},
			},
		},
		err: context.Canceled,
	}

	result, err := newStubDiscovery(stub).Discover(context.Background(), DiscoveryRequest{})
	if err == nil {
		t.Fatal("expected a cancellation error")
	}
	if CategoryOf(err) != CategoryCancelled {
		t.Fatalf("category = %s, want cancelled", CategoryOf(err))
	}
	if len(result.Devices) != 1 {
		t.Fatalf("expected the partial device to be preserved, got %d devices", len(result.Devices))
	}
	device := result.Devices[0]
	if device.EndpointReference != "urn:uuid:partial-a" {
		t.Fatalf("unexpected endpoint reference: %q", device.EndpointReference)
	}
	if len(device.Addresses) != 1 || device.Addresses[0] != "http://10.0.0.3/onvif/device_service" {
		t.Fatalf("unexpected addresses: %+v", device.Addresses)
	}
}

// TestDiscoveryCancellationWrappedReturnsPartialDevices proves the partial
// return also triggers when the underlying error merely wraps context.Canceled
// rather than being the sentinel itself.
func TestDiscoveryCancellationWrappedReturnsPartialDevices(t *testing.T) {
	stub := stubDiscoverer{
		result: discovery.Result{
			Devices: []discovery.Device{
				{EndpointReference: "urn:uuid:partial-b"},
			},
		},
		err: fmt.Errorf("round ended: %w", context.Canceled),
	}

	result, err := newStubDiscovery(stub).Discover(context.Background(), DiscoveryRequest{})
	if CategoryOf(err) != CategoryCancelled {
		t.Fatalf("category = %s, want cancelled", CategoryOf(err))
	}
	if len(result.Devices) != 1 || result.Devices[0].EndpointReference != "urn:uuid:partial-b" {
		t.Fatalf("expected the partial device to be preserved, got %+v", result.Devices)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatal("the returned error must still unwrap to context.Canceled")
	}
}
