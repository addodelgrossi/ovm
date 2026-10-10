package fakedevice

import (
	"encoding/xml"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

// Media Set actions modeled by the synthetic device.
const (
	ActionMedia1SetVideoEncoderConfiguration = media1Namespace + "/SetVideoEncoderConfiguration"
	ActionMedia2SetVideoEncoderConfiguration = media2Namespace + "/SetVideoEncoderConfiguration"
)

// EncoderSetBehavior models how cameras react to encoder changes.
type EncoderSetBehavior string

const (
	// EncoderSetApply validates and applies the requested values.
	EncoderSetApply EncoderSetBehavior = ""
	// EncoderSetClamp silently clamps an out-of-range bitrate into range.
	EncoderSetClamp EncoderSetBehavior = "clamp"
	// EncoderSetIgnore accepts the request without changing anything, as some
	// firmware does.
	EncoderSetIgnore EncoderSetBehavior = "ignore"
)

// ResolutionData is one advertised resolution.
type ResolutionData struct {
	Width  int
	Height int
}

// Media2EncoderOptionData is one tt:VideoEncoder2ConfigurationOptions entry.
type Media2EncoderOptionData struct {
	Encoding     string
	Profiles     []string
	FrameRates   []float64
	Resolutions  []ResolutionData
	QualityMin   float64
	QualityMax   float64
	BitrateMin   int
	BitrateMax   int
	GovLengthMin int
	GovLengthMax int
}

func init() {
	media1Operations["SetVideoEncoderConfiguration"] = ActionMedia1SetVideoEncoderConfiguration
	media2Operations["SetVideoEncoderConfiguration"] = ActionMedia2SetVideoEncoderConfiguration
	mutationHandlers[ActionMedia1SetVideoEncoderConfiguration] = (*Device).setMedia1Encoder
	mutationHandlers[ActionMedia2SetVideoEncoderConfiguration] = (*Device).setMedia2Encoder
}

var configModifyFault = FaultSpec{Kind: FaultInvalidArgVal, Reason: "ConfigModify: the configuration values are not supported"}

func childElement(elements []textElement, namespace, local string) *textElement {
	for index := range elements {
		if elements[index].XMLName.Space == namespace && elements[index].XMLName.Local == local {
			return &elements[index]
		}
	}
	return nil
}

func childText(elements []textElement, namespace, local string) (string, bool) {
	child := childElement(elements, namespace, local)
	if child == nil {
		return "", false
	}
	return strings.TrimSpace(child.Value), true
}

func childInt(elements []textElement, namespace, local string) (int, bool) {
	text, ok := childText(elements, namespace, local)
	if !ok {
		return 0, false
	}
	value, err := strconv.Atoi(text)
	return value, err == nil
}

func childFloat(elements []textElement, namespace, local string) (float64, bool) {
	text, ok := childText(elements, namespace, local)
	if !ok {
		return 0, false
	}
	value, err := strconv.ParseFloat(text, 64)
	return value, err == nil
}

func attrValue(attrs []xml.Attr, local string) (string, bool) {
	for _, attr := range attrs {
		if attr.Name.Space == "" && attr.Name.Local == local {
			return attr.Value, true
		}
	}
	return "", false
}

// encoderRequest is the parsed subset of a Set request configuration.
type encoderRequest struct {
	token     string
	encoding  string
	width     int
	height    int
	quality   float64
	frameRate float64
	bitrate   int
	govLength int
	profile   string
	hasRate   bool
}

func parseEncoderRequest(operation operationWire, namespace string) (encoderRequest, bool) {
	configuration := childElement(operation.Elements, namespace, "Configuration")
	if configuration == nil {
		return encoderRequest{}, false
	}
	var request encoderRequest
	var ok bool
	if request.token, ok = attrValue(configuration.Attrs, "token"); !ok || request.token == "" {
		return encoderRequest{}, false
	}
	if request.encoding, ok = childText(configuration.Elements, typeNamespace, "Encoding"); !ok {
		return encoderRequest{}, false
	}
	resolution := childElement(configuration.Elements, typeNamespace, "Resolution")
	if resolution == nil {
		return encoderRequest{}, false
	}
	width, okWidth := childInt(resolution.Elements, typeNamespace, "Width")
	height, okHeight := childInt(resolution.Elements, typeNamespace, "Height")
	quality, okQuality := childFloat(configuration.Elements, typeNamespace, "Quality")
	if !okWidth || !okHeight || !okQuality {
		return encoderRequest{}, false
	}
	request.width, request.height, request.quality = width, height, quality
	if rate := childElement(configuration.Elements, typeNamespace, "RateControl"); rate != nil {
		frameRate, okRate := childFloat(rate.Elements, typeNamespace, "FrameRateLimit")
		bitrate, okBitrate := childInt(rate.Elements, typeNamespace, "BitrateLimit")
		if !okRate || !okBitrate {
			return encoderRequest{}, false
		}
		request.frameRate, request.bitrate, request.hasRate = frameRate, bitrate, true
	}
	if value, present := attrValue(configuration.Attrs, "GovLength"); present {
		request.govLength, _ = strconv.Atoi(value)
	}
	request.profile, _ = attrValue(configuration.Attrs, "Profile")
	if codec := childElement(configuration.Elements, typeNamespace, "H264"); codec != nil {
		request.govLength, _ = childInt(codec.Elements, typeNamespace, "GovLength")
		request.profile, _ = childText(codec.Elements, typeNamespace, "H264Profile")
	}
	return request, true
}

func clampInt(value, minimum, maximum int) int {
	return min(max(value, minimum), maximum)
}

// media2Options returns the advertised option entries as Media2EncoderOptionData.
func media2Options(inventory MediaInventory) []Media2EncoderOptionData {
	if len(inventory.Media2EncoderOptions) > 0 {
		return inventory.Media2EncoderOptions
	}
	legacy := inventory.Media2VideoEncoderOptions
	return []Media2EncoderOptionData{{
		Encoding:    legacy.Encoding,
		Resolutions: []ResolutionData{{legacy.WidthMax, legacy.HeightMax}, {legacy.WidthMin, legacy.HeightMin}},
		QualityMin:  legacy.QualityMin, QualityMax: legacy.QualityMax,
		BitrateMin: legacy.BitrateMin, BitrateMax: legacy.BitrateMax,
		GovLengthMin: legacy.GovLengthMin, GovLengthMax: legacy.GovLengthMax,
	}}
}

func (d *Device) setMedia2Encoder(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	request, ok := parseEncoderRequest(operation, media2Namespace)
	if !ok {
		d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "Invalid encoder configuration"})
		return
	}
	inventory := d.cfg().MediaInventory
	index := slices.IndexFunc(inventory.Media2VideoEncoders, func(e VideoEncoder2ConfigurationData) bool { return e.Token == request.token })
	optionIndex := slices.IndexFunc(media2Options(inventory), func(o Media2EncoderOptionData) bool { return o.Encoding == request.encoding })
	if index < 0 || optionIndex < 0 || !request.hasRate {
		d.writeFault(response, version, configModifyFault)
		return
	}
	option := media2Options(inventory)[optionIndex]
	behavior := inventory.EncoderSetBehavior
	bitrate := request.bitrate
	if bitrate < option.BitrateMin || bitrate > option.BitrateMax {
		if behavior != EncoderSetClamp {
			d.writeFault(response, version, configModifyFault)
			return
		}
		bitrate = clampInt(bitrate, option.BitrateMin, option.BitrateMax)
	}
	if behavior == EncoderSetClamp && inventory.EncoderBitrateCeiling > 0 {
		bitrate = min(bitrate, inventory.EncoderBitrateCeiling)
	}
	validResolution := slices.Contains(option.Resolutions, ResolutionData{request.width, request.height})
	validRate := len(option.FrameRates) == 0 || slices.Contains(option.FrameRates, request.frameRate)
	validGov := request.govLength == 0 || (request.govLength >= option.GovLengthMin && request.govLength <= option.GovLengthMax)
	validProfile := request.profile == "" || len(option.Profiles) == 0 || slices.Contains(option.Profiles, request.profile)
	validQuality := request.quality >= option.QualityMin && request.quality <= option.QualityMax
	if !validResolution || !validRate || !validGov || !validProfile || !validQuality || request.frameRate <= 0 {
		d.writeFault(response, version, configModifyFault)
		return
	}
	if behavior != EncoderSetIgnore {
		d.mutate(func(next *normalizedConfig) {
			encoders := slices.Clone(next.MediaInventory.Media2VideoEncoders)
			encoder := &encoders[index]
			encoder.Encoding, encoder.Width, encoder.Height = request.encoding, request.width, request.height
			encoder.Quality, encoder.FrameRateLimit, encoder.BitrateLimit = request.quality, request.frameRate, bitrate
			if request.govLength > 0 {
				encoder.GovLength = request.govLength
			}
			if request.profile != "" {
				encoder.Profile = request.profile
			}
			next.MediaInventory.Media2VideoEncoders = encoders
		})
	}
	d.writeMediaSuccess(response, version, mediaVer20, `<tr2:SetVideoEncoderConfigurationResponse/>`)
}

