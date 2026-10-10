package app

import (
	"context"
	"net/url"
	"strings"

	media1 "github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
)

// profileAssociationTypes is the set of configuration types requested from the
// Media2 GetProfiles so the device returns the typed configuration
// associations the orchestration matches against.
var profileAssociationTypes = []media2.ConfigurationType{
	media2.ConfigurationVideoSource,
	media2.ConfigurationVideoEncoder,
	media2.ConfigurationAudioEncoder,
}

// fetchProfiles reads the profile list, preferring Media2 (requesting the
// association configuration types) and falling back to Media1 only when the
// Media2 call satisfies the narrow fallback predicate. It returns the
// normalized profiles, the answering service, whether that was a genuine
// fallback, and a fatal error for a non-eligible failure.
func (r *profileResolver) fetchProfiles(ctx context.Context) ([]normProfile, MediaSourceService, bool, error) {
	if r.clients.media2Available {
		var out []media2.Profile
		err := r.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
			var opts []media2.RequestOptions
			if header != nil {
				opts = []media2.RequestOptions{{HeaderXML: header}}
			}
			profiles, e := r.clients.media2.GetProfiles(ctx, "", profileAssociationTypes, opts...)
			if e != nil {
				return e
			}
			out = profiles
			return nil
		})
		if err == nil {
			return normalizeMedia2Profiles(out), MediaServiceMedia2, false, nil
		}
		if !shouldFallBackToMedia1(err) {
			return nil, MediaServiceUnknown, false, err
		}
		// Narrow fallback: Media2 reported ActionNotSupported for GetProfiles.
		if !r.clients.media1Available {
			return nil, MediaServiceUnknown, false, err
		}
		profiles, perr := r.readMedia1Profiles(ctx)
		if perr != nil {
			return nil, MediaServiceUnknown, false, perr
		}
		return normalizeMedia1Profiles(profiles), MediaServiceMedia1, true, nil
	}
	if r.clients.media1Available {
		profiles, perr := r.readMedia1Profiles(ctx)
		if perr != nil {
			return nil, MediaServiceUnknown, false, perr
		}
		// Direct Media1 because Media2 is absent — NOT a fallback.
		return normalizeMedia1Profiles(profiles), MediaServiceMedia1, false, nil
	}
	return nil, MediaServiceUnknown, false, media2.ErrNilResponse
}

func normalizeMedia2Profiles(in []media2.Profile) []normProfile {
	out := make([]normProfile, 0, len(in))
	for _, p := range in {
		out = append(out, normProfile{
			token:                   p.Token,
			name:                    p.Name,
			fixed:                   p.Fixed,
			hasFixed:                p.HasFixed,
			videoSourceConfigToken:  p.Associations.VideoSourceConfigurationToken,
			videoSourceToken:        p.Associations.VideoSourceToken,
			videoEncoderConfigToken: p.Associations.VideoEncoderConfigurationToken,
			audioEncoderConfigToken: p.Associations.AudioEncoderConfigurationToken,
		})
	}
	return out
}

func normalizeMedia1Profiles(in []media1.Profile) []normProfile {
	out := make([]normProfile, 0, len(in))
	for _, p := range in {
		out = append(out, normProfile{
			token:                   p.Token,
			name:                    p.Name,
			fixed:                   p.Fixed,
			hasFixed:                p.HasFixed,
			videoSourceConfigToken:  p.Associations.VideoSourceConfigurationToken,
			videoSourceToken:        p.Associations.VideoSourceToken,
			videoEncoderConfigToken: p.Associations.VideoEncoderConfigurationToken,
			audioEncoderConfigToken: p.Associations.AudioEncoderConfigurationToken,
		})
	}
	return out
}

