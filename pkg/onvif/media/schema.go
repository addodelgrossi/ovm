package media

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"
)

const unbounded = -1

type childRule struct {
	namespace string
	local     string
	min       int
	max       int
}

func rule(namespace, local string, min, max int) childRule {
	return childRule{namespace: namespace, local: local, min: min, max: max}
}

// validateChildSequence enforces an XSD sequence without ever rendering an
// attacker-controlled QName. When allowExtensions is true, unknown children are
// accepted only after every declared rule, matching a trailing xs:any.
func validateChildSequence(parent *element, rules []childRule, allowExtensions bool) error {
	if len(bytes.TrimSpace(parent.text)) != 0 {
		return fmt.Errorf("%w: response mixed content", ErrUnexpectedElement)
	}
	counts := make([]int, len(rules))
	current := 0
	extensionsStarted := false
	for _, child := range parent.children {
		if extensionsStarted {
			if matchesAnyRule(child, rules) {
				return fmt.Errorf("%w: response child cardinality", ErrInvalidField)
			}
			continue
		}

		matched := -1
		for index := current; index < len(rules); index++ {
			candidate := rules[index]
			if child.name.Space == candidate.namespace && child.name.Local == candidate.local {
				matched = index
				break
			}
		}
		if matched < 0 {
			if allowExtensions && requiredRulesSatisfied(counts, rules, current) && !matchesAnyRule(child, rules) {
				extensionsStarted = true
				continue
			}
			return fmt.Errorf("%w: response child sequence", ErrUnexpectedElement)
		}
		for index := current; index < matched; index++ {
			if counts[index] < rules[index].min {
				return fmt.Errorf("%w: %s", ErrMissingField, rules[index].local)
			}
		}
		current = matched
		counts[matched]++
		//nolint:gosec // matched is a non-negative index produced by the bounded rules loop above.
		if rules[matched].max != unbounded && counts[matched] > rules[matched].max {
			return fmt.Errorf("%w: response child cardinality", ErrInvalidField)
		}
	}
	for index, childRule := range rules {
		if counts[index] < childRule.min {
			return fmt.Errorf("%w: %s", ErrMissingField, childRule.local)
		}
	}
	return nil
}

func requiredRulesSatisfied(counts []int, rules []childRule, start int) bool {
	for index := start; index < len(rules); index++ {
		if counts[index] < rules[index].min {
			return false
		}
	}
	return true
}

func matchesAnyRule(child *element, rules []childRule) bool {
	for _, candidate := range rules {
		if child.name.Space == candidate.namespace && child.name.Local == candidate.local {
			return true
		}
	}
	return false
}

func validateNoOrdinaryAttributes(node *element) error {
	for _, attribute := range node.attrs {
		if attribute.Name.Space == "xmlns" || (attribute.Name.Space == "" && attribute.Name.Local == "xmlns") {
			continue
		}
		return fmt.Errorf("%w: response attributes", ErrUnexpectedElement)
	}
	return nil
}

func validateWrapper(root *element, rules ...childRule) error {
	if err := validateNoOrdinaryAttributes(root); err != nil {
		return err
	}
	if err := validateChildSequence(root, rules, false); err != nil {
		return err
	}
	return validateUniqueCollectionTokens(root, rules)
}

// validateUniqueCollectionTokens rejects ambiguous identities before any entry
// is selected or normalized. Performing this preflight at the response wrapper
// makes duplicate rejection independent of the entries' wire order.
func validateUniqueCollectionTokens(root *element, rules []childRule) error {
	if len(rules) != 1 {
		return nil
	}
	switch rules[0].local {
	case "Profiles", "Configurations", "VideoSources":
	default:
		return nil
	}
	seen := make(map[string]struct{}, len(root.children))
	for _, entry := range root.childrenNamed(rules[0].namespace, rules[0].local) {
		token, ok := entry.unqualifiedAttr("token")
		if !ok {
			continue
		}
		if _, duplicate := seen[token]; duplicate {
			return fmt.Errorf("%w: duplicate response token", ErrInvalidField)
		}
		seen[token] = struct{}{}
	}
	return nil
}

