package media

import "fmt"

// decodeProfileAssociations extracts the optional typed configuration
// references embedded in a tt:Profile: the video-source configuration (and its
// physical SourceToken), the video-encoder configuration, and the
// audio-encoder configuration. Each member is optional per the Media WSDL, so
// an absent member leaves its presence flag false and its token empty. A
// present VideoSourceConfiguration is fully validated before either
// association is published: its inherited ConfigurationEntity fields, source
// token, Bounds rectangle, and child order must all conform to the shared
// ONVIF XSD. Only namespace-resolved expected elements and attributes are read,
// so a lookalike local name in a foreign namespace never satisfies an
// association. The caller has already validated the profile child sequence;
// this function enforces the nested configuration rules and simple-content
// safety.
func decodeProfileAssociations(profile *element) (ProfileAssociations, error) {
	var associations ProfileAssociations

	videoSourceConfig, err := optionalElement(profile, SchemaNamespace, "VideoSourceConfiguration")
	if err != nil {
		return ProfileAssociations{}, err
	}
	if videoSourceConfig != nil {
		if err := validateVideoSourceConfigurationSequence(videoSourceConfig); err != nil {
			return ProfileAssociations{}, err
		}
		token, _, _, err := decodeConfigurationEntity(videoSourceConfig)
		if err != nil {
			return ProfileAssociations{}, err
		}
		sourceToken, err := requiredString(videoSourceConfig, SchemaNamespace, "SourceToken")
		if err != nil {
			return ProfileAssociations{}, err
		}
		if err := validateResponseToken(sourceToken, "VideoSourceConfiguration SourceToken"); err != nil {
			return ProfileAssociations{}, err
		}
		bounds, err := requiredElement(videoSourceConfig, SchemaNamespace, "Bounds")
		if err != nil {
			return ProfileAssociations{}, err
		}
		if err := validateIntRectangle(bounds); err != nil {
			return ProfileAssociations{}, err
		}

		// Publish association tokens only after the complete
		// VideoSourceConfiguration has passed validation.
		associations.VideoSourceConfigurationToken = token
		associations.HasVideoSourceConfiguration = true
		associations.VideoSourceToken = sourceToken
		associations.HasVideoSource = true
	}

	videoEncoderConfig, err := optionalElement(profile, SchemaNamespace, "VideoEncoderConfiguration")
	if err != nil {
		return ProfileAssociations{}, err
	}
	if videoEncoderConfig != nil {
		token, err := requiredConfigurationToken(videoEncoderConfig, "VideoEncoderConfiguration token attribute")
		if err != nil {
			return ProfileAssociations{}, err
		}
		associations.VideoEncoderConfigurationToken = token
		associations.HasVideoEncoderConfiguration = true
	}

	audioEncoderConfig, err := optionalElement(profile, SchemaNamespace, "AudioEncoderConfiguration")
	if err != nil {
		return ProfileAssociations{}, err
	}
	if audioEncoderConfig != nil {
		token, err := requiredConfigurationToken(audioEncoderConfig, "AudioEncoderConfiguration token attribute")
		if err != nil {
			return ProfileAssociations{}, err
		}
		associations.AudioEncoderConfigurationToken = token
		associations.HasAudioEncoderConfiguration = true
	}

	return associations, nil
}

// requiredConfigurationToken reads and validates the unqualified token
// attribute of a configuration member. A missing attribute is a missing field;
// a present attribute must satisfy the response-token invariants. The field
// name alone identifies failures so no attacker-controlled value is echoed.
func requiredConfigurationToken(configuration *element, field string) (string, error) {
	token, ok := configuration.unqualifiedAttr("token")
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrMissingField, field)
	}
	if err := validateResponseToken(token, field); err != nil {
		return "", err
	}
	return token, nil
}

// validateIntRectangle validates the complete tt:IntRectangle value used by
// VideoSourceConfiguration Bounds. The four attributes are required,
// unqualified xs:int values; child content and undeclared attributes are not
// permitted by the shared ONVIF XSD.
func validateIntRectangle(bounds *element) error {
	if err := validateChildSequence(bounds, nil, false); err != nil {
		return fmt.Errorf("%w: Bounds content", ErrInvalidField)
	}
	required := []string{"x", "y", "width", "height"}
	seen := make(map[string]bool, len(required))
	for _, attribute := range bounds.attrs {
		if attribute.Name.Space == "xmlns" || (attribute.Name.Space == "" && attribute.Name.Local == "xmlns") {
			continue
		}
		if attribute.Name.Space != "" {
			return fmt.Errorf("%w: Bounds attributes", ErrInvalidField)
		}
		known := false
		for _, field := range required {
			if attribute.Name.Local == field {
				known = true
				break
			}
		}
		if !known || seen[attribute.Name.Local] {
			return fmt.Errorf("%w: Bounds attributes", ErrInvalidField)
		}
		if _, err := parseAttrInt(attribute.Value); err != nil {
			return fmt.Errorf("%w: Bounds %s attribute", ErrInvalidField, attribute.Name.Local)
		}
		seen[attribute.Name.Local] = true
	}
	for _, field := range required {
		if !seen[field] {
			return fmt.Errorf("%w: Bounds %s attribute", ErrMissingField, field)
		}
	}
	return nil
}

