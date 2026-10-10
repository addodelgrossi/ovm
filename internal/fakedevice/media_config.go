package fakedevice

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	maxMediaEntries  = 256
	maxMediaPathLen  = 1024
	maxMediaNameLen  = 64
	maxMediaTokenLen = 64
)

// validateMediaInventory bounds all data that can be expanded into a synthetic
// response and enforces the required ONVIF value domains. Errors identify only
// the field, never caller-provided values.
func validateMediaInventory(media MediaInventory) error {
	if err := validateMediaXSIntFields(media); err != nil {
		return err
	}
	if err := validateMediaPath("snapshot path", media.SnapshotPath); err != nil {
		return err
	}
	if err := validateMediaPath("stream path", media.StreamPath); err != nil {
		return err
	}
	if err := validateSnapshotQuery(media.SnapshotQuery); err != nil {
		return err
	}
	if len(media.Profiles) > maxMediaEntries || len(media.Media1Profiles) > maxMediaEntries || len(media.VideoSources) > maxMediaEntries ||
		len(media.VideoEncoders) > maxMediaEntries || len(media.AudioEncoders) > maxMediaEntries ||
		len(media.Media2VideoEncoders) > maxMediaEntries || len(media.Media2AudioEncoders) > maxMediaEntries {
		return errors.New("fake-device media inventory exceeds the entry limit")
	}

	profiles := make(map[string]struct{}, len(media.Profiles))
	videoSources := make(map[string]struct{}, len(media.VideoSources))
	videoEncoders := make(map[string]struct{}, len(media.VideoEncoders))
	audioEncoders := make(map[string]struct{}, len(media.AudioEncoders))
	media2VideoEncoders := make(map[string]struct{}, len(media.Media2VideoEncoders))
	media2AudioEncoders := make(map[string]struct{}, len(media.Media2AudioEncoders))

	for _, source := range media.VideoSources {
		if err := validateMediaToken("video source token", source.Token); err != nil {
			return err
		}
		if _, duplicate := videoSources[source.Token]; duplicate {
			return errors.New("fake-device media inventory has a duplicate video source token")
		}
		videoSources[source.Token] = struct{}{}
		if source.Framerate <= 0 || source.Width <= 0 || source.Height <= 0 {
			return errors.New("fake-device video source dimensions and framerate must be positive")
		}
	}

	for _, encoder := range media.VideoEncoders {
		if err := validateMediaToken("video encoder token", encoder.Token); err != nil {
			return err
		}
		if err := validateMediaName("video encoder name", encoder.Name); err != nil {
			return err
		}
		if _, duplicate := videoEncoders[encoder.Token]; duplicate {
			return errors.New("fake-device media inventory has a duplicate video encoder token")
		}
		videoEncoders[encoder.Token] = struct{}{}
		if !validMedia1VideoEncoding(encoder.Encoding) {
			return errors.New("fake-device video encoder encoding is invalid")
		}
		if encoder.Width <= 0 || encoder.Height <= 0 || encoder.Quality < 0 || encoder.FrameRateLimit <= 0 ||
			encoder.BitrateLimit <= 0 || encoder.GovLength <= 0 || encoder.UseCount < 0 {
			return errors.New("fake-device video encoder values are outside supported ranges")
		}
	}

	for _, encoder := range media.AudioEncoders {
		if err := validateMediaToken("audio encoder token", encoder.Token); err != nil {
			return err
		}
		if err := validateMediaName("audio encoder name", encoder.Name); err != nil {
			return err
		}
		if _, duplicate := audioEncoders[encoder.Token]; duplicate {
			return errors.New("fake-device media inventory has a duplicate audio encoder token")
		}
		audioEncoders[encoder.Token] = struct{}{}
		if !validMedia1AudioEncoding(encoder.Encoding) {
			return errors.New("fake-device audio encoder encoding is invalid")
		}
		if encoder.Bitrate <= 0 || encoder.SampleRate <= 0 || encoder.UseCount < 0 {
			return errors.New("fake-device audio encoder values are outside supported ranges")
		}
	}

	for _, encoder := range media.Media2VideoEncoders {
		if err := validateMediaToken("Media2 video encoder token", encoder.Token); err != nil {
			return err
		}
		if err := validateMediaName("Media2 video encoder name", encoder.Name); err != nil {
			return err
		}
		if _, duplicate := media2VideoEncoders[encoder.Token]; duplicate {
			return errors.New("fake-device Media2 inventory has a duplicate video encoder token")
		}
		media2VideoEncoders[encoder.Token] = struct{}{}
		if !validMedia2Encoding(encoder.Encoding) || encoder.Width <= 0 || encoder.Height <= 0 || encoder.Quality < 0 ||
			math.IsNaN(encoder.Quality) || math.IsInf(encoder.Quality, 0) || encoder.UseCount < 0 {
			return errors.New("fake-device Media2 video encoder values are invalid")
		}
		if encoder.FrameRateLimit <= 0 || math.IsNaN(encoder.FrameRateLimit) || math.IsInf(encoder.FrameRateLimit, 0) ||
			encoder.BitrateLimit <= 0 || encoder.AverageBitRate < 0 || encoder.GovLength <= 0 || !validMedia2ConfiguredProfile(encoder.Profile) {
			return errors.New("fake-device Media2 video encoder rate-control values are invalid")
		}
	}

	for _, encoder := range media.Media2AudioEncoders {
		if err := validateMediaToken("Media2 audio encoder token", encoder.Token); err != nil {
			return err
		}
		if err := validateMediaName("Media2 audio encoder name", encoder.Name); err != nil {
			return err
		}
		if _, duplicate := media2AudioEncoders[encoder.Token]; duplicate {
			return errors.New("fake-device Media2 inventory has a duplicate audio encoder token")
		}
		media2AudioEncoders[encoder.Token] = struct{}{}
		if !validMedia2Encoding(encoder.Encoding) || encoder.Bitrate <= 0 || encoder.SampleRate <= 0 || encoder.UseCount < 0 {
			return errors.New("fake-device Media2 audio encoder values are invalid")
		}
	}

	for _, profile := range media.Profiles {
		if err := validateMediaToken("profile token", profile.Token); err != nil {
			return err
		}
		if err := validateMediaName("profile name", profile.Name); err != nil {
			return err
		}
		if _, duplicate := profiles[profile.Token]; duplicate {
			return errors.New("fake-device media inventory has a duplicate profile token")
		}
		profiles[profile.Token] = struct{}{}
		if profile.VideoSourceConfigurationToken != "" {
			if err := validateMediaToken("profile video source configuration token", profile.VideoSourceConfigurationToken); err != nil {
				return err
			}
			if _, ok := videoSources[profile.VideoSourceToken]; !ok {
				return errors.New("fake-device profile references an unknown Media2 video source")
			}
		}
		if profile.Media1VideoSourceConfigurationToken != "" {
			if err := validateMediaToken("profile Media1 video source configuration token", profile.Media1VideoSourceConfigurationToken); err != nil {
				return err
			}
		}
		// Profiles is always the Media2 inventory, so its shared association
		// fields must resolve against Media2 configurations.
		if profile.VideoEncoderToken != "" {
			if _, ok := media2VideoEncoders[profile.VideoEncoderToken]; !ok {
				return errors.New("fake-device profile references an unknown Media2 video encoder")
			}
		}
		if profile.AudioEncoderToken != "" {
			if _, ok := media2AudioEncoders[profile.AudioEncoderToken]; !ok {
				return errors.New("fake-device profile references an unknown Media2 audio encoder")
			}
		}
		// Only validate this entry's Media1 associations when Profiles is also
		// serving Media1. A distinct Media1Profiles inventory is authoritative
		// for every Media1 operation and may use entirely different tokens.
		if len(media.Media1Profiles) == 0 {
			if profile.media1VideoSourceConfigurationToken() != "" {
				if _, ok := videoSources[profile.media1VideoSourceToken()]; !ok {
					return errors.New("fake-device profile references an unknown Media1 video source")
				}
			}
			if token := profile.media1VideoEncoderToken(); token != "" {
				if _, ok := videoEncoders[token]; !ok {
					return errors.New("fake-device profile references an unknown Media1 video encoder")
				}
			}
			if token := profile.media1AudioEncoderToken(); token != "" {
				if _, ok := audioEncoders[token]; !ok {
					return errors.New("fake-device profile references an unknown Media1 audio encoder")
				}
			}
		}
	}

	// Validate the optional distinct Media1 profile list against the Media1
	// configuration sets. These profiles are rendered only by the Media1
	// GetProfiles, so their effective Media1 association tokens (override or
	// shared field) must name known Media1 configurations and sources.
	media1Profiles := make(map[string]struct{}, len(media.Media1Profiles))
	for _, profile := range media.Media1Profiles {
		if err := validateMediaToken("Media1 profile token", profile.Token); err != nil {
			return err
		}
		if err := validateMediaName("Media1 profile name", profile.Name); err != nil {
			return err
		}
		if _, duplicate := media1Profiles[profile.Token]; duplicate {
			return errors.New("fake-device Media1 inventory has a duplicate profile token")
		}
		media1Profiles[profile.Token] = struct{}{}
		if cfg := profile.media1VideoSourceConfigurationToken(); cfg != "" {
			if err := validateMediaToken("Media1 profile video source configuration token", cfg); err != nil {
				return err
			}
			if _, ok := videoSources[profile.media1VideoSourceToken()]; !ok {
				return errors.New("fake-device Media1 profile references an unknown video source")
			}
		}
		if venc := profile.media1VideoEncoderToken(); venc != "" {
			if _, ok := videoEncoders[venc]; !ok {
				return errors.New("fake-device Media1 profile references an unknown Media1 video encoder")
			}
		}
		if aenc := profile.media1AudioEncoderToken(); aenc != "" {
			if _, ok := audioEncoders[aenc]; !ok {
				return errors.New("fake-device Media1 profile references an unknown Media1 audio encoder")
			}
		}
	}

	options := media.VideoEncoderOptions
	if !validMedia1VideoEncoding(options.Encoding) || options.QualityMin < 0 || options.QualityMin > options.QualityMax ||
		options.WidthMin <= 0 || options.WidthMin > options.WidthMax || options.HeightMin <= 0 || options.HeightMin > options.HeightMax ||
		options.FrameRateMin <= 0 || options.FrameRateMin > options.FrameRateMax || options.BitrateMin <= 0 || options.BitrateMin > options.BitrateMax ||
		options.GovLengthMin <= 0 || options.GovLengthMin > options.GovLengthMax {
		return errors.New("fake-device video encoder option ranges are invalid")
	}
	media2Options := media.Media2VideoEncoderOptions
	if !validMedia2Encoding(media2Options.Encoding) || media2Options.QualityMin < 0 || media2Options.QualityMin > media2Options.QualityMax ||
		math.IsNaN(media2Options.QualityMin) || math.IsNaN(media2Options.QualityMax) || math.IsInf(media2Options.QualityMin, 0) || math.IsInf(media2Options.QualityMax, 0) ||
		media2Options.WidthMin <= 0 || media2Options.WidthMin > media2Options.WidthMax || media2Options.HeightMin <= 0 || media2Options.HeightMin > media2Options.HeightMax ||
		media2Options.BitrateMin <= 0 || media2Options.BitrateMin > media2Options.BitrateMax || media2Options.GovLengthMin <= 0 || media2Options.GovLengthMin > media2Options.GovLengthMax {
		return errors.New("fake-device Media2 video encoder option ranges are invalid")
	}
	return nil
}

