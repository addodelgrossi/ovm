package media2

import (
	"fmt"
	"strings"

	"github.com/addodelgrossi/ovm/pkg/onvif/soap"
)

// preservedDiagnosticXML redacts a complete element before applying the final
// retention bound. The order is security-sensitive: truncating first could cut
// inside a sensitive opening tag and prevent the lexical redactor from seeing
// its closing delimiter. Copied from the hardened pkg/onvif/device decoder.
func preservedDiagnosticXML(element *element, payload []byte) []byte {
	redacted := soap.RedactXML(element.outerXML(payload))
	if len(redacted) > maxPreservedExtensionBytes {
		redacted = redacted[:maxPreservedExtensionBytes]
	}
	return append([]byte(nil), redacted...)
}

// decodeProfiles normalizes a GetProfilesResponse against the ver20 Media2
// WSDL. Each MediaProfile carries a required token attribute and a required
// Name; both the wrapper Profiles element and the Name element are in the
// ver20 (tr2) namespace because the Media2 schema declares them with
// elementFormDefault="qualified". The complete element is preserved as bounded,
// redacted XML. An empty profile list is valid (minOccurs="0").
func decodeProfiles(root *element, raw []byte) ([]Profile, error) {
	if err := requireRoot(root, "GetProfilesResponse"); err != nil {
		return nil, err
	}
	if err := validateWrapper(root, rule(Namespace, "Profiles", 0, unbounded)); err != nil {
		return nil, err
	}
	entries := root.childrenNamed(Namespace, "Profiles")
	profiles := make([]Profile, 0, len(entries))
	for _, entry := range entries {
		if err := validateProfileSequence(entry); err != nil {
			return nil, err
		}
		token, ok := entry.unqualifiedAttr("token")
		if !ok {
			return nil, fmt.Errorf("%w: MediaProfile token attribute", ErrMissingField)
		}
		if err := validateResponseToken(token, "MediaProfile token attribute"); err != nil {
			return nil, err
		}
		name, err := requiredString(entry, Namespace, "Name")
		if err != nil {
			return nil, err
		}
		if err := validateResponseName(name, "MediaProfile Name"); err != nil {
			return nil, err
		}
		profile := Profile{
			Token:  token,
			Name:   name,
			RawXML: preservedDiagnosticXML(entry, raw),
		}
		if fixed, ok := entry.unqualifiedAttr("fixed"); ok {
			parsed, parseErr := parseBool(fixed)
			if parseErr != nil {
				return nil, fmt.Errorf("%w: MediaProfile fixed", ErrInvalidField)
			}
			profile.Fixed = parsed
			profile.HasFixed = true
		}
		associations, err := decodeProfileAssociations(entry)
		if err != nil {
			return nil, err
		}
		profile.Associations = associations
		profiles = append(profiles, profile)
	}
	return profiles, nil
}

// decodeURIResponse normalizes a GetStreamUriResponse or GetSnapshotUriResponse.
// The ver20 responses carry a bare Uri string child (in the tr2 namespace)
// rather than the ver10 tt:MediaUri structure.
func decodeURIResponse(root *element, responseLocal string) (string, error) {
	if err := requireRoot(root, responseLocal); err != nil {
		return "", err
	}
	if err := validateWrapper(root, rule(Namespace, "Uri", 1, 1)); err != nil {
		return "", err
	}
	uri, err := requiredText(root, Namespace, "Uri")
	if err != nil {
		return "", err
	}
	if err := validateOperationURI(uri, responseLocal); err != nil {
		return "", err
	}
	return uri, nil
}

// decodeVideoEncoderConfigurations normalizes a
// GetVideoEncoderConfigurationsResponse (ver20). The Configurations wrapper
// elements are in the tr2 namespace; their contents are
// tt:VideoEncoder2Configuration fields in the tt namespace.
func decodeVideoEncoderConfigurations(root *element, raw []byte) ([]VideoEncoderConfiguration, error) {
	if err := requireRoot(root, "GetVideoEncoderConfigurationsResponse"); err != nil {
		return nil, err
	}
	if err := validateWrapper(root, rule(Namespace, "Configurations", 0, unbounded)); err != nil {
		return nil, err
	}
	entries := root.childrenNamed(Namespace, "Configurations")
	configs := make([]VideoEncoderConfiguration, 0, len(entries))
	for _, entry := range entries {
		if err := validateVideoEncoderSequence(entry); err != nil {
			return nil, err
		}
		token, name, useCount, hasUseCount, err := decodeConfigurationEntity(entry)
		if err != nil {
			return nil, err
		}
		config := VideoEncoderConfiguration{
			Token:       token,
			Name:        name,
			UseCount:    useCount,
			HasUseCount: hasUseCount,
			RawXML:      preservedDiagnosticXML(entry, raw),
		}
		encoding, err := requiredString(entry, SchemaNamespace, "Encoding")
		if err != nil {
			return nil, err
		}
		if err := validateEncoding(encoding, "VideoEncoderConfiguration Encoding"); err != nil {
			return nil, err
		}
		config.Encoding = encoding
		resolution, hasResolution, err := decodeResolution(entry)
		if err != nil {
			return nil, err
		}
		config.Resolution = resolution
		config.HasResolution = hasResolution
		quality, err := parseRequiredFiniteFloat(entry, SchemaNamespace, "Quality")
		if err != nil {
			return nil, err
		}
		if quality < 0 {
			return nil, fmt.Errorf("%w: VideoEncoderConfiguration Quality", ErrInvalidField)
		}
		config.Quality = quality
		config.HasQuality = true
		if err := decodeConfiguredRateControl(entry, &config); err != nil {
			return nil, err
		}
		if err := decodeConfiguredAttributes(entry, &config); err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}
	return configs, nil
}

