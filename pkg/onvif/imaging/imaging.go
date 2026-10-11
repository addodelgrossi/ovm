// Package imaging implements a typed client for a bounded subset of the ONVIF
// Imaging (ver20) service: GetImagingSettings, GetOptions, and
// SetImagingSettings, limited to brightness, color saturation, contrast,
// sharpness, and the IR cut filter.
//
// Responses are decoded namespace-strictly: elements match by namespace URI
// and local name, so prefixes are not significant and foreign namespaces are
// ignored rather than misread. Values are validated (finite numbers, the
// IrCutFilterMode enumeration, Min <= Max) and errors never echo response or
// request values. SetImagingSettings sends only the modeled fields, which the
// Imaging specification treats as a partial update; unmodeled settings such as
// exposure and focus are left untouched on the device.
//
// Normative sources:
//   - ONVIF Imaging WSDL (ver20): https://www.onvif.org/ver20/imaging/wsdl/imaging.wsdl
//   - ONVIF schema: https://www.onvif.org/ver10/schema/onvif.xsd
//   - ONVIF Imaging Service Specification:
//     https://www.onvif.org/specs/srv/img/ONVIF-Imaging-Service-Spec.pdf
//
// This package does not import Wails, Cobra, persistence, or UI packages.
package imaging

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

const (
	// Namespace is the ONVIF Imaging (ver20) WSDL target namespace.
	Namespace = "http://www.onvif.org/ver20/imaging/wsdl"
	// SchemaNamespace is the shared ONVIF schema namespace.
	SchemaNamespace = "http://www.onvif.org/ver10/schema"

	ActionGetImagingSettings = Namespace + "/GetImagingSettings"
	ActionGetOptions         = Namespace + "/GetOptions"
	ActionSetImagingSettings = Namespace + "/SetImagingSettings"

	maxTokenLength = 64
)

var (
	// ErrInvalidInput reports a caller value that cannot be sent safely.
	ErrInvalidInput = errors.New("imaging: invalid request input")
	// ErrInvalidResponse reports a response that is malformed or out of range.
	ErrInvalidResponse = errors.New("imaging: invalid response")
	// ErrNilResponse reports a transport that returned neither a response
	// nor an error.
	ErrNilResponse = errors.New("imaging: transport returned no response")
)

// IrCutFilterMode is tt:IrCutFilterMode.
type IrCutFilterMode string

const (
	// IrCutOn keeps the IR cut filter in place: day (color) mode.
	IrCutOn IrCutFilterMode = "ON"
	// IrCutOff removes the filter: night (monochrome, IR-sensitive) mode.
	IrCutOff IrCutFilterMode = "OFF"
	// IrCutAuto lets the camera switch by light level.
	IrCutAuto IrCutFilterMode = "AUTO"
)

// Settings is the modeled subset of tt:ImagingSettings20. A nil field is
// absent on the wire.
type Settings struct {
	Brightness      *float64
	ColorSaturation *float64
	Contrast        *float64
	Sharpness       *float64
	IrCutFilter     *IrCutFilterMode
}

// FloatRange is tt:FloatRange.
type FloatRange struct {
	Min float64
	Max float64
}

// Options is the modeled subset of tt:ImagingOptions20.
type Options struct {
	Brightness       *FloatRange
	ColorSaturation  *FloatRange
	Contrast         *FloatRange
	Sharpness        *FloatRange
	IrCutFilterModes []IrCutFilterMode
}

// Transport is the SOAP seam, satisfied by *soap.Client.
type Transport interface {
	Call(ctx context.Context, endpoint string, request soap.Request) (*soap.Response, error)
}

// RequestOptions carries an optional SOAP header such as WS-Security.
type RequestOptions struct {
	HeaderXML []byte
}

// Client issues Imaging operations. It is safe for concurrent use when its
// Transport is.
type Client struct {
	transport Transport
	endpoint  string
}

// NewClient binds a client to an Imaging service endpoint.
func NewClient(transport Transport, endpoint string) (*Client, error) {
	if transport == nil {
		return nil, errors.New("imaging: transport is required")
	}
	if endpoint == "" {
		return nil, errors.New("imaging: endpoint is required")
	}
	return &Client{transport: transport, endpoint: endpoint}, nil
}

func (c *Client) call(ctx context.Context, action string, body string, opts []RequestOptions) ([]byte, error) {
	request := soap.Request{Action: action, BodyXML: []byte(body)}
	for _, opt := range opts {
		if len(opt.HeaderXML) > 0 {
			request.HeaderXML = opt.HeaderXML
		}
	}
	response, err := c.transport.Call(ctx, c.endpoint, request)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, ErrNilResponse
	}
	return response.BodyXML, nil
}

