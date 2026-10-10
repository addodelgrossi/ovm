package media

// This file models the typed results of the implemented ONVIF Media (ver10)
// operations. Only the commonly consumed fields of large ONVIF structures are
// normalized; the remaining detail is preserved as bounded, credential-redacted
// raw XML for diagnostics and future modeling, exactly as pkg/onvif/device does
// for its capability and interface structures.

// StreamType selects unicast or multicast RTP streaming for GetStreamUri. The
// values are the ONVIF tt:StreamType enumeration.
//
// Source: tt:StreamType in https://www.onvif.org/ver10/schema/onvif.xsd
type StreamType string

const (
	// StreamRTPUnicast requests an RTP unicast stream.
	StreamRTPUnicast StreamType = "RTP-Unicast"
	// StreamRTPMulticast requests an RTP multicast stream.
	StreamRTPMulticast StreamType = "RTP-Multicast"
)

// TransportProtocol selects the stream transport for GetStreamUri. The values
// are the ONVIF tt:TransportProtocol enumeration. TransportTCP is deprecated by
// the schema but still accepted because deployed devices continue to use it;
// RTSP is the conventional choice for RTP-over-TCP.
//
// Source: tt:TransportProtocol in https://www.onvif.org/ver10/schema/onvif.xsd
type TransportProtocol string

const (
	// TransportUDP is RTP/UDP.
	TransportUDP TransportProtocol = "UDP"
	// TransportTCP is the deprecated RTP/TCP value retained for compatibility.
	TransportTCP TransportProtocol = "TCP"
	// TransportRTSP is RTP/RTSP/TCP.
	TransportRTSP TransportProtocol = "RTSP"
	// TransportHTTP is RTP/RTSP/HTTP/TCP.
	TransportHTTP TransportProtocol = "HTTP"
)

// StreamSetup is the caller-supplied streaming configuration for GetStreamUri.
// It maps to the ONVIF tt:StreamSetup type: a stream kind plus a transport
// protocol.
type StreamSetup struct {
	// Stream selects unicast or multicast. It is required.
	Stream StreamType
	// Protocol selects the transport protocol. It is required.
	Protocol TransportProtocol
}

// VideoResolution is a pixel resolution from a video source, configuration,
// or advertised option.
type VideoResolution struct {
	Width  int
	Height int
}

// VideoEncoding is the Media1 tt:VideoEncoding enumeration.
type VideoEncoding string

const (
	VideoEncodingJPEG  VideoEncoding = "JPEG"
	VideoEncodingMPEG4 VideoEncoding = "MPEG4"
	VideoEncodingH264  VideoEncoding = "H264"
)

// AudioEncoding is the Media1 tt:AudioEncoding enumeration.
type AudioEncoding string

const (
	AudioEncodingG711 AudioEncoding = "G711"
	AudioEncodingG726 AudioEncoding = "G726"
	AudioEncodingAAC  AudioEncoding = "AAC"
)

// IntRange is an inclusive ONVIF tt:IntRange. Decoders require Min <= Max.
type IntRange struct {
	Min int
	Max int
}

// CodecOptions models the required common fields of a Media1 JPEG, MPEG4, or
// H264 option block. GovLengthRange and Profiles are populated only for codecs
// whose official option type defines them.
type CodecOptions struct {
	Resolutions           []VideoResolution
	FrameRateRange        IntRange
	EncodingIntervalRange IntRange
	GovLengthRange        *IntRange
	Profiles              []string
}

// ProfileAssociations models the typed configuration references embedded in a
// tt:Profile. Each configuration member is optional in the Media WSDL, so every
// token is paired with a presence flag; a caller must never infer "unassigned"
// from an empty token string alone, because an empty-but-present token is a
// malformed device response rather than an absent association, and the decoder
// rejects it before it reaches this type. The physical VideoSourceToken is the
// tt:SourceToken carried inside tt:VideoSourceConfiguration, distinct from that
// configuration's own reference token.
type ProfileAssociations struct {
	// VideoSourceConfigurationToken is the token attribute of the embedded
	// tt:VideoSourceConfiguration.
	VideoSourceConfigurationToken string
	// HasVideoSourceConfiguration reports whether a VideoSourceConfiguration
	// member was present.
	HasVideoSourceConfiguration bool
	// VideoSourceToken is the physical tt:SourceToken referenced by the
	// VideoSourceConfiguration. It is populated only when a
	// VideoSourceConfiguration is present.
	VideoSourceToken string
	// HasVideoSource reports whether a physical SourceToken was present within
	// the VideoSourceConfiguration.
	HasVideoSource bool
	// VideoEncoderConfigurationToken is the token attribute of the embedded
	// tt:VideoEncoderConfiguration.
	VideoEncoderConfigurationToken string
	// HasVideoEncoderConfiguration reports whether a VideoEncoderConfiguration
	// member was present.
	HasVideoEncoderConfiguration bool
	// AudioEncoderConfigurationToken is the token attribute of the embedded
	// tt:AudioEncoderConfiguration.
	AudioEncoderConfigurationToken string
	// HasAudioEncoderConfiguration reports whether an AudioEncoderConfiguration
	// member was present.
	HasAudioEncoderConfiguration bool
}

// Profile is the normalized result entry of GetProfiles. The full tt:Profile
// structure embeds every assigned configuration; identity fields and the typed
// configuration associations are modeled, and the complete element is preserved
// as bounded, redacted XML for the remaining detail.
type Profile struct {
	// Token is the profile reference token (the required token attribute).
	Token string
	// Fixed reports whether the profile is a fixed (non-deletable) profile,
	// when the device reports the attribute.
	Fixed bool
	// HasFixed reports whether a fixed attribute was present.
	HasFixed bool
	// Name is the user-readable profile name.
	Name string
	// Associations carries the typed, optional configuration references
	// embedded in the profile so callers need not parse RawXML.
	Associations ProfileAssociations
	// RawXML is the bounded, credential-redacted Profile element retained for
	// diagnostics because the full configuration tree is not modeled here.
	RawXML []byte
}

