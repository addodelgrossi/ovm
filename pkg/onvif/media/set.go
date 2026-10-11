package media

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
// (trt:SetVideoEncoderConfiguration). forcePersistence asks the device to keep
// the change across reboots. The configuration is serialized from its typed
// fields, including the required Multicast and SessionTimeout; vendor
// extensions are not round-tripped. Errors never echo values.
func (c *Client) SetVideoEncoderConfiguration(ctx context.Context, config VideoEncoderConfiguration, forcePersistence bool, opts ...RequestOptions) error {
	inner, err := buildVideoEncoderConfiguration(config)
	if err != nil {
		return err
	}
	body := []byte(`<trt:SetVideoEncoderConfiguration xmlns:trt="` + Namespace + `" xmlns:tt="` + SchemaNamespace + `">` +
		inner + `<trt:ForcePersistence>` + strconv.FormatBool(forcePersistence) + `</trt:ForcePersistence>` +
		`</trt:SetVideoEncoderConfiguration>`)
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
	if _, valid := validVideoEncoding(string(config.Encoding)); !valid {
		return "", fmt.Errorf("%w: Configuration Encoding", ErrInvalidInput)
	}
	if config.Resolution.Width <= 0 || config.Resolution.Height <= 0 {
		return "", fmt.Errorf("%w: Configuration Resolution", ErrInvalidInput)
	}
	if math.IsNaN(config.Quality) || math.IsInf(config.Quality, 0) || config.Quality < 0 {
		return "", fmt.Errorf("%w: Configuration Quality", ErrInvalidInput)
	}
	if err := validateXSDDuration(config.SessionTimeout); err != nil {
		return "", fmt.Errorf("%w: Configuration SessionTimeout", ErrInvalidInput)
	}

	var builder strings.Builder
	builder.WriteString(`<trt:Configuration token="` + escapeToken(config.Token) + `">`)
	builder.WriteString(`<tt:Name>` + escapeToken(config.Name) + `</tt:Name>`)
	builder.WriteString(`<tt:UseCount>` + strconv.Itoa(config.UseCount) + `</tt:UseCount>`)
	builder.WriteString(`<tt:Encoding>` + string(config.Encoding) + `</tt:Encoding>`)
	builder.WriteString(`<tt:Resolution><tt:Width>` + strconv.Itoa(config.Resolution.Width) + `</tt:Width><tt:Height>` +
		strconv.Itoa(config.Resolution.Height) + `</tt:Height></tt:Resolution>`)
	builder.WriteString(`<tt:Quality>` + strconv.FormatFloat(config.Quality, 'f', -1, 64) + `</tt:Quality>`)
	if config.HasFrameRateLimit || config.HasEncodingInterval || config.HasBitrateLimit {
		if !config.HasFrameRateLimit || !config.HasBitrateLimit || config.FrameRateLimit <= 0 || config.BitrateLimit <= 0 ||
			(config.HasEncodingInterval && config.EncodingInterval <= 0) {
			return "", fmt.Errorf("%w: Configuration RateControl", ErrInvalidInput)
		}
		interval := config.EncodingInterval
		if !config.HasEncodingInterval {
			interval = 1
		}
		builder.WriteString(`<tt:RateControl><tt:FrameRateLimit>` + strconv.Itoa(config.FrameRateLimit) +
			`</tt:FrameRateLimit><tt:EncodingInterval>` + strconv.Itoa(interval) +
			`</tt:EncodingInterval><tt:BitrateLimit>` + strconv.Itoa(config.BitrateLimit) + `</tt:BitrateLimit></tt:RateControl>`)
	}
	switch config.Encoding {
	case VideoEncodingH264, VideoEncodingMPEG4:
		if config.HasGovLength || config.HasCodecProfile {
			codec := string(config.Encoding)
			if !config.HasGovLength || !config.HasCodecProfile || config.GovLength <= 0 || !validCodecProfile(codec, config.CodecProfile) {
				return "", fmt.Errorf("%w: Configuration codec block", ErrInvalidInput)
			}
			profileElement := "H264Profile"
			if config.Encoding == VideoEncodingMPEG4 {
				profileElement = "Mpeg4Profile"
			}
			builder.WriteString(`<tt:` + codec + `><tt:GovLength>` + strconv.Itoa(config.GovLength) + `</tt:GovLength><tt:` +
				profileElement + `>` + config.CodecProfile + `</tt:` + profileElement + `></tt:` + codec + `>`)
		}
	}
	multicast, err := buildMulticast(config.Multicast)
	if err != nil {
		return "", err
	}
	builder.WriteString(multicast)
	builder.WriteString(`<tt:SessionTimeout>` + config.SessionTimeout + `</tt:SessionTimeout>`)
	builder.WriteString(`</trt:Configuration>`)
	return builder.String(), nil
}

func buildMulticast(multicast Multicast) (string, error) {
	if multicast.Port < 0 || multicast.Port > 65535 || multicast.TTL < 0 || multicast.TTL > 255 {
		return "", fmt.Errorf("%w: Configuration Multicast", ErrInvalidInput)
	}
	if multicast.AddressType != "IPv4" && multicast.AddressType != "IPv6" {
		return "", fmt.Errorf("%w: Configuration Multicast address type", ErrInvalidInput)
	}
	parsed, err := netip.ParseAddr(multicast.Address)
	if err != nil || parsed.Is4() != (multicast.AddressType == "IPv4") {
		return "", fmt.Errorf("%w: Configuration Multicast address", ErrInvalidInput)
	}
	element := multicast.AddressType + "Address"
	return `<tt:Multicast><tt:Address><tt:Type>` + multicast.AddressType + `</tt:Type><tt:` + element + `>` + parsed.String() +
		`</tt:` + element + `></tt:Address><tt:Port>` + strconv.Itoa(multicast.Port) + `</tt:Port><tt:TTL>` +
		strconv.Itoa(multicast.TTL) + `</tt:TTL><tt:AutoStart>` + strconv.FormatBool(multicast.AutoStart) +
		`</tt:AutoStart></tt:Multicast>`, nil
}

// decodeStreamingFields returns the Multicast and SessionTimeout values of a
// configuration already accepted by validateRequiredStreamingFields.
func decodeStreamingFields(entry *element) (Multicast, string) {
	var result Multicast
	multicast := entry.child(SchemaNamespace, "Multicast")
	address := multicast.child(SchemaNamespace, "Address")
	result.AddressType = address.child(SchemaNamespace, "Type").trimmedText()
	if value := address.child(SchemaNamespace, result.AddressType+"Address"); value != nil {
		if parsed, err := netip.ParseAddr(value.trimmedText()); err == nil {
			result.Address = parsed.String()
		}
	}
	result.Port, _ = strconv.Atoi(multicast.child(SchemaNamespace, "Port").trimmedText())
	result.TTL, _ = strconv.Atoi(multicast.child(SchemaNamespace, "TTL").trimmedText())
	result.AutoStart, _ = parseBool(multicast.child(SchemaNamespace, "AutoStart").trimmedText())
	return result, entry.child(SchemaNamespace, "SessionTimeout").trimmedText()
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return true
		}
	}
	return false
}
