package fakedevice

import (
	"net/http"
	"slices"
	"strconv"
)

const imagingNamespace = "http://www.onvif.org/ver20/imaging/wsdl"

// Imaging ver20 actions modeled by the synthetic device.
const (
	ActionImagingGetImagingSettings = imagingNamespace + "/GetImagingSettings"
	ActionImagingGetOptions         = imagingNamespace + "/GetOptions"
	ActionImagingSetImagingSettings = imagingNamespace + "/SetImagingSettings"
)

// ImagingInventory is the synthetic Imaging service state.
type ImagingInventory struct {
	Sources []ImagingSourceData
	// IgnoreIrCut accepts IR cut changes without applying them, as some
	// firmware does when the mode is locked by a schedule.
	IgnoreIrCut bool
}

// ImagingSourceData is the imaging state of one video source. All values use
// the 0-100 range advertised by GetOptions.
type ImagingSourceData struct {
	VideoSourceToken string
	Brightness       float64
	ColorSaturation  float64
	Contrast         float64
	Sharpness        float64
	IrCutFilter      string
}

func (i ImagingInventory) withDefaults() ImagingInventory {
	if len(i.Sources) == 0 {
		i.Sources = []ImagingSourceData{{
			VideoSourceToken: "vsrc0", Brightness: 50, ColorSaturation: 50, Contrast: 50, Sharpness: 50, IrCutFilter: "AUTO",
		}}
	} else {
		i.Sources = slices.Clone(i.Sources)
	}
	return i
}

func init() {
	extraOperations[imagingNamespace] = map[string]string{
		"GetImagingSettings": ActionImagingGetImagingSettings,
		"GetOptions":         ActionImagingGetOptions,
		"SetImagingSettings": ActionImagingSetImagingSettings,
	}
	extraHandlers[ActionImagingGetImagingSettings] = (*Device).getImagingSettings
	extraHandlers[ActionImagingGetOptions] = (*Device).getImagingOptions
	extraHandlers[ActionImagingSetImagingSettings] = (*Device).setImagingSettings
}

func (d *Device) imagingSource(operation operationWire) (int, bool) {
	token, ok := childText(operation.Elements, imagingNamespace, "VideoSourceToken")
	if !ok {
		return -1, false
	}
	index := slices.IndexFunc(d.cfg().Imaging.Sources, func(s ImagingSourceData) bool { return s.VideoSourceToken == token })
	return index, index >= 0
}

var noSourceFault = FaultSpec{Kind: FaultInvalidArgVal, Reason: "NoSource: the video source does not exist"}

func (d *Device) getImagingSettings(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	index, ok := d.imagingSource(operation)
	if !ok {
		d.writeFault(response, version, noSourceFault)
		return
	}
	source := d.cfg().Imaging.Sources[index]
	d.writeSuccess(response, version, `<timg:GetImagingSettingsResponse xmlns:timg="`+imagingNamespace+`"><timg:ImagingSettings>`+
		`<tt:Brightness>`+ftoa(source.Brightness)+`</tt:Brightness>`+
		`<tt:ColorSaturation>`+ftoa(source.ColorSaturation)+`</tt:ColorSaturation>`+
		`<tt:Contrast>`+ftoa(source.Contrast)+`</tt:Contrast>`+
		`<tt:Exposure><tt:Mode>AUTO</tt:Mode></tt:Exposure>`+
		`<tt:IrCutFilter>`+escapeXML(source.IrCutFilter)+`</tt:IrCutFilter>`+
		`<tt:Sharpness>`+ftoa(source.Sharpness)+`</tt:Sharpness>`+
		`</timg:ImagingSettings></timg:GetImagingSettingsResponse>`)
}

func (d *Device) getImagingOptions(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	if _, ok := d.imagingSource(operation); !ok {
		d.writeFault(response, version, noSourceFault)
		return
	}
	scale := func(local string) string { return floatRangeXML(local, 0, 100) }
	d.writeSuccess(response, version, `<timg:GetOptionsResponse xmlns:timg="`+imagingNamespace+`"><timg:ImagingOptions>`+
		scale("Brightness")+scale("ColorSaturation")+scale("Contrast")+
		`<tt:IrCutFilterModes>ON</tt:IrCutFilterModes><tt:IrCutFilterModes>OFF</tt:IrCutFilterModes><tt:IrCutFilterModes>AUTO</tt:IrCutFilterModes>`+
		scale("Sharpness")+`</timg:ImagingOptions></timg:GetOptionsResponse>`)
}

func (d *Device) setImagingSettings(response http.ResponseWriter, version SOAPVersion, operation operationWire) {
	index, ok := d.imagingSource(operation)
	if !ok {
		d.writeFault(response, version, noSourceFault)
		return
	}
	settings := childElement(operation.Elements, imagingNamespace, "ImagingSettings")
	if settings == nil {
		d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "SettingsInvalid: ImagingSettings is required"})
		return
	}
	source := d.cfg().Imaging.Sources[index]
	values := map[string]*float64{
		"Brightness": &source.Brightness, "ColorSaturation": &source.ColorSaturation,
		"Contrast": &source.Contrast, "Sharpness": &source.Sharpness,
	}
	for local, target := range values {
		text, present := childText(settings.Elements, typeNamespace, local)
		if !present {
			continue
		}
		value, err := strconv.ParseFloat(text, 64)
		if err != nil || value < 0 || value > 100 {
			d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "SettingsInvalid: a value is out of range"})
			return
		}
		*target = value
	}
	if mode, present := childText(settings.Elements, typeNamespace, "IrCutFilter"); present {
		if mode != "ON" && mode != "OFF" && mode != "AUTO" {
			d.writeFault(response, version, FaultSpec{Kind: FaultInvalidArgVal, Reason: "SettingsInvalid: unsupported IR cut filter mode"})
			return
		}
		if !d.cfg().Imaging.IgnoreIrCut {
			source.IrCutFilter = mode
		}
	}
	d.mutate(func(next *normalizedConfig) {
		sources := slices.Clone(next.Imaging.Sources)
		sources[index] = source
		next.Imaging.Sources = sources
	})
	d.writeSuccess(response, version, `<timg:SetImagingSettingsResponse xmlns:timg="`+imagingNamespace+`"/>`)
}