func (d *Device) setMedia1Encoder(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	request, ok := parseEncoderRequest(operation, media1Namespace)
	if !ok {
		d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "Invalid encoder configuration"})
		return
	}
	inventory := d.cfg().MediaInventory
	options := inventory.VideoEncoderOptions
	index := slices.IndexFunc(inventory.VideoEncoders, func(e VideoEncoderConfigurationData) bool { return e.Token == request.token })
	if index < 0 || request.encoding != options.Encoding || !request.hasRate {
		d.writeFault(response, version, configModifyFault)
		return
	}
	bitrate := request.bitrate
	if bitrate < options.BitrateMin || bitrate > options.BitrateMax {
		if inventory.EncoderSetBehavior != EncoderSetClamp {
			d.writeFault(response, version, configModifyFault)
			return
		}
		bitrate = clampInt(bitrate, options.BitrateMin, options.BitrateMax)
	}
	frameRate := int(request.frameRate)
	if request.width < options.WidthMin || request.width > options.WidthMax || request.height < options.HeightMin || request.height > options.HeightMax ||
		frameRate < options.FrameRateMin || frameRate > options.FrameRateMax ||
		(request.govLength != 0 && (request.govLength < options.GovLengthMin || request.govLength > options.GovLengthMax)) {
		d.writeFault(response, version, configModifyFault)
		return
	}
	if inventory.EncoderSetBehavior != EncoderSetIgnore {
		d.mutate(func(next *normalizedConfig) {
			encoders := slices.Clone(next.MediaInventory.VideoEncoders)
			encoder := &encoders[index]
			encoder.Encoding, encoder.Width, encoder.Height = request.encoding, request.width, request.height
			encoder.Quality, encoder.FrameRateLimit, encoder.BitrateLimit = int(request.quality), frameRate, bitrate
			if request.govLength > 0 {
				encoder.GovLength = request.govLength
			}
			next.MediaInventory.VideoEncoders = encoders
		})
	}
	d.writeMediaSuccess(response, version, mediaVer10, `<trt:SetVideoEncoderConfigurationResponse/>`)
}