// decodeVideoEncoderConfigurationOptions validates every required field of
// each tt:VideoEncoder2ConfigurationOptions entry and preserves its extensions.
func decodeVideoEncoderConfigurationOptions(root *element, raw []byte) (VideoEncoderConfigurationOptions, error) {
	if err := requireRoot(root, "GetVideoEncoderConfigurationOptionsResponse"); err != nil {
		return VideoEncoderConfigurationOptions{}, err
	}
	if err := validateWrapper(root, rule(Namespace, "Options", 1, unbounded)); err != nil {
		return VideoEncoderConfigurationOptions{}, err
	}
	entries := root.childrenNamed(Namespace, "Options")
	if len(entries) == 0 {
		return VideoEncoderConfigurationOptions{}, fmt.Errorf("%w: Options", ErrMissingField)
	}
	result := VideoEncoderConfigurationOptions{
		Options:    make([]VideoEncoderOption, 0, len(entries)),
		OptionsXML: make([][]byte, 0, len(entries)),
	}
	for _, entry := range entries {
		if err := validateOptionsSequence(entry); err != nil {
			return VideoEncoderConfigurationOptions{}, err
		}
		encoding, err := requiredString(entry, SchemaNamespace, "Encoding")
		if err != nil {
			return VideoEncoderConfigurationOptions{}, err
		}
		if err := validateEncoding(encoding, "Options Encoding"); err != nil {
			return VideoEncoderConfigurationOptions{}, err
		}
		qualityRange, err := decodeFloatRange(entry, "QualityRange")
		if err != nil {
			return VideoEncoderConfigurationOptions{}, err
		}
		if qualityRange.Min < 0 {
			return VideoEncoderConfigurationOptions{}, fmt.Errorf("%w: QualityRange", ErrInvalidField)
		}
		resolutionElements := entry.childrenNamed(SchemaNamespace, "ResolutionsAvailable")
		if len(resolutionElements) == 0 {
			return VideoEncoderConfigurationOptions{}, fmt.Errorf("%w: ResolutionsAvailable", ErrMissingField)
		}
		resolutions := make([]VideoResolution, 0, len(resolutionElements))
		for _, resolutionElement := range resolutionElements {
			resolution, err := decodeResolutionElement(resolutionElement)
			if err != nil {
				return VideoEncoderConfigurationOptions{}, err
			}
			resolutions = append(resolutions, resolution)
		}
		bitrateRange, err := decodeIntRange(entry, "BitrateRange")
		if err != nil {
			return VideoEncoderConfigurationOptions{}, err
		}
		if bitrateRange.Min <= 0 {
			return VideoEncoderConfigurationOptions{}, fmt.Errorf("%w: BitrateRange", ErrInvalidField)
		}
		govLengthRange, err := decodeGovLengthRange(entry)
		if err != nil {
			return VideoEncoderConfigurationOptions{}, err
		}
		if govLengthRange != nil && govLengthRange.Min <= 0 {
			return VideoEncoderConfigurationOptions{}, fmt.Errorf("%w: GovLengthRange", ErrInvalidField)
		}
		preserved := preservedDiagnosticXML(entry, raw)
		result.Options = append(result.Options, VideoEncoderOption{
			Encoding:             encoding,
			QualityRange:         qualityRange,
			ResolutionsAvailable: resolutions,
			BitrateRange:         bitrateRange,
			GovLengthRange:       govLengthRange,
			RawXML:               preserved,
		})
		result.OptionsXML = append(result.OptionsXML, append([]byte(nil), preserved...))
	}
	return result, nil
}