// GetImagingSettings reads the settings of a video source.
func (c *Client) GetImagingSettings(ctx context.Context, videoSourceToken string, opts ...RequestOptions) (Settings, error) {
	if err := validateToken(videoSourceToken); err != nil {
		return Settings{}, err
	}
	body, err := c.call(ctx, ActionGetImagingSettings,
		`<timg:GetImagingSettings xmlns:timg="`+Namespace+`"><timg:VideoSourceToken>`+escape(videoSourceToken)+
			`</timg:VideoSourceToken></timg:GetImagingSettings>`, opts)
	if err != nil {
		return Settings{}, err
	}
	var response struct {
		XMLName  xml.Name      `xml:"http://www.onvif.org/ver20/imaging/wsdl GetImagingSettingsResponse"`
		Settings *settingsWire `xml:"http://www.onvif.org/ver20/imaging/wsdl ImagingSettings"`
	}
	if err := xml.Unmarshal(body, &response); err != nil || response.Settings == nil {
		return Settings{}, fmt.Errorf("%w: GetImagingSettingsResponse", ErrInvalidResponse)
	}
	return response.Settings.decode()
}

// GetOptions reads the valid ranges and modes of a video source.
func (c *Client) GetOptions(ctx context.Context, videoSourceToken string, opts ...RequestOptions) (Options, error) {
	if err := validateToken(videoSourceToken); err != nil {
		return Options{}, err
	}
	body, err := c.call(ctx, ActionGetOptions,
		`<timg:GetOptions xmlns:timg="`+Namespace+`"><timg:VideoSourceToken>`+escape(videoSourceToken)+
			`</timg:VideoSourceToken></timg:GetOptions>`, opts)
	if err != nil {
		return Options{}, err
	}
	var response struct {
		XMLName xml.Name     `xml:"http://www.onvif.org/ver20/imaging/wsdl GetOptionsResponse"`
		Options *optionsWire `xml:"http://www.onvif.org/ver20/imaging/wsdl ImagingOptions"`
	}
	if err := xml.Unmarshal(body, &response); err != nil || response.Options == nil {
		return Options{}, fmt.Errorf("%w: GetOptionsResponse", ErrInvalidResponse)
	}
	return response.Options.decode()
}

// SetImagingSettings sends the non-nil fields of settings.
func (c *Client) SetImagingSettings(ctx context.Context, videoSourceToken string, settings Settings, forcePersistence bool, opts ...RequestOptions) error {
	if err := validateToken(videoSourceToken); err != nil {
		return err
	}
	inner, err := encodeSettings(settings)
	if err != nil {
		return err
	}
	body, err := c.call(ctx, ActionSetImagingSettings,
		`<timg:SetImagingSettings xmlns:timg="`+Namespace+`" xmlns:tt="`+SchemaNamespace+`"><timg:VideoSourceToken>`+
			escape(videoSourceToken)+`</timg:VideoSourceToken><timg:ImagingSettings>`+inner+
			`</timg:ImagingSettings><timg:ForcePersistence>`+strconv.FormatBool(forcePersistence)+
			`</timg:ForcePersistence></timg:SetImagingSettings>`, opts)
	if err != nil {
		return err
	}
	var response struct {
		XMLName xml.Name `xml:"http://www.onvif.org/ver20/imaging/wsdl SetImagingSettingsResponse"`
		Inner   []byte   `xml:",innerxml"`
	}
	if err := xml.Unmarshal(body, &response); err != nil || strings.TrimSpace(string(response.Inner)) != "" {
		return fmt.Errorf("%w: SetImagingSettingsResponse", ErrInvalidResponse)
	}
	return nil
}

type settingsWire struct {
	Brightness      *string `xml:"http://www.onvif.org/ver10/schema Brightness"`
	ColorSaturation *string `xml:"http://www.onvif.org/ver10/schema ColorSaturation"`
	Contrast        *string `xml:"http://www.onvif.org/ver10/schema Contrast"`
	IrCutFilter     *string `xml:"http://www.onvif.org/ver10/schema IrCutFilter"`
	Sharpness       *string `xml:"http://www.onvif.org/ver10/schema Sharpness"`
}

func (w settingsWire) decode() (Settings, error) {
	var settings Settings
	var err error
	if settings.Brightness, err = optionalFloat(w.Brightness, "Brightness"); err != nil {
		return Settings{}, err
	}
	if settings.ColorSaturation, err = optionalFloat(w.ColorSaturation, "ColorSaturation"); err != nil {
		return Settings{}, err
	}
	if settings.Contrast, err = optionalFloat(w.Contrast, "Contrast"); err != nil {
		return Settings{}, err
	}
	if settings.Sharpness, err = optionalFloat(w.Sharpness, "Sharpness"); err != nil {
		return Settings{}, err
	}
	if w.IrCutFilter != nil {
		mode := IrCutFilterMode(strings.TrimSpace(*w.IrCutFilter))
		if !validMode(mode) {
			return Settings{}, fmt.Errorf("%w: IrCutFilter", ErrInvalidResponse)
		}
		settings.IrCutFilter = &mode
	}
	return settings, nil
}