// media1ProfileFor returns the Media1 profile whose token matches prof when the
// root GetProfiles was answered by Media2, so a Media1 fallback for an encoder
// or source operation matches that service's OWN association tokens rather than
// reusing the Media2-derived tokens. The two services are distinct wire models
// and a camera may reference different per-service configuration tokens for the
// same profile.
//
// When the profiles already came from Media1 (direct Media1, no Media2), prof
// already carries Media1 tokens and is returned unchanged. When the profiles
// came from Media2 but the Media1 GetProfiles read fails or lists no profile
// with the same token, ok is false and the caller records a safe,
// non-supported status instead of matching against a cross-version token.
func (r *profileResolver) media1ProfileFor(ctx context.Context, prof normProfile) (normProfile, bool, error) {
	if r.profilesSource != MediaServiceMedia2 {
		// Profiles were read directly from Media1 (or no profiles source was
		// recorded, e.g. a direct Media1 flow): prof already holds Media1
		// association tokens.
		return prof, true, nil
	}
	m1Profiles, err := r.readMedia1Profiles(ctx)
	if err != nil {
		return normProfile{}, false, err
	}
	profiles := normalizeMedia1Profiles(m1Profiles)
	index, matches := uniqueTokenIndex(len(profiles), prof.token, func(i int) string { return profiles[i].token })
	switch matches {
	case 0:
		return normProfile{}, false, nil
	case 1:
		return profiles[index], true, nil
	default:
		return normProfile{}, false, newError(CategoryProtocol, "the camera returned ambiguous media1 profiles", nil)
	}
}

// resolveVideoSource resolves the physical video source for a profile. There is
// no Media2 equivalent of GetVideoSources, so this is always a Media1 read; it
// matches the profile's typed physical source token rather than guessing by
// order. When Media1 is unavailable or the token does not match any source, the
// status is a safe non-supported outcome and no data is invented.
func (r *profileResolver) resolveVideoSource(ctx context.Context, prof normProfile) MediaOperationStatus {
	if !r.clients.media1Available {
		return MediaOperationStatus{Operation: MediaOpVideoSource, State: CapabilityUnsupported, Detail: "no media1 service to read video sources"}
	}
	// GetVideoSources is Media1-only. When the profile list came from Media2,
	// resolve the corresponding Media1 profile first and use ITS physical
	// source association token rather than assuming the Media2 source token is
	// valid in the Media1 source list.
	m1Prof, ok, perr := r.media1ProfileFor(ctx, prof)
	if perr != nil {
		return classifyMediaOp(perr).withOp(MediaOpVideoSource)
	}
	if !ok {
		return MediaOperationStatus{Operation: MediaOpVideoSource, State: CapabilityUnknown, Source: MediaServiceMedia1, Detail: "no media1 profile matched the profile token"}
	}
	if m1Prof.videoSourceToken == "" {
		return MediaOperationStatus{Operation: MediaOpVideoSource, State: CapabilityUnknown, Source: MediaServiceMedia1, Detail: "the profile has no physical video source association"}
	}
	sources, err := r.readMedia1VideoSources(ctx)
	if err != nil {
		return classifyMediaOp(err).withOp(MediaOpVideoSource)
	}
	_, matches := uniqueTokenIndex(len(sources), m1Prof.videoSourceToken, func(i int) string { return sources[i].Token })
	switch matches {
	case 0:
		// A mismatch is a safe protocol outcome, not invented data.
		return MediaOperationStatus{Operation: MediaOpVideoSource, State: CapabilityUnknown, Source: MediaServiceMedia1, Detail: "no video source matched the profile association"}
	case 1:
		return MediaOperationStatus{Operation: MediaOpVideoSource, State: CapabilitySupported, Source: MediaServiceMedia1}
	default:
		return MediaOperationStatus{Operation: MediaOpVideoSource, State: CapabilityUnknown, Source: MediaServiceMedia1, Detail: "the camera returned ambiguous physical video sources"}
	}
}

// resolveVideoEncoder resolves the video encoder summary, preferring the Media2
// configuration filtered by profile token and falling back to the Media1
// all-config read (matched by the typed association) only on the narrow
// predicate.
func (r *profileResolver) resolveVideoEncoder(ctx context.Context, prof normProfile) (*VideoConfigDTO, MediaOperationStatus) {
	if r.clients.media2Available {
		var configs []media2.VideoEncoderConfiguration
		err := r.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
			var opts []media2.RequestOptions
			if header != nil {
				opts = []media2.RequestOptions{{HeaderXML: header}}
			}
			c, e := r.clients.media2.GetVideoEncoderConfigurations(ctx, "", prof.token, opts...)
			if e != nil {
				return e
			}
			configs = c
			return nil
		})
		if err == nil {
			dto, selectionErr := media2VideoDTO(configs, prof.videoEncoderConfigToken)
			if selectionErr != nil {
				return nil, MediaOperationStatus{Operation: MediaOpVideoEncoder, State: CapabilityUnknown, Source: MediaServiceMedia2, Detail: selectionErr.Error()}
			}
			return dto, MediaOperationStatus{Operation: MediaOpVideoEncoder, State: CapabilitySupported, Source: MediaServiceMedia2}
		}
		if !shouldFallBackToMedia1(err) {
			return nil, classifyMediaOp(err).withOp(MediaOpVideoEncoder)
		}
		if !r.clients.media1Available {
			return nil, classifyMediaOp(err).withOp(MediaOpVideoEncoder)
		}
		dto, status := r.media1VideoEncoder(ctx, prof)
		status.FellBack = true
		return dto, status
	}
	if r.clients.media1Available {
		return r.media1VideoEncoder(ctx, prof)
	}
	return nil, MediaOperationStatus{Operation: MediaOpVideoEncoder, State: CapabilityUnsupported, Detail: "no media service answered the video encoder read"}
}