func validateProfileSequence(profile *element) error {
	return validateChildSequence(profile, []childRule{
		rule(SchemaNamespace, "Name", 1, 1),
		rule(SchemaNamespace, "VideoSourceConfiguration", 0, 1),
		rule(SchemaNamespace, "AudioSourceConfiguration", 0, 1),
		rule(SchemaNamespace, "VideoEncoderConfiguration", 0, 1),
		rule(SchemaNamespace, "AudioEncoderConfiguration", 0, 1),
		rule(SchemaNamespace, "VideoAnalyticsConfiguration", 0, 1),
		rule(SchemaNamespace, "PTZConfiguration", 0, 1),
		rule(SchemaNamespace, "MetadataConfiguration", 0, 1),
		rule(SchemaNamespace, "Extension", 0, 1),
	}, false)
}

func validateVideoSourceSequence(source *element) error {
	return validateChildSequence(source, []childRule{
		rule(SchemaNamespace, "Framerate", 1, 1),
		rule(SchemaNamespace, "Resolution", 1, 1),
		rule(SchemaNamespace, "Imaging", 0, 1),
		rule(SchemaNamespace, "Extension", 0, 1),
	}, false)
}

func validateVideoEncoderSequence(configuration *element) error {
	return validateChildSequence(configuration, []childRule{
		rule(SchemaNamespace, "Name", 1, 1),
		rule(SchemaNamespace, "UseCount", 1, 1),
		rule(SchemaNamespace, "Encoding", 1, 1),
		rule(SchemaNamespace, "Resolution", 1, 1),
		rule(SchemaNamespace, "Quality", 1, 1),
		rule(SchemaNamespace, "RateControl", 0, 1),
		rule(SchemaNamespace, "MPEG4", 0, 1),
		rule(SchemaNamespace, "H264", 0, 1),
		rule(SchemaNamespace, "Multicast", 1, 1),
		rule(SchemaNamespace, "SessionTimeout", 1, 1),
	}, true)
}

func validateAudioEncoderSequence(configuration *element) error {
	return validateChildSequence(configuration, []childRule{
		rule(SchemaNamespace, "Name", 1, 1),
		rule(SchemaNamespace, "UseCount", 1, 1),
		rule(SchemaNamespace, "Encoding", 1, 1),
		rule(SchemaNamespace, "Bitrate", 1, 1),
		rule(SchemaNamespace, "SampleRate", 1, 1),
		rule(SchemaNamespace, "Multicast", 1, 1),
		rule(SchemaNamespace, "SessionTimeout", 1, 1),
	}, true)
}

func validateMediaURISequence(mediaURI *element) error {
	return validateChildSequence(mediaURI, []childRule{
		rule(SchemaNamespace, "Uri", 1, 1),
		rule(SchemaNamespace, "InvalidAfterConnect", 1, 1),
		rule(SchemaNamespace, "InvalidAfterReboot", 1, 1),
		rule(SchemaNamespace, "Timeout", 1, 1),
	}, true)
}

func validateOptionsSequence(options *element) error {
	return validateChildSequence(options, []childRule{
		rule(SchemaNamespace, "QualityRange", 1, 1),
		rule(SchemaNamespace, "JPEG", 0, 1),
		rule(SchemaNamespace, "MPEG4", 0, 1),
		rule(SchemaNamespace, "H264", 0, 1),
		rule(SchemaNamespace, "Extension", 0, 1),
	}, false)
}

func validateCodecOptionsSequence(options *element, codec string) error {
	rules := []childRule{rule(SchemaNamespace, "ResolutionsAvailable", 1, unbounded)}
	if codec != "JPEG" {
		rules = append(rules, rule(SchemaNamespace, "GovLengthRange", 1, 1))
	}
	rules = append(rules,
		rule(SchemaNamespace, "FrameRateRange", 1, 1),
		rule(SchemaNamespace, "EncodingIntervalRange", 1, 1),
	)
	switch codec {
	case "MPEG4":
		rules = append(rules, rule(SchemaNamespace, "Mpeg4ProfilesSupported", 1, unbounded))
	case "H264":
		rules = append(rules, rule(SchemaNamespace, "H264ProfilesSupported", 1, unbounded))
	}
	return validateChildSequence(options, rules, false)
}

