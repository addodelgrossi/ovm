package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"time"

	media1 "github.com/addodelgrossi/ovm/pkg/onvif/media"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// EncoderRequest selects one profile's video encoder.
type EncoderRequest struct {
	Host         string
	Credentials  *Credentials
	ProfileToken string
	Timeout      time.Duration
}

// EncoderSettings is the editable state of a video encoder configuration.
type EncoderSettings struct {
	ConfigurationToken string             `json:"configuration_token"`
	Source             MediaSourceService `json:"source"`
	Encoding           string             `json:"encoding"`
	Width              int                `json:"width"`
	Height             int                `json:"height"`
	FrameRate          float64            `json:"frame_rate"`
	BitrateKbps        int                `json:"bitrate_kbps"`
	GOPLength          int                `json:"gop_length"`
	CodecProfile       string             `json:"codec_profile"`
}

// ResolutionDTO is one supported resolution.
type ResolutionDTO struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// EncoderCodecOptions are the values the device accepts for one codec.
// FrameRates lists discrete rates when the device advertises them; otherwise
// FrameRateMin/Max bound the rate.
type EncoderCodecOptions struct {
	Encoding     string          `json:"encoding"`
	Resolutions  []ResolutionDTO `json:"resolutions"`
	FrameRates   []float64       `json:"frame_rates"`
	FrameRateMin float64         `json:"frame_rate_min"`
	FrameRateMax float64         `json:"frame_rate_max"`
	BitrateMin   int             `json:"bitrate_min"`
	BitrateMax   int             `json:"bitrate_max"`
	GOPMin       int             `json:"gop_min"`
	GOPMax       int             `json:"gop_max"`
	Profiles     []string        `json:"profiles"`
}

// EncoderView is the current encoder state and its options.
type EncoderView struct {
	ProfileToken string                `json:"profile_token"`
	Current      EncoderSettings       `json:"current"`
	Codecs       []EncoderCodecOptions `json:"codecs"`
}

// EncoderChange is the complete requested encoder state.
type EncoderChange struct {
	Encoding     string  `json:"encoding"`
	Width        int     `json:"width"`
	Height       int     `json:"height"`
	FrameRate    float64 `json:"frame_rate"`
	BitrateKbps  int     `json:"bitrate_kbps"`
	GOPLength    int     `json:"gop_length"`
	CodecProfile string  `json:"codec_profile"`
}

// EncoderApplyResult is the verified result of an encoder change.
type EncoderApplyResult struct {
	ProfileToken string          `json:"profile_token"`
	Before       EncoderSettings `json:"before"`
	After        EncoderSettings `json:"after"`
	Report       MutationReport  `json:"report"`
}

// EncoderUseCase reads and changes a profile's video encoder.
type EncoderUseCase interface {
	ReadEncoder(ctx context.Context, request EncoderRequest) (EncoderView, error)
	ApplyEncoder(ctx context.Context, request EncoderRequest, change EncoderChange) (EncoderApplyResult, error)
}

type media2Encoder interface {
	GetProfiles(ctx context.Context, token string, types []media2.ConfigurationType, opts ...media2.RequestOptions) ([]media2.Profile, error)
	GetVideoEncoderConfigurations(ctx context.Context, configurationToken, profileToken string, opts ...media2.RequestOptions) ([]media2.VideoEncoderConfiguration, error)
	GetVideoEncoderConfigurationOptions(ctx context.Context, configurationToken, profileToken string, opts ...media2.RequestOptions) (media2.VideoEncoderConfigurationOptions, error)
	SetVideoEncoderConfiguration(ctx context.Context, config media2.VideoEncoderConfiguration, opts ...media2.RequestOptions) error
}

type media1Encoder interface {
	GetProfiles(ctx context.Context, opts ...media1.RequestOptions) ([]media1.Profile, error)
	GetVideoEncoderConfigurations(ctx context.Context, opts ...media1.RequestOptions) ([]media1.VideoEncoderConfiguration, error)
	GetVideoEncoderConfigurationOptions(ctx context.Context, configurationToken, profileToken string, opts ...media1.RequestOptions) (media1.VideoEncoderConfigurationOptions, error)
	SetVideoEncoderConfiguration(ctx context.Context, config media1.VideoEncoderConfiguration, forcePersistence bool, opts ...media1.RequestOptions) error
}

