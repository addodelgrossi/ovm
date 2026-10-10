package app

import (
	"context"
	"slices"
	"strconv"

	"github.com/addodelgrossi/ovm/pkg/onvif/imaging"
	"github.com/addodelgrossi/ovm/pkg/onvif/media2"
)

const imagingWSDLNamespace = "http://www.onvif.org/ver20/imaging/wsdl"

// ImagingRange is an inclusive numeric range advertised by the camera.
type ImagingRange struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// ImagingSettingsDTO is the modeled image state. A nil value is not reported
// by the camera.
type ImagingSettingsDTO struct {
	Brightness      *float64 `json:"brightness,omitempty"`
	ColorSaturation *float64 `json:"color_saturation,omitempty"`
	Contrast        *float64 `json:"contrast,omitempty"`
	Sharpness       *float64 `json:"sharpness,omitempty"`
	// IrCutFilter is "ON" (day), "OFF" (night), "AUTO", or empty.
	IrCutFilter string `json:"ir_cut_filter,omitempty"`
}

// ImagingOptionsDTO is what the camera accepts. A nil range means the
// setting cannot be changed.
type ImagingOptionsDTO struct {
	Brightness      *ImagingRange `json:"brightness,omitempty"`
	ColorSaturation *ImagingRange `json:"color_saturation,omitempty"`
	Contrast        *ImagingRange `json:"contrast,omitempty"`
	Sharpness       *ImagingRange `json:"sharpness,omitempty"`
	IrCutModes      []string      `json:"ir_cut_modes"`
}

// ImagingView is the image state of the profile's video source.
type ImagingView struct {
	VideoSourceToken string             `json:"video_source_token"`
	Settings         ImagingSettingsDTO `json:"settings"`
	Options          ImagingOptionsDTO  `json:"options"`
}

// ImagingApplyResult is the verified result of an image change.
type ImagingApplyResult struct {
	Before ImagingSettingsDTO `json:"before"`
	After  ImagingSettingsDTO `json:"after"`
	Report MutationReport     `json:"report"`
}

// ImagingUseCase reads and changes image settings, including IR cut.
type ImagingUseCase interface {
	ReadImaging(ctx context.Context, request EncoderRequest) (ImagingView, error)
	ApplyImaging(ctx context.Context, request EncoderRequest, change ImagingSettingsDTO) (ImagingApplyResult, error)
}

// NewProductionImaging returns the production ImagingUseCase.
func NewProductionImaging() ImagingUseCase { return &productionMedia{} }

type imagingClient interface {
	GetImagingSettings(ctx context.Context, videoSourceToken string, opts ...imaging.RequestOptions) (imaging.Settings, error)
	GetOptions(ctx context.Context, videoSourceToken string, opts ...imaging.RequestOptions) (imaging.Options, error)
	SetImagingSettings(ctx context.Context, videoSourceToken string, settings imaging.Settings, forcePersistence bool, opts ...imaging.RequestOptions) error
}

var _ imagingClient = (*imaging.Client)(nil)

type imagingSession struct {
	session *session
	client  imagingClient
	source  string
}

// openImaging resolves the profile's physical video source and the Imaging
// service endpoint, which must be same-origin with the camera.
func (p *productionMedia) openImaging(ctx context.Context, request EncoderRequest) (*imagingSession, context.Context, context.CancelFunc, error) {
	encoder, opCtx, cancel, err := p.openEncoder(ctx, request)
	if err != nil {
		return nil, nil, nil, err
	}
	fail := func(err error) (*imagingSession, context.Context, context.CancelFunc, error) {
		cancel()
		return nil, nil, nil, err
	}
	source, err := encoder.videoSourceToken(opCtx)
	if err != nil {
		return fail(mapConfigError(err))
	}
	services, err := encoder.seam.Services(opCtx)
	if err != nil {
		return fail(mapMediaError(err))
	}
	var endpoints []string
	for _, service := range services {
		if service.Namespace == imagingWSDLNamespace {
			endpoints = append(endpoints, service.OperationalXAddr())
		}
	}
	if len(endpoints) == 0 {
		return fail(newError(CategoryProtocol, "the camera does not offer the Imaging service", nil))
	}
	if len(endpoints) > 1 {
		return fail(securityError("the camera advertises duplicate imaging services"))
	}
	endpoint, err := validateSameOriginEndpoint(encoder.cameraURL, endpoints[0])
	if err != nil {
		return fail(securityError("the imaging service endpoint is not same-origin with the camera"))
	}
	client, err := imaging.NewClient(encoder.session.soapClient, endpoint.String())
	if err != nil {
		return fail(securityError("the imaging client could not be constructed for the advertised endpoint"))
	}
	return &imagingSession{session: encoder.session, client: client, source: source}, opCtx, cancel, nil
}