type rangeWire struct {
	Min string `xml:"http://www.onvif.org/ver10/schema Min"`
	Max string `xml:"http://www.onvif.org/ver10/schema Max"`
}

type optionsWire struct {
	Brightness       *rangeWire `xml:"http://www.onvif.org/ver10/schema Brightness"`
	ColorSaturation  *rangeWire `xml:"http://www.onvif.org/ver10/schema ColorSaturation"`
	Contrast         *rangeWire `xml:"http://www.onvif.org/ver10/schema Contrast"`
	IrCutFilterModes []string   `xml:"http://www.onvif.org/ver10/schema IrCutFilterModes"`
	Sharpness        *rangeWire `xml:"http://www.onvif.org/ver10/schema Sharpness"`
}

func (w optionsWire) decode() (Options, error) {
	var options Options
	var err error
	if options.Brightness, err = optionalRange(w.Brightness, "Brightness"); err != nil {
		return Options{}, err
	}
	if options.ColorSaturation, err = optionalRange(w.ColorSaturation, "ColorSaturation"); err != nil {
		return Options{}, err
	}
	if options.Contrast, err = optionalRange(w.Contrast, "Contrast"); err != nil {
		return Options{}, err
	}
	if options.Sharpness, err = optionalRange(w.Sharpness, "Sharpness"); err != nil {
		return Options{}, err
	}
	for _, value := range w.IrCutFilterModes {
		mode := IrCutFilterMode(strings.TrimSpace(value))
		if !validMode(mode) {
			return Options{}, fmt.Errorf("%w: IrCutFilterModes", ErrInvalidResponse)
		}
		options.IrCutFilterModes = append(options.IrCutFilterModes, mode)
	}
	return options, nil
}

func optionalFloat(value *string, field string) (*float64, error) {
	if value == nil {
		return nil, nil
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(*value), 64)
	if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidResponse, field)
	}
	return &parsed, nil
}

func optionalRange(value *rangeWire, field string) (*FloatRange, error) {
	if value == nil {
		return nil, nil
	}
	minimum, minErr := optionalFloat(&value.Min, field+" Min")
	maximum, maxErr := optionalFloat(&value.Max, field+" Max")
	if minErr != nil || maxErr != nil || *minimum > *maximum {
		return nil, fmt.Errorf("%w: %s", ErrInvalidResponse, field)
	}
	return &FloatRange{Min: *minimum, Max: *maximum}, nil
}

// encodeSettings writes the non-nil fields in tt:ImagingSettings20 order.
func encodeSettings(settings Settings) (string, error) {
	var builder strings.Builder
	write := func(local string, value *float64) error {
		if value == nil {
			return nil
		}
		if math.IsNaN(*value) || math.IsInf(*value, 0) {
			return fmt.Errorf("%w: %s", ErrInvalidInput, local)
		}
		builder.WriteString(`<tt:` + local + `>` + strconv.FormatFloat(*value, 'f', -1, 64) + `</tt:` + local + `>`)
		return nil
	}
	if err := write("Brightness", settings.Brightness); err != nil {
		return "", err
	}
	if err := write("ColorSaturation", settings.ColorSaturation); err != nil {
		return "", err
	}
	if err := write("Contrast", settings.Contrast); err != nil {
		return "", err
	}
	if settings.IrCutFilter != nil {
		if !validMode(*settings.IrCutFilter) {
			return "", fmt.Errorf("%w: IrCutFilter", ErrInvalidInput)
		}
		builder.WriteString(`<tt:IrCutFilter>` + string(*settings.IrCutFilter) + `</tt:IrCutFilter>`)
	}
	if err := write("Sharpness", settings.Sharpness); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func validMode(mode IrCutFilterMode) bool {
	return mode == IrCutOn || mode == IrCutOff || mode == IrCutAuto
}

func validateToken(token string) error {
	if token == "" || !utf8.ValidString(token) || utf8.RuneCountInString(token) > maxTokenLength {
		return fmt.Errorf("%w: VideoSourceToken", ErrInvalidInput)
	}
	for _, r := range token {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return fmt.Errorf("%w: VideoSourceToken", ErrInvalidInput)
		}
	}
	return nil
}

func escape(value string) string {
	var builder strings.Builder
	_ = xml.EscapeText(&builder, []byte(value))
	return builder.String()
}
