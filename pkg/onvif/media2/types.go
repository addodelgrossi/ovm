package media2

// This file models the typed results of the implemented ONVIF Media2 (ver20)
// operations. The Media2 wire vocabulary differs from the ver10 Media service,
// so these types are intentionally distinct from pkg/onvif/media and are never
// shared with it. Only the commonly consumed fields of large ONVIF structures
// are normalized; the remaining detail is preserved as bounded,
// credential-redacted raw XML.

// ConfigurationType is a Media2 configuration category used to filter
// GetProfiles. The values are the ONVIF tr2:ConfigurationEnumeration. Omitting
// all values requests profile metadata without configuration information.
// "All" requests every associated configuration but, per the WSDL, is never
// returned in a profile's own type list.
//
// Source: tr2:ConfigurationEnumeration in
// https://www.onvif.org/ver20/media/wsdl/media.wsdl
type ConfigurationType string

const (
	// ConfigurationAll requests all configuration types.
	ConfigurationAll ConfigurationType = "All"
	// ConfigurationVideoSource filters video source configurations.
	ConfigurationVideoSource ConfigurationType = "VideoSource"
	// ConfigurationVideoEncoder filters video encoder configurations.
	ConfigurationVideoEncoder ConfigurationType = "VideoEncoder"
	// ConfigurationAudioSource filters audio source configurations.
	ConfigurationAudioSource ConfigurationType = "AudioSource"
	// ConfigurationAudioEncoder filters audio encoder configurations.
	ConfigurationAudioEncoder ConfigurationType = "AudioEncoder"
	// ConfigurationAudioOutput filters audio output configurations.
	ConfigurationAudioOutput ConfigurationType = "AudioOutput"
	// ConfigurationAudioDecoder filters audio decoder configurations.
	ConfigurationAudioDecoder ConfigurationType = "AudioDecoder"
	// ConfigurationMetadata filters metadata configurations.
	ConfigurationMetadata ConfigurationType = "Metadata"
	// ConfigurationAnalytics filters analytics configurations.
	ConfigurationAnalytics ConfigurationType = "Analytics"
	// ConfigurationPTZ filters PTZ configurations.
	ConfigurationPTZ ConfigurationType = "PTZ"
	// ConfigurationReceiver filters receiver configurations.
	ConfigurationReceiver ConfigurationType = "Receiver"
)

// StreamProtocol selects the Media2 streaming transport for GetStreamUri. The
// Media2 service replaces the ver10 tt:StreamSetup with a single
// tr2:TransportProtocol string, so these values are entirely distinct from the
// ver10 TransportProtocol enumeration and must not be mixed with it.
//
// Source: tr2:TransportProtocol in
// https://www.onvif.org/ver20/media/wsdl/media.wsdl
type StreamProtocol string

const (
	// StreamRtspUnicast selects RTP over UDP unicast controlled by RTSP.
	StreamRtspUnicast StreamProtocol = "RtspUnicast"
	// StreamRtspMulticast is RTP multicast over RTSP.
	StreamRtspMulticast StreamProtocol = "RtspMulticast"
	// StreamRtspsUnicast is RTP unicast over RTSP secured by TLS.
	StreamRtspsUnicast StreamProtocol = "RtspsUnicast"
	// StreamRtspsMulticast is RTP multicast over RTSP secured by TLS.
	StreamRtspsMulticast StreamProtocol = "RtspsMulticast"
	// StreamRTSP is the plain RTSP transport value.
	StreamRTSP StreamProtocol = "RTSP"
	// StreamRtspOverHTTP tunnels RTP/RTSP over HTTP.
	StreamRtspOverHTTP StreamProtocol = "RtspOverHttp"
)

// VideoResolution is a positive pixel resolution from a Media2 encoder.
type VideoResolution struct {
	Width  int
	Height int
}

// IntRange and FloatRange are inclusive ONVIF ranges. Decoders require both
// endpoints, finite float values, and Min <= Max.
// IntRange is an inclusive integer range.
type IntRange struct {
	Min int
	Max int
}

// FloatRange is an inclusive finite floating-point range.
type FloatRange struct {
	Min float64
	Max float64
}

// VideoEncoderOption is one schema-validated
// tt:VideoEncoder2ConfigurationOptions entry.
type VideoEncoderOption struct {
	Encoding             string
	QualityRange         FloatRange
	ResolutionsAvailable []VideoResolution
	BitrateRange         IntRange
	GovLengthRange       *IntRange
	RawXML               []byte
}

