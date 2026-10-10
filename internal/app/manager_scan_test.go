package app

import (
	"context"
	"testing"
)

func TestScanAddsNewDevicesDeterministically(t *testing.T) {
	h := newHarness(t)
	h.discovery.result = DiscoveryResult{Devices: []DiscoveredDevice{
		withEPR("urn:uuid:1", "http://192.168.0.2/onvif/device_service"),
		withEPR("urn:uuid:2", "http://192.168.0.3/onvif/device_service"),
	}}
	result, err := h.manager.Scan(context.Background(), DiscoveryRequest{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(result.Devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(result.Devices))
	}
	if len(result.Added) != 2 {
		t.Fatalf("expected 2 added, got %v", result.Added)
	}
	if len(result.Conflicts) != 0 {
		t.Fatalf("expected no conflicts, got %+v", result.Conflicts)
	}
}

func TestScanEPRMatchRetainsIDAcrossEndpointChange(t *testing.T) {
	h := newHarness(t)
	h.discovery.result = DiscoveryResult{Devices: []DiscoveredDevice{
		withEPR("urn:uuid:stable", "http://192.168.0.5/onvif/device_service"),
	}}
	first, err := h.manager.Scan(context.Background(), DiscoveryRequest{})
	if err != nil {
		t.Fatalf("scan1: %v", err)
	}
	id := first.Devices[0].ID

	// Same EPR, different endpoint: must update in place, keep ID.
	h.discovery.result = DiscoveryResult{Devices: []DiscoveredDevice{
		withEPR("urn:uuid:stable", "http://192.168.0.9/onvif/device_service"),
	}}
	second, err := h.manager.Scan(context.Background(), DiscoveryRequest{})
	if err != nil {
		t.Fatalf("scan2: %v", err)
	}
	if len(second.Devices) != 1 {
		t.Fatalf("expected single device, got %d", len(second.Devices))
	}
	if second.Devices[0].ID != id {
		t.Fatalf("expected ID retained %q, got %q", id, second.Devices[0].ID)
	}
	if second.Devices[0].Endpoint != "http://192.168.0.9/onvif/device_service" {
		t.Fatalf("expected endpoint updated, got %q", second.Devices[0].Endpoint)
	}
	if len(second.Updated) != 1 {
		t.Fatalf("expected 1 updated, got %v", second.Updated)
	}
}

func TestScanEndpointFallbackDedupe(t *testing.T) {
	h := newHarness(t)
	// First a device with no EPR at an endpoint.
	if _, err := h.manager.AddManualDevice(context.Background(), ManualDeviceRequest{Endpoint: "http://192.168.0.20/onvif/device_service"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// Discovery of the same endpoint with an EPR adopts the EPR without a new
	// record.
	h.discovery.result = DiscoveryResult{Devices: []DiscoveredDevice{
		withEPR("urn:uuid:adopt", "http://192.168.0.20/onvif/device_service"),
	}}
	result, err := h.manager.Scan(context.Background(), DiscoveryRequest{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(result.Devices) != 1 {
		t.Fatalf("expected 1 device, got %d", len(result.Devices))
	}
	if result.Devices[0].EndpointReference != "urn:uuid:adopt" {
		t.Fatalf("expected EPR adopted, got %q", result.Devices[0].EndpointReference)
	}
}

func TestScanEPRMismatchConflictDoesNotCorrupt(t *testing.T) {
	h := newHarness(t)
	h.discovery.result = DiscoveryResult{Devices: []DiscoveredDevice{
		withEPR("urn:uuid:owner", "http://192.168.0.30/onvif/device_service"),
	}}
	if _, err := h.manager.Scan(context.Background(), DiscoveryRequest{}); err != nil {
		t.Fatalf("seed scan: %v", err)
	}
	// A different EPR claims the same endpoint: must be an epr_mismatch
	// conflict and must not change the stored record.
	h.discovery.result = DiscoveryResult{Devices: []DiscoveredDevice{
		withEPR("urn:uuid:intruder", "http://192.168.0.30/onvif/device_service"),
	}}
	result, err := h.manager.Scan(context.Background(), DiscoveryRequest{})
	if err != nil {
		t.Fatalf("conflict scan: %v", err)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].Reason != ScanConflictEPRMismatch {
		t.Fatalf("expected epr_mismatch conflict, got %+v", result.Conflicts)
	}
	if len(result.Devices) != 1 || result.Devices[0].EndpointReference != "urn:uuid:owner" {
		t.Fatalf("catalog corrupted: %+v", result.Devices)
	}
}

func TestScanEndpointOwnedConflict(t *testing.T) {
	h := newHarness(t)
	// Two separate devices by EPR at two endpoints.
	h.discovery.result = DiscoveryResult{Devices: []DiscoveredDevice{
		withEPR("urn:uuid:a", "http://192.168.0.40/onvif/device_service"),
		withEPR("urn:uuid:b", "http://192.168.0.41/onvif/device_service"),
	}}
	if _, err := h.manager.Scan(context.Background(), DiscoveryRequest{}); err != nil {
		t.Fatalf("seed scan: %v", err)
	}
	// Device A now advertises B's endpoint: adopting it would collide, so it is
	// an endpoint_owned conflict.
	h.discovery.result = DiscoveryResult{Devices: []DiscoveredDevice{
		withEPR("urn:uuid:a", "http://192.168.0.41/onvif/device_service"),
	}}
	result, err := h.manager.Scan(context.Background(), DiscoveryRequest{})
	if err != nil {
		t.Fatalf("conflict scan: %v", err)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].Reason != ScanConflictEndpointOwned {
		t.Fatalf("expected endpoint_owned conflict, got %+v", result.Conflicts)
	}
	if len(result.Devices) != 2 {
		t.Fatalf("catalog corrupted, expected 2 devices, got %d", len(result.Devices))
	}
}

func TestScanInvalidEndpointIsConflict(t *testing.T) {
	h := newHarness(t)
	h.discovery.result = DiscoveryResult{Devices: []DiscoveredDevice{
		withEPR("urn:uuid:x"), // no addresses
	}}
	result, err := h.manager.Scan(context.Background(), DiscoveryRequest{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(result.Conflicts) != 1 || result.Conflicts[0].Reason != ScanConflictInvalid {
		t.Fatalf("expected invalid conflict, got %+v", result.Conflicts)
	}
	if len(result.Devices) != 0 {
		t.Fatalf("expected empty catalog, got %d", len(result.Devices))
	}
}

func TestScanPreservesDiagnostic(t *testing.T) {
	h := newHarness(t)
	h.discovery.result = DiscoveryResult{Diagnostic: &DiscoveryDiagnostic{Category: "no_response", Detail: "no devices answered"}}
	result, err := h.manager.Scan(context.Background(), DiscoveryRequest{})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if result.Diagnostic == nil || result.Diagnostic.Category != "no_response" {
		t.Fatalf("expected diagnostic preserved, got %+v", result.Diagnostic)
	}
}

func TestScanCancellationPreservesPartialDiscovery(t *testing.T) {
	h := newHarness(t)
	// Discovery returns a cancellation error but carries partial devices.
	h.discovery.result = DiscoveryResult{Devices: []DiscoveredDevice{
		withEPR("urn:uuid:partial", "http://192.168.0.60/onvif/device_service"),
	}}
	h.discovery.err = newError(CategoryCancelled, "discovery cancelled", context.Canceled)
	result, err := h.manager.Scan(context.Background(), DiscoveryRequest{})
	managed := mustManagerError(t, err)
	if managed.Category != CategoryCancelled {
		t.Fatalf("expected cancelled, got %s", managed.Category)
	}
	// Partial device should still have been reconciled.
	if len(result.Devices) != 1 {
		t.Fatalf("expected partial device reconciled, got %d", len(result.Devices))
	}
}