// media1VideoEncoder reads the Media1 all-config list (cached) and matches the
// profile's typed video encoder association token. A missing or mismatched
// configuration is a safe outcome, not invented data.
func (r *profileResolver) media1VideoEncoder(ctx context.Context, prof normProfile) (*VideoConfigDTO, MediaOperationStatus) {
	// Resolve the Media1 profile for the token so the match uses the Media1
	// video encoder association, never the Media2-derived token.
	m1Prof, ok, perr := r.media1ProfileFor(ctx, prof)
	if perr != nil {
		return nil, classifyMediaOp(perr).withOp(MediaOpVideoEncoder)
	}
	if !ok {
		return nil, MediaOperationStatus{Operation: MediaOpVideoEncoder, State: CapabilityUnknown, Source: MediaServiceMedia1, Detail: "no media1 profile matched the profile token"}
	}
	configs, err := r.readMedia1VideoEncoders(ctx)
	if err != nil {
		return nil, classifyMediaOp(err).withOp(MediaOpVideoEncoder)
	}
	if m1Prof.videoEncoderConfigToken == "" {
		return nil, MediaOperationStatus{Operation: MediaOpVideoEncoder, State: CapabilityUnknown, Source: MediaServiceMedia1, Detail: "the profile has no video encoder association"}
	}
	index, matches := uniqueTokenIndex(len(configs), m1Prof.videoEncoderConfigToken, func(i int) string { return configs[i].Token })
	switch matches {
	case 0:
		return nil, MediaOperationStatus{Operation: MediaOpVideoEncoder, State: CapabilityUnknown, Source: MediaServiceMedia1, Detail: "no video encoder matched the profile association"}
	case 1:
		return media1VideoDTO(configs[index]), MediaOperationStatus{Operation: MediaOpVideoEncoder, State: CapabilitySupported, Source: MediaServiceMedia1}
	default:
		return nil, MediaOperationStatus{Operation: MediaOpVideoEncoder, State: CapabilityUnknown, Source: MediaServiceMedia1, Detail: "the camera returned ambiguous media1 video encoders"}
	}
}

// resolveAudioEncoder mirrors resolveVideoEncoder for the audio configuration.
func (r *profileResolver) resolveAudioEncoder(ctx context.Context, prof normProfile) (*AudioConfigDTO, MediaOperationStatus) {
	if r.clients.media2Available {
		var configs []media2.AudioEncoderConfiguration
		err := r.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
			var opts []media2.RequestOptions
			if header != nil {
				opts = []media2.RequestOptions{{HeaderXML: header}}
			}
			c, e := r.clients.media2.GetAudioEncoderConfigurations(ctx, "", prof.token, opts...)
			if e != nil {
				return e
			}
			configs = c
			return nil
		})
		if err == nil {
			dto, selectionErr := media2AudioDTO(configs, prof.audioEncoderConfigToken)
			if selectionErr != nil {
				return nil, MediaOperationStatus{Operation: MediaOpAudioEncoder, State: CapabilityUnknown, Source: MediaServiceMedia2, Detail: selectionErr.Error()}
			}
			return dto, MediaOperationStatus{Operation: MediaOpAudioEncoder, State: CapabilitySupported, Source: MediaServiceMedia2}
		}
		if !shouldFallBackToMedia1(err) {
			return nil, classifyMediaOp(err).withOp(MediaOpAudioEncoder)
		}
		if !r.clients.media1Available {
			return nil, classifyMediaOp(err).withOp(MediaOpAudioEncoder)
		}
		dto, status := r.media1AudioEncoder(ctx, prof)
		status.FellBack = true
		return dto, status
	}
	if r.clients.media1Available {
		return r.media1AudioEncoder(ctx, prof)
	}
	return nil, MediaOperationStatus{Operation: MediaOpAudioEncoder, State: CapabilityUnsupported, Detail: "no media service answered the audio encoder read"}
}