// videoSourceToken resolves the physical video source of the profile.
func (e *encoderSession) videoSourceToken(ctx context.Context) (string, error) {
	if e.media2 != nil {
		var profiles []media2.Profile
		err := e.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
			var callErr error
			profiles, callErr = e.media2.GetProfiles(ctx, e.profileToken, []media2.ConfigurationType{media2.ConfigurationVideoSource}, media2Options(header)...)
			return callErr
		})
		if err == nil {
			index := slices.IndexFunc(profiles, func(p media2.Profile) bool { return p.Token == e.profileToken })
			if index >= 0 && profiles[index].Associations.HasVideoSource {
				return profiles[index].Associations.VideoSourceToken, nil
			}
			return "", newError(CategoryProtocol, "the profile has no video source", nil)
		}
		if !shouldFallBackToMedia1(err) || e.media1 == nil {
			return "", err
		}
	}
	if _, err := e.loadMedia1(ctx); err != nil {
		return "", err
	}
	if !e.media1Profile.Associations.HasVideoSource {
		return "", newError(CategoryProtocol, "the profile has no video source", nil)
	}
	return e.media1Profile.Associations.VideoSourceToken, nil
}

func (s *imagingSession) read(ctx context.Context) (ImagingSettingsDTO, error) {
	var settings imaging.Settings
	err := s.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var callErr error
		settings, callErr = s.client.GetImagingSettings(ctx, s.source, imagingOptions(header)...)
		return callErr
	})
	if err != nil {
		return ImagingSettingsDTO{}, err
	}
	dto := ImagingSettingsDTO{
		Brightness: settings.Brightness, ColorSaturation: settings.ColorSaturation,
		Contrast: settings.Contrast, Sharpness: settings.Sharpness,
	}
	if settings.IrCutFilter != nil {
		dto.IrCutFilter = string(*settings.IrCutFilter)
	}
	return dto, nil
}

func (s *imagingSession) options(ctx context.Context) (ImagingOptionsDTO, error) {
	var options imaging.Options
	err := s.session.invokeWithAuth(ctx, func(ctx context.Context, header []byte) error {
		var callErr error
		options, callErr = s.client.GetOptions(ctx, s.source, imagingOptions(header)...)
		return callErr
	})
	if err != nil {
		return ImagingOptionsDTO{}, err
	}
	convert := func(r *imaging.FloatRange) *ImagingRange {
		if r == nil {
			return nil
		}
		return &ImagingRange{Min: r.Min, Max: r.Max}
	}
	dto := ImagingOptionsDTO{
		Brightness: convert(options.Brightness), ColorSaturation: convert(options.ColorSaturation),
		Contrast: convert(options.Contrast), Sharpness: convert(options.Sharpness),
		IrCutModes: []string{},
	}
	for _, mode := range options.IrCutFilterModes {
		dto.IrCutModes = append(dto.IrCutModes, string(mode))
	}
	return dto, nil
}

// ReadImaging returns the image settings and options of the profile's source.
func (p *productionMedia) ReadImaging(ctx context.Context, request EncoderRequest) (ImagingView, error) {
	session, opCtx, cancel, err := p.openImaging(ctx, request)
	if err != nil {
		return ImagingView{}, err
	}
	defer cancel()
	settings, err := session.read(opCtx)
	if err != nil {
		return ImagingView{}, mapConfigError(err)
	}
	options, err := session.options(opCtx)
	if err != nil {
		return ImagingView{}, mapConfigError(err)
	}
	return ImagingView{VideoSourceToken: session.source, Settings: settings, Options: options}, nil
}

