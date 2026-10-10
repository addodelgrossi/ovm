package app

// Media profile and snapshot DTOs.
//
// These types are the normalized, credential-free presentation of a camera's
// media profiles and of a retrieved snapshot. They are produced by the Media
// use cases (see media_adapter.go) from the typed Media2 (ver20) and Media1
// (ver10) wire clients. Three rules govern every field:
//
//  1. No operational secrets ever cross this boundary. A stream or snapshot
//     URI may carry a query string that is itself a credential (for example a
//     signed token). The DTO contains ONLY the validated, presentation-safe
//     URI with its userinfo and query stripped entirely. There is no exported
//     accessor that returns an operational (connect-ready) URI: the operational
//     value is consumed inside the use case and never stored on a DTO.
//  2. English only, deterministic order. Profiles are sorted by token; the
//     per-operation statuses are emitted in a fixed order; every status records
//     which service answered (provenance) and whether a Media1 result was a
//     genuine fallback after a Media2 ActionNotSupported.
//  3. Repository snake_case JSON conventions on every serialized field.

// MediaSourceService identifies which ONVIF media service answered an
// operation. It is the provenance marker required by the use case: callers can
// tell whether a value came from the ver20 Media2 service or the ver10 Media1
// fallback.
type MediaSourceService string

const (
	// MediaServiceUnknown means no service produced a value (the operation did
	// not run or produced no conclusive result).
	MediaServiceUnknown MediaSourceService = ""
	// MediaServiceMedia2 means the ONVIF Media2 (ver20) service answered.
	MediaServiceMedia2 MediaSourceService = "media2"
	// MediaServiceMedia1 means the ONVIF Media1 (ver10) service answered.
	MediaServiceMedia1 MediaSourceService = "media1"
)

// MediaOperationName is the stable, machine-readable name of a per-profile
// media operation whose capability and provenance are reported independently.
type MediaOperationName string

const (
	// MediaOpProfiles is the source GetProfiles operation.
	MediaOpProfiles MediaOperationName = "profiles"
	// MediaOpVideoSource is the physical video source read (Media1 only).
	MediaOpVideoSource MediaOperationName = "video_source"
	// MediaOpVideoEncoder is the video encoder configuration read.
	MediaOpVideoEncoder MediaOperationName = "video_encoder"
	// MediaOpAudioEncoder is the audio encoder configuration read.
	MediaOpAudioEncoder MediaOperationName = "audio_encoder"
	// MediaOpStreamTCP is the RTSP-over-TCP stream URI read.
	MediaOpStreamTCP MediaOperationName = "stream_tcp"
	// MediaOpStreamUDP is the RTP-over-UDP stream URI read.
	MediaOpStreamUDP MediaOperationName = "stream_udp"
	// MediaOpSnapshotURI is the snapshot URI read.
	MediaOpSnapshotURI MediaOperationName = "snapshot_uri"
)

// mediaOperationOrder is the fixed, deterministic order in which per-profile
// operation statuses are emitted. The orchestration appends statuses in this
// exact sequence so a caller can rely on the ordering without sorting.
var mediaOperationOrder = []MediaOperationName{
	MediaOpProfiles,
	MediaOpVideoSource,
	MediaOpVideoEncoder,
	MediaOpAudioEncoder,
	MediaOpStreamTCP,
	MediaOpStreamUDP,
	MediaOpSnapshotURI,
}

// MediaOperationStatus records the capability state, the service that answered
// (provenance), whether a Media1 result was a genuine fallback, and a safe,
// credential-free English detail for one per-profile media operation. Detail is
// empty on a clean success.
type MediaOperationStatus struct {
	// Operation is the stable per-operation name.
	Operation MediaOperationName `json:"operation"`
	// State is the normalized capability state (reusing the Device enum).
	State CapabilityState `json:"state"`
	// Source is the service that produced the value, or MediaServiceUnknown.
	Source MediaSourceService `json:"source,omitempty"`
	// FellBack reports whether this value came from Media1 ONLY because the
	// Media2 attempt returned the narrow ActionNotSupported. A direct Media1
	// result because Media2 was absent is NOT a fallback and leaves this false.
	FellBack bool `json:"fell_back,omitempty"`
	// Detail is a safe English explanation for a non-supported state; empty on
	// success. It never contains a URI, token, query, or credential.
	Detail string `json:"detail,omitempty"`
}

// VideoConfigDTO is the normalized, presentation-safe video encoder summary of
// a profile. All fields are safe to serialize; none carries a secret.
type VideoConfigDTO struct {
	// ConfigToken is the video encoder configuration token.
	ConfigToken string `json:"config_token,omitempty"`
	// Codec is the video codec (for example "H264", "H265", "JPEG").
	Codec string `json:"codec,omitempty"`
	// Width and Height are the configured resolution in pixels.
	Width  int `json:"width,omitempty"`
	Height int `json:"height,omitempty"`
	// FrameRateFPS is the configured frames-per-second. It is a float64 to
	// preserve the Media2 rate-control type, which models the frame-rate limit
	// as a floating-point value.
	FrameRateFPS float64 `json:"frame_rate_fps,omitempty"`
	// HasFrameRate reports whether a frame-rate value was present, so a
	// legitimate configured 0 is distinguishable from an absent value.
	HasFrameRate bool `json:"has_frame_rate,omitempty"`
	// BitrateKbps is the configured bitrate in kbps, when reported.
	BitrateKbps int `json:"bitrate_kbps,omitempty"`
	// GOPLength is the configured group-of-pictures length, when reported.
	GOPLength int `json:"gop_length,omitempty"`
	// Profile is the configured codec profile (for example "Main"), when
	// reported.
	Profile string `json:"profile,omitempty"`
}

