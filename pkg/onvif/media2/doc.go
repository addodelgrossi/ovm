// Package media2 implements a typed, reusable client for a bounded subset of
// the ONVIF Media2 (ver20) service.
//
// # Scope
//
// The package models exactly the following ONVIF ver20 Media2 operations:
// GetProfiles, GetStreamUri, GetSnapshotUri, GetVideoEncoderConfigurations,
// GetVideoEncoderConfigurationOptions, GetAudioEncoderConfigurations, and the
// single mutation SetVideoEncoderConfiguration. Each
// operation uses the official Media2 WSDL target namespace
// http://www.onvif.org/ver20/media/wsdl and an action URI of the form
// namespace/Operation (see namespace.go). No other Media2 operations,
// mutations, discovery, imaging, or events are implemented here.
// SetVideoEncoderConfiguration serializes the typed configuration (including
// Multicast) and does not round-trip vendor extension elements.
//
// # GetVideoSources is intentionally absent
//
// The task that produced this package asked to carefully verify whether
// GetVideoSources is an official ver20 Media2 operation and to expose it only
// if it is. It is not. The ver20 Media2 WSDL
// (https://www.onvif.org/ver20/media/wsdl/media.wsdl) defines no
// GetVideoSources operation. The ver20 service instead exposes
// GetVideoSourceConfigurations and GetVideoSourceModes, and physical video
// sources are reached through the ver10 Media service's GetVideoSources
// (implemented in pkg/onvif/media) or through the ver10 Device/Imaging
// services. Exposing a GetVideoSources method here would require fabricating a
// non-existent ver20 action URI and request element, so this package omits it
// deliberately. A caller that needs physical video sources should use
// media.Client.GetVideoSources. This decision is additionally asserted by the
// package test TestNoGetVideoSourcesOperation.
//
// # Media2 wire types are kept separate from ver10 Media
//
// The Media2 vocabulary differs from the ver10 Media service and is never
// shared with pkg/onvif/media:
//
//   - GetStreamUri takes a single tr2:TransportProtocol string (RtspUnicast,
//     RtspMulticast, RtspsUnicast, RtspsMulticast, RTSP, RtspOverHttp) instead
//     of a tt:StreamSetup, so RTP-over-TCP via RTSP and RTSP-over-HTTP are
//     selected by distinct enum values from the ver10 UDP/TCP/RTSP/HTTP set.
//   - GetStreamUri and GetSnapshotUri return a bare Uri string, not a
//     tt:MediaUri structure.
//   - GetProfiles is filterable by an optional profile token and an optional
//     list of tr2:ConfigurationEnumeration types. Omitting Type returns profile
//     metadata without configuration information; a single All requests every
//     associated configuration, while specific values request only those
//     configuration categories.
//   - The encoder configuration getters share the tr2:GetConfiguration request
//     body (optional configuration token and profile token) and return
//     tt:VideoEncoder2Configuration / tt:AudioEncoder2Configuration elements.
//
// The Media2 schema uses elementFormDefault="qualified", so the response
// wrapper elements and MediaProfile child elements declared in the ver20 schema
// are in the ver20 namespace, while the encoder configuration fields (declared
// by tt types) are in the shared ver10 schema namespace. The decoder matches
// each element in its correct namespace.
//
// # Modeling, validation, and provenance
//
// Response parsing is namespace-strict: known structures are matched by
// namespace URI and local name only, so XML prefixes are never semantically
// significant (intentional prefix tolerance). Foreign namespaces are rejected.
// Caller-supplied reference tokens are validated (non-empty, valid UTF-8,
// within the schema's 64-character bound, no control characters) and then
// XML-escaped with encoding/xml. Stream protocol and configuration-type enums
// and request cardinality are validated before any request is serialized.
// Rejections never echo the offending token or value. Unmodeled detail is
// preserved as bounded, credential-redacted XML.
//
// Normative sources:
//   - ONVIF Media2 WSDL (ver20):
//     https://www.onvif.org/ver20/media/wsdl/media.wsdl
//   - ONVIF schema: https://www.onvif.org/ver10/schema/onvif.xsd
//   - ONVIF Media2 Service Specification:
//     https://www.onvif.org/specs/srv/media/ONVIF-Media2-Service-Spec.pdf
//
// # Caller seam
//
// The client depends on the Transport interface, satisfied by *soap.Client, so
// internal/app and tests can inject a transport without a network. SOAP faults,
// cancellation, and transport failures are returned unchanged and remain
// errors.Is-compatible with the pkg/onvif/soap sentinels and context errors.
// Typed decode, validation, and input failures are reported through the media2
// sentinels (see errors.go) and never carry raw, unredacted response XML or any
// request token, URI, or credential value.
//
// This package does not import Wails, Cobra, persistence, or UI packages.
package media2
