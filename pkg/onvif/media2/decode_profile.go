package media2

import "fmt"

// optionalElement returns the single child matching {namespace}local, or nil
// when absent. A repeated child is ambiguous and rejected, matching the
// at-most-once cardinality of the optional members this package decodes.
func optionalElement(parent *element, namespace, local string) (*element, error) {
	if parent.hasDuplicateChild(namespace, local) {
		return nil, fmt.Errorf("%w: duplicate %s", ErrInvalidField, local)
	}
	return parent.child(namespace, local), nil
}

// decodeProfileAssociations extracts the typed configuration references from a
// tr2:MediaProfile. The references live in the optional tr2:Configurations
// child (a tr2:ConfigurationSet). Each member of the set is optional, so an
// absent member leaves its presence flag false. The ConfigurationSet child
// order, cardinality, and namespaces are strictly validated, while its
// documented trailing extensions are tolerated. The caller has already
// validated that at most one tr2:Configurations child is present via the
// profile sequence. Only namespace-resolved expected elements and attributes
// are read, so a lookalike local name in a foreign namespace never satisfies an
// association. Errors identify the field only; no attacker-controlled value is
// echoed.
func decodeProfileAssociations(profile *element) (ProfileAssociations, error) {
	var associations ProfileAssociations

	configurations, err := optionalElement(profile, Namespace, "Configurations")
	if err != nil {
		return ProfileAssociations{}, err
	}
	if configurations == nil {
		return associations, nil
	}
	if err := validateConfigurationSetSequence(configurations); err != nil {
		return ProfileAssociations{}, err
	}

	videoSource, err := optionalElement(configurations, Namespace, "VideoSource")
	if err != nil {
		return ProfileAssociations{}, err
	}
	if videoSource != nil {
		if err := validateVideoSourceConfigurationSequence(videoSource); err != nil {
			return ProfileAssociations{}, err
		}
		token, _, _, _, err := decodeConfigurationEntity(videoSource)
		if err != nil {
			return ProfileAssociations{}, err
		}
		sourceToken, err := requiredString(videoSource, SchemaNamespace, "SourceToken")
		if err != nil {
			return ProfileAssociations{}, err
		}
		if err := validateResponseToken(sourceToken, "VideoSource SourceToken"); err != nil {
			return ProfileAssociations{}, err
		}
		bounds, err := requiredElement(videoSource, SchemaNamespace, "Bounds")
		if err != nil {
			return ProfileAssociations{}, err
		}
		if err := validateIntRectangle(bounds); err != nil {
			return ProfileAssociations{}, err
		}

		// Publish association tokens only after the complete inherited
		// VideoSourceConfiguration has passed validation.
		associations.VideoSourceConfigurationToken = token
		associations.HasVideoSourceConfiguration = true
		associations.VideoSourceToken = sourceToken
		associations.HasVideoSource = true
	}

	videoEncoder, err := optionalElement(configurations, Namespace, "VideoEncoder")
	if err != nil {
		return ProfileAssociations{}, err
	}
	if videoEncoder != nil {
		token, err := requiredConfigurationToken(videoEncoder, "VideoEncoder token attribute")
		if err != nil {
			return ProfileAssociations{}, err
		}
		associations.VideoEncoderConfigurationToken = token
		associations.HasVideoEncoderConfiguration = true
	}

	audioEncoder, err := optionalElement(configurations, Namespace, "AudioEncoder")
	if err != nil {
		return ProfileAssociations{}, err
	}
	if audioEncoder != nil {
		token, err := requiredConfigurationToken(audioEncoder, "AudioEncoder token attribute")
		if err != nil {
			return ProfileAssociations{}, err
		}
		associations.AudioEncoderConfigurationToken = token
		associations.HasAudioEncoderConfiguration = true
	}

	return associations, nil
}

// requiredConfigurationToken reads and validates the unqualified token
// attribute of a ConfigurationSet member. A missing attribute is a missing
// field; a present attribute must satisfy the response-token invariants.
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

// decodeConfiguredRateControl decodes the optional tt:RateControl
// (tt:VideoRateControl2) block of a VideoEncoder2Configuration. The block is
// optional; when present FrameRateLimit (xs:float, > 0) and BitrateLimit
// (xs:int, > 0) are required, AverageBitRate (xs:int, >= 0) is an optional
// child, and ConstantBitRate is an optional unqualified xs:boolean attribute.
// Invalid values are rejected without echoing response content.
func decodeConfiguredRateControl(entry *element, config *VideoEncoderConfiguration) error {
	rateControl, err := optionalElement(entry, SchemaNamespace, "RateControl")
	if err != nil {
		return err
	}
	if rateControl == nil {
		return nil
	}
	if err := validateRateControl2Sequence(rateControl); err != nil {
		return err
	}
	if value, present := rateControl.unqualifiedAttr("ConstantBitRate"); present {
		constantBitRate, err := parseBool(value)
		if err != nil {
			return fmt.Errorf("%w: RateControl ConstantBitRate attribute", ErrInvalidField)
		}
		config.ConstantBitRate = constantBitRate
		config.HasConstantBitRate = true
	}
	frameRateLimit, err := parseRequiredFiniteFloat(rateControl, SchemaNamespace, "FrameRateLimit")
	if err != nil {
		return err
	}
	if frameRateLimit <= 0 {
		return fmt.Errorf("%w: RateControl FrameRateLimit", ErrInvalidField)
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
	config.BitrateLimit = bitrateLimit
	config.HasBitrateLimit = true

	if average, err := optionalElement(rateControl, SchemaNamespace, "AverageBitRate"); err != nil {
		return err
	} else if average != nil {
		averageBitRate, err := parseRequiredInt(rateControl, SchemaNamespace, "AverageBitRate")
		if err != nil {
			return err
		}
		if averageBitRate < 0 {
			return fmt.Errorf("%w: RateControl AverageBitRate", ErrInvalidField)
		}
		config.AverageBitRate = averageBitRate
		config.HasAverageBitRate = true
	}
	return nil
}

// decodeConfiguredAttributes decodes the optional unqualified GovLength and
// Profile attributes of a VideoEncoder2Configuration element. GovLength must be
// a positive integer; Profile must be one of the Media2 profile vocabulary. A
// qualified lookalike attribute (one carrying a namespace) does not satisfy
// these unqualified fields and is ignored, because unqualifiedAttr only matches
// attributes with no namespace. Malformed or out-of-range values are rejected
// with field-only errors.
func decodeConfiguredAttributes(entry *element, config *VideoEncoderConfiguration) error {
	if value, present := entry.unqualifiedAttr("GovLength"); present {
		govLength, err := parseAttrInt(value)
		if err != nil || govLength <= 0 {
			return fmt.Errorf("%w: GovLength attribute", ErrInvalidField)
		}
		config.GovLength = govLength
		config.HasGovLength = true
	}
	if value, present := entry.unqualifiedAttr("Profile"); present {
		if !validMedia2Profile(value) {
			return fmt.Errorf("%w: Profile attribute", ErrInvalidField)
		}
		config.Profile = value
		config.HasProfile = true
	}
	return nil
}

// validMedia2Profile reports whether value is a profile from the Media2
// configuration profile vocabulary.
func validMedia2Profile(value string) bool {
	switch value {
	case "Simple", "AdvancedSimple", "Baseline", "Main", "Main10", "Extended", "High":
		return true
	default:
		return false
	}
}