var (
	_ media2Encoder = (*media2.Client)(nil)
	_ media1Encoder = (*media1.Client)(nil)
)

// NewProductionEncoder returns the production EncoderUseCase.
func NewProductionEncoder() EncoderUseCase { return &productionMedia{} }

// encoderSession binds one authenticated session to the encoder of a profile,
// preferring Media2 and falling back to Media1 only when Media2 is absent or
// reports ActionNotSupported.
type encoderSession struct {
	session      *session
	seam         *authDeviceSeam
	cameraURL    *url.URL
	media2       media2Encoder
	media1       media1Encoder
	profileToken string
	source       MediaSourceService
	media2Config media2.VideoEncoderConfiguration
	media1Config media1.VideoEncoderConfiguration
	// media1Profile is the profile read by the last Media1 load.
	media1Profile media1.Profile
}

func (p *productionMedia) openEncoder(ctx context.Context, request EncoderRequest) (*encoderSession, context.Context, context.CancelFunc, error) {
	if err := ValidateSnapshotProfileToken(request.ProfileToken); err != nil {
		return nil, nil, nil, err
	}
	cameraURL, seam, timeout, err := p.prepare(request.Host, request.Timeout, request.Credentials)
	if err != nil {
		return nil, nil, nil, err
	}
	opCtx, cancel := context.WithTimeout(ctx, timeout)
	seam.negotiate(opCtx)
	clients, err := p.resolveServices(opCtx, cameraURL, seam)
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	encoder := &encoderSession{session: seam.session, seam: seam, cameraURL: cameraURL, profileToken: request.ProfileToken}
	if clients.media2Available {
		encoder.media2, _ = clients.media2.(media2Encoder)
	}
	if clients.media1Available {
		encoder.media1, _ = clients.media1.(media1Encoder)
	}
	if encoder.media2 == nil && encoder.media1 == nil {
		cancel()
		return nil, nil, nil, newError(CategoryProtocol, "the camera advertises no usable media service", nil)
	}
	return encoder, opCtx, cancel, nil
}

// load reads the profile's encoder configuration into the session.
func (e *encoderSession) load(ctx context.Context) (EncoderSettings, error) {
	if e.media2 != nil {
		settings, err := e.loadMedia2(ctx)
		if err == nil || !shouldFallBackToMedia1(err) || e.media1 == nil {
			return settings, err
		}
	}
	return e.loadMedia1(ctx)
}

func (e *encoderSession) loadMedia2(ctx context.Context) (EncoderSettings, error) {
	var profiles []media2.Profile
	err := e.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var callErr error
		profiles, callErr = e.media2.GetProfiles(ctx, e.profileToken, []media2.ConfigurationType{media2.ConfigurationVideoEncoder}, media2Options(header)...)
		return callErr
	})
	if err != nil {
		return EncoderSettings{}, err
	}
	index := slices.IndexFunc(profiles, func(p media2.Profile) bool { return p.Token == e.profileToken })
	if index < 0 || !profiles[index].Associations.HasVideoEncoderConfiguration {
		return EncoderSettings{}, newError(CategoryProtocol, "the profile has no video encoder configuration", nil)
	}
	token := profiles[index].Associations.VideoEncoderConfigurationToken
	var configs []media2.VideoEncoderConfiguration
	err = e.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var callErr error
		configs, callErr = e.media2.GetVideoEncoderConfigurations(ctx, token, "", media2Options(header)...)
		return callErr
	})
	if err != nil {
		return EncoderSettings{}, err
	}
	index = slices.IndexFunc(configs, func(c media2.VideoEncoderConfiguration) bool { return c.Token == token })
	if index < 0 {
		return EncoderSettings{}, newError(CategoryProtocol, "the video encoder configuration was not found", nil)
	}
	e.source, e.media2Config = MediaServiceMedia2, configs[index]
	return media2Settings(configs[index]), nil
}

