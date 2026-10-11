package media

// Normative ONVIF Media (ver10) service namespaces and action URIs.
//
// The Media service is defined by the ONVIF Media WSDL. Its target namespace
// and each SOAP action are taken directly from that WSDL. Most actions follow
// the conventional namespace-plus-operation form, but GetVideoSources has an
// anomalous published soapAction and must be kept as the exact WSDL literal.
//
// Normative sources:
//   - ONVIF Media WSDL (ver10):
//     https://www.onvif.org/ver10/media/wsdl/media.wsdl
//   - ONVIF schema (shared types):
//     https://www.onvif.org/ver10/schema/onvif.xsd
//   - ONVIF Media Service Specification:
//     https://www.onvif.org/specs/srv/media/ONVIF-Media-Service-Spec.pdf
const (
	// Namespace is the ONVIF Media (ver10) WSDL target namespace. Response
	// elements for every modeled operation live in this namespace. In the WSDL
	// this namespace is conventionally bound to the prefix "trt"; the prefix is
	// not semantically significant on the wire.
	Namespace = "http://www.onvif.org/ver10/media/wsdl"
	// SchemaNamespace is the shared ONVIF schema namespace used for common
	// types such as Profile, VideoEncoderConfiguration, StreamSetup, and
	// MediaUri. It is conventionally bound to the prefix "tt".
	SchemaNamespace = "http://www.onvif.org/ver10/schema"
)

// SOAP action URIs for the modeled Media (ver10) operations. Values are the
// exact soapAction literals published by the ONVIF Media WSDL. GetVideoSources
// is intentionally not assembled from Namespace: the official WSDL omits the
// namespace separator and includes a trailing slash for that operation.
const (
	// ActionGetProfiles is the GetProfiles action URI.
	ActionGetProfiles = Namespace + "/GetProfiles"
	// ActionGetStreamURI is the GetStreamUri action URI.
	ActionGetStreamURI = Namespace + "/GetStreamUri"
	// ActionGetSnapshotURI is the GetSnapshotUri action URI.
	ActionGetSnapshotURI = Namespace + "/GetSnapshotUri"
	// ActionGetVideoSources is the exact anomalous GetVideoSources action URI.
	ActionGetVideoSources = "http://www.onvif.org/ver10/media/wsdlGetVideoSources/"
	// ActionGetVideoEncoderConfigurations is the GetVideoEncoderConfigurations
	// action URI.
	ActionGetVideoEncoderConfigurations = Namespace + "/GetVideoEncoderConfigurations"
	// ActionGetVideoEncoderConfigurationOptions is the
	// GetVideoEncoderConfigurationOptions action URI.
	ActionGetVideoEncoderConfigurationOptions = Namespace + "/GetVideoEncoderConfigurationOptions"
	// ActionGetAudioEncoderConfigurations is the GetAudioEncoderConfigurations
	// action URI.
	ActionGetAudioEncoderConfigurations = Namespace + "/GetAudioEncoderConfigurations"
	// ActionSetVideoEncoderConfiguration is the SetVideoEncoderConfiguration
	// action URI.
	ActionSetVideoEncoderConfiguration = Namespace + "/SetVideoEncoderConfiguration"
)
