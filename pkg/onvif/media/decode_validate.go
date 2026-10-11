package media

import (
	"fmt"
	"math"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxWireURIBytes = 16 << 10

var xsdDurationPattern = regexp.MustCompile(`^P(?:(?:[0-9]+Y)?(?:[0-9]+M)?(?:[0-9]+D)?(?:T(?:[0-9]+H)?(?:[0-9]+M)?(?:[0-9]+(?:\.[0-9]+)?S)?)?)$`)

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
	parsed, err := strconv.ParseInt(strings.TrimSpace(value), 10, 32)
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

func validateXSDDuration(value string) error {
	if value == "" || !xsdDurationPattern.MatchString(value) || value == "P" || value == "PT" || value == "-P" || value == "-PT" || strings.HasSuffix(value, "T") {
		return fmt.Errorf("%w: Timeout", ErrInvalidField)
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

func validVideoEncoding(value string) (VideoEncoding, bool) {
	switch VideoEncoding(value) {
	case VideoEncodingJPEG, VideoEncodingMPEG4, VideoEncodingH264:
		return VideoEncoding(value), true
	default:
		return "", false
	}
}

func validAudioEncoding(value string) (AudioEncoding, bool) {
	switch AudioEncoding(value) {
	case AudioEncodingG711, AudioEncodingG726, AudioEncodingAAC:
		return AudioEncoding(value), true
	default:
		return "", false
	}
}

func validateRequiredStreamingFields(configuration *element) error {
	multicast, err := requiredElement(configuration, SchemaNamespace, "Multicast")
	if err != nil {
		return err
	}
	if err := validateMulticastSequence(multicast); err != nil {
		return err
	}
	address, err := requiredElement(multicast, SchemaNamespace, "Address")
	if err != nil {
		return err
	}
	if err := validateChildSequence(address, []childRule{
		rule(SchemaNamespace, "Type", 1, 1),
		rule(SchemaNamespace, "IPv4Address", 0, 1),
		rule(SchemaNamespace, "IPv6Address", 0, 1),
	}, false); err != nil {
		return err
	}
	addressType, err := requiredText(address, SchemaNamespace, "Type")
	if err != nil {
		return err
	}
	switch addressType {
	case "IPv4":
		value, err := requiredText(address, SchemaNamespace, "IPv4Address")
		if err != nil || net.ParseIP(value) == nil || strings.Contains(value, ":") {
			return fmt.Errorf("%w: multicast IPv4Address", ErrInvalidField)
		}
	case "IPv6":
		value, err := requiredText(address, SchemaNamespace, "IPv6Address")
		if err != nil || net.ParseIP(value) == nil || !strings.Contains(value, ":") {
			return fmt.Errorf("%w: multicast IPv6Address", ErrInvalidField)
		}
	default:
		return fmt.Errorf("%w: multicast address Type", ErrInvalidField)
	}
	port, err := parseRequiredInt(multicast, SchemaNamespace, "Port")
	if err != nil || port < 0 || port > 65535 {
		return fmt.Errorf("%w: multicast Port", ErrInvalidField)
	}
	ttl, err := parseRequiredInt(multicast, SchemaNamespace, "TTL")
	if err != nil || ttl < 0 || ttl > 255 {
		return fmt.Errorf("%w: multicast TTL", ErrInvalidField)
	}
	autoStart, err := requiredText(multicast, SchemaNamespace, "AutoStart")
	if err != nil {
		return err
	}
	if _, err := parseBool(autoStart); err != nil {
		return fmt.Errorf("%w: multicast AutoStart", ErrInvalidField)
	}
	timeout, err := requiredText(configuration, SchemaNamespace, "SessionTimeout")
	if err != nil {
		return err
	}
	if err := validateXSDDuration(timeout); err != nil {
		return fmt.Errorf("%w: SessionTimeout", ErrInvalidField)
	}
	return nil
}