func (e *encoderSession) loadMedia1(ctx context.Context) (EncoderSettings, error) {
	var profiles []media1.Profile
	err := e.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var callErr error
		profiles, callErr = e.media1.GetProfiles(ctx, media1Options(header)...)
		return callErr
	})
	if err != nil {
		return EncoderSettings{}, err
	}
	index := slices.IndexFunc(profiles, func(p media1.Profile) bool { return p.Token == e.profileToken })
	if index < 0 {
		return EncoderSettings{}, usageError("the media profile was not found")
	}
	e.media1Profile = profiles[index]
	if !profiles[index].Associations.HasVideoEncoderConfiguration {
		return EncoderSettings{}, newError(CategoryProtocol, "the profile has no video encoder configuration", nil)
	}
	token := profiles[index].Associations.VideoEncoderConfigurationToken
	var configs []media1.VideoEncoderConfiguration
	err = e.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var callErr error
		configs, callErr = e.media1.GetVideoEncoderConfigurations(ctx, media1Options(header)...)
		return callErr
	})
	if err != nil {
		return EncoderSettings{}, err
	}
	index = slices.IndexFunc(configs, func(c media1.VideoEncoderConfiguration) bool { return c.Token == token })
	if index < 0 {
		return EncoderSettings{}, newError(CategoryProtocol, "the video encoder configuration was not found", nil)
	}
	e.source, e.media1Config = MediaServiceMedia1, configs[index]
	return media1Settings(configs[index]), nil
}

func (e *encoderSession) options(ctx context.Context) ([]EncoderCodecOptions, error) {
	if e.source == MediaServiceMedia2 {
		var options media2.VideoEncoderConfigurationOptions
		err := e.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
			var callErr error
			options, callErr = e.media2.GetVideoEncoderConfigurationOptions(ctx, e.media2Config.Token, e.profileToken, media2Options(header)...)
			return callErr
		})
		if err != nil {
			return nil, err
		}
		return media2CodecOptions(options), nil
	}
	var options media1.VideoEncoderConfigurationOptions
	err := e.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var callErr error
		options, callErr = e.media1.GetVideoEncoderConfigurationOptions(ctx, e.media1Config.Token, e.profileToken, media1Options(header)...)
		return callErr
	})
	if err != nil {
		return nil, err
	}
	return media1CodecOptions(options), nil
}

func (e *encoderSession) apply(ctx context.Context, change EncoderChange) error {
	if e.source == MediaServiceMedia2 {
		config := e.media2Config
		config.Encoding = change.Encoding
		config.Resolution = media2.VideoResolution{Width: change.Width, Height: change.Height}
		config.FrameRateLimit, config.HasFrameRateLimit = change.FrameRate, true
		config.BitrateLimit, config.HasBitrateLimit = change.BitrateKbps, true
		if change.GOPLength > 0 {
			config.GovLength, config.HasGovLength = change.GOPLength, true
		}
		config.Profile, config.HasProfile = change.CodecProfile, change.CodecProfile != ""
		return e.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
			return e.media2.SetVideoEncoderConfiguration(ctx, config, media2Options(header)...)
		})
	}
	config := e.media1Config
	config.Encoding = media1.VideoEncoding(change.Encoding)
	config.Resolution = media1.VideoResolution{Width: change.Width, Height: change.Height}
	config.FrameRateLimit, config.HasFrameRateLimit = int(math.Round(change.FrameRate)), true
	config.BitrateLimit, config.HasBitrateLimit = change.BitrateKbps, true
	if !config.HasEncodingInterval {
		config.EncodingInterval, config.HasEncodingInterval = 1, true
	}
	config.HasGovLength, config.HasCodecProfile = false, false
	if config.Encoding == media1.VideoEncodingH264 || config.Encoding == media1.VideoEncodingMPEG4 {
		config.GovLength, config.HasGovLength = change.GOPLength, change.GOPLength > 0
		config.CodecProfile, config.HasCodecProfile = change.CodecProfile, change.CodecProfile != ""
	}
	return e.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		return e.media1.SetVideoEncoderConfiguration(ctx, config, true, media1Options(header)...)
	})
}

// ReadEncoder returns the encoder settings and options for a profile.
func (p *productionMedia) ReadEncoder(ctx context.Context, request EncoderRequest) (EncoderView, error) {
	encoder, opCtx, cancel, err := p.openEncoder(ctx, request)
	if err != nil {
		return EncoderView{}, err
	}
	defer cancel()
	current, err := encoder.load(opCtx)
	if err != nil {
		return EncoderView{}, mapConfigError(err)
	}
	codecs, err := encoder.options(opCtx)
	if err != nil {
		return EncoderView{}, mapConfigError(err)
	}
	return EncoderView{ProfileToken: request.ProfileToken, Current: current, Codecs: codecs}, nil
}