// decodeConfiguredRateControl decodes the optional tt:RateControl block of a
// VideoEncoderConfiguration into the configured FrameRateLimit,
// EncodingInterval, and BitrateLimit fields. The block is optional
// (minOccurs="0"); when present every member is required by the
// tt:VideoRateControl type and must be a positive integer. A nonpositive,
// missing, or malformed member is rejected. Errors identify the field only.
func decodeConfiguredRateControl(entry *element, config *VideoEncoderConfiguration) error {
	rateControl, err := optionalElement(entry, SchemaNamespace, "RateControl")
	if err != nil {
		return err
	}
	if rateControl == nil {
		return nil
	}
	if err := validateRateControlSequence(rateControl); err != nil {
		return err
	}
	frameRateLimit, err := parseRequiredInt(rateControl, SchemaNamespace, "FrameRateLimit")
	if err != nil {
		return err
	}
	if frameRateLimit <= 0 {
		return fmt.Errorf("%w: RateControl FrameRateLimit", ErrInvalidField)
	}
	encodingInterval, err := parseRequiredInt(rateControl, SchemaNamespace, "EncodingInterval")
	if err != nil {
		return err
	}
	if encodingInterval <= 0 {
		return fmt.Errorf("%w: RateControl EncodingInterval", ErrInvalidField)
	}
	bitrateLimit, err := parseRequiredInt(rateControl, SchemaNamespace, "BitrateLimit")
	if err != nil {
		return err
	}
	if bitrateLimit <= 0 {
		return fmt.Errorf("%w: RateControl BitrateLimit", ErrInvalidField)
	}
	config.FrameRateLimit = frameRateLimit
	config.HasFrameRateLimit = true
	config.EncodingInterval = encodingInterval
	config.HasEncodingInterval = true
	config.BitrateLimit = bitrateLimit
	config.HasBitrateLimit = true
	return nil
}

// decodeConfiguredCodec decodes the optional codec-specific block
// (tt:H264 or tt:MPEG4) into the configured GovLength and codec profile. JPEG
// configurations legitimately carry neither block, so their absence is not an
// error. At most one codec block may be present; both present together is a
// malformed response. GovLength must be positive and the profile must be a
// valid value for the codec. Errors identify the field only.
func decodeConfiguredCodec(entry *element, config *VideoEncoderConfiguration) error {
	h264, err := optionalElement(entry, SchemaNamespace, "H264")
	if err != nil {
		return err
	}
	mpeg4, err := optionalElement(entry, SchemaNamespace, "MPEG4")
	if err != nil {
		return err
	}
	if h264 != nil && mpeg4 != nil {
		return fmt.Errorf("%w: VideoEncoderConfiguration codec block", ErrInvalidField)
	}
	switch {
	case h264 != nil:
		if err := validateH264ConfigurationSequence(h264); err != nil {
			return err
		}
		govLength, err := parseRequiredInt(h264, SchemaNamespace, "GovLength")
		if err != nil {
			return err
		}
		if govLength <= 0 {
			return fmt.Errorf("%w: H264 GovLength", ErrInvalidField)
		}
		profile, err := requiredString(h264, SchemaNamespace, "H264Profile")
		if err != nil {
			return err
		}
		if !validCodecProfile("H264", profile) {
			return fmt.Errorf("%w: H264Profile", ErrInvalidField)
		}
		config.GovLength = govLength
		config.HasGovLength = true
		config.CodecProfile = profile
		config.HasCodecProfile = true
	case mpeg4 != nil:
		if err := validateMPEG4ConfigurationSequence(mpeg4); err != nil {
			return err
		}
		govLength, err := parseRequiredInt(mpeg4, SchemaNamespace, "GovLength")
		if err != nil {
			return err
		}
		if govLength <= 0 {
			return fmt.Errorf("%w: MPEG4 GovLength", ErrInvalidField)
		}
		profile, err := requiredString(mpeg4, SchemaNamespace, "Mpeg4Profile")
		if err != nil {
			return err
		}
		if !validCodecProfile("MPEG4", profile) {
			return fmt.Errorf("%w: Mpeg4Profile", ErrInvalidField)
		}
		config.GovLength = govLength
		config.HasGovLength = true
		config.CodecProfile = profile
		config.HasCodecProfile = true
	}
	return nil
}
