// Package media implements a typed, reusable client for a bounded subset of the
// ONVIF Media (ver10) service.
//
// # Scope
//
// The package models exactly the following ONVIF ver10 Media operations:
// GetProfiles, GetStreamUri, GetSnapshotUri, GetVideoSources,
// GetVideoEncoderConfigurations, GetVideoEncoderConfigurationOptions,
// GetAudioEncoderConfigurations, and the single mutation
// SetVideoEncoderConfiguration. Each operation uses the official Media WSDL
// target namespace http://www.onvif.org/ver10/media/wsdl and an action URI of
// the form namespace/Operation (see namespace.go). No other Media operations,
// mutations, discovery, imaging, events, or ver20 Media2 operations are
// implemented here. SetVideoEncoderConfiguration serializes the typed
// configuration, including the required Multicast and SessionTimeout, and does
// not round-trip vendor extension elements. The ver20 Media2 service is a separate package
// (pkg/onvif/media2) with separate wire types.
//
// GetStreamUri builds a tt:StreamSetup carrying the stream kind (RTP-Unicast or
// RTP-Multicast) and transport protocol (UDP, TCP, RTSP, or HTTP), so a caller
// can request, for example, RTP/UDP or RTP-over-TCP via RTSP. The returned
// tt:MediaUri is normalized without redacting the URI, because the URI is the
// operation's product.
//
// # Modeling and provenance
//
// Operations and element names follow the ONVIF Media WSDL and shared schema.
// Response parsing is namespace-strict: known structures are matched by
// namespace URI and local name only, so XML prefixes are never semantically
// significant (intentional prefix tolerance). Local-name tolerance is confined
// to payloads whose enclosing ONVIF namespace has already been validated;
// foreign namespaces are rejected rather than silently accepted.
//
// Useful typed data is normalized without any UI, CLI, or storage dependency.
// Profile, video-source, encoder-configuration, and option detail that this
// project does not yet model is preserved as bounded, credential-redacted XML
// for diagnostics and future work, never without a size bound.
//
// # Input validation and escaping
//
// Caller-supplied reference tokens are validated (non-empty, valid UTF-8,
// within the schema's 64-character bound, no control characters) and then
// XML-escaped with encoding/xml so legitimate XML special characters are
// encoded rather than stripped and cannot break out of their element.
// Stream-setup enums and request cardinality are validated before any request
// is serialized. Rejections never echo the offending token or value.
//
// Normative sources:
//   - ONVIF Media WSDL (ver10):
//     https://www.onvif.org/ver10/media/wsdl/media.wsdl
//   - ONVIF schema: https://www.onvif.org/ver10/schema/onvif.xsd
//   - ONVIF Media Service Specification:
//     https://www.onvif.org/specs/srv/media/ONVIF-Media-Service-Spec.pdf
//
// # Caller seam
//
// The client depends on the Transport interface, satisfied by *soap.Client, so
// internal/app and tests can inject a transport without a network. SOAP faults,
// cancellation, and transport failures are returned unchanged and remain
// errors.Is-compatible with the pkg/onvif/soap sentinels and context errors.
// Typed decode, validation, and input failures are reported through the media
// sentinels (see errors.go) and never carry raw, unredacted response XML or any
// request token, URI, or credential value.
//
// This package does not import Wails, Cobra, persistence, or UI packages.
package media