// ApplyEncoder validates change against the advertised options, applies it,
// and reads it back.
func (p *productionMedia) ApplyEncoder(ctx context.Context, request EncoderRequest, change EncoderChange) (EncoderApplyResult, error) {
	encoder, opCtx, cancel, err := p.openEncoder(ctx, request)
	if err != nil {
		return EncoderApplyResult{}, err
	}
	defer cancel()
	before, err := encoder.load(opCtx)
	if err != nil {
		return EncoderApplyResult{}, mapConfigError(err)
	}
	codecs, err := encoder.options(opCtx)
	if err != nil {
		return EncoderApplyResult{}, mapConfigError(err)
	}
	if err := validateEncoderChange(change, codecs); err != nil {
		return EncoderApplyResult{}, err
	}
	result := EncoderApplyResult{ProfileToken: request.ProfileToken, Before: before}
	if err := encoder.apply(opCtx, change); err != nil {
		return result, mapSetError(err, "the camera rejected the encoder settings")
	}
	requested := encoderFields(EncoderSettings{
		Encoding: change.Encoding, Width: change.Width, Height: change.Height, FrameRate: change.FrameRate,
		BitrateKbps: change.BitrateKbps, GOPLength: change.GOPLength, CodecProfile: change.CodecProfile,
	}, change)
	var after EncoderSettings
	afterFields, ok := readBack(opCtx, p.readback(), requested, func(ctx context.Context) (fieldValues, error) {
		settings, err := encoder.load(ctx)
		if err != nil {
			return fieldValues{}, err
		}
		after = settings
		return encoderFields(settings, change), nil
	})
	if !ok {
		result.Report = MutationReport{Outcome: OutcomeDisconnected, Changes: []FieldChange{}}
		return result, nil
	}
	result.After = after
	result.Report = compareMutation(encoderFields(before, change), requested, afterFields)
	return result, nil
}

// encoderFields presents settings for comparison; GOP and profile are only
// compared when the change sets them.
func encoderFields(settings EncoderSettings, change EncoderChange) fieldValues {
	fields := newFieldValues()
	fields.set("encoding", settings.Encoding)
	fields.set("resolution", fmt.Sprintf("%dx%d", settings.Width, settings.Height))
	fields.set("frame_rate", strconv.FormatFloat(settings.FrameRate, 'f', -1, 64))
	fields.set("bitrate_kbps", strconv.Itoa(settings.BitrateKbps))
	if change.GOPLength > 0 {
		fields.set("gop_length", strconv.Itoa(settings.GOPLength))
	}
	if change.CodecProfile != "" {
		fields.set("codec_profile", settings.CodecProfile)
	}
	return fields
}

func validateEncoderChange(change EncoderChange, codecs []EncoderCodecOptions) error {
	index := slices.IndexFunc(codecs, func(o EncoderCodecOptions) bool { return o.Encoding == change.Encoding })
	if index < 0 {
		return usageError("the camera does not support the requested codec")
	}
	option := codecs[index]
	if !slices.Contains(option.Resolutions, ResolutionDTO{Width: change.Width, Height: change.Height}) {
		return usageError("the camera does not support the requested resolution")
	}
	if len(option.FrameRates) > 0 {
		if !slices.Contains(option.FrameRates, change.FrameRate) {
			return usageError("the camera does not support the requested frame rate")
		}
	} else if change.FrameRate < option.FrameRateMin || change.FrameRate > option.FrameRateMax || change.FrameRate <= 0 {
		return usageError("the camera does not support the requested frame rate")
	}
	if change.BitrateKbps < option.BitrateMin || change.BitrateKbps > option.BitrateMax || change.BitrateKbps <= 0 {
		return usageError("the requested bitrate is outside the supported range")
	}
	if change.GOPLength > 0 && option.GOPMax > 0 && (change.GOPLength < option.GOPMin || change.GOPLength > option.GOPMax) {
		return usageError("the requested GOP length is outside the supported range")
	}
	if change.CodecProfile != "" && len(option.Profiles) > 0 && !slices.Contains(option.Profiles, change.CodecProfile) {
		return usageError("the camera does not support the requested codec profile")
	}
	return nil
}

func media2Settings(config media2.VideoEncoderConfiguration) EncoderSettings {
	return EncoderSettings{
		ConfigurationToken: config.Token, Source: MediaServiceMedia2, Encoding: config.Encoding,
		Width: config.Resolution.Width, Height: config.Resolution.Height,
		FrameRate: config.FrameRateLimit, BitrateKbps: config.BitrateLimit,
		GOPLength: config.GovLength, CodecProfile: config.Profile,
	}
}

