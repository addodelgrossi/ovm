package media2

import (
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxWireURIBytes = 16 << 10

func validateResponseToken(value, field string) error {
	if strings.TrimSpace(value) == "" || utf8.RuneCountInString(value) > maxReferenceTokenLen {
		return fmt.Errorf("%w: %s", ErrInvalidField, field)
	}
	for _, character := range value {
		if character < 0x20 || (character >= 0x7f && character <= 0x9f) {
			return fmt.Errorf("%w: %s", ErrInvalidField, field)
		}
	}
	return nil
}

func validateResponseName(value, field string) error {
	if utf8.RuneCountInString(value) > maxReferenceTokenLen {
		return fmt.Errorf("%w: %s", ErrInvalidField, field)
	}
	return nil
}

func validateEncoding(value, field string) error {
	if value == "" || len(value) > 256 || !utf8.ValidString(value) {
		return fmt.Errorf("%w: %s", ErrInvalidField, field)
	}
	for _, character := range value {
		if character < 0x20 || (character >= 0x7f && character <= 0x9f) {
			return fmt.Errorf("%w: %s", ErrInvalidField, field)
		}
	}
	return nil
}

func parseRequiredInt(parent *element, namespace, local string) (int, error) {
	value, err := requiredText(parent, namespace, local)
	if err != nil {
		return 0, err
	}
	parsed, parseErr := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
	if parseErr != nil {
		return 0, fmt.Errorf("%w: %s", ErrInvalidField, local)
	}
	return int(parsed), nil
}

// parseAttrInt parses an xs:int from an unqualified attribute value. It
// requires a decimal integer with only surrounding whitespace and enforces the
// XML Schema 32-bit signed range.
func parseAttrInt(value string) (int, error) {
	trimmed := strings.TrimSpace(value)
	parsed, err := strconv.ParseInt(trimmed, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("%w: attribute integer", ErrInvalidField)
	}
	return int(parsed), nil
}

func parseRequiredFiniteFloat(parent *element, namespace, local string) (float64, error) {
	value, err := requiredText(parent, namespace, local)
	if err != nil {
		return 0, err
	}
	parsed, parseErr := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if parseErr != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) {
		return 0, fmt.Errorf("%w: %s", ErrInvalidField, local)
	}
	return parsed, nil
}

func validateOperationURI(value, responseLocal string) error {
	if value == "" || len(value) > maxWireURIBytes || strings.TrimSpace(value) != value {
		return fmt.Errorf("%w: Uri", ErrInvalidField)
	}
	for _, character := range value {
		if character < 0x20 || (character >= 0x7f && character <= 0x9f) {
			return fmt.Errorf("%w: Uri", ErrInvalidField)
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: Uri", ErrInvalidField)
	}
	scheme := strings.ToLower(parsed.Scheme)
	if responseLocal == "GetSnapshotUriResponse" {
		if scheme != "http" && scheme != "https" {
			return fmt.Errorf("%w: Uri", ErrInvalidField)
		}
		return nil
	}
	if scheme != "rtsp" && scheme != "rtsps" && scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: Uri", ErrInvalidField)
	}
	return nil
}

func decodeIntRange(parent *element, local string) (IntRange, error) {
	rangeElement, err := requiredElement(parent, SchemaNamespace, local)
	if err != nil {
		return IntRange{}, err
	}
	if err := validateRangeSequence(rangeElement); err != nil {
		return IntRange{}, err
	}
	minimum, err := parseRequiredInt(rangeElement, SchemaNamespace, "Min")
	if err != nil {
		return IntRange{}, err
	}
	maximum, err := parseRequiredInt(rangeElement, SchemaNamespace, "Max")
	if err != nil {
		return IntRange{}, err
	}
	if minimum > maximum {
		return IntRange{}, fmt.Errorf("%w: %s", ErrInvalidField, local)
	}
	return IntRange{Min: minimum, Max: maximum}, nil
}

func decodeFloatRange(parent *element, local string) (FloatRange, error) {
	rangeElement, err := requiredElement(parent, SchemaNamespace, local)
	if err != nil {
		return FloatRange{}, err
	}
	if err := validateRangeSequence(rangeElement); err != nil {
		return FloatRange{}, err
	}
	minimum, err := parseRequiredFiniteFloat(rangeElement, SchemaNamespace, "Min")
	if err != nil {
		return FloatRange{}, err
	}
	maximum, err := parseRequiredFiniteFloat(rangeElement, SchemaNamespace, "Max")
	if err != nil {
		return FloatRange{}, err
	}
	if minimum > maximum {
		return FloatRange{}, fmt.Errorf("%w: %s", ErrInvalidField, local)
	}
	return FloatRange{Min: minimum, Max: maximum}, nil
}

func decodeGovLengthRange(options *element) (*IntRange, error) {
	value, present := options.unqualifiedAttr("GovLengthRange")
	if !present {
		return nil, nil
	}
	parts := strings.Fields(value)
	if len(parts) != 2 {
		return nil, fmt.Errorf("%w: GovLengthRange", ErrInvalidField)
	}
	minimum, minErr := parseAttrInt(parts[0])
	maximum, maxErr := parseAttrInt(parts[1])
	if minErr != nil || maxErr != nil || minimum > maximum {
		return nil, fmt.Errorf("%w: GovLengthRange", ErrInvalidField)
	}
	result := IntRange{Min: minimum, Max: maximum}
	return &result, nil
}