func (r *profileResolver) media1AudioEncoder(ctx context.Context, prof normProfile) (*AudioConfigDTO, MediaOperationStatus) {
	m1Prof, ok, perr := r.media1ProfileFor(ctx, prof)
	if perr != nil {
		return nil, classifyMediaOp(perr).withOp(MediaOpAudioEncoder)
	}
	if !ok {
		return nil, MediaOperationStatus{Operation: MediaOpAudioEncoder, State: CapabilityUnknown, Source: MediaServiceMedia1, Detail: "no media1 profile matched the profile token"}
	}
	configs, err := r.readMedia1AudioEncoders(ctx)
	if err != nil {
		return nil, classifyMediaOp(err).withOp(MediaOpAudioEncoder)
	}
	if m1Prof.audioEncoderConfigToken == "" {
		return nil, MediaOperationStatus{Operation: MediaOpAudioEncoder, State: CapabilityUnknown, Source: MediaServiceMedia1, Detail: "the profile has no audio encoder association"}
	}
	index, matches := uniqueTokenIndex(len(configs), m1Prof.audioEncoderConfigToken, func(i int) string { return configs[i].Token })
	switch matches {
	case 0:
		return nil, MediaOperationStatus{Operation: MediaOpAudioEncoder, State: CapabilityUnknown, Source: MediaServiceMedia1, Detail: "no audio encoder matched the profile association"}
	case 1:
		return media1AudioDTO(configs[index]), MediaOperationStatus{Operation: MediaOpAudioEncoder, State: CapabilitySupported, Source: MediaServiceMedia1}
	default:
		return nil, MediaOperationStatus{Operation: MediaOpAudioEncoder, State: CapabilityUnknown, Source: MediaServiceMedia1, Detail: "the camera returned ambiguous media1 audio encoders"}
	}
}

// resolveStream resolves one transport's stream URI, validates it, and returns
// the safe presentation URI plus a status with the correct operation name and
// fallback flag. An invalid URI becomes that operation's safe non-supported
// status and never enters the DTO.
func (r *profileResolver) resolveStream(ctx context.Context, token string, tcp bool) (string, MediaOperationStatus) {
	op := MediaOpStreamUDP
	if tcp {
		op = MediaOpStreamTCP
	}
	raw, src, fellBack, err := r.streamURI(ctx, token, tcp)
	if err != nil {
		return "", classifyMediaOp(err).withOpFellBack(op, fellBack)
	}
	safe, verr := r.validateStreamURI(raw)
	if verr != nil {
		return "", MediaOperationStatus{Operation: op, State: CapabilityUnknown, Source: src, FellBack: fellBack, Detail: "the returned stream URI was not valid"}
	}
	return safe, MediaOperationStatus{Operation: op, State: CapabilitySupported, Source: src, FellBack: fellBack}
}

// streamURI fetches one stream URI (tcp selects RTSP-over-TCP, else RTP/UDP),
// preferring Media2 and falling back to Media1 only on the narrow predicate.
func (r *profileResolver) streamURI(ctx context.Context, token string, tcp bool) (string, MediaSourceService, bool, error) {
	if r.clients.media2Available {
		protocol := media2.StreamRtspUnicast // UDP
		if tcp {
			protocol = media2.StreamRTSP // TCP
		}
		var uri string
		err := r.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
			var opts []media2.RequestOptions
			if header != nil {
				opts = []media2.RequestOptions{{HeaderXML: header}}
			}
			u, e := r.clients.media2.GetStreamUri(ctx, protocol, token, opts...)
			if e != nil {
				return e
			}
			uri = u
			return nil
		})
		if err == nil {
			return uri, MediaServiceMedia2, false, nil
		}
		if !shouldFallBackToMedia1(err) {
			return "", MediaServiceUnknown, false, err
		}
		if !r.clients.media1Available {
			return "", MediaServiceUnknown, false, err
		}
		uri, serr := r.media1StreamURI(ctx, token, tcp)
		if serr != nil {
			return "", MediaServiceUnknown, true, serr
		}
		return uri, MediaServiceMedia1, true, nil
	}
	if r.clients.media1Available {
		uri, serr := r.media1StreamURI(ctx, token, tcp)
		if serr != nil {
			return "", MediaServiceUnknown, false, serr
		}
		return uri, MediaServiceMedia1, false, nil
	}
	return "", MediaServiceUnknown, false, media2.ErrNilResponse
}