func media1Settings(config media1.VideoEncoderConfiguration) EncoderSettings {
	return EncoderSettings{
		ConfigurationToken: config.Token, Source: MediaServiceMedia1, Encoding: string(config.Encoding),
		Width: config.Resolution.Width, Height: config.Resolution.Height,
		FrameRate: float64(config.FrameRateLimit), BitrateKbps: config.BitrateLimit,
		GOPLength: config.GovLength, CodecProfile: config.CodecProfile,
	}
}

func media2CodecOptions(options media2.VideoEncoderConfigurationOptions) []EncoderCodecOptions {
	out := make([]EncoderCodecOptions, 0, len(options.Options))
	for _, option := range options.Options {
		codec := EncoderCodecOptions{
			Encoding:   option.Encoding,
			FrameRates: append([]float64{}, option.FrameRatesSupported...),
			BitrateMin: option.BitrateRange.Min, BitrateMax: option.BitrateRange.Max,
			Profiles: append([]string{}, option.ProfilesSupported...),
		}
		for _, resolution := range option.ResolutionsAvailable {
			codec.Resolutions = append(codec.Resolutions, ResolutionDTO{Width: resolution.Width, Height: resolution.Height})
		}
		if len(codec.FrameRates) > 0 {
			codec.FrameRateMin, codec.FrameRateMax = slices.Min(codec.FrameRates), slices.Max(codec.FrameRates)
		}
		if option.GovLengthRange != nil {
			codec.GOPMin, codec.GOPMax = option.GovLengthRange.Min, option.GovLengthRange.Max
		}
		out = append(out, codec)
	}
	return out
}

// media1CodecOptions flattens the per-codec Media1 option blocks. Media1 has
// no bitrate range in the base options, so the bitrate bound is permissive and
// the device remains the final authority (its read back is verified).
func media1CodecOptions(options media1.VideoEncoderConfigurationOptions) []EncoderCodecOptions {
	var out []EncoderCodecOptions
	add := func(encoding string, block *media1.CodecOptions) {
		if block == nil {
			return
		}
		codec := EncoderCodecOptions{
			Encoding:     encoding,
			FrameRates:   []float64{},
			FrameRateMin: float64(block.FrameRateRange.Min), FrameRateMax: float64(block.FrameRateRange.Max),
			BitrateMin: 1, BitrateMax: math.MaxInt32,
			Profiles: append([]string{}, block.Profiles...),
		}
		for _, resolution := range block.Resolutions {
			codec.Resolutions = append(codec.Resolutions, ResolutionDTO{Width: resolution.Width, Height: resolution.Height})
		}
		if block.GovLengthRange != nil {
			codec.GOPMin, codec.GOPMax = block.GovLengthRange.Min, block.GovLengthRange.Max
		}
		out = append(out, codec)
	}
	add("H264", options.H264)
	add("MPEG4", options.MPEG4)
	add("JPEG", options.JPEG)
	return out
}

func media2Options(header []byte) []media2.RequestOptions {
	if header == nil {
		return nil
	}
	return []media2.RequestOptions{{HeaderXML: header}}
}

func media1Options(header []byte) []media1.RequestOptions {
	if header == nil {
		return nil
	}
	return []media1.RequestOptions{{HeaderXML: header}}
}

// mapConfigError maps a read failure in a configuration use case.
func mapConfigError(err error) error {
	var appErr *Error
	if errors.As(err, &appErr) {
		return err
	}
	return mapMediaError(err)
}

// mapSetError maps a Set failure. A device that rejects the values answers
// with an InvalidArgVal (ConfigModify) fault, which is reported as a usage
// error with a fixed message.
func mapSetError(err error, rejected string) error {
	switch {
	case errors.Is(err, soap.ErrInvalidArgVal):
		return newError(CategoryUsage, rejected, nil)
	case errors.Is(err, soap.ErrActionNotSupported):
		return newError(CategoryProtocol, "the camera does not support this change", nil)
	default:
		return mapConfigError(err)
	}
}

// readback returns the read-back policy; tests shorten it.
func (p *productionMedia) readback() readbackPolicy {
	if p.readbackPolicy != nil {
		return *p.readbackPolicy
	}
	return defaultReadback
}
