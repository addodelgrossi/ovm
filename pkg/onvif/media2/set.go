package media2

import (
	"context"
	"fmt"
	"math"
	"net/netip"
	"strconv"
	"strings"
	"unicode/utf8"
)

// maxNameLength is the tt:Name maxLength.
const maxNameLength = 64

// SetVideoEncoderConfiguration replaces one video encoder configuration
// (tr2:SetVideoEncoderConfiguration). Media2 persists the change; there is no
// ForcePersistence flag. The configuration is serialized from its typed
// fields, including Multicast when present; vendor extension elements are not
// round-tripped. Every field is validated before serialization and errors
// never echo values.
func (c *Client) SetVideoEncoderConfiguration(ctx context.Context, config VideoEncoderConfiguration, opts ...RequestOptions) error {
	inner, err := buildVideoEncoderConfiguration(config)
	if err != nil {
		return err
	}
	body := []byte(`<tr2:SetVideoEncoderConfiguration xmlns:tr2="` + Namespace + `" xmlns:tt="` + SchemaNamespace + `">` +
		inner + `</tr2:SetVideoEncoderConfiguration>`)
	root, _, err := c.call(ctx, ActionSetVideoEncoderConfiguration, body, opts)
	if err != nil {
		return err
	}
	if err := requireRoot(root, "SetVideoEncoderConfigurationResponse"); err != nil {
		return err
	}
	return validateWrapper(root)
}

func buildVideoEncoderConfiguration(config VideoEncoderConfiguration) (string, error) {
	if err := validateReferenceToken("Configuration token", config.Token); err != nil {
		return "", err
	}
	if !utf8.ValidString(config.Name) || utf8.RuneCountInString(config.Name) > maxNameLength || hasControl(config.Name) {
		return "", fmt.Errorf("%w: Configuration Name", ErrInvalidInput)
	}
	if config.UseCount < 0 {
		return "", fmt.Errorf("%w: Configuration UseCount", ErrInvalidInput)
	}
	if err := validateEncoding(config.Encoding, "Configuration Encoding"); err != nil {
		return "", fmt.Errorf("%w: Configuration Encoding", ErrInvalidInput)
	}
	if config.Resolution.Width <= 0 || config.Resolution.Height <= 0 {
		return "", fmt.Errorf("%w: Configuration Resolution", ErrInvalidInput)
	}
	if !finiteNonNegative(config.Quality) {
		return "", fmt.Errorf("%w: Configuration Quality", ErrInvalidInput)
	}
	var attributes string
	if config.HasGovLength {
		if config.GovLength <= 0 {
			return "", fmt.Errorf("%w: Configuration GovLength", ErrInvalidInput)
		}
		attributes += ` GovLength="` + strconv.Itoa(config.GovLength) + `"`
	}
	if config.HasProfile {
		if !validMedia2Profile(config.Profile) {
			return "", fmt.Errorf("%w: Configuration Profile", ErrInvalidInput)
		}
		attributes += ` Profile="` + config.Profile + `"`
	}

	var builder strings.Builder
	builder.WriteString(`<tr2:Configuration token="` + escapeAttribute(config.Token) + `"` + attributes + `>`)
	builder.WriteString(`<tt:Name>` + escapeToken(config.Name) + `</tt:Name>`)
	builder.WriteString(`<tt:UseCount>` + strconv.Itoa(config.UseCount) + `</tt:UseCount>`)
	builder.WriteString(`<tt:Encoding>` + escapeToken(config.Encoding) + `</tt:Encoding>`)
	builder.WriteString(`<tt:Resolution><tt:Width>` + strconv.Itoa(config.Resolution.Width) + `</tt:Width><tt:Height>` +
		strconv.Itoa(config.Resolution.Height) + `</tt:Height></tt:Resolution>`)
	if config.HasFrameRateLimit || config.HasBitrateLimit {
		if !config.HasFrameRateLimit || !config.HasBitrateLimit ||
			math.IsNaN(config.FrameRateLimit) || math.IsInf(config.FrameRateLimit, 0) || config.FrameRateLimit <= 0 || config.BitrateLimit <= 0 {
			return "", fmt.Errorf("%w: Configuration RateControl", ErrInvalidInput)
		}
		rateAttributes := ""
		if config.HasConstantBitRate {
			rateAttributes = ` ConstantBitRate="` + strconv.FormatBool(config.ConstantBitRate) + `"`
		}
		builder.WriteString(`<tt:RateControl` + rateAttributes + `><tt:FrameRateLimit>` + formatFloat(config.FrameRateLimit) +
			`</tt:FrameRateLimit><tt:BitrateLimit>` + strconv.Itoa(config.BitrateLimit) + `</tt:BitrateLimit></tt:RateControl>`)
	}
	if config.Multicast != nil {
		multicast, err := buildMulticast(*config.Multicast)
		if err != nil {
			return "", err
		}
		builder.WriteString(multicast)
	}
	builder.WriteString(`<tt:Quality>` + formatFloat(config.Quality) + `</tt:Quality>`)
	builder.WriteString(`</tr2:Configuration>`)
	return builder.String(), nil
}