func (r *profileResolver) media1StreamURI(ctx context.Context, token string, tcp bool) (string, error) {
	setup := media1.StreamSetup{Stream: media1.StreamRTPUnicast, Protocol: media1.TransportUDP}
	if tcp {
		setup.Protocol = media1.TransportRTSP
	}
	var uri string
	err := r.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var opts []media1.RequestOptions
		if header != nil {
			opts = []media1.RequestOptions{{HeaderXML: header}}
		}
		u, e := r.clients.media1.GetStreamUri(ctx, token, setup, opts...)
		if e != nil {
			return e
		}
		uri = u.URI
		return nil
	})
	if err != nil {
		return "", err
	}
	return uri, nil
}

// resolveSnapshotPresentation resolves the snapshot URI and returns the safe
// presentation value plus the operation status. The snapshot use case uses the
// separate snapshotURI method to obtain the operational URI for fetching.
func (r *profileResolver) resolveSnapshotPresentation(ctx context.Context, token string) (string, MediaOperationStatus) {
	raw, src, fellBack, err := r.snapshotURIFull(ctx, token)
	if err != nil {
		return "", classifyMediaOp(err).withOpFellBack(MediaOpSnapshotURI, fellBack)
	}
	safe, verr := r.validateSnapshotPresentationURI(raw)
	if verr != nil {
		return "", MediaOperationStatus{Operation: MediaOpSnapshotURI, State: CapabilityUnknown, Source: src, FellBack: fellBack, Detail: "the returned snapshot URI was not valid"}
	}
	return safe, MediaOperationStatus{Operation: MediaOpSnapshotURI, State: CapabilitySupported, Source: src, FellBack: fellBack}
}

// snapshotURI is retained for the snapshot use case: it returns the operational
// snapshot URI (query preserved) and the answering service.
func (r *profileResolver) snapshotURI(ctx context.Context, token string) (string, MediaSourceService, error) {
	raw, src, _, err := r.snapshotURIFull(ctx, token)
	return raw, src, err
}

// snapshotURIFull fetches the operational snapshot URI preferring Media2 and
// falling back to Media1 only on the narrow predicate.
func (r *profileResolver) snapshotURIFull(ctx context.Context, token string) (string, MediaSourceService, bool, error) {
	if r.clients.media2Available {
		var uri string
		err := r.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
			var opts []media2.RequestOptions
			if header != nil {
				opts = []media2.RequestOptions{{HeaderXML: header}}
			}
			u, e := r.clients.media2.GetSnapshotUri(ctx, token, opts...)
			if e != nil {
				return e
			}
			uri = u
			return nil
		})
		if err == nil {
			return uri, MediaServiceMedia2, false, nil
		}
		if !shouldFallBackToMedia1(err) {
			return "", MediaServiceUnknown, false, err
		}
		if !r.clients.media1Available {
			return "", MediaServiceUnknown, false, err
		}
		uri, serr := r.media1SnapshotURI(ctx, token)
		if serr != nil {
			return "", MediaServiceUnknown, true, serr
		}
		return uri, MediaServiceMedia1, true, nil
	}
	if r.clients.media1Available {
		uri, serr := r.media1SnapshotURI(ctx, token)
		if serr != nil {
			return "", MediaServiceUnknown, false, serr
		}
		return uri, MediaServiceMedia1, false, nil
	}
	return "", MediaServiceUnknown, false, media2.ErrNilResponse
}

func (r *profileResolver) media1SnapshotURI(ctx context.Context, token string) (string, error) {
	var uri string
	err := r.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var opts []media1.RequestOptions
		if header != nil {
			opts = []media1.RequestOptions{{HeaderXML: header}}
		}
		u, e := r.clients.media1.GetSnapshotUri(ctx, token, opts...)
		if e != nil {
			return e
		}
		uri = u.URI
		return nil
	})
	if err != nil {
		return "", err
	}
	return uri, nil
}

// --- Lazy Media1 whole-list caches ------------------------------------------

