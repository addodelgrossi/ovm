package media

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

// decodeProfiles normalizes a GetProfilesResponse against the ver10 Media WSDL.
// Each Profile carries a required token attribute and a required Name; the
// complete element is preserved as bounded, redacted XML. An empty profile list
// is valid (minOccurs="0").
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
			return nil, fmt.Errorf("%w: Profile token attribute", ErrMissingField)
		}
		if err := validateResponseToken(token, "Profile token attribute"); err != nil {
			return nil, err
		}
		name, err := requiredString(entry, SchemaNamespace, "Name")
		if err != nil {
			return nil, err
		}
		if err := validateResponseName(name, "Profile Name"); err != nil {
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
				return nil, fmt.Errorf("%w: Profile fixed", ErrInvalidField)
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

// decodeMediaURI normalizes the required tt:MediaUri fields. The URI scheme is
// constrained by the operation, while any userinfo/query remains part of the
// operational result and is never included in an error.
func decodeMediaURI(root *element, responseLocal string) (MediaURI, error) {
	if err := requireRoot(root, responseLocal); err != nil {
		return MediaURI{}, err
	}
	if err := validateWrapper(root, rule(Namespace, "MediaUri", 1, 1)); err != nil {
		return MediaURI{}, err
	}
	mediaURI, err := requiredElement(root, Namespace, "MediaUri")
	if err != nil {
		return MediaURI{}, err
	}
	if err := validateMediaURISequence(mediaURI); err != nil {
		return MediaURI{}, err
	}
	uri, err := requiredText(mediaURI, SchemaNamespace, "Uri")
	if err != nil {
		return MediaURI{}, err
	}
	if err := validateOperationURI(uri, responseLocal); err != nil {
		return MediaURI{}, err
	}
	invalidAfterConnectText, err := requiredText(mediaURI, SchemaNamespace, "InvalidAfterConnect")
	if err != nil {
		return MediaURI{}, err
	}
	invalidAfterConnect, err := parseBool(invalidAfterConnectText)
	if err != nil {
		return MediaURI{}, fmt.Errorf("%w: InvalidAfterConnect", ErrInvalidField)
	}
	invalidAfterRebootText, err := requiredText(mediaURI, SchemaNamespace, "InvalidAfterReboot")
	if err != nil {
		return MediaURI{}, err
	}
	invalidAfterReboot, err := parseBool(invalidAfterRebootText)
	if err != nil {
		return MediaURI{}, fmt.Errorf("%w: InvalidAfterReboot", ErrInvalidField)
	}
	timeout, err := requiredText(mediaURI, SchemaNamespace, "Timeout")
	if err != nil {
		return MediaURI{}, err
	}
	if err := validateXSDDuration(timeout); err != nil {
		return MediaURI{}, err
	}
	return MediaURI{
		URI:                    uri,
		InvalidAfterConnect:    invalidAfterConnect,
		HasInvalidAfterConnect: true,
		InvalidAfterReboot:     invalidAfterReboot,
		HasInvalidAfterReboot:  true,
		Timeout:                timeout,
	}, nil
}

// decodeVideoSources normalizes a GetVideoSourcesResponse (ver10 only). Each
// VideoSource carries a required token attribute; Framerate and Resolution are
// modeled when present. The complete element is preserved as bounded, redacted
// XML.
func decodeVideoSources(root *element, raw []byte) ([]VideoSource, error) {
	if err := requireRoot(root, "GetVideoSourcesResponse"); err != nil {
		return nil, err
	}
	if err := validateWrapper(root, rule(Namespace, "VideoSources", 0, unbounded)); err != nil {
		return nil, err
	}
	entries := root.childrenNamed(Namespace, "VideoSources")
	sources := make([]VideoSource, 0, len(entries))
	for _, entry := range entries {
		if err := validateVideoSourceSequence(entry); err != nil {
			return nil, err
		}
		token, ok := entry.unqualifiedAttr("token")
		if !ok {
			return nil, fmt.Errorf("%w: VideoSource token attribute", ErrMissingField)
		}
		if err := validateResponseToken(token, "VideoSource token attribute"); err != nil {
			return nil, err
		}
		framerate, err := parseRequiredFiniteFloat(entry, SchemaNamespace, "Framerate")
		if err != nil {
			return nil, err
		}
		if framerate <= 0 {
			return nil, fmt.Errorf("%w: VideoSource Framerate", ErrInvalidField)
		}
		resolution, hasResolution, err := decodeResolution(entry)
		if err != nil {
			return nil, err
		}
		sources = append(sources, VideoSource{
			Token:         token,
			Framerate:     framerate,
			HasFramerate:  true,
			Resolution:    resolution,
			HasResolution: hasResolution,
			RawXML:        preservedDiagnosticXML(entry, raw),
		})
	}
	return sources, nil
}

// decodeVideoEncoderConfigurations normalizes a
// GetVideoEncoderConfigurationsResponse (ver10).
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
		if err := validateRequiredStreamingFields(entry); err != nil {
			return nil, err
		}
		token, name, useCount, err := decodeConfigurationEntity(entry)
		if err != nil {
			return nil, err
		}
		config := VideoEncoderConfiguration{
			Token:    token,
			Name:     name,
			UseCount: useCount,
			RawXML:   preservedDiagnosticXML(entry, raw),
		}
		encodingText, err := requiredString(entry, SchemaNamespace, "Encoding")
		if err != nil {
			return nil, err
		}
		encoding, valid := validVideoEncoding(encodingText)
		if !valid {
			return nil, fmt.Errorf("%w: VideoEncoderConfiguration Encoding", ErrInvalidField)
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
		if err := decodeConfiguredCodec(entry, &config); err != nil {
			return nil, err
		}
		configs = append(configs, config)
	}
	return configs, nil
}