func buildMulticast(multicast Multicast) (string, error) {
	if multicast.Port < 0 || multicast.Port > 65535 || multicast.TTL < 0 || multicast.TTL > 255 {
		return "", fmt.Errorf("%w: Configuration Multicast", ErrInvalidInput)
	}
	address := `<tt:Address><tt:Type>IPv4</tt:Type></tt:Address>`
	switch multicast.AddressType {
	case "IPv4", "IPv6":
		address = `<tt:Address><tt:Type>` + multicast.AddressType + `</tt:Type>`
		if multicast.Address != "" {
			parsed, err := netip.ParseAddr(multicast.Address)
			if err != nil || parsed.Is4() != (multicast.AddressType == "IPv4") {
				return "", fmt.Errorf("%w: Configuration Multicast address", ErrInvalidInput)
			}
			address += `<tt:` + multicast.AddressType + `Address>` + parsed.String() + `</tt:` + multicast.AddressType + `Address>`
		}
		address += `</tt:Address>`
	case "":
	default:
		return "", fmt.Errorf("%w: Configuration Multicast address type", ErrInvalidInput)
	}
	return `<tt:Multicast>` + address +
		`<tt:Port>` + strconv.Itoa(multicast.Port) + `</tt:Port>` +
		`<tt:TTL>` + strconv.Itoa(multicast.TTL) + `</tt:TTL>` +
		`<tt:AutoStart>` + strconv.FormatBool(multicast.AutoStart) + `</tt:AutoStart></tt:Multicast>`, nil
}

// decodeMulticast decodes the optional tt:Multicast block of a configuration.
func decodeMulticast(entry *element) (*Multicast, error) {
	node, err := optionalElement(entry, SchemaNamespace, "Multicast")
	if err != nil || node == nil {
		return nil, err
	}
	result := &Multicast{}
	if address := node.child(SchemaNamespace, "Address"); address != nil {
		addressType := address.child(SchemaNamespace, "Type")
		if addressType == nil {
			return nil, fmt.Errorf("%w: Multicast Address Type", ErrMissingField)
		}
		result.AddressType = addressType.trimmedText()
		if result.AddressType != "IPv4" && result.AddressType != "IPv6" {
			return nil, fmt.Errorf("%w: Multicast Address Type", ErrInvalidField)
		}
		if value := address.child(SchemaNamespace, result.AddressType+"Address"); value != nil {
			parsed, err := netip.ParseAddr(value.trimmedText())
			if err != nil {
				return nil, fmt.Errorf("%w: Multicast Address", ErrInvalidField)
			}
			result.Address = parsed.String()
		}
	}
	port, err := parseRequiredInt(node, SchemaNamespace, "Port")
	if err != nil {
		return nil, err
	}
	ttl, err := parseRequiredInt(node, SchemaNamespace, "TTL")
	if err != nil {
		return nil, err
	}
	autoStart, err := requiredText(node, SchemaNamespace, "AutoStart")
	if err != nil {
		return nil, err
	}
	start, err := parseBool(autoStart)
	if err != nil || port < 0 || port > 65535 || ttl < 0 || ttl > 255 {
		return nil, fmt.Errorf("%w: Multicast", ErrInvalidField)
	}
	result.Port, result.TTL, result.AutoStart = port, ttl, start
	return result, nil
}

// decodeOptionAttributes decodes the optional list and boolean attributes of
// tt:VideoEncoder2ConfigurationOptions.
func decodeOptionAttributes(entry *element) ([]float64, []string, bool, error) {
	var frameRates []float64
	if value, present := entry.unqualifiedAttr("FrameRatesSupported"); present {
		for _, field := range strings.Fields(value) {
			rate, err := strconv.ParseFloat(field, 64)
			if err != nil || !finiteNonNegative(rate) || rate == 0 {
				return nil, nil, false, fmt.Errorf("%w: FrameRatesSupported", ErrInvalidField)
			}
			frameRates = append(frameRates, rate)
		}
	}
	var profiles []string
	if value, present := entry.unqualifiedAttr("ProfilesSupported"); present {
		for _, field := range strings.Fields(value) {
			if !validMedia2Profile(field) {
				return nil, nil, false, fmt.Errorf("%w: ProfilesSupported", ErrInvalidField)
			}
			profiles = append(profiles, field)
		}
	}
	constantBitRate := false
	if value, present := entry.unqualifiedAttr("ConstantBitRateSupported"); present {
		parsed, err := parseBool(value)
		if err != nil {
			return nil, nil, false, fmt.Errorf("%w: ConstantBitRateSupported", ErrInvalidField)
		}
		constantBitRate = parsed
	}
	return frameRates, profiles, constantBitRate, nil
}

func finiteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return true
		}
	}
	return false
}

// escapeAttribute escapes a validated value for a double-quoted attribute.
func escapeAttribute(value string) string {
	return strings.ReplaceAll(escapeToken(value), `"`, "&quot;")
}
