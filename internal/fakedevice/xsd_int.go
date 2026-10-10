package fakedevice

import "errors"

const (
	minXSInt = -1 << 31
	maxXSInt = 1<<31 - 1
)

// fitsXSInt reports whether value is in the fixed signed 32-bit value space of
// XML Schema xs:int. Go int may be wider on the host running the fake device.
func fitsXSInt(value int) bool {
	return value >= minXSInt && value <= maxXSInt
}

func allFitXSInt(values ...int) bool {
	for _, value := range values {
		if !fitsXSInt(value) {
			return false
		}
	}
	return true
}

// validateInventoryXSIntFields validates configurable Device fields that are
// emitted as xs:int values. Service versions use the full signed range; MTU is
// optional and nonnegative, and IPv4 prefix length has its protocol range.
func validateInventoryXSIntFields(inventory Inventory) error {
	for _, service := range inventory.Services {
		if !allFitXSInt(service.VersionMajor, service.VersionMinor) {
			return errors.New("fake-device service version is outside xs:int")
		}
	}
	for _, networkInterface := range inventory.NetworkInterfaces {
		if networkInterface.MTU < 0 || !fitsXSInt(networkInterface.MTU) {
			return errors.New("fake-device network interface MTU is outside its supported xs:int range")
		}
		if networkInterface.IPv4Prefix < 0 || networkInterface.IPv4Prefix > 32 {
			return errors.New("fake-device network interface IPv4 prefix is outside its supported range")
		}
	}
	return nil
}

// validateMediaXSIntFields validates every configurable Media integer that is
// serialized into an xs:int element, attribute, or list member. Float-valued
// wire fields backed by Go ints (Media1 Quality and source Framerate) are
// intentionally excluded; semantic positivity and ordering remain enforced by
// validateMediaInventory.
func validateMediaXSIntFields(media MediaInventory) error {
	for _, source := range media.VideoSources {
		if !allFitXSInt(source.Width, source.Height) {
			return errors.New("fake-device video source dimensions are outside xs:int")
		}
	}
	for _, encoder := range media.VideoEncoders {
		if !allFitXSInt(encoder.Width, encoder.Height, encoder.FrameRateLimit, encoder.BitrateLimit, encoder.GovLength, encoder.UseCount) {
			return errors.New("fake-device Media1 video encoder integers are outside xs:int")
		}
	}
	for _, encoder := range media.AudioEncoders {
		if !allFitXSInt(encoder.Bitrate, encoder.SampleRate, encoder.UseCount) {
			return errors.New("fake-device Media1 audio encoder integers are outside xs:int")
		}
	}
	for _, encoder := range media.Media2VideoEncoders {
		if !allFitXSInt(encoder.Width, encoder.Height, encoder.BitrateLimit, encoder.AverageBitRate, encoder.GovLength, encoder.UseCount) {
			return errors.New("fake-device Media2 video encoder integers are outside xs:int")
		}
	}
	for _, encoder := range media.Media2AudioEncoders {
		if !allFitXSInt(encoder.Bitrate, encoder.SampleRate, encoder.UseCount) {
			return errors.New("fake-device Media2 audio encoder integers are outside xs:int")
		}
	}

	options := media.VideoEncoderOptions
	if !allFitXSInt(
		options.QualityMin, options.QualityMax,
		options.WidthMin, options.WidthMax,
		options.HeightMin, options.HeightMax,
		options.FrameRateMin, options.FrameRateMax,
		options.BitrateMin, options.BitrateMax,
		options.GovLengthMin, options.GovLengthMax,
	) {
		return errors.New("fake-device Media1 video encoder option integers are outside xs:int")
	}
	media2Options := media.Media2VideoEncoderOptions
	if !allFitXSInt(
		media2Options.WidthMin, media2Options.WidthMax,
		media2Options.HeightMin, media2Options.HeightMax,
		media2Options.BitrateMin, media2Options.BitrateMax,
		media2Options.GovLengthMin, media2Options.GovLengthMax,
	) {
		return errors.New("fake-device Media2 video encoder option integers are outside xs:int")
	}
	return nil
}