// decodeVideoEncoderConfigurationOptions validates the required quality range
// and every present Media1 codec option block while retaining redacted XML for
// extension fields.
func decodeVideoEncoderConfigurationOptions(root *element, raw []byte) (VideoEncoderConfigurationOptions, error) {
	if err := requireRoot(root, "GetVideoEncoderConfigurationOptionsResponse"); err != nil {
		return VideoEncoderConfigurationOptions{}, err
	}
	if err := validateWrapper(root, rule(Namespace, "Options", 1, 1)); err != nil {
		return VideoEncoderConfigurationOptions{}, err
	}
	options, err := requiredElement(root, Namespace, "Options")
	if err != nil {
		return VideoEncoderConfigurationOptions{}, err
	}
	if err := validateOptionsSequence(options); err != nil {
		return VideoEncoderConfigurationOptions{}, err
	}
	qualityRange, err := decodeIntRange(options, "QualityRange")
	if err != nil {
		return VideoEncoderConfigurationOptions{}, err
	}
	if qualityRange.Min < 0 {
		return VideoEncoderConfigurationOptions{}, fmt.Errorf("%w: QualityRange", ErrInvalidField)
	}
	result := VideoEncoderConfigurationOptions{
		QualityRange: qualityRange,
		RawXML:       preservedDiagnosticXML(options, raw),
	}
	for _, codec := range []struct {
		local  string
		target **CodecOptions
	}{{"JPEG", &result.JPEG}, {"MPEG4", &result.MPEG4}, {"H264", &result.H264}} {
		node, err := optionalElement(options, SchemaNamespace, codec.local)
		if err != nil {
			return VideoEncoderConfigurationOptions{}, err
		}
		if node == nil {
			continue
		}
		decoded, err := decodeCodecOptions(node, codec.local)
		if err != nil {
			return VideoEncoderConfigurationOptions{}, err
		}
		*codec.target = &decoded
	}
	return result, nil
}