func validateResolutionSequence(resolution *element) error {
	return validateChildSequence(resolution, []childRule{
		rule(SchemaNamespace, "Width", 1, 1),
		rule(SchemaNamespace, "Height", 1, 1),
	}, false)
}

func validateRangeSequence(valueRange *element) error {
	return validateChildSequence(valueRange, []childRule{
		rule(SchemaNamespace, "Min", 1, 1),
		rule(SchemaNamespace, "Max", 1, 1),
	}, false)
}

func validateMulticastSequence(multicast *element) error {
	return validateChildSequence(multicast, []childRule{
		rule(SchemaNamespace, "Address", 1, 1),
		rule(SchemaNamespace, "Port", 1, 1),
		rule(SchemaNamespace, "TTL", 1, 1),
		rule(SchemaNamespace, "AutoStart", 1, 1),
	}, true)
}

func validateSimpleContent(node *element, field string) error {
	if len(node.children) != 0 {
		return fmt.Errorf("%w: %s content", ErrInvalidField, field)
	}
	if err := validateNoOrdinaryAttributes(node); err != nil {
		return fmt.Errorf("%w: %s attributes", ErrInvalidField, field)
	}
	return nil
}

// validateVideoSourceConfigurationSequence enforces the inherited
// tt:ConfigurationEntity fields followed by the tt:VideoSourceConfiguration
// fields. After Bounds, the XSD permits zero or more elements from namespaces
// other than tt, followed by at most one tt:Extension.
func validateVideoSourceConfigurationSequence(configuration *element) error {
	coreRules := []childRule{
		rule(SchemaNamespace, "Name", 1, 1),
		rule(SchemaNamespace, "UseCount", 1, 1),
		rule(SchemaNamespace, "SourceToken", 1, 1),
		rule(SchemaNamespace, "Bounds", 1, 1),
	}
	if err := validateChildSequence(configuration, coreRules, true); err != nil {
		return err
	}

	boundsIndex := -1
	for index, child := range configuration.children {
		if child.name.Space == SchemaNamespace && child.name.Local == "Bounds" {
			boundsIndex = index
			break
		}
	}
	if boundsIndex < 0 {
		return fmt.Errorf("%w: Bounds", ErrMissingField)
	}

	extensionSeen := false
	for _, child := range configuration.children[boundsIndex+1:] {
		if child.name.Space == SchemaNamespace && child.name.Local == "Extension" {
			if extensionSeen {
				return fmt.Errorf("%w: response child cardinality", ErrInvalidField)
			}
			if err := validateVideoSourceConfigurationExtension(child); err != nil {
				return err
			}
			extensionSeen = true
			continue
		}
		if extensionSeen || child.name.Space == "" || child.name.Space == SchemaNamespace {
			return fmt.Errorf("%w: response child sequence", ErrUnexpectedElement)
		}
	}
	return nil
}

// validateVideoSourceConfigurationExtension validates the declared extension
// members before an association is exposed. Unknown future members remain
// acceptable only inside the nested target-namespace extension points defined
// by the shared ONVIF schema.
func validateVideoSourceConfigurationExtension(extension *element) error {
	if err := validateNoOrdinaryAttributes(extension); err != nil {
		return fmt.Errorf("%w: VideoSourceConfiguration Extension attributes", ErrInvalidField)
	}
	if err := validateChildSequence(extension, []childRule{
		rule(SchemaNamespace, "Rotate", 0, 1),
		rule(SchemaNamespace, "Extension", 0, 1),
	}, false); err != nil {
		return err
	}

	rotate, err := optionalElement(extension, SchemaNamespace, "Rotate")
	if err != nil {
		return err
	}
	if rotate != nil {
		if err := validateRotateConfiguration(rotate); err != nil {
			return err
		}
	}
	nested, err := optionalElement(extension, SchemaNamespace, "Extension")
	if err != nil {
		return err
	}
	if nested != nil {
		return validateVideoSourceConfigurationExtension2(nested)
	}
	return nil
}

