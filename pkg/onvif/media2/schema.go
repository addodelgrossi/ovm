package media2

import (
	"bytes"
	"fmt"
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
	if len(rules) != 1 || (rules[0].local != "Profiles" && rules[0].local != "Configurations") {
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
		rule(Namespace, "Name", 1, 1),
		rule(Namespace, "Configurations", 0, 1),
	}, false)
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
			extensionSeen = true
			continue
		}
		if extensionSeen || child.name.Space == "" || child.name.Space == SchemaNamespace {
			return fmt.Errorf("%w: response child sequence", ErrUnexpectedElement)
		}
	}
	return nil
}

func validateVideoEncoderSequence(configuration *element) error {
	return validateChildSequence(configuration, []childRule{
		rule(SchemaNamespace, "Name", 1, 1),
		rule(SchemaNamespace, "UseCount", 1, 1),
		rule(SchemaNamespace, "Encoding", 1, 1),
		rule(SchemaNamespace, "Resolution", 1, 1),
		rule(SchemaNamespace, "RateControl", 0, 1),
		rule(SchemaNamespace, "Multicast", 0, 1),
		rule(SchemaNamespace, "Quality", 1, 1),
	}, true)
}

func validateAudioEncoderSequence(configuration *element) error {
	return validateChildSequence(configuration, []childRule{
		rule(SchemaNamespace, "Name", 1, 1),
		rule(SchemaNamespace, "UseCount", 1, 1),
		rule(SchemaNamespace, "Encoding", 1, 1),
		rule(SchemaNamespace, "Multicast", 0, 1),
		rule(SchemaNamespace, "Bitrate", 1, 1),
		rule(SchemaNamespace, "SampleRate", 1, 1),
	}, true)
}

func validateOptionsSequence(options *element) error {
	return validateChildSequence(options, []childRule{
		rule(SchemaNamespace, "Encoding", 1, 1),
		rule(SchemaNamespace, "QualityRange", 1, 1),
		rule(SchemaNamespace, "ResolutionsAvailable", 1, unbounded),
		rule(SchemaNamespace, "BitrateRange", 1, 1),
	}, true)
}

func validateResolutionSequence(resolution *element) error {
	return validateChildSequence(resolution, []childRule{
		rule(SchemaNamespace, "Width", 1, 1),
		rule(SchemaNamespace, "Height", 1, 1),
	}, true)
}

func validateRangeSequence(valueRange *element) error {
	return validateChildSequence(valueRange, []childRule{
		rule(SchemaNamespace, "Min", 1, 1),
		rule(SchemaNamespace, "Max", 1, 1),
	}, false)
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

// validateConfigurationSetSequence enforces the exact declared
// tr2:ConfigurationSet sequence. Every member is optional; the trailing WSDL
// wildcard remains available only after the declared members. VideoSourceMode
// is deliberately absent because it is not a ConfigurationSet member.
func validateConfigurationSetSequence(set *element) error {
	return validateChildSequence(set, []childRule{
		rule(Namespace, "VideoSource", 0, 1),
		rule(Namespace, "AudioSource", 0, 1),
		rule(Namespace, "VideoEncoder", 0, 1),
		rule(Namespace, "AudioEncoder", 0, 1),
		rule(Namespace, "Analytics", 0, 1),
		rule(Namespace, "PTZ", 0, 1),
		rule(Namespace, "Metadata", 0, 1),
		rule(Namespace, "AudioOutput", 0, 1),
		rule(Namespace, "AudioDecoder", 0, 1),
		rule(Namespace, "Receiver", 0, 1),
	}, true)
}

// validateRateControl2Sequence enforces the tt:VideoRateControl2 child order.
// FrameRateLimit and BitrateLimit are required and AverageBitRate is optional.
// ConstantBitRate is not a child: the XSD declares it as an optional
// unqualified boolean attribute on the RateControl element.
func validateRateControl2Sequence(rateControl *element) error {
	return validateChildSequence(rateControl, []childRule{
		rule(SchemaNamespace, "FrameRateLimit", 1, 1),
		rule(SchemaNamespace, "BitrateLimit", 1, 1),
		rule(SchemaNamespace, "AverageBitRate", 0, 1),
	}, true)
}
