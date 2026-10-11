package fakedevice

import (
	"strings"
	"unicode/utf8"
)

// mediaRequest contains the validated fields needed to synthesize an operation
// response. Values are never copied into diagnostics or request records.
type mediaRequest struct {
	profileToken       string
	configurationToken string
	protocol           string
	stream             string
	configurationTypes []string
}

// validateMediaRequestWrapper validates the exact request child QNames,
// sequence/cardinality, simple-content shape, supported enumerations, and
// reference-token bounds from the official Media ver10/ver20 WSDLs.
func validateMediaRequestWrapper(action string, operation operationWire) (mediaRequest, error) {
	if action != ActionMedia1GetVideoSources &&
		!strings.HasPrefix(action, media1Namespace+"/") &&
		!strings.HasPrefix(action, media2Namespace+"/") {
		return mediaRequest{}, nil
	}
	if !operationNamespaceAttributesOnly(operation) {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	switch action {
	case ActionMedia1GetProfiles,
		ActionMedia1GetVideoSources,
		ActionMedia1GetVideoEncoderConfigurations,
		ActionMedia1GetAudioEncoderConfigurations:
		if err := requireNoElementChildren(operation); err != nil {
			return mediaRequest{}, err
		}
		return mediaRequest{}, nil

	case ActionMedia2GetProfiles:
		return validateMedia2GetProfiles(operation)

	case ActionMedia1GetVideoEncoderConfigurationOptions:
		return validateOptionalTokens(operation, media1Namespace)
	case ActionMedia2GetVideoEncoderConfigurations,
		ActionMedia2GetVideoEncoderConfigurationOptions,
		ActionMedia2GetAudioEncoderConfigurations:
		return validateOptionalTokens(operation, media2Namespace)

	case ActionMedia1GetStreamURI:
		return validateMedia1StreamURI(operation)
	case ActionMedia2GetStreamURI:
		return validateMedia2StreamURI(operation)

	case ActionMedia1GetSnapshotURI:
		return validateProfileTokenOnly(operation, media1Namespace)
	case ActionMedia2GetSnapshotURI:
		return validateProfileTokenOnly(operation, media2Namespace)
	default:
		return mediaRequest{}, nil
	}
}

func validateMedia2GetProfiles(operation operationWire) (mediaRequest, error) {
	if strings.TrimSpace(operation.Value) != "" {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	var result mediaRequest
	seenToken := false
	seenTypes := make(map[string]struct{}, len(operation.Elements))
	for index, child := range operation.Elements {
		if child.XMLName.Space != media2Namespace || !simpleElementContent(child) {
			return mediaRequest{}, errInvalidRequestWrapper
		}
		value := child.Value
		switch child.XMLName.Local {
		case "Token":
			if seenToken || index != 0 || len(seenTypes) != 0 || !validReferenceTokenText(value) {
				return mediaRequest{}, errInvalidRequestWrapper
			}
			seenToken = true
			result.profileToken = value
		case "Type":
			if !validMedia2ConfigurationType(value) {
				return mediaRequest{}, errInvalidRequestWrapper
			}
			if _, duplicate := seenTypes[value]; duplicate {
				return mediaRequest{}, errInvalidRequestWrapper
			}
			seenTypes[value] = struct{}{}
			result.configurationTypes = append(result.configurationTypes, value)
		default:
			return mediaRequest{}, errInvalidRequestWrapper
		}
	}
	// The WSDL documentation defines All as a single-type request, not a value
	// to combine with individual types.
	if _, all := seenTypes["All"]; all && len(seenTypes) != 1 {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	return result, nil
}

// validateOptionalTokens validates the tr2:GetConfiguration shape and the
// equivalent Media1 options request: ConfigurationToken followed by
// ProfileToken, each optional and at most once.
func validateOptionalTokens(operation operationWire, namespace string) (mediaRequest, error) {
	if strings.TrimSpace(operation.Value) != "" || len(operation.Elements) > 2 {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	var result mediaRequest
	for index, child := range operation.Elements {
		if child.XMLName.Space != namespace || !simpleElementContent(child) {
			return mediaRequest{}, errInvalidRequestWrapper
		}
		value := child.Value
		if !validReferenceTokenText(value) {
			return mediaRequest{}, errInvalidRequestWrapper
		}
		switch child.XMLName.Local {
		case "ConfigurationToken":
			if index != 0 || result.configurationToken != "" {
				return mediaRequest{}, errInvalidRequestWrapper
			}
			result.configurationToken = value
		case "ProfileToken":
			if result.profileToken != "" {
				return mediaRequest{}, errInvalidRequestWrapper
			}
			result.profileToken = value
		default:
			return mediaRequest{}, errInvalidRequestWrapper
		}
	}
	return result, nil
}

func validateProfileTokenOnly(operation operationWire, namespace string) (mediaRequest, error) {
	if len(operation.Elements) != 1 || strings.TrimSpace(operation.Value) != "" {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	child := operation.Elements[0]
	value := child.Value
	if child.XMLName != xmlName(namespace, "ProfileToken") || !simpleElementContent(child) || !validReferenceTokenText(value) {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	return mediaRequest{profileToken: value}, nil
}

func validateMedia1StreamURI(operation operationWire) (mediaRequest, error) {
	if len(operation.Elements) != 2 || strings.TrimSpace(operation.Value) != "" {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	setup := operation.Elements[0]
	profile := operation.Elements[1]
	if setup.XMLName != xmlName(media1Namespace, "StreamSetup") || strings.TrimSpace(setup.Value) != "" || len(setup.Elements) < 2 {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	stream := setup.Elements[0]
	transport := setup.Elements[1]
	streamValue := stream.Value
	if stream.XMLName != xmlName(typeNamespace, "Stream") || !simpleElementContent(stream) || !validMedia1StreamType(streamValue) {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	if transport.XMLName != xmlName(typeNamespace, "Transport") || !namespaceAttributesOnly(transport) || strings.TrimSpace(transport.Value) != "" || len(transport.Elements) < 1 || len(transport.Elements) > 2 {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	protocol := transport.Elements[0]
	protocolValue := protocol.Value
	if protocol.XMLName != xmlName(typeNamespace, "Protocol") || !simpleElementContent(protocol) || !validMedia1TransportProtocol(protocolValue) {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	if len(transport.Elements) == 2 {
		tunnel := transport.Elements[1]
		if tunnel.XMLName != xmlName(typeNamespace, "Tunnel") || !namespaceAttributesOnly(tunnel) || strings.TrimSpace(tunnel.Value) != "" {
			return mediaRequest{}, errInvalidRequestWrapper
		}
	}
	profileValue := profile.Value
	if profile.XMLName != xmlName(media1Namespace, "ProfileToken") || !simpleElementContent(profile) || !validReferenceTokenText(profileValue) {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	return mediaRequest{profileToken: profileValue, protocol: protocolValue, stream: streamValue}, nil
}

func validateMedia2StreamURI(operation operationWire) (mediaRequest, error) {
	if len(operation.Elements) != 2 || strings.TrimSpace(operation.Value) != "" {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	protocol := operation.Elements[0]
	profile := operation.Elements[1]
	protocolValue := protocol.Value
	profileValue := profile.Value
	if protocol.XMLName != xmlName(media2Namespace, "Protocol") || !simpleElementContent(protocol) || !validMedia2TransportProtocol(protocolValue) ||
		profile.XMLName != xmlName(media2Namespace, "ProfileToken") || !simpleElementContent(profile) || !validReferenceTokenText(profileValue) {
		return mediaRequest{}, errInvalidRequestWrapper
	}
	return mediaRequest{profileToken: profileValue, protocol: protocolValue}, nil
}

func namespaceAttributesOnly(element textElement) bool {
	for _, attribute := range element.Attrs {
		if attribute.Name.Space == "xmlns" || (attribute.Name.Space == "" && attribute.Name.Local == "xmlns") {
			continue
		}
		return false
	}
	return true
}

func validReferenceTokenText(value string) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxMediaTokenLen {
		return false
	}
	return safeMediaText(value)
}

func validMedia1StreamType(value string) bool {
	return value == "RTP-Unicast" || value == "RTP-Multicast"
}

func validMedia1TransportProtocol(value string) bool {
	return value == "UDP" || value == "TCP" || value == "RTSP" || value == "HTTP"
}

func validMedia2TransportProtocol(value string) bool {
	switch value {
	case "RtspUnicast", "RtspMulticast", "RtspsUnicast", "RtspsMulticast", "RTSP", "RtspOverHttp":
		return true
	default:
		return false
	}
}

func validMedia2ConfigurationType(value string) bool {
	switch value {
	case "All", "VideoSource", "VideoEncoder", "AudioSource", "AudioEncoder", "AudioOutput", "AudioDecoder", "Metadata", "Analytics", "PTZ", "Receiver":
		return true
	default:
		return false
	}
}

func operationNamespaceAttributesOnly(operation operationWire) bool {
	for _, attribute := range operation.Attrs {
		if attribute.Name.Space == "xmlns" || (attribute.Name.Space == "" && attribute.Name.Local == "xmlns") {
			continue
		}
		return false
	}
	return true
}
