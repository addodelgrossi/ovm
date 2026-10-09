// Package device implements a typed, reusable client for a bounded subset of
// the ONVIF Device Management service.
//
// # Scope
//
// The package models exactly the following ONVIF ver10 Device operations:
// GetSystemDateAndTime, GetDeviceInformation, GetCapabilities, GetServices,
// GetScopes, GetHostname, and GetNetworkInterfaces. Each operation uses the
// official Device Management WSDL target namespace
// http://www.onvif.org/ver10/device/wsdl and an action URI of the form
// namespace/Operation (see namespace.go). No other Device operations,
// discovery, media, imaging, events, or mutations are implemented here.
//
// # Modeling and provenance
//
// Operations and element names follow ADR 0002 (ONVIF modeling and
// specification provenance). Response parsing is namespace-strict: known
// structures are matched by namespace URI and local name only, so XML prefixes
// are never semantically significant (intentional prefix tolerance). Local-name
// tolerance is confined to payloads whose enclosing ONVIF namespace has already
// been validated; foreign namespaces are rejected rather than silently
// accepted.
//
// Useful typed data is normalized without any UI, CLI, or storage dependency.
// Capability detail and network-interface detail that this project does not yet
// model are preserved as bounded, credential-redacted XML for diagnostics and
// future work, never without a size bound.
//
// Normative sources:
//   - ONVIF Device Management WSDL:
//     https://www.onvif.org/ver10/device/wsdl/devicemgmt.wsdl
//   - ONVIF schema: https://www.onvif.org/ver10/schema/onvif.xsd
//   - ONVIF Core Specification:
//     https://www.onvif.org/specs/core/ONVIF-Core-Specification.pdf
//
// # Caller seam
//
// The client depends on the Transport interface, satisfied by *soap.Client, so
// internal/app and tests can inject a transport without a network. SOAP faults,
// cancellation, and transport failures are returned unchanged and remain
// errors.Is-compatible with the pkg/onvif/soap sentinels and context errors.
// Typed decode and validation failures are reported through the device
// sentinels (see errors.go) and never carry raw, unredacted response XML.
//
// This package does not import Wails, Cobra, persistence, or UI packages.
package device