func (r *profileResolver) readMedia1Profiles(ctx context.Context) ([]media1.Profile, error) {
	if r.m1ProfilesDone {
		return r.m1Profiles, r.m1ProfilesErr
	}
	r.m1ProfilesDone = true
	err := r.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var opts []media1.RequestOptions
		if header != nil {
			opts = []media1.RequestOptions{{HeaderXML: header}}
		}
		p, e := r.clients.media1.GetProfiles(ctx, opts...)
		if e != nil {
			return e
		}
		r.m1Profiles = p
		return nil
	})
	r.m1ProfilesErr = err
	return r.m1Profiles, err
}

func (r *profileResolver) readMedia1VideoSources(ctx context.Context) ([]media1.VideoSource, error) {
	if r.m1VideoSrcDone {
		return r.m1VideoSrc, r.m1VideoSrcErr
	}
	r.m1VideoSrcDone = true
	err := r.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var opts []media1.RequestOptions
		if header != nil {
			opts = []media1.RequestOptions{{HeaderXML: header}}
		}
		s, e := r.clients.media1.GetVideoSources(ctx, opts...)
		if e != nil {
			return e
		}
		r.m1VideoSrc = s
		return nil
	})
	r.m1VideoSrcErr = err
	return r.m1VideoSrc, err
}

func (r *profileResolver) readMedia1VideoEncoders(ctx context.Context) ([]media1.VideoEncoderConfiguration, error) {
	if r.m1VideoEncDone {
		return r.m1VideoEnc, r.m1VideoEncErr
	}
	r.m1VideoEncDone = true
	err := r.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var opts []media1.RequestOptions
		if header != nil {
			opts = []media1.RequestOptions{{HeaderXML: header}}
		}
		c, e := r.clients.media1.GetVideoEncoderConfigurations(ctx, opts...)
		if e != nil {
			return e
		}
		r.m1VideoEnc = c
		return nil
	})
	r.m1VideoEncErr = err
	return r.m1VideoEnc, err
}

func (r *profileResolver) readMedia1AudioEncoders(ctx context.Context) ([]media1.AudioEncoderConfiguration, error) {
	if r.m1AudioEncDone {
		return r.m1AudioEnc, r.m1AudioEncErr
	}
	r.m1AudioEncDone = true
	err := r.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var opts []media1.RequestOptions
		if header != nil {
			opts = []media1.RequestOptions{{HeaderXML: header}}
		}
		c, e := r.clients.media1.GetAudioEncoderConfigurations(ctx, opts...)
		if e != nil {
			return e
		}
		r.m1AudioEnc = c
		return nil
	})
	r.m1AudioEncErr = err
	return r.m1AudioEnc, err
}

// --- Typed DTO builders ------------------------------------------------------

// media2VideoDTO selects the configuration matching the association token (or
// the sole returned configuration when the token is empty) and builds the
// presentation DTO. Missing or ambiguous results return a safe protocol error;
// duplicate wire tokens can never be resolved by response order.
func media2VideoDTO(configs []media2.VideoEncoderConfiguration, token string) (*VideoConfigDTO, error) {
	index, err := selectByToken(len(configs), token, func(i int) string { return configs[i].Token })
	if err != nil {
		return nil, err
	}
	cfg := configs[index]
	dto := &VideoConfigDTO{
		ConfigToken: cfg.Token,
		Codec:       cfg.Encoding,
		Width:       cfg.Resolution.Width,
		Height:      cfg.Resolution.Height,
		BitrateKbps: cfg.BitrateLimit,
	}
	if cfg.HasFrameRateLimit {
		dto.FrameRateFPS = cfg.FrameRateLimit
		dto.HasFrameRate = true
	}
	if cfg.HasGovLength {
		dto.GOPLength = cfg.GovLength
	}
	if cfg.HasProfile {
		dto.Profile = cfg.Profile
	}
	return dto, nil
}

func media1VideoDTO(cfg media1.VideoEncoderConfiguration) *VideoConfigDTO {
	dto := &VideoConfigDTO{
		ConfigToken: cfg.Token,
		Codec:       string(cfg.Encoding),
		Width:       cfg.Resolution.Width,
		Height:      cfg.Resolution.Height,
		BitrateKbps: cfg.BitrateLimit,
	}
	if cfg.HasFrameRateLimit {
		dto.FrameRateFPS = float64(cfg.FrameRateLimit)
		dto.HasFrameRate = true
	}
	if cfg.HasGovLength {
		dto.GOPLength = cfg.GovLength
	}
	if cfg.HasCodecProfile {
		dto.Profile = cfg.CodecProfile
	}
	return dto
}