// ProfileAssociations models the typed configuration references embedded in a
// tr2:MediaProfile via its optional tr2:Configurations (tr2:ConfigurationSet).
// Each member of the set is optional, so every token is paired with a presence
// flag. The physical VideoSourceToken is the tt:SourceToken carried inside the
// tr2:VideoSource configuration, distinct from that configuration's own
// reference token. Only namespace-resolved expected elements and attributes are
// read: a lookalike local name in a foreign namespace never satisfies an
// association.
type ProfileAssociations struct {
	// VideoSourceConfigurationToken is the token attribute of the
	// tr2:VideoSource configuration member.
	VideoSourceConfigurationToken string
	// HasVideoSourceConfiguration reports whether a tr2:VideoSource member was
	// present.
	HasVideoSourceConfiguration bool
	// VideoSourceToken is the physical tt:SourceToken referenced by the
	// tr2:VideoSource configuration. Populated only when that member is
	// present.
	VideoSourceToken string
	// HasVideoSource reports whether a physical SourceToken was present within
	// the tr2:VideoSource configuration.
	HasVideoSource bool
	// VideoEncoderConfigurationToken is the token attribute of the
	// tr2:VideoEncoder configuration member.
	VideoEncoderConfigurationToken string
	// HasVideoEncoderConfiguration reports whether a tr2:VideoEncoder member
	// was present.
	HasVideoEncoderConfiguration bool
	// AudioEncoderConfigurationToken is the token attribute of the
	// tr2:AudioEncoder configuration member.
	AudioEncoderConfigurationToken string
	// HasAudioEncoderConfiguration reports whether a tr2:AudioEncoder member
	// was present.
	HasAudioEncoderConfiguration bool
}

// Profile is the normalized result entry of GetProfiles. The full
// tr2:MediaProfile structure embeds every assigned configuration; identity
// fields and the typed configuration associations are modeled, and the complete
// element is preserved as bounded, redacted XML for the remaining detail.
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
	// Associations carries the typed, optional configuration references from
	// the profile's tr2:ConfigurationSet so callers need not parse RawXML.
	Associations ProfileAssociations
	// RawXML is the bounded, credential-redacted MediaProfile element retained
	// for diagnostics because the full configuration tree is not modeled here.
	RawXML []byte
}

// VideoEncoderConfiguration is the normalized result entry of
// GetVideoEncoderConfigurations (ver20, tt:VideoEncoder2Configuration). It
// models the ConfigurationEntity base plus the common encoder fields. UseCount
// is deprecated for Media2 but remains required by the inherited XSD type.
type VideoEncoderConfiguration struct {
	// Token is the configuration reference token.
	Token string
	// Name is the user-readable configuration name.
	Name string
	// UseCount is the required inherited number of internal references.
	UseCount int
	// HasUseCount is true for every successfully decoded configuration and is
	// retained for symmetry with callers that distinguish wire presence.
	HasUseCount bool
	// Encoding is the video codec (for example "H264", "H265", "JPEG").
	Encoding string
	// Resolution is the configured resolution.
	Resolution VideoResolution
	// HasResolution is always true for a successfully decoded configuration.
	HasResolution bool
	// Quality is the required finite relative quality value.
	Quality float64
	// HasQuality is always true for a successfully decoded configuration.
	HasQuality bool
	// FrameRateLimit is the configured maximum output frame rate from the
	// optional tt:RateControl (tt:VideoRateControl2) block. It is a float
	// because the Media2 rate-control type models it as xs:float.
	FrameRateLimit float64
	// HasFrameRateLimit reports whether a RateControl FrameRateLimit was
	// present.
	HasFrameRateLimit bool
	// BitrateLimit is the configured maximum output bitrate (kbps) from the
	// optional tt:RateControl block.
	BitrateLimit int
	// HasBitrateLimit reports whether a RateControl BitrateLimit was present.
	HasBitrateLimit bool
	// ConstantBitRate is the optional unqualified boolean attribute on the
	// tt:RateControl element.
	ConstantBitRate bool
	// HasConstantBitRate reports whether the ConstantBitRate attribute was
	// present, including when its value was explicitly false.
	HasConstantBitRate bool
	// AverageBitRate is the configured optional non-negative average bitrate
	// from the tt:RateControl block.
	AverageBitRate int
	// HasAverageBitRate reports whether a RateControl AverageBitRate was
	// present.
	HasAverageBitRate bool
	// GovLength is the configured group-of-video length from the optional
	// unqualified GovLength attribute on the configuration element. It must be
	// positive when present.
	GovLength int
	// HasGovLength reports whether a GovLength attribute was present.
	HasGovLength bool
	// Profile is the configured codec profile from the optional unqualified
	// Profile attribute, validated against the Media2 profile vocabulary.
	Profile string
	// HasProfile reports whether a Profile attribute was present.
	HasProfile bool
	// RawXML is the bounded, credential-redacted configuration element.
	RawXML []byte
}

// VideoEncoderConfigurationOptions is the normalized result of
// GetVideoEncoderConfigurationOptions (ver20). Each required option field and
// range is validated; redacted XML remains available for extensions.
type VideoEncoderConfigurationOptions struct {
	Options    []VideoEncoderOption
	OptionsXML [][]byte
}

// AudioEncoderConfiguration is the normalized result entry of
// GetAudioEncoderConfigurations (ver20, tt:AudioEncoder2Configuration).
type AudioEncoderConfiguration struct {
	// Token is the configuration reference token.
	Token string
	// Name is the user-readable configuration name.
	Name string
	// UseCount is the required inherited number of internal references.
	UseCount int
	// HasUseCount is true for every successfully decoded configuration and is
	// retained for symmetry with callers that distinguish wire presence.
	HasUseCount bool
	// Encoding is the audio media subtype (for example "PCMU", "G726", or
	// "MP4A-LATM").
	Encoding string
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