// ApplyImaging validates change against the options, applies its non-nil
// fields persistently, and reads them back.
func (p *productionMedia) ApplyImaging(ctx context.Context, request EncoderRequest, change ImagingSettingsDTO) (ImagingApplyResult, error) {
	session, opCtx, cancel, err := p.openImaging(ctx, request)
	if err != nil {
		return ImagingApplyResult{}, err
	}
	defer cancel()
	before, err := session.read(opCtx)
	if err != nil {
		return ImagingApplyResult{}, mapConfigError(err)
	}
	options, err := session.options(opCtx)
	if err != nil {
		return ImagingApplyResult{}, mapConfigError(err)
	}
	settings, err := validateImagingChange(change, options)
	if err != nil {
		return ImagingApplyResult{}, err
	}
	result := ImagingApplyResult{Before: before}
	err = session.session.invokeWithAuth(opCtx, func(ctx context.Context, header []byte) error {
		return session.client.SetImagingSettings(ctx, session.source, settings, true, imagingOptions(header)...)
	})
	if err != nil {
		return result, mapSetError(err, "the camera rejected the image settings")
	}
	requested := imagingFields(change, change)
	var after ImagingSettingsDTO
	afterFields, ok := readBack(opCtx, p.readback(), requested, func(ctx context.Context) (fieldValues, error) {
		current, err := session.read(ctx)
		if err != nil {
			return fieldValues{}, err
		}
		after = current
		return imagingFields(current, change), nil
	})
	if !ok {
		result.Report = MutationReport{Outcome: OutcomeDisconnected, Changes: []FieldChange{}}
		return result, nil
	}
	result.After = after
	result.Report = compareMutation(imagingFields(before, change), requested, afterFields)
	return result, nil
}

func validateImagingChange(change ImagingSettingsDTO, options ImagingOptionsDTO) (imaging.Settings, error) {
	var settings imaging.Settings
	check := func(value *float64, allowed *ImagingRange, name string) (*float64, error) {
		if value == nil {
			return nil, nil
		}
		if allowed == nil || *value < allowed.Min || *value > allowed.Max {
			return nil, usageError("the requested %s is outside the supported range", name)
		}
		copied := *value
		return &copied, nil
	}
	var err error
	if settings.Brightness, err = check(change.Brightness, options.Brightness, "brightness"); err != nil {
		return imaging.Settings{}, err
	}
	if settings.ColorSaturation, err = check(change.ColorSaturation, options.ColorSaturation, "color saturation"); err != nil {
		return imaging.Settings{}, err
	}
	if settings.Contrast, err = check(change.Contrast, options.Contrast, "contrast"); err != nil {
		return imaging.Settings{}, err
	}
	if settings.Sharpness, err = check(change.Sharpness, options.Sharpness, "sharpness"); err != nil {
		return imaging.Settings{}, err
	}
	if change.IrCutFilter != "" {
		if !slices.Contains(options.IrCutModes, change.IrCutFilter) {
			return imaging.Settings{}, usageError("the camera does not support the requested IR cut filter mode")
		}
		mode := imaging.IrCutFilterMode(change.IrCutFilter)
		settings.IrCutFilter = &mode
	}
	if settings == (imaging.Settings{}) {
		return imaging.Settings{}, usageError("no image setting was requested")
	}
	return settings, nil
}

// imagingFields presents the fields the change sets.
func imagingFields(settings ImagingSettingsDTO, change ImagingSettingsDTO) fieldValues {
	fields := newFieldValues()
	number := func(value *float64) string {
		if value == nil {
			return ""
		}
		return strconv.FormatFloat(*value, 'f', -1, 64)
	}
	if change.IrCutFilter != "" {
		fields.set("ir_cut_filter", settings.IrCutFilter)
	}
	if change.Brightness != nil {
		fields.set("brightness", number(settings.Brightness))
	}
	if change.ColorSaturation != nil {
		fields.set("color_saturation", number(settings.ColorSaturation))
	}
	if change.Contrast != nil {
		fields.set("contrast", number(settings.Contrast))
	}
	if change.Sharpness != nil {
		fields.set("sharpness", number(settings.Sharpness))
	}
	return fields
}

func imagingOptions(header []byte) []imaging.RequestOptions {
	if header == nil {
		return nil
	}
	return []imaging.RequestOptions{{HeaderXML: header}}
}