func validateRotateConfiguration(rotate *element) error {
	if err := validateChildSequence(rotate, []childRule{
		rule(SchemaNamespace, "Mode", 1, 1),
		rule(SchemaNamespace, "Degree", 0, 1),
		rule(SchemaNamespace, "Extension", 0, 1),
	}, false); err != nil {
		return err
	}
	if mirror, ok := rotate.unqualifiedAttr("Mirror"); ok {
		if _, err := parseBool(mirror); err != nil {
			return fmt.Errorf("%w: Rotate Mirror attribute", ErrInvalidField)
		}
	}
	modeNode, err := requiredElement(rotate, SchemaNamespace, "Mode")
	if err != nil {
		return err
	}
	if err := validateSimpleContent(modeNode, "Rotate Mode"); err != nil {
		return err
	}
	mode := string(modeNode.text)
	switch mode {
	case "OFF", "ON", "AUTO":
	default:
		return fmt.Errorf("%w: Rotate Mode", ErrInvalidField)
	}

	degree, err := optionalElement(rotate, SchemaNamespace, "Degree")
	if err != nil {
		return err
	}
	if degree != nil {
		if err := validateSimpleContent(degree, "Rotate Degree"); err != nil {
			return err
		}
		if _, err := parseRequiredInt(rotate, SchemaNamespace, "Degree"); err != nil {
			return err
		}
	}
	nested, err := optionalElement(rotate, SchemaNamespace, "Extension")
	if err != nil {
		return err
	}
	if nested != nil {
		return validateTargetNamespaceWildcard(nested, "Rotate Extension")
	}
	return nil
}

func validateVideoSourceConfigurationExtension2(extension *element) error {
	if err := validateNoOrdinaryAttributes(extension); err != nil {
		return fmt.Errorf("%w: VideoSourceConfiguration Extension2 attributes", ErrInvalidField)
	}
	if len(bytes.TrimSpace(extension.text)) != 0 {
		return fmt.Errorf("%w: VideoSourceConfiguration Extension2 mixed content", ErrInvalidField)
	}

	phase := 0
	sceneSeen := false
	wildcardSeen := false
	for _, child := range extension.children {
		switch {
		case !wildcardSeen && phase == 0 && child.name.Space == SchemaNamespace && child.name.Local == "LensDescription":
			if err := validateLensDescription(child); err != nil {
				return err
			}
		case !wildcardSeen && !sceneSeen && child.name.Space == SchemaNamespace && child.name.Local == "SceneOrientation":
			phase = 1
			sceneSeen = true
			if err := validateSceneOrientation(child); err != nil {
				return err
			}
		default:
			if child.name.Space != SchemaNamespace {
				return fmt.Errorf("%w: VideoSourceConfiguration Extension2 child namespace", ErrUnexpectedElement)
			}
			if child.name.Local == "LensDescription" || child.name.Local == "SceneOrientation" {
				return fmt.Errorf("%w: VideoSourceConfiguration Extension2 child sequence", ErrUnexpectedElement)
			}
			wildcardSeen = true
			phase = 2
		}
	}
	return nil
}

func validateLensDescription(lens *element) error {
	if err := validateChildSequence(lens, []childRule{
		rule(SchemaNamespace, "Offset", 1, 1),
		rule(SchemaNamespace, "Projection", 1, unbounded),
		rule(SchemaNamespace, "XFactor", 1, 1),
	}, true); err != nil {
		return err
	}
	if focalLength, ok := lens.unqualifiedAttr("FocalLength"); ok {
		if !validFiniteFloat(focalLength) {
			return fmt.Errorf("%w: LensDescription FocalLength attribute", ErrInvalidField)
		}
	}

	offset, err := requiredElement(lens, SchemaNamespace, "Offset")
	if err != nil {
		return err
	}
	if len(offset.children) != 0 || len(bytes.TrimSpace(offset.text)) != 0 {
		return fmt.Errorf("%w: LensDescription Offset content", ErrInvalidField)
	}
	for _, name := range []string{"x", "y"} {
		if value, ok := offset.unqualifiedAttr(name); ok && !validFiniteFloat(value) {
			return fmt.Errorf("%w: LensDescription Offset attribute", ErrInvalidField)
		}
	}

	for _, projection := range lens.childrenNamed(SchemaNamespace, "Projection") {
		if err := validateChildSequence(projection, []childRule{
			rule(SchemaNamespace, "Angle", 1, 1),
			rule(SchemaNamespace, "Radius", 1, 1),
			rule(SchemaNamespace, "Transmittance", 0, 1),
		}, true); err != nil {
			return err
		}
		for _, field := range []string{"Angle", "Radius"} {
			if _, err := parseRequiredFiniteFloat(projection, SchemaNamespace, field); err != nil {
				return err
			}
		}
		if transmittance, err := optionalElement(projection, SchemaNamespace, "Transmittance"); err != nil {
			return err
		} else if transmittance != nil {
			if err := validateSimpleContent(transmittance, "LensDescription Transmittance"); err != nil {
				return err
			}
			if _, err := parseRequiredFiniteFloat(projection, SchemaNamespace, "Transmittance"); err != nil {
				return err
			}
		}
	}
	if _, err := parseRequiredFiniteFloat(lens, SchemaNamespace, "XFactor"); err != nil {
		return err
	}
	return nil
}

