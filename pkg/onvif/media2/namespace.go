package media2

// Normative ONVIF Media2 (ver20) service namespaces and action URIs.
//
// The Media2 service is defined by the ONVIF Media2 WSDL. Its target namespace
// and the SOAP action for each operation are taken directly from that WSDL and
// the ONVIF Media2 Service Specification. Action URIs follow the ONVIF
// convention of the service namespace, a slash, and the operation name.
//
// The Media2 wire vocabulary is deliberately kept separate from the ver10 Media
// service (pkg/onvif/media): the target namespace differs, GetStreamUri takes a
// transport-protocol string rather than a tt:StreamSetup, and GetStreamUri /
// GetSnapshotUri return a bare Uri string rather than a tt:MediaUri. Mixing the
// two vocabularies would produce malformed requests, so the two services do not
// share request builders or response decoders.
//
// Normative sources:
//   - ONVIF Media2 WSDL (ver20):
//     https://www.onvif.org/ver20/media/wsdl/media.wsdl
//   - ONVIF schema (shared types):
//     https://www.onvif.org/ver10/schema/onvif.xsd
//   - ONVIF Media2 Service Specification:
//     https://www.onvif.org/specs/srv/media/ONVIF-Media2-Service-Spec.pdf
const (
	// Namespace is the ONVIF Media2 (ver20) WSDL target namespace. Its
	// elementFormDefault is "qualified", so the response wrapper elements and
	// the MediaProfile child elements declared in this schema live in this
	// namespace. It is conventionally bound to the prefix "tr2"; the prefix is
	// not semantically significant on the wire.
	Namespace = "http://www.onvif.org/ver20/media/wsdl"
	// SchemaNamespace is the shared ONVIF schema namespace used for the encoder
	// configuration types (tt:VideoEncoder2Configuration,
	// tt:AudioEncoder2Configuration) and their fields. It is conventionally
	// bound to the prefix "tt".
	SchemaNamespace = "http://www.onvif.org/ver10/schema"
)

// SOAP action URIs for the modeled Media2 (ver20) operations. Each is the
// Media2 WSDL namespace followed by a slash and the WSDL operation name, as
// required by the ONVIF SOAP binding (see the soapAction attribute of each
// wsdl:operation in the ver20 media.wsdl).
//
// GetVideoSources is intentionally absent: the ver20 Media2 WSDL defines no
// GetVideoSources operation (it exposes GetVideoSourceConfigurations and
// GetVideoSourceModes instead). See doc.go for the full rationale.
const (
	// ActionGetProfiles is the GetProfiles action URI.
	ActionGetProfiles = Namespace + "/GetProfiles"
	// ActionGetStreamURI is the GetStreamUri action URI.
	ActionGetStreamURI = Namespace + "/GetStreamUri"
	// ActionGetSnapshotURI is the GetSnapshotUri action URI.
	ActionGetSnapshotURI = Namespace + "/GetSnapshotUri"
	// ActionGetVideoEncoderConfigurations is the GetVideoEncoderConfigurations
	// action URI.
	ActionGetVideoEncoderConfigurations = Namespace + "/GetVideoEncoderConfigurations"
	// ActionGetVideoEncoderConfigurationOptions is the
	// GetVideoEncoderConfigurationOptions action URI.
	ActionGetVideoEncoderConfigurationOptions = Namespace + "/GetVideoEncoderConfigurationOptions"
	// ActionGetAudioEncoderConfigurations is the GetAudioEncoderConfigurations
	// action URI.
	ActionGetAudioEncoderConfigurations = Namespace + "/GetAudioEncoderConfigurations"
)