func media2AudioDTO(configs []media2.AudioEncoderConfiguration, token string) (*AudioConfigDTO, error) {
	index, err := selectByToken(len(configs), token, func(i int) string { return configs[i].Token })
	if err != nil {
		return nil, err
	}
	cfg := configs[index]
	return &AudioConfigDTO{
		ConfigToken:   cfg.Token,
		Codec:         cfg.Encoding,
		BitrateKbps:   cfg.Bitrate,
		SampleRateKHz: cfg.SampleRate,
	}, nil
}

func media1AudioDTO(cfg media1.AudioEncoderConfiguration) *AudioConfigDTO {
	return &AudioConfigDTO{
		ConfigToken:   cfg.Token,
		Codec:         string(cfg.Encoding),
		BitrateKbps:   cfg.Bitrate,
		SampleRateKHz: cfg.SampleRate,
	}
}

// uniqueTokenIndex scans until it can classify token as absent, unique, or
// ambiguous. The count is capped at two because callers do not need the exact
// number of duplicate identities.
func uniqueTokenIndex(n int, token string, tokenAt func(i int) string) (int, int) {
	matched := -1
	matches := 0
	for i := 0; i < n; i++ {
		if tokenAt(i) != token {
			continue
		}
		matched = i
		matches++
		if matches > 1 {
			return 0, 2
		}
	}
	return matched, matches
}

// selectByToken requires exactly one configuration to satisfy the requested
// identity. When token is empty, exactly one unfiltered result is required.
// When token is non-empty, the collection is checked for duplicate matching
// tokens so ambiguity is rejected rather than silently resolved by response
// order. Every failure is a fixed, token-free protocol error.
func selectByToken(n int, token string, tokenAt func(i int) string) (int, error) {
	if n == 0 {
		return 0, newError(CategoryProtocol, "the camera returned no matching media configuration", nil)
	}
	if token == "" {
		if n == 1 {
			return 0, nil
		}
		return 0, newError(CategoryProtocol, "the camera returned ambiguous media configurations", nil)
	}

	matched, matches := uniqueTokenIndex(n, token, tokenAt)
	switch matches {
	case 0:
		return 0, newError(CategoryProtocol, "the camera returned no matching media configuration", nil)
	case 1:
		return matched, nil
	default:
		return 0, newError(CategoryProtocol, "the camera returned ambiguous media configurations", nil)
	}
}

// --- URI validators ----------------------------------------------------------

// validateStreamURI enforces that a stream URI is an absolute rtsp/rtsps URL
// with a host, no userinfo, no fragment, no control characters, and whose
// hostname equals the camera hostname (the RTSP port may differ). It returns
// the safe presentation URI with userinfo and query removed. Errors never
// include the URI value.
func (r *profileResolver) validateStreamURI(raw string) (string, error) {
	if raw == "" || containsControl(raw) {
		return "", usageError("the stream URI is not valid")
	}
	parsed, err := url.Parse(raw)
	if err != nil || !parsed.IsAbs() {
		return "", usageError("the stream URI is not valid")
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "rtsp" && scheme != "rtsps" {
		return "", usageError("the stream URI scheme is not supported")
	}
	if parsed.User != nil {
		return "", usageError("the stream URI must not contain credentials")
	}
	if parsed.Fragment != "" || parsed.RawFragment != "" {
		return "", usageError("the stream URI must not contain a fragment")
	}
	if parsed.Hostname() == "" {
		return "", usageError("the stream URI is not valid")
	}
	if !hostnameEqual(parsed.Hostname(), r.cameraURL.Hostname()) {
		return "", usageError("the stream URI host does not match the camera")
	}
	if urlHasDecodedControl(parsed) {
		return "", usageError("the stream URI is not valid")
	}
	return stripURIForPresentation(parsed), nil
}

// validateSnapshotPresentationURI enforces that a snapshot URI is an absolute
// http/https URL with no userinfo/fragment/control characters and is exact
// same-origin with the camera. It returns the safe presentation URI with
// userinfo and query removed. Errors never include the URI value.
func (r *profileResolver) validateSnapshotPresentationURI(raw string) (string, error) {
	parsed, err := validateSnapshotURI(r.cameraURL, raw)
	if err != nil {
		return "", err
	}
	return stripURIForPresentation(parsed), nil
}