func validateSceneOrientation(scene *element) error {
	if err := validateNoOrdinaryAttributes(scene); err != nil {
		return fmt.Errorf("%w: SceneOrientation attributes", ErrInvalidField)
	}
	if err := validateChildSequence(scene, []childRule{
		rule(SchemaNamespace, "Mode", 1, 1),
		rule(SchemaNamespace, "Orientation", 0, 1),
	}, false); err != nil {
		return err
	}
	modeNode, err := requiredElement(scene, SchemaNamespace, "Mode")
	if err != nil {
		return err
	}
	if err := validateSimpleContent(modeNode, "SceneOrientation Mode"); err != nil {
		return err
	}
	switch string(modeNode.text) {
	case "MANUAL", "AUTO":
	default:
		return fmt.Errorf("%w: SceneOrientation Mode", ErrInvalidField)
	}
	orientation, err := optionalElement(scene, SchemaNamespace, "Orientation")
	if err != nil {
		return err
	}
	if orientation != nil {
		if err := validateSimpleContent(orientation, "SceneOrientation Orientation"); err != nil {
			return err
		}
	}
	return nil
}

func validFiniteFloat(value string) bool {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	return err == nil && !math.IsNaN(parsed) && !math.IsInf(parsed, 0)
}

func validateTargetNamespaceWildcard(extension *element, field string) error {
	if err := validateNoOrdinaryAttributes(extension); err != nil {
		return fmt.Errorf("%w: %s attributes", ErrInvalidField, field)
	}
	if len(bytes.TrimSpace(extension.text)) != 0 {
		return fmt.Errorf("%w: %s mixed content", ErrInvalidField, field)
	}
	for _, child := range extension.children {
		if child.name.Space != SchemaNamespace {
			return fmt.Errorf("%w: %s child namespace", ErrUnexpectedElement, field)
		}
	}
	return nil
}

// validateRateControlSequence enforces the tt:VideoRateControl child order:
// FrameRateLimit, EncodingInterval, BitrateLimit. Trailing extensions are
// accepted so devices advertising additional rate-control detail are not
// rejected.
func validateRateControlSequence(rateControl *element) error {
	return validateChildSequence(rateControl, []childRule{
		rule(SchemaNamespace, "FrameRateLimit", 1, 1),
		rule(SchemaNamespace, "EncodingInterval", 1, 1),
		rule(SchemaNamespace, "BitrateLimit", 1, 1),
	}, true)
}

// validateH264ConfigurationSequence enforces the tt:H264Configuration child
// order: GovLength, H264Profile. Trailing extensions are accepted.
func validateH264ConfigurationSequence(configuration *element) error {
	return validateChildSequence(configuration, []childRule{
		rule(SchemaNamespace, "GovLength", 1, 1),
		rule(SchemaNamespace, "H264Profile", 1, 1),
	}, true)
}

// validateMPEG4ConfigurationSequence enforces the tt:Mpeg4Configuration child
// order: GovLength, Mpeg4Profile. Trailing extensions are accepted.
func validateMPEG4ConfigurationSequence(configuration *element) error {
	return validateChildSequence(configuration, []childRule{
		rule(SchemaNamespace, "GovLength", 1, 1),
		rule(SchemaNamespace, "Mpeg4Profile", 1, 1),
	}, true)
}