// MediaURI is the normalized result of GetStreamUri and GetSnapshotUri. It maps
// to the ONVIF tt:MediaUri type. The URI is retained verbatim because it is the
// operation's product and the caller needs it to connect; callers that log it
// are responsible for redaction.
type MediaURI struct {
	// URI is the stream or snapshot URI.
	URI string
	// InvalidAfterConnect reports whether the URI is only valid until the
	// connection is established.
	InvalidAfterConnect bool
	// HasInvalidAfterConnect is always true for a successfully decoded
	// tt:MediaUri and is retained for presence-aware callers.
	HasInvalidAfterConnect bool
	// InvalidAfterReboot reports whether the URI is invalid after a reboot.
	InvalidAfterReboot bool
	// HasInvalidAfterReboot is always true for a successfully decoded
	// tt:MediaUri and is retained for presence-aware callers.
	HasInvalidAfterReboot bool
	// Timeout is the required raw non-negative xs:duration string.
	Timeout string
}

// VideoSource is the normalized result entry of GetVideoSources (ver10 only).
// Only identity and framerate/resolution are modeled; the rest is preserved as
// bounded, redacted XML.
type VideoSource struct {
	// Token is the video source reference token.
	Token string
	// Framerate is the required positive maximum frame rate.
	Framerate float64
	// HasFramerate is always true for a successfully decoded source.
	HasFramerate bool
	// Resolution is the required positive source resolution.
	Resolution VideoResolution
	// HasResolution is always true for a successfully decoded source.
	HasResolution bool
	// RawXML is the bounded, credential-redacted VideoSource element.
	RawXML []byte
}

// VideoEncoderConfiguration is the normalized result entry of
// GetVideoEncoderConfigurations (ver10). It models the ConfigurationEntity base
// (token, Name, UseCount) plus the common encoder fields and the configured
// rate-control and codec-specific values; the remainder is preserved as
// bounded, redacted XML.
type VideoEncoderConfiguration struct {
	// Token is the configuration reference token.
	Token string
	// Name is the user-readable configuration name.
	Name string
	// UseCount is the number of internal references to this configuration.
	UseCount int
	// Encoding is the validated Media1 video codec.
	Encoding VideoEncoding
	// Resolution is the configured resolution.
	Resolution VideoResolution
	// HasResolution is always true for a successfully decoded configuration.
	HasResolution bool
	// Quality is the required finite relative quality value.
	Quality float64
	// HasQuality is always true for a successfully decoded configuration.
	HasQuality bool
	// FrameRateLimit is the configured maximum output frame rate from the
	// optional tt:RateControl block.
	FrameRateLimit int
	// HasFrameRateLimit reports whether a RateControl FrameRateLimit was
	// present.
	HasFrameRateLimit bool
	// EncodingInterval is the configured encoding interval (encode one of every
	// N frames) from the optional tt:RateControl block.
	EncodingInterval int
	// HasEncodingInterval reports whether a RateControl EncodingInterval was
	// present.
	HasEncodingInterval bool
	// BitrateLimit is the configured maximum output bitrate (kbps) from the
	// optional tt:RateControl block.
	BitrateLimit int
	// HasBitrateLimit reports whether a RateControl BitrateLimit was present.
	HasBitrateLimit bool
	// GovLength is the configured group-of-video length from the codec-specific
	// tt:H264 or tt:MPEG4 block. JPEG has no GOV length.
	GovLength int
	// HasGovLength reports whether a codec GovLength was present.
	HasGovLength bool
	// CodecProfile is the configured codec profile from the tt:H264Profile or
	// tt:Mpeg4Profile element. JPEG has no profile.
	CodecProfile string
	// HasCodecProfile reports whether a codec profile was present.
	HasCodecProfile bool
	// RawXML is the bounded, credential-redacted configuration element.
	RawXML []byte
}

// VideoEncoderConfigurationOptions is the normalized, schema-validated result
// of GetVideoEncoderConfigurationOptions (ver10). Codec blocks remain optional
// exactly as in tt:VideoEncoderConfigurationOptions; every present range is
// checked for required endpoints and Min <= Max.
type VideoEncoderConfigurationOptions struct {
	QualityRange IntRange
	JPEG         *CodecOptions
	MPEG4        *CodecOptions
	H264         *CodecOptions
	// RawXML retains a bounded, credential-redacted copy for extensions not yet
	// modeled by this public foundation.
	RawXML []byte
}

// AudioEncoderConfiguration is the normalized result entry of
// GetAudioEncoderConfigurations (ver10). It models the ConfigurationEntity base
// plus common audio fields; the remainder is preserved as bounded, redacted
// XML.
type AudioEncoderConfiguration struct {
	// Token is the configuration reference token.
	Token string
	// Name is the user-readable configuration name.
	Name string
	// UseCount is the number of internal references to this configuration.
	UseCount int
	// Encoding is the validated Media1 audio codec.
	Encoding AudioEncoding
	// Bitrate is the required positive output bitrate in kbps.
	Bitrate int
	// HasBitrate is always true for a successfully decoded configuration.
	HasBitrate bool
	// SampleRate is the required positive output sample rate in kHz.
	SampleRate int
	// HasSampleRate is always true for a successfully decoded configuration.
	HasSampleRate bool
	// RawXML is the bounded, credential-redacted configuration element.
	RawXML []byte
}