func decodeCodecOptions(node *element, codec string) (CodecOptions, error) {
	if err := validateCodecOptionsSequence(node, codec); err != nil {
		return CodecOptions{}, err
	}
	resolutionElements := node.childrenNamed(SchemaNamespace, "ResolutionsAvailable")
	if len(resolutionElements) == 0 {
		return CodecOptions{}, fmt.Errorf("%w: ResolutionsAvailable", ErrMissingField)
	}
	result := CodecOptions{Resolutions: make([]VideoResolution, 0, len(resolutionElements))}
	for _, resolutionElement := range resolutionElements {
		resolution, err := decodeResolutionElement(resolutionElement)
		if err != nil {
			return CodecOptions{}, err
		}
		result.Resolutions = append(result.Resolutions, resolution)
	}
	var err error
	if codec != "JPEG" {
		govLengthRange, rangeErr := decodeIntRange(node, "GovLengthRange")
		if rangeErr != nil {
			return CodecOptions{}, rangeErr
		}
		result.GovLengthRange = &govLengthRange
	}
	result.FrameRateRange, err = decodeIntRange(node, "FrameRateRange")
	if err != nil {
		return CodecOptions{}, err
	}
	result.EncodingIntervalRange, err = decodeIntRange(node, "EncodingIntervalRange")
	if err != nil {
		return CodecOptions{}, err
	}
	if result.FrameRateRange.Min <= 0 || result.EncodingIntervalRange.Min <= 0 ||
		(result.GovLengthRange != nil && result.GovLengthRange.Min <= 0) {
		return CodecOptions{}, fmt.Errorf("%w: codec option range", ErrInvalidField)
	}
	if codec == "MPEG4" || codec == "H264" {
		profileLocal := "Mpeg4ProfilesSupported"
		if codec == "H264" {
			profileLocal = "H264ProfilesSupported"
		}
		profiles := node.childrenNamed(SchemaNamespace, profileLocal)
		if len(profiles) == 0 {
			return CodecOptions{}, fmt.Errorf("%w: %s", ErrMissingField, profileLocal)
		}
		for _, profile := range profiles {
			if err := validateSimpleContent(profile, profileLocal); err != nil {
				return CodecOptions{}, err
			}
			value := string(profile.text)
			if !validCodecProfile(codec, value) {
				return CodecOptions{}, fmt.Errorf("%w: %s", ErrInvalidField, profileLocal)
			}
			result.Profiles = append(result.Profiles, value)
		}
	}
	return result, nil
}

func validCodecProfile(codec, profile string) bool {
	if codec == "MPEG4" {
		return profile == "SP" || profile == "ASP"
	}
	switch profile {
	case "Baseline", "Main", "Extended", "High":
		return true
	default:
		return false
	}
}

// decodeAudioEncoderConfigurations normalizes a
// GetAudioEncoderConfigurationsResponse (ver10).
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
		if err := validateRequiredStreamingFields(entry); err != nil {
			return nil, err
		}
		token, name, useCount, err := decodeConfigurationEntity(entry)
		if err != nil {
			return nil, err
		}
		config := AudioEncoderConfiguration{
			Token:    token,
			Name:     name,
			UseCount: useCount,
			RawXML:   preservedDiagnosticXML(entry, raw),
		}
		encodingText, err := requiredString(entry, SchemaNamespace, "Encoding")
		if err != nil {
			return nil, err
		}
		encoding, valid := validAudioEncoding(encodingText)
		if !valid {
			return nil, fmt.Errorf("%w: AudioEncoderConfiguration Encoding", ErrInvalidField)
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

// decodeConfigurationEntity extracts the tt:ConfigurationEntity base fields
// shared by video and audio encoder configurations: the required token
// attribute, the required Name, and the required UseCount.
func decodeConfigurationEntity(entry *element) (token, name string, useCount int, err error) {
	tokenValue, ok := entry.unqualifiedAttr("token")
	if !ok {
		return "", "", 0, fmt.Errorf("%w: Configuration token attribute", ErrMissingField)
	}
	if err := validateResponseToken(tokenValue, "Configuration token attribute"); err != nil {
		return "", "", 0, err
	}
	nameValue, err := requiredString(entry, SchemaNamespace, "Name")
	if err != nil {
		return "", "", 0, err
	}
	if err := validateResponseName(nameValue, "Configuration Name"); err != nil {
		return "", "", 0, err
	}
	useCountValue, err := parseRequiredInt(entry, SchemaNamespace, "UseCount")
	if err != nil {
		return "", "", 0, err
	}
	if useCountValue < 0 {
		return "", "", 0, fmt.Errorf("%w: UseCount", ErrInvalidField)
	}
	return tokenValue, nameValue, useCountValue, nil
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

func optionalElement(parent *element, namespace, local string) (*element, error) {
	if parent.hasDuplicateChild(namespace, local) {
		return nil, fmt.Errorf("%w: duplicate %s", ErrInvalidField, local)
	}
	return parent.child(namespace, local), nil
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