// decodeAudioEncoderConfigurations normalizes a
// GetAudioEncoderConfigurationsResponse (ver20).
func decodeAudioEncoderConfigurations(root *element, raw []byte) ([]AudioEncoderConfiguration, error) {
	if err := requireRoot(root, "GetAudioEncoderConfigurationsResponse"); err != nil {
		return nil, err
	}
	if err := validateWrapper(root, rule(Namespace, "Configurations", 0, unbounded)); err != nil {
		return nil, err
	}
	entries := root.childrenNamed(Namespace, "Configurations")
	configs := make([]AudioEncoderConfiguration, 0, len(entries))
	for _, entry := range entries {
		if err := validateAudioEncoderSequence(entry); err != nil {
			return nil, err
		}
		token, name, useCount, hasUseCount, err := decodeConfigurationEntity(entry)
		if err != nil {
			return nil, err
		}
		config := AudioEncoderConfiguration{
			Token:       token,
			Name:        name,
			UseCount:    useCount,
			HasUseCount: hasUseCount,
			RawXML:      preservedDiagnosticXML(entry, raw),
		}
		encoding, err := requiredString(entry, SchemaNamespace, "Encoding")
		if err != nil {
			return nil, err
		}
		if err := validateEncoding(encoding, "AudioEncoderConfiguration Encoding"); err != nil {
			return nil, err
		}
		config.Encoding = encoding
		bitrate, err := parseRequiredInt(entry, SchemaNamespace, "Bitrate")
		if err != nil {
			return nil, err
		}
		if bitrate <= 0 {
			return nil, fmt.Errorf("%w: AudioEncoderConfiguration Bitrate", ErrInvalidField)
		}
		config.Bitrate = bitrate
		config.HasBitrate = true
		sampleRate, err := parseRequiredInt(entry, SchemaNamespace, "SampleRate")
		if err != nil {
			return nil, err
		}
		if sampleRate <= 0 {
			return nil, fmt.Errorf("%w: AudioEncoderConfiguration SampleRate", ErrInvalidField)
		}
		config.SampleRate = sampleRate
		config.HasSampleRate = true
		configs = append(configs, config)
	}
	return configs, nil
}

// decodeConfigurationEntity extracts the required ConfigurationEntity fields.
// UseCount is deprecated for Media2 but remains required by the inherited XSD
// type, so a missing value is malformed rather than silently synthesized.
func decodeConfigurationEntity(entry *element) (token, name string, useCount int, hasUseCount bool, err error) {
	tokenValue, ok := entry.unqualifiedAttr("token")
	if !ok {
		return "", "", 0, false, fmt.Errorf("%w: Configuration token attribute", ErrMissingField)
	}
	if err := validateResponseToken(tokenValue, "Configuration token attribute"); err != nil {
		return "", "", 0, false, err
	}
	nameValue, err := requiredString(entry, SchemaNamespace, "Name")
	if err != nil {
		return "", "", 0, false, err
	}
	if err := validateResponseName(nameValue, "Configuration Name"); err != nil {
		return "", "", 0, false, err
	}
	useCountValue, err := parseRequiredInt(entry, SchemaNamespace, "UseCount")
	if err != nil {
		return "", "", 0, false, err
	}
	if useCountValue < 0 {
		return "", "", 0, false, fmt.Errorf("%w: UseCount", ErrInvalidField)
	}
	return tokenValue, nameValue, useCountValue, true, nil
}

// decodeResolution decodes the required tt:Resolution child.
func decodeResolution(parent *element) (VideoResolution, bool, error) {
	node, err := requiredElement(parent, SchemaNamespace, "Resolution")
	if err != nil {
		return VideoResolution{}, false, err
	}
	resolution, err := decodeResolutionElement(node)
	if err != nil {
		return VideoResolution{}, false, err
	}
	return resolution, true, nil
}

func decodeResolutionElement(node *element) (VideoResolution, error) {
	if err := validateResolutionSequence(node); err != nil {
		return VideoResolution{}, err
	}
	width, err := parseRequiredInt(node, SchemaNamespace, "Width")
	if err != nil {
		return VideoResolution{}, err
	}
	height, err := parseRequiredInt(node, SchemaNamespace, "Height")
	if err != nil {
		return VideoResolution{}, err
	}
	if width <= 0 || height <= 0 {
		return VideoResolution{}, fmt.Errorf("%w: Resolution", ErrInvalidField)
	}
	return VideoResolution{Width: width, Height: height}, nil
}

func requiredElement(parent *element, namespace, local string) (*element, error) {
	if parent.hasDuplicateChild(namespace, local) {
		return nil, fmt.Errorf("%w: duplicate %s", ErrInvalidField, local)
	}
	child := parent.child(namespace, local)
	if child == nil {
		return nil, fmt.Errorf("%w: %s", ErrMissingField, local)
	}
	return child, nil
}

func requiredText(parent *element, namespace, local string) (string, error) {
	child, err := requiredElement(parent, namespace, local)
	if err != nil {
		return "", err
	}
	if err := validateSimpleContent(child, local); err != nil {
		return "", err
	}
	return child.trimmedText(), nil
}

func requiredString(parent *element, namespace, local string) (string, error) {
	child, err := requiredElement(parent, namespace, local)
	if err != nil {
		return "", err
	}
	if err := validateSimpleContent(child, local); err != nil {
		return "", err
	}
	return string(child.text), nil
}

// parseBool parses the XML schema boolean lexical space (true/false/1/0).
func parseBool(value string) (bool, error) {
	switch strings.TrimSpace(value) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean")
	}
}