func validateMediaPath(field, value string) error {
	if value == "" || len(value) > maxMediaPathLen || !utf8.ValidString(value) || !strings.HasPrefix(value, "/") {
		return fmt.Errorf("fake-device media %s is invalid", field)
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f || character == '\\' {
			return fmt.Errorf("fake-device media %s is invalid", field)
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Opaque != "" {
		return fmt.Errorf("fake-device media %s must be a path without query or fragment", field)
	}
	return nil
}

// validateSnapshotQuery bounds the optional synthetic snapshot query component.
// It must be a bare "key=value" fragment: no leading "?", no control
// characters, within a length bound, and parseable by url.ParseQuery. It never
// carries a credential; it is a synthetic marker only. An empty value is valid.
func validateSnapshotQuery(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > maxMediaPathLen || !utf8.ValidString(value) || strings.HasPrefix(value, "?") {
		return errors.New("fake-device media snapshot query is invalid")
	}
	for _, character := range value {
		if character < 0x20 || character == 0x7f {
			return errors.New("fake-device media snapshot query is invalid")
		}
	}
	if _, err := url.ParseQuery(value); err != nil {
		return errors.New("fake-device media snapshot query is invalid")
	}
	return nil
}

func validateMediaToken(field, value string) error {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxMediaTokenLen || !safeMediaText(value) {
		return fmt.Errorf("fake-device %s is invalid", field)
	}
	return nil
}

func validateMediaName(field, value string) error {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxMediaNameLen || !safeMediaText(value) {
		return fmt.Errorf("fake-device %s is invalid", field)
	}
	return nil
}

func safeMediaText(value string) bool {
	for _, character := range value {
		if character < 0x20 || (character >= 0x7f && character <= 0x9f) || character == utf8.RuneError {
			return false
		}
	}
	return true
}

func validMedia1VideoEncoding(value string) bool {
	return value == "JPEG" || value == "MPEG4" || value == "H264"
}

func validMedia1AudioEncoding(value string) bool {
	return value == "G711" || value == "G726" || value == "AAC"
}

// validMedia2ConfiguredProfile reports whether value is a profile from the
// Media2 configuration profile vocabulary emitted as the unqualified Profile
// attribute.
func validMedia2ConfiguredProfile(value string) bool {
	switch value {
	case "Simple", "AdvancedSimple", "Baseline", "Main", "Main10", "Extended", "High":
		return true
	default:
		return false
	}
}

func validMedia2Encoding(value string) bool {
	if value == "" || len(value) > 256 || !utf8.ValidString(value) {
		return false
	}
	return safeMediaText(value)
}