// AudioConfigDTO is the normalized, presentation-safe audio encoder summary.
type AudioConfigDTO struct {
	// ConfigToken is the audio encoder configuration token.
	ConfigToken string `json:"config_token,omitempty"`
	// Codec is the audio codec (for example "G711", "AAC", "PCMU").
	Codec string `json:"codec,omitempty"`
	// BitrateKbps is the configured output bitrate in kbps, when reported.
	BitrateKbps int `json:"bitrate_kbps,omitempty"`
	// SampleRateKHz is the configured sample rate in kHz, when reported.
	SampleRateKHz int `json:"sample_rate_khz,omitempty"`
}

// StreamURIDTO holds the safe, presentation-facing stream URIs for a profile,
// one per transport. Both values are validated and have their userinfo and
// query stripped entirely before they are stored here; there is NO operational
// (connect-ready) URI on the DTO and no accessor that returns one.
type StreamURIDTO struct {
	// TCPURI is the safe RTSP-over-TCP stream URI (userinfo/query removed).
	// Empty when not available.
	TCPURI string `json:"tcp_uri,omitempty"`
	// UDPURI is the safe RTP-over-UDP stream URI (userinfo/query removed).
	// Empty when not available.
	UDPURI string `json:"udp_uri,omitempty"`
}

// SnapshotURIDTO holds the safe snapshot URI for a profile. The value is
// validated and has its userinfo and query stripped entirely before it is
// stored; there is NO operational URI on the DTO and no accessor that returns
// one.
type SnapshotURIDTO struct {
	// URI is the safe snapshot URI for presentation (userinfo/query removed).
	// Empty when not available.
	URI string `json:"uri,omitempty"`
}

// MediaProfileDTO is the normalized, deterministic, credential-free view of one
// media profile and the media values resolved for it. Profiles are sorted by
// Token. Every value records its provenance through the per-operation statuses.
type MediaProfileDTO struct {
	// Token is the profile reference token (the sort key).
	Token string `json:"token"`
	// Name is the user-readable profile name.
	Name string `json:"name,omitempty"`
	// Fixed reports whether the profile is a fixed (non-deletable) profile.
	Fixed bool `json:"fixed,omitempty"`
	// HasFixed reports whether the device supplied the fixed attribute, so a
	// legitimate false is distinguishable from an absent attribute.
	HasFixed bool `json:"has_fixed,omitempty"`
	// Source is the service that produced the profile itself (provenance).
	Source MediaSourceService `json:"source,omitempty"`

	// VideoSourceConfigToken is the token of the profile's video source
	// configuration (the configuration element's own reference token).
	VideoSourceConfigToken string `json:"video_source_config_token,omitempty"`
	// VideoSourceToken is the physical video source token (tt:SourceToken)
	// referenced by the video source configuration.
	VideoSourceToken string `json:"video_source_token,omitempty"`
	// VideoEncoderConfigToken is the token of the profile's video encoder
	// configuration.
	VideoEncoderConfigToken string `json:"video_encoder_config_token,omitempty"`

	// Video and Audio are the normalized encoder summaries, when resolved.
	Video *VideoConfigDTO `json:"video,omitempty"`
	Audio *AudioConfigDTO `json:"audio,omitempty"`

	// Stream and Snapshot carry the safe stream and snapshot URIs.
	Stream   *StreamURIDTO   `json:"stream,omitempty"`
	Snapshot *SnapshotURIDTO `json:"snapshot,omitempty"`

	// Operations is the per-operation capability and provenance record for this
	// profile, emitted in the fixed mediaOperationOrder.
	Operations []MediaOperationStatus `json:"operations,omitempty"`
}

// MediaProfilesResult is the normalized result of the profile use case: the
// deterministically-sorted profiles and the services that were discovered.
type MediaProfilesResult struct {
	// Profiles are sorted by Token.
	Profiles []MediaProfileDTO `json:"profiles"`
	// Media2Available reports whether a Media2 (ver20) service endpoint was
	// discovered and used.
	Media2Available bool `json:"media2_available"`
	// Media1Available reports whether a Media1 (ver10) service endpoint was
	// discovered and used.
	Media1Available bool `json:"media1_available"`
}

// SnapshotMetadata is the credential-free result of the snapshot retrieval use
// case. It deliberately exposes NO image bytes and NO base64: the JPEG is
// written to disk atomically and only its metadata is returned so a JSON
// encoding can never carry the image or a secret.
type SnapshotMetadata struct {
	// ProfileToken is the profile whose snapshot was retrieved.
	ProfileToken string `json:"profile_token"`
	// OutputPath is the safely-normalized absolute path of the written JPEG.
	OutputPath string `json:"output_path"`
	// ByteSize is the number of bytes written.
	ByteSize int64 `json:"byte_size"`
	// ContentType is the parsed response content type (always image/jpeg).
	ContentType string `json:"content_type"`
	// Source is the service that produced the snapshot URI (provenance).
	Source MediaSourceService `json:"source"`
}
